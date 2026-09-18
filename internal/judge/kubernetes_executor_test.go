package judge

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"testing"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
	ktesting "k8s.io/client-go/testing"

	"github.com/nglong14/CodeDuel/internal/config"
)

func testKubernetesConfig() config.JudgeConfig {
	return config.JudgeConfig{
		Concurrency:        1,
		MaxCodeBytes:       64 << 10,
		MaxOutputBytes:     1 << 20,
		CompileTimeout:     10 * time.Second,
		TestTimeout:        5 * time.Second,
		TotalTimeout:       30 * time.Second,
		CleanupTimeout:     10 * time.Second,
		AttemptLease:       2 * time.Minute,
		NanoCPUs:           1_000_000_000,
		MemoryBytes:        256 << 20,
		MemorySwapBytes:    256 << 20,
		PIDLimit:           64,
		WorkspaceBytes:     64 << 20,
		TmpfsBytes:         16 << 20,
		PythonImage:        "registry/sandbox-python:3.13",
		CPPImage:           "registry/sandbox-cpp:gcc14",
		JavaImage:          "registry/sandbox-java:temurin21",
		Executor:           config.JudgeExecutorKubernetes,
		K8sNamespace:       "codeduel-dev",
		K8sRuntimeClass:    "gvisor",
		K8sNodeSelector:    map[string]string{"sandbox": "true"},
		K8sTolerationKey:   "sandbox",
		K8sTolerationValue: "true",
	}
}

func newTestExecutor(t *testing.T, objects ...runtime.Object) (*KubernetesJobExecutor, *fake.Clientset) {
	t.Helper()
	client := fake.NewSimpleClientset(objects...)
	cfg := testKubernetesConfig()
	executor, err := newKubernetesJobExecutor(context.Background(), client, cfg, nil)
	if err != nil {
		t.Fatalf("newKubernetesJobExecutor: %v", err)
	}
	executor.pollInterval = time.Millisecond
	return executor, client
}

func sampleRequest() ExecutionRequest {
	return ExecutionRequest{
		Language: LanguagePython,
		Source:   []byte("print(input())"),
		Tests:    []TestCase{{Input: []byte("hi"), Expected: []byte("hi")}},
		Limits:   limitsFromConfig(testKubernetesConfig()),
	}
}

// reactToJobWithPod installs a reactor so that every created Job spawns a pod
// with the given container state, imitating the Job controller + kubelet that
// the fake clientset does not run. It writes to the object tracker directly:
// calling a typed client method here would re-enter the fake's reaction lock
// and deadlock.
func reactToJobWithPod(client *fake.Clientset, namespace string, state corev1.ContainerState, podReason string) {
	client.PrependReactor("create", "jobs", func(action ktesting.Action) (bool, runtime.Object, error) {
		job := action.(ktesting.CreateAction).GetObject().(*batchv1.Job)
		pod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      job.Name + "-pod",
				Namespace: namespace,
				Labels:    job.Spec.Template.Labels,
			},
			Status: corev1.PodStatus{
				Reason: podReason,
				ContainerStatuses: []corev1.ContainerStatus{{
					Name:  sandboxContainerName,
					State: state,
				}},
			},
		}
		if err := client.Tracker().Add(pod); err != nil {
			return true, nil, err
		}
		return false, nil, nil
	})
}

func terminatedState(message string, exitCode int32, reason string) corev1.ContainerState {
	return corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{
		Message:  message,
		ExitCode: exitCode,
		Reason:   reason,
	}}
}

func harnessMessage(t *testing.T, kind OutcomeKind, testsPassed int) string {
	t.Helper()
	raw, err := json.Marshal(harnessResult{Kind: string(kind), TestsPassed: testsPassed})
	if err != nil {
		t.Fatalf("marshal harness result: %v", err)
	}
	return string(raw)
}

