package judge

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"

	"github.com/nglong14/CodeDuel/internal/config"
)

const (
	sandboxContainerName = "sandbox"
	sandboxHarnessPath = "/opt/codeduel/harness"
	sandboxPayloadEnv = "CODEDUEL_PAYLOAD"
	jobNameLabel = "batch.kubernetes.io/job-name"

	reasonDeadlineExceeded = "DeadlineExceeded"
	reasonOOMKilled        = "OOMKilled"

	defaultPollInterval = 250 * time.Millisecond
	uidNonRoot          = int64(10001)
	gidNonRoot          = int64(10001)
)

var errJobNeverRunning = errors.New("sandbox Job never reached a graded state")

type harnessResult struct {
	Kind        string `json:"kind"`
	TestsPassed int    `json:"tests_passed"`
}

// sandboxPayload is the base64-encoded JSON handed to the harness via env.
type sandboxPayload struct {
	Language       string            `json:"language"`
	Source         []byte            `json:"source"`
	Tests          []sandboxTestCase `json:"tests"`
	MaxOutputBytes int64             `json:"max_output_bytes"`
	CompileTimeout time.Duration     `json:"compile_timeout"`
	TestTimeout    time.Duration     `json:"test_timeout"`
}

type sandboxTestCase struct {
	Input    []byte `json:"input"`
	Expected []byte `json:"expected"`
}

type KubernetesJobExecutor struct {
	client        kubernetes.Interface
	namespace     string
	runtimeClass  string
	images        map[Language]string
	nodeSelector  map[string]string
	tolerationKey string
	tolerationVal string
	instanceID    string
	staleAge      time.Duration
	startupGrace  time.Duration
	pollInterval  time.Duration
	logger        *slog.Logger
	now           func() time.Time
}

// NewKubernetesJobExecutor builds an executor from in-cluster credentials.
func NewKubernetesJobExecutor(
	ctx context.Context,
	cfg config.JudgeConfig,
	logger *slog.Logger,
) (*KubernetesJobExecutor, error) {
	restCfg, err := rest.InClusterConfig()
	if err != nil {
		return nil, fmt.Errorf("load in-cluster Kubernetes config: %w", err)
	}
	client, err := kubernetes.NewForConfig(restCfg)
	if err != nil {
		return nil, fmt.Errorf("create Kubernetes client: %w", err)
	}
	return newKubernetesJobExecutor(ctx, client, cfg, logger)
}

func newKubernetesJobExecutor(
	ctx context.Context,
	client kubernetes.Interface,
	cfg config.JudgeConfig,
	logger *slog.Logger,
) (*KubernetesJobExecutor, error) {
	if client == nil {
		return nil, errors.New("kubernetes client is required")
	}
	if logger == nil {
		logger = slog.New(slog.DiscardHandler)
	}
	if cfg.Executor != config.JudgeExecutorKubernetes {
		return nil, fmt.Errorf("kubernetes executor requires JUDGE_EXECUTOR=%q", config.JudgeExecutorKubernetes)
	}
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("validate Judge config: %w", err)
	}
	// Fail fast if the API server is unreachable, mirroring the Docker Ping check.
	if _, err := client.Discovery().ServerVersion(); err != nil {
		return nil, fmt.Errorf("reach Kubernetes API server: %w", err)
	}

	executor := &KubernetesJobExecutor{
		client:        client,
		namespace:     cfg.K8sNamespace,
		runtimeClass:  cfg.K8sRuntimeClass,
		nodeSelector:  cfg.K8sNodeSelector,
		tolerationKey: cfg.K8sTolerationKey,
		tolerationVal: cfg.K8sTolerationValue,
		images: map[Language]string{
			LanguagePython: cfg.PythonImage,
			LanguageCPP:    cfg.CPPImage,
			LanguageJava:   cfg.JavaImage,
		},
		instanceID:   uuid.NewString(),
		staleAge:     cfg.AttemptLease,
		startupGrace: cfg.TotalTimeout,
		pollInterval: defaultPollInterval,
		logger:       logger,
		now:          time.Now,
	}
	if err := executor.cleanupStaleJobs(ctx); err != nil {
		return nil, fmt.Errorf("clean stale sandbox Jobs: %w", err)
	}
	logger.Info("kubernetes sandbox executor initialized",
		"instance_id", executor.instanceID,
		"namespace", executor.namespace,
		"runtime_class", executor.runtimeClass,
	)
	return executor, nil
}