func TestKubernetesExecuteOutcomes(t *testing.T) {
	tests := []struct {
		name       string
		state      corev1.ContainerState
		podReason  string
		wantKind   OutcomeKind
		wantPassed int
	}{
		{
			name:       "pass",
			state:      terminatedState(harnessMessage(t, OutcomePass, 3), 0, "Completed"),
			wantKind:   OutcomePass,
			wantPassed: 3,
		},
		{
			name:       "wrong answer",
			state:      terminatedState(harnessMessage(t, OutcomeWrongAnswer, 1), 10, "Error"),
			wantKind:   OutcomeWrongAnswer,
			wantPassed: 1,
		},
		{
			name:     "compile error",
			state:    terminatedState(harnessMessage(t, OutcomeCompileError, 0), 11, "Error"),
			wantKind: OutcomeCompileError,
		},
		{
			name:       "runtime error",
			state:      terminatedState(harnessMessage(t, OutcomeRuntimeError, 2), 12, "Error"),
			wantKind:   OutcomeRuntimeError,
			wantPassed: 2,
		},
		{
			name:     "output limit",
			state:    terminatedState(harnessMessage(t, OutcomeOutputLimit, 0), 13, "Error"),
			wantKind: OutcomeOutputLimit,
		},
		{
			name:     "harness timeout",
			state:    terminatedState(harnessMessage(t, OutcomeTimeout, 0), 124, "Error"),
			wantKind: OutcomeTimeout,
		},
		{
			name:     "oom killed maps to runtime error",
			state:    terminatedState("", 137, reasonOOMKilled),
			wantKind: OutcomeRuntimeError,
		},
		{
			name:     "deadline exceeded maps to timeout",
			state:    terminatedState("", 137, reasonDeadlineExceeded),
			wantKind: OutcomeTimeout,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			executor, client := newTestExecutor(t)
			reactToJobWithPod(client, executor.namespace, tt.state, "")
			outcome, err := executor.Execute(context.Background(), sampleRequest())
			if err != nil {
				t.Fatalf("Execute: %v", err)
			}
			if outcome.Kind != tt.wantKind {
				t.Fatalf("outcome.Kind = %q, want %q", outcome.Kind, tt.wantKind)
			}
			if outcome.TestsPassed != tt.wantPassed {
				t.Fatalf("outcome.TestsPassed = %d, want %d", outcome.TestsPassed, tt.wantPassed)
			}
		})
	}
}

func TestKubernetesExecuteImagePullFailureIsInfraError(t *testing.T) {
	executor, client := newTestExecutor(t)
	reactToJobWithPod(client, executor.namespace, corev1.ContainerState{
		Waiting: &corev1.ContainerStateWaiting{Reason: "ImagePullBackOff"},
	}, "")
	_, err := executor.Execute(context.Background(), sampleRequest())
	if !errors.Is(err, errJobNeverRunning) {
		t.Fatalf("Execute error = %v, want errJobNeverRunning", err)
	}
}

func TestKubernetesExecutePodDeadlineExceeded(t *testing.T) {
	executor, client := newTestExecutor(t)
	reactToJobWithPod(client, executor.namespace, corev1.ContainerState{
		Running: &corev1.ContainerStateRunning{},
	}, reasonDeadlineExceeded)
	outcome, err := executor.Execute(context.Background(), sampleRequest())
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if outcome.Kind != OutcomeTimeout {
		t.Fatalf("outcome.Kind = %q, want timeout", outcome.Kind)
	}
}

func TestKubernetesExecuteContextTimeoutYieldsTimeout(t *testing.T) {
	executor, client := newTestExecutor(t)
	// A pod that stays Running forever forces the wait loop to hit the deadline.
	reactToJobWithPod(client, executor.namespace, corev1.ContainerState{
		Running: &corev1.ContainerStateRunning{},
	}, "")
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	outcome, err := executor.Execute(ctx, sampleRequest())
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if outcome.Kind != OutcomeTimeout {
		t.Fatalf("outcome.Kind = %q, want timeout", outcome.Kind)
	}
}