func (e *KubernetesJobExecutor) Close() error { return nil }

func (e *KubernetesJobExecutor) Execute(ctx context.Context, request ExecutionRequest) (ExecutionOutcome, error) {
	if e == nil || e.client == nil {
		return ExecutionOutcome{}, errors.New("kubernetes executor is not initialized")
	}
	if err := request.Validate(); err != nil {
		return ExecutionOutcome{}, fmt.Errorf("validate execution request: %w", err)
	}
	if _, ok := e.images[request.Language]; !ok {
		return ExecutionOutcome{}, fmt.Errorf("no image configured for language %q", request.Language)
	}

	attemptID := uuid.NewString()
	job, err := e.buildJob(request, attemptID)
	if err != nil {
		return ExecutionOutcome{}, err
	}
	created, err := e.client.BatchV1().Jobs(e.namespace).Create(ctx, job, metav1.CreateOptions{})
	if err != nil {
		return ExecutionOutcome{}, fmt.Errorf("create sandbox Job: %w", err)
	}
	defer e.deleteJob(created.Name)

	return e.awaitOutcome(ctx, request, attemptID)
}

// awaitOutcome polls the submission's pod until it reaches a graded terminal
// state, an infrastructure fault, or the total-timeout window elapses.
func (e *KubernetesJobExecutor) awaitOutcome(
	ctx context.Context,
	request ExecutionRequest,
	attemptID string,
) (ExecutionOutcome, error) {
	start := e.now()
	deadline := start.Add(request.Limits.TotalTimeout + request.Limits.CleanupTimeout + e.startupGrace)
	selector := metav1.ListOptions{LabelSelector: attemptLabel + "=" + attemptID}

	for {
		pods, err := e.client.CoreV1().Pods(e.namespace).List(ctx, selector)
		if err != nil {
			return ExecutionOutcome{}, fmt.Errorf("list sandbox pods: %w", err)
		}
		for index := range pods.Items {
			class := classifyPod(&pods.Items[index])
			if class.terminal {
				return class.outcome, nil
			}
			if class.pullErr != "" {
				return ExecutionOutcome{}, fmt.Errorf("%w: %s", errJobNeverRunning, class.pullErr)
			}
		}
		if e.jobDeadlineExceeded(ctx, jobNameForAttempt(attemptID)) {
			return ExecutionOutcome{Kind: OutcomeTimeout}, nil
		}

		if ctx.Err() != nil {
			if errors.Is(ctx.Err(), context.DeadlineExceeded) {
				return ExecutionOutcome{Kind: OutcomeTimeout}, nil
			}
			return ExecutionOutcome{}, ctx.Err()
		}
		if !e.now().Before(deadline) {
			return ExecutionOutcome{Kind: OutcomeTimeout}, nil
		}

		timer := time.NewTimer(e.pollInterval)
		select {
		case <-ctx.Done():
			timer.Stop()
			if errors.Is(ctx.Err(), context.DeadlineExceeded) {
				return ExecutionOutcome{Kind: OutcomeTimeout}, nil
			}
			return ExecutionOutcome{}, ctx.Err()
		case <-timer.C:
		}
	}
}

func (e *KubernetesJobExecutor) jobDeadlineExceeded(ctx context.Context, name string) bool {
	job, err := e.client.BatchV1().Jobs(e.namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return false
	}
	for _, condition := range job.Status.Conditions {
		if condition.Type == batchv1.JobFailed &&
			condition.Status == corev1.ConditionTrue &&
			condition.Reason == reasonDeadlineExceeded {
			return true
		}
	}
	return false
}

type podClassification struct {
	outcome  ExecutionOutcome
	terminal bool
	pullErr string
}

func classifyPod(pod *corev1.Pod) podClassification {
	if pod == nil {
		return podClassification{}
	}
	// A pod deleted for exceeding the Job's active deadline surfaces here.
	if pod.Status.Reason == reasonDeadlineExceeded {
		return podClassification{outcome: ExecutionOutcome{Kind: OutcomeTimeout}, terminal: true}
	}
	status, ok := containerStatus(pod, sandboxContainerName)
	if !ok {
		return podClassification{}
	}
	if term := status.State.Terminated; term != nil {
		return podClassification{outcome: classifyTerminated(term), terminal: true}
	}
	if wait := status.State.Waiting; wait != nil {
		switch wait.Reason {
		case "ImagePullBackOff", "ErrImagePull", "InvalidImageName", "ImageInspectError", "RegistryUnavailable":
			return podClassification{pullErr: wait.Reason}
		}
	}
	return podClassification{}
}