func TestKubernetesExecuteContextCancelPropagates(t *testing.T) {
	executor, client := newTestExecutor(t)
	reactToJobWithPod(client, executor.namespace, corev1.ContainerState{
		Running: &corev1.ContainerStateRunning{},
	}, "")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := executor.Execute(ctx, sampleRequest())
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Execute error = %v, want context canceled", err)
	}
}

func TestKubernetesExecuteJobDeletedAfterGrading(t *testing.T) {
	executor, client := newTestExecutor(t)
	reactToJobWithPod(client, executor.namespace, terminatedState(harnessMessage(t, OutcomePass, 1), 0, "Completed"), "")
	if _, err := executor.Execute(context.Background(), sampleRequest()); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	jobs, err := client.BatchV1().Jobs(executor.namespace).List(context.Background(), metav1.ListOptions{})
	if err != nil {
		t.Fatalf("list jobs: %v", err)
	}
	if len(jobs.Items) != 0 {
		t.Fatalf("job count = %d, want 0 (cleaned up)", len(jobs.Items))
	}
}

func TestCleanupStaleJobsRemovesOnlyExpired(t *testing.T) {
	namespace := "codeduel-dev"
	fresh := &batchv1.Job{ObjectMeta: metav1.ObjectMeta{
		Name:              "codeduel-sandbox-fresh",
		Namespace:         namespace,
		Labels:            map[string]string{sandboxLabel: "true"},
		CreationTimestamp: metav1.NewTime(time.Now()),
	}}
	stale := &batchv1.Job{ObjectMeta: metav1.ObjectMeta{
		Name:              "codeduel-sandbox-stale",
		Namespace:         namespace,
		Labels:            map[string]string{sandboxLabel: "true"},
		CreationTimestamp: metav1.NewTime(time.Now().Add(-10 * time.Minute)),
	}}
	// newTestExecutor runs cleanupStaleJobs during construction, which is exactly
	// the orphan-reclaim path we want to assert.
	_, client := newTestExecutor(t, fresh, stale)

	jobs, err := client.BatchV1().Jobs(namespace).List(context.Background(), metav1.ListOptions{})
	if err != nil {
		t.Fatalf("list jobs: %v", err)
	}
	if len(jobs.Items) != 1 || jobs.Items[0].Name != "codeduel-sandbox-fresh" {
		t.Fatalf("remaining jobs = %+v, want only the fresh job", jobs.Items)
	}
}

func TestBuildJobEnforcesIsolationAndPlacement(t *testing.T) {
	executor, _ := newTestExecutor(t)
	request := sampleRequest()
	job, err := executor.buildJob(request, "attempt-123")
	if err != nil {
		t.Fatalf("buildJob: %v", err)
	}
	spec := job.Spec.Template.Spec

	if spec.RuntimeClassName == nil || *spec.RuntimeClassName != "gvisor" {
		t.Fatalf("RuntimeClassName = %v, want gvisor", spec.RuntimeClassName)
	}
	if spec.RestartPolicy != corev1.RestartPolicyNever {
		t.Fatalf("RestartPolicy = %q, want Never", spec.RestartPolicy)
	}
	if spec.AutomountServiceAccountToken == nil || *spec.AutomountServiceAccountToken {
		t.Fatal("AutomountServiceAccountToken must be false")
	}
	if spec.SecurityContext == nil || spec.SecurityContext.RunAsNonRoot == nil || !*spec.SecurityContext.RunAsNonRoot {
		t.Fatal("pod must run as non-root")
	}
	if got := spec.NodeSelector["sandbox"]; got != "true" {
		t.Fatalf("NodeSelector[sandbox] = %q, want true", got)
	}
	if len(spec.Tolerations) != 1 || spec.Tolerations[0].Key != "sandbox" ||
		spec.Tolerations[0].Effect != corev1.TaintEffectNoSchedule {
		t.Fatalf("Tolerations = %+v, want sandbox NoSchedule", spec.Tolerations)
	}
	if job.Spec.BackoffLimit == nil || *job.Spec.BackoffLimit != 0 {
		t.Fatalf("BackoffLimit = %v, want 0", job.Spec.BackoffLimit)
	}
	if job.Spec.ActiveDeadlineSeconds == nil || *job.Spec.ActiveDeadlineSeconds <= 0 {
		t.Fatalf("ActiveDeadlineSeconds = %v, want positive", job.Spec.ActiveDeadlineSeconds)
	}

	if len(spec.Containers) != 1 {
		t.Fatalf("container count = %d, want 1", len(spec.Containers))
	}
	container := spec.Containers[0]
	if container.Image != "registry/sandbox-python:3.13" {
		t.Fatalf("image = %q, want the python sandbox image", container.Image)
	}
	sc := container.SecurityContext
	if sc == nil || sc.ReadOnlyRootFilesystem == nil || !*sc.ReadOnlyRootFilesystem {
		t.Fatal("container rootfs must be read-only")
	}
	if sc.AllowPrivilegeEscalation == nil || *sc.AllowPrivilegeEscalation {
		t.Fatal("container must not allow privilege escalation")
	}
	if sc.Capabilities == nil || len(sc.Capabilities.Drop) != 1 || sc.Capabilities.Drop[0] != "ALL" {
		t.Fatalf("capabilities drop = %+v, want ALL", sc.Capabilities)
	}

	cpu := container.Resources.Limits[corev1.ResourceCPU]
	if cpu.MilliValue() != 1000 {
		t.Fatalf("cpu limit = %dm, want 1000m", cpu.MilliValue())
	}
	mem := container.Resources.Limits[corev1.ResourceMemory]
	if mem.Value() != request.Limits.MemoryBytes {
		t.Fatalf("memory limit = %d, want %d", mem.Value(), request.Limits.MemoryBytes)
	}

	// The submission payload must round-trip through the container env.
	var payloadValue string
	for _, env := range container.Env {
		if env.Name == sandboxPayloadEnv {
			payloadValue = env.Value
		}
	}
	if payloadValue == "" {
		t.Fatal("missing submission payload env")
	}
	raw, err := base64.StdEncoding.DecodeString(payloadValue)
	if err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	var payload sandboxPayload
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if payload.Language != string(LanguagePython) || len(payload.Tests) != 1 {
		t.Fatalf("payload = %+v", payload)
	}
}

func TestClassifyPodPending(t *testing.T) {
	pod := &corev1.Pod{Status: corev1.PodStatus{
		ContainerStatuses: []corev1.ContainerStatus{{
			Name:  sandboxContainerName,
			State: corev1.ContainerState{Waiting: &corev1.ContainerStateWaiting{Reason: "ContainerCreating"}},
		}},
	}}
	class := classifyPod(pod)
	if class.terminal || class.pullErr != "" {
		t.Fatalf("classification = %+v, want non-terminal with no pull error", class)
	}
}

func TestParseHarnessResultRejectsUnknownKind(t *testing.T) {
	if _, ok := parseHarnessResult(`{"kind":"exploded","tests_passed":0}`); ok {
		t.Fatal("parseHarnessResult accepted an unknown kind")
	}
	if _, ok := parseHarnessResult("not json"); ok {
		t.Fatal("parseHarnessResult accepted non-JSON")
	}
	result, ok := parseHarnessResult(`{"kind":"pass","tests_passed":4}`)
	if !ok || result.TestsPassed != 4 {
		t.Fatalf("parseHarnessResult = (%+v, %v)", result, ok)
	}
}

func TestNewKubernetesExecutorRejectsDockerConfig(t *testing.T) {
	cfg := testKubernetesConfig()
	cfg.Executor = config.JudgeExecutorDocker
	if _, err := newKubernetesJobExecutor(context.Background(), fake.NewSimpleClientset(), cfg, nil); err == nil {
		t.Fatal("newKubernetesJobExecutor accepted a docker executor config")
	}
}