func classifyTerminated(term *corev1.ContainerStateTerminated) ExecutionOutcome {
	if term.Reason == reasonDeadlineExceeded {
		return ExecutionOutcome{Kind: OutcomeTimeout}
	}
	if term.Reason == reasonOOMKilled {
		return ExecutionOutcome{Kind: OutcomeRuntimeError}
	}
	if result, ok := parseHarnessResult(term.Message); ok {
		return ExecutionOutcome{Kind: OutcomeKind(result.Kind), TestsPassed: result.TestsPassed}
	}
	if term.ExitCode == 0 {
		return ExecutionOutcome{Kind: OutcomePass}
	}
	return ExecutionOutcome{Kind: OutcomeRuntimeError}
}

func parseHarnessResult(message string) (harnessResult, bool) {
	if message == "" {
		return harnessResult{}, false
	}
	var result harnessResult
	if err := json.Unmarshal([]byte(message), &result); err != nil {
		return harnessResult{}, false
	}
	if !validOutcomeKind(OutcomeKind(result.Kind)) {
		return harnessResult{}, false
	}
	return result, true
}

func validOutcomeKind(kind OutcomeKind) bool {
	switch kind {
	case OutcomePass, OutcomeWrongAnswer, OutcomeCompileError,
		OutcomeRuntimeError, OutcomeOutputLimit, OutcomeTimeout:
		return true
	default:
		return false
	}
}

func containerStatus(pod *corev1.Pod, name string) (corev1.ContainerStatus, bool) {
	for _, status := range pod.Status.ContainerStatuses {
		if status.Name == name {
			return status, true
		}
	}
	return corev1.ContainerStatus{}, false
}

// buildJob constructs the batch/v1 Job for one submission. It is pure so the
// pod's isolation, placement, and resource contract can be unit tested.
func (e *KubernetesJobExecutor) buildJob(request ExecutionRequest, attemptID string) (*batchv1.Job, error) {
	payload, err := encodePayload(request)
	if err != nil {
		return nil, err
	}
	labels := map[string]string{
		sandboxLabel:  "true",
		attemptLabel:  attemptID,
		instanceLabel: e.instanceID,
	}
	runtimeClass := e.runtimeClass
	backoffLimit := int32(0)
	activeDeadline := int64(request.Limits.TotalTimeout/time.Second) + 1
	ttl := int32(request.Limits.CleanupTimeout / time.Second)
	automount := false
	nonRoot := true
	noEscalation := false
	readOnlyRoot := true

	var tolerations []corev1.Toleration
	if e.tolerationKey != "" {
		tolerations = []corev1.Toleration{{
			Key:      e.tolerationKey,
			Operator: corev1.TolerationOpEqual,
			Value:    e.tolerationVal,
			Effect:   corev1.TaintEffectNoSchedule,
		}}
	}

	resourceList := corev1.ResourceList{
		corev1.ResourceCPU:              *resource.NewMilliQuantity(request.Limits.NanoCPUs/1_000_000, resource.DecimalSI),
		corev1.ResourceMemory:           *resource.NewQuantity(request.Limits.MemoryBytes, resource.BinarySI),
		corev1.ResourceEphemeralStorage: *resource.NewQuantity(request.Limits.WorkspaceBytes, resource.BinarySI),
	}

	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      jobNameForAttempt(attemptID),
			Namespace: e.namespace,
			Labels:    labels,
		},
		Spec: batchv1.JobSpec{
			BackoffLimit:            &backoffLimit,
			ActiveDeadlineSeconds:   &activeDeadline,
			TTLSecondsAfterFinished: &ttl,
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: labels},
				Spec: corev1.PodSpec{
					RestartPolicy:                corev1.RestartPolicyNever,
					RuntimeClassName:             &runtimeClass,
					NodeSelector:                 e.nodeSelector,
					Tolerations:                  tolerations,
					AutomountServiceAccountToken: &automount,
					SecurityContext: &corev1.PodSecurityContext{
						RunAsNonRoot:   &nonRoot,
						RunAsUser:      ptr(uidNonRoot),
						RunAsGroup:     ptr(gidNonRoot),
						SeccompProfile: &corev1.SeccompProfile{Type: corev1.SeccompProfileTypeRuntimeDefault},
					},
					Containers: []corev1.Container{{
						Name:    sandboxContainerName,
						Image:   e.images[request.Language],
						Command: []string{sandboxHarnessPath},
						Env: []corev1.EnvVar{
							{Name: sandboxPayloadEnv, Value: payload},
							{Name: "HOME", Value: "/tmp"},
							{Name: "LANG", Value: "C.UTF-8"},
							{Name: "LC_ALL", Value: "C.UTF-8"},
						},
						Resources: corev1.ResourceRequirements{
							Requests: resourceList,
							Limits:   resourceList,
						},
						SecurityContext: &corev1.SecurityContext{
							RunAsNonRoot:             &nonRoot,
							AllowPrivilegeEscalation: &noEscalation,
							ReadOnlyRootFilesystem:   &readOnlyRoot,
							Capabilities:             &corev1.Capabilities{Drop: []corev1.Capability{"ALL"}},
						},
						VolumeMounts: []corev1.VolumeMount{
							{Name: "workspace", MountPath: sandboxWorkspace},
							{Name: "tmp", MountPath: "/tmp"},
						},
					}},
					Volumes: []corev1.Volume{
						{Name: "workspace", VolumeSource: corev1.VolumeSource{
							EmptyDir: &corev1.EmptyDirVolumeSource{SizeLimit: quantityPtr(request.Limits.WorkspaceBytes)},
						}},
						{Name: "tmp", VolumeSource: corev1.VolumeSource{
							EmptyDir: &corev1.EmptyDirVolumeSource{SizeLimit: quantityPtr(request.Limits.TmpfsBytes)},
						}},
					},
				},
			},
		},
	}
	return job, nil
}

func (e *KubernetesJobExecutor) deleteJob(name string) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	propagation := metav1.DeletePropagationBackground
	err := e.client.BatchV1().Jobs(e.namespace).Delete(ctx, name, metav1.DeleteOptions{
		PropagationPolicy: &propagation,
	})
	if err != nil {
		e.logger.Warn("delete sandbox Job", "job", name, "err", err)
	}
}

// cleanupStaleJobs deletes submission Jobs older than the attempt lease, mirroring
// DockerExecutor.cleanupStaleResources. It only touches Jobs this Judge fleet
// created (identified by the sandbox label).
func (e *KubernetesJobExecutor) cleanupStaleJobs(ctx context.Context) error {
	jobs, err := e.client.BatchV1().Jobs(e.namespace).List(ctx, metav1.ListOptions{
		LabelSelector: sandboxLabel + "=true",
	})
	if err != nil {
		return fmt.Errorf("list sandbox Jobs: %w", err)
	}
	cutoff := e.now().Add(-e.staleAge)
	propagation := metav1.DeletePropagationBackground
	for index := range jobs.Items {
		job := &jobs.Items[index]
		if job.CreationTimestamp.Time.After(cutoff) {
			continue
		}
		if err := e.client.BatchV1().Jobs(e.namespace).Delete(ctx, job.Name, metav1.DeleteOptions{
			PropagationPolicy: &propagation,
		}); err != nil {
			return fmt.Errorf("remove stale sandbox Job %s: %w", job.Name, err)
		}
	}
	return nil
}

func encodePayload(request ExecutionRequest) (string, error) {
	tests := make([]sandboxTestCase, len(request.Tests))
	for index, test := range request.Tests {
		tests[index] = sandboxTestCase{Input: test.Input, Expected: test.Expected}
	}
	payload := sandboxPayload{
		Language:       string(request.Language),
		Source:         request.Source,
		Tests:          tests,
		MaxOutputBytes: request.Limits.MaxOutputBytes,
		CompileTimeout: request.Limits.CompileTimeout,
		TestTimeout:    request.Limits.TestTimeout,
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("encode sandbox payload: %w", err)
	}
	return base64.StdEncoding.EncodeToString(encoded), nil
}

func jobNameForAttempt(attemptID string) string {
	return "codeduel-sandbox-" + attemptID
}

func ptr[T any](value T) *T { return &value }

func quantityPtr(bytes int64) *resource.Quantity {
	return resource.NewQuantity(bytes, resource.BinarySI)
}

var _ Executor = (*KubernetesJobExecutor)(nil)
