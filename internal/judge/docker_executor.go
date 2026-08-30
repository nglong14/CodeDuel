package judge

import (
	"archive/tar"
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/moby/moby/api/pkg/stdcopy"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/mount"
	"github.com/moby/moby/client"
	"github.com/nglong14/CodeDuel/internal/config"
)

const (
	sandboxUser       = "10001:10001"
	sandboxWorkspace  = "/workspace"
	sandboxLabel      = "com.codeduel.sandbox"
	attemptLabel      = "com.codeduel.attempt"
	instanceLabel     = "com.codeduel.instance"
	resourceTypeLabel = "com.codeduel.resource"
)

var errProcessTimeout = errors.New("sandbox process timed out")

type dockerEngine interface {
	Ping(context.Context, client.PingOptions) (client.PingResult, error)
	Info(context.Context, client.InfoOptions) (client.SystemInfoResult, error)
	ImageInspect(context.Context, string, ...client.ImageInspectOption) (client.ImageInspectResult, error)
	VolumeCreate(context.Context, client.VolumeCreateOptions) (client.VolumeCreateResult, error)
	VolumeList(context.Context, client.VolumeListOptions) (client.VolumeListResult, error)
	VolumeRemove(context.Context, string, client.VolumeRemoveOptions) (client.VolumeRemoveResult, error)
	ContainerCreate(context.Context, client.ContainerCreateOptions) (client.ContainerCreateResult, error)
	CopyToContainer(context.Context, string, client.CopyToContainerOptions) (client.CopyToContainerResult, error)
	ContainerAttach(context.Context, string, client.ContainerAttachOptions) (client.ContainerAttachResult, error)
	ContainerStart(context.Context, string, client.ContainerStartOptions) (client.ContainerStartResult, error)
	ContainerWait(context.Context, string, client.ContainerWaitOptions) client.ContainerWaitResult
	ContainerInspect(context.Context, string, client.ContainerInspectOptions) (client.ContainerInspectResult, error)
	ContainerKill(context.Context, string, client.ContainerKillOptions) (client.ContainerKillResult, error)
	ContainerList(context.Context, client.ContainerListOptions) (client.ContainerListResult, error)
	ContainerRemove(context.Context, string, client.ContainerRemoveOptions) (client.ContainerRemoveResult, error)
	Close() error
}

type languageSpec struct {
	sourceName string
	compile    []string
	run        []string
}

var languageSpecs = map[Language]languageSpec{
	LanguagePython: {
		sourceName: "main.py",
		compile:    []string{"python3", "-I", "-B", "-m", "py_compile", "/workspace/main.py"},
		run:        []string{"python3", "-I", "-B", "/workspace/main.py"},
	},
	LanguageCPP: {
		sourceName: "main.cpp",
		compile: []string{
			"g++", "-std=c++20", "-O2", "-pipe", "-o", "/workspace/main", "/workspace/main.cpp",
		},
		run: []string{"/workspace/main"},
	},
	LanguageJava: {
		sourceName: "Main.java",
		compile:    []string{"javac", "-encoding", "UTF-8", "-d", "/workspace", "/workspace/Main.java"},
		run:        []string{"java", "-XX:+ExitOnOutOfMemoryError", "-cp", "/workspace", "Main"},
	},
}

type DockerExecutor struct {
	engine           dockerEngine
	images           map[Language]string
	instanceID       string
	staleResourceAge time.Duration
}

func NewDockerExecutor(ctx context.Context, cfg config.JudgeConfig, logger *slog.Logger) (*DockerExecutor, error) {
	engine, err := client.New(client.FromEnv)
	if err != nil {
		return nil, fmt.Errorf("create Docker client: %w", err)
	}
	executor, err := newDockerExecutor(ctx, engine, cfg, logger)
	if err != nil {
		_ = engine.Close()
		return nil, err
	}
	return executor, nil
}

func newDockerExecutor(
	ctx context.Context,
	engine dockerEngine,
	cfg config.JudgeConfig,
	logger *slog.Logger,
) (*DockerExecutor, error) {
	if engine == nil {
		return nil, errors.New("docker engine is required")
	}
	if logger == nil {
		logger = slog.New(slog.DiscardHandler)
	}
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("validate Judge config: %w", err)
	}
	if _, err := engine.Ping(ctx, client.PingOptions{NegotiateAPIVersion: true}); err != nil {
		return nil, fmt.Errorf("ping Docker daemon: %w", err)
	}
	info, err := engine.Info(ctx, client.InfoOptions{})
	if err != nil {
		return nil, fmt.Errorf("inspect Docker daemon: %w", err)
	}
	if err := validateDockerHost(info); err != nil {
		return nil, err
	}

	configuredImages := map[Language]string{
		LanguagePython: cfg.PythonImage,
		LanguageCPP:    cfg.CPPImage,
		LanguageJava:   cfg.JavaImage,
	}
	images := make(map[Language]string, len(configuredImages))
	for language, reference := range configuredImages {
		inspection, err := engine.ImageInspect(ctx, reference)
		if err != nil {
			return nil, fmt.Errorf("inspect %s sandbox image %q: %w", language, reference, err)
		}
		if inspection.ID == "" {
			return nil, fmt.Errorf("inspect %s sandbox image %q: empty image ID", language, reference)
		}
		images[language] = inspection.ID
	}

	executor := &DockerExecutor{
		engine:           engine,
		images:           images,
		instanceID:       uuid.NewString(),
		staleResourceAge: cfg.AttemptLease,
	}
	if err := executor.cleanupStaleResources(ctx); err != nil {
		return nil, fmt.Errorf("clean stale sandbox resources: %w", err)
	}
	logger.Info("sandbox executor initialized",
		"instance_id", executor.instanceID,
		"python_image_id", images[LanguagePython],
		"cpp_image_id", images[LanguageCPP],
		"java_image_id", images[LanguageJava],
	)
	return executor, nil
}

func (e *DockerExecutor) Close() error {
	if e == nil || e.engine == nil {
		return nil
	}
	return e.engine.Close()
}

func (e *DockerExecutor) Execute(ctx context.Context, request ExecutionRequest) (
	outcome ExecutionOutcome,
	err error,
) {
	if e == nil || e.engine == nil {
		return ExecutionOutcome{}, errors.New("docker executor is not initialized")
	}
	if err := request.Validate(); err != nil {
		return ExecutionOutcome{}, fmt.Errorf("validate execution request: %w", err)
	}
	spec := languageSpecs[request.Language]
	imageID, ok := e.images[request.Language]
	if !ok || imageID == "" {
		return ExecutionOutcome{}, fmt.Errorf("no image configured for language %q", request.Language)
	}

	attemptID := uuid.NewString()
	resources := &attemptResources{
		engine:         e.engine,
		cleanupTimeout: request.Limits.CleanupTimeout,
	}
	defer func() {
		if cleanupErr := resources.cleanup(); cleanupErr != nil {
			outcome = ExecutionOutcome{}
			err = errors.Join(err, cleanupErr)
		}
	}()

	totalCtx, cancelTotal := context.WithTimeout(ctx, request.Limits.TotalTimeout)
	defer cancelTotal()
	labels := e.resourceLabels(attemptID)
	volumeName, err := e.createWorkspace(totalCtx, attemptID, labels, request.Limits.WorkspaceBytes)
	resources.volume = volumeName
	if err != nil {
		return ExecutionOutcome{}, err
	}

	stagingID, err := e.createContainer(
		totalCtx,
		imageID,
		attemptID+"-staging",
		labelsWithType(labels, "staging"),
		[]string{"sleep", "infinity"},
		volumeName,
		false,
		request.Limits,
	)
	resources.addContainer(stagingID)
	if err != nil {
		return ExecutionOutcome{}, err
	}
	if _, err := e.engine.ContainerStart(totalCtx, stagingID, client.ContainerStartOptions{}); err != nil {
		return ExecutionOutcome{}, fmt.Errorf("start source staging container: %w", err)
	}
	archive, err := sourceArchive(spec.sourceName, request.Source)
	if err != nil {
		return ExecutionOutcome{}, fmt.Errorf("archive source: %w", err)
	}
	if _, err := e.engine.CopyToContainer(totalCtx, stagingID, client.CopyToContainerOptions{
		DestinationPath: sandboxWorkspace,
		Content:         bytes.NewReader(archive),
		CopyUIDGID:      true,
	}); err != nil {
		return ExecutionOutcome{}, fmt.Errorf("copy source to compile container: %w", err)
	}
	compileID, err := e.createContainer(
		totalCtx,
		imageID,
		attemptID+"-compile",
		labelsWithType(labels, "compile"),
		spec.compile,
		volumeName,
		false,
		request.Limits,
	)
	resources.addContainer(compileID)
	if err != nil {
		return ExecutionOutcome{}, err
	}

	compileCtx, cancelCompile := context.WithTimeout(totalCtx, request.Limits.CompileTimeout)
	compileResult, runErr := e.runContainer(compileCtx, compileID, nil, request.Limits.MaxOutputBytes, request.Limits.CleanupTimeout)
	cancelCompile()
	if runErr != nil {
		return classifyProcessError(ctx, runErr, 0)
	}
	if compileResult.exitCode != 0 {
		return ExecutionOutcome{Kind: OutcomeCompileError}, nil
	}
	if err := resources.removeContainer(compileID); err != nil {
		return ExecutionOutcome{}, fmt.Errorf("remove compile container: %w", err)
	}

	for index, test := range request.Tests {
		runtimeID, err := e.createContainer(
			totalCtx,
			imageID,
			fmt.Sprintf("%s-test-%d", attemptID, index+1),
			labelsWithType(labels, "runtime"),
			spec.run,
			volumeName,
			true,
			request.Limits,
		)
		resources.addContainer(runtimeID)
		if err != nil {
			return ExecutionOutcome{}, err
		}
		testCtx, cancelTest := context.WithTimeout(totalCtx, request.Limits.TestTimeout)
		result, runErr := e.runContainer(
			testCtx,
			runtimeID,
			test.Input,
			request.Limits.MaxOutputBytes,
			request.Limits.CleanupTimeout,
		)
		cancelTest()
		if runErr != nil {
			return classifyProcessError(ctx, runErr, index)
		}
		inspection, err := e.engine.ContainerInspect(totalCtx, runtimeID, client.ContainerInspectOptions{})
		if err != nil {
			return ExecutionOutcome{}, fmt.Errorf("inspect runtime container: %w", err)
		}
		if err := resources.removeContainer(runtimeID); err != nil {
			return ExecutionOutcome{}, fmt.Errorf("remove runtime container: %w", err)
		}
		if inspection.Container.State == nil {
			return ExecutionOutcome{}, errors.New("inspect runtime container: missing state")
		}
		if result.exitCode != 0 || inspection.Container.State.OOMKilled {
			return ExecutionOutcome{Kind: OutcomeRuntimeError, TestsPassed: index}, nil
		}
		if !outputMatches(result.stdout, test.Expected) {
			return ExecutionOutcome{Kind: OutcomeWrongAnswer, TestsPassed: index}, nil
		}
	}
	return ExecutionOutcome{Kind: OutcomePass, TestsPassed: len(request.Tests)}, nil
}

func classifyProcessError(parent context.Context, err error, testsPassed int) (ExecutionOutcome, error) {
	if errors.Is(err, errOutputLimit) {
		return ExecutionOutcome{Kind: OutcomeOutputLimit, TestsPassed: testsPassed}, nil
	}
	if errors.Is(err, errProcessTimeout) {
		if parent.Err() != nil && !errors.Is(parent.Err(), context.DeadlineExceeded) {
			return ExecutionOutcome{}, parent.Err()
		}
		return ExecutionOutcome{Kind: OutcomeTimeout, TestsPassed: testsPassed}, nil
	}
	return ExecutionOutcome{}, err
}

func (e *DockerExecutor) createWorkspace(
	ctx context.Context,
	attemptID string,
	labels map[string]string,
	size int64,
) (string, error) {
	name := "codeduel-sandbox-" + attemptID
	result, err := e.engine.VolumeCreate(ctx, client.VolumeCreateOptions{
		Name:   name,
		Driver: "local",
		DriverOpts: map[string]string{
			"type":   "tmpfs",
			"device": "tmpfs",
			"o": fmt.Sprintf(
				"size=%d,uid=10001,gid=10001,mode=0700,nosuid,nodev",
				size,
			),
		},
		Labels: labelsWithType(labels, "workspace"),
	})
	if err != nil {
		return name, fmt.Errorf("create sandbox workspace: %w", err)
	}
	if result.Volume.Name == "" {
		return name, errors.New("create sandbox workspace: empty volume name")
	}
	return name, nil
}

func (e *DockerExecutor) createContainer(
	ctx context.Context,
	imageID, name string,
	labels map[string]string,
	command []string,
	volumeName string,
	workspaceReadOnly bool,
	limits Limits,
) (string, error) {
	options := sandboxContainerOptions(imageID, name, labels, command, volumeName, workspaceReadOnly, limits)
	created, err := e.engine.ContainerCreate(ctx, options)
	if err != nil {
		return options.Name, fmt.Errorf("create sandbox container: %w", err)
	}
	if created.ID == "" {
		return options.Name, errors.New("create sandbox container: empty container ID")
	}
	if len(created.Warnings) > 0 {
		return options.Name, fmt.Errorf("create sandbox container: daemon warnings: %s", strings.Join(created.Warnings, "; "))
	}
	return options.Name, nil
}

func validateDockerHost(result client.SystemInfoResult) error {
	info := result.Info
	if info.OSType != "linux" {
		return fmt.Errorf("docker sandbox requires a Linux daemon, got %q", info.OSType)
	}
	if !info.MemoryLimit || !info.SwapLimit || !info.CPUCfsQuota || !info.PidsLimit {
		return errors.New("docker daemon does not enforce all required memory, swap, CPU, and PID limits")
	}
	seccompEnabled := false
	for _, option := range info.SecurityOptions {
		if strings.HasPrefix(option, "name=seccomp") {
			seccompEnabled = true
			break
		}
	}
	if !seccompEnabled {
		return errors.New("docker daemon does not report an enabled seccomp profile")
	}
	return nil
}

func sandboxContainerOptions(
	imageID, name string,
	labels map[string]string,
	command []string,
	volumeName string,
	workspaceReadOnly bool,
	limits Limits,
) client.ContainerCreateOptions {
	pids := limits.PIDLimit
	return client.ContainerCreateOptions{
		Name:  "codeduel-sandbox-" + name,
		Image: imageID,
		Config: &container.Config{
			User:            sandboxUser,
			AttachStdin:     true,
			AttachStdout:    true,
			AttachStderr:    true,
			OpenStdin:       true,
			StdinOnce:       true,
			Env:             []string{"HOME=/tmp", "LANG=C.UTF-8", "LC_ALL=C.UTF-8"},
			Cmd:             slices.Clone(command),
			WorkingDir:      sandboxWorkspace,
			NetworkDisabled: true,
			Labels:          labels,
		},
		HostConfig: &container.HostConfig{
			LogConfig:      container.LogConfig{Type: "none"},
			NetworkMode:    container.NetworkMode("none"),
			CapDrop:        []string{"ALL"},
			Privileged:     false,
			ReadonlyRootfs: true,
			SecurityOpt:    []string{"no-new-privileges=true"},
			ShmSize:        limits.TmpfsBytes,
			Tmpfs: map[string]string{
				"/tmp": fmt.Sprintf("rw,nosuid,nodev,noexec,size=%d,mode=1777", limits.TmpfsBytes),
			},
			Resources: container.Resources{
				Memory:     limits.MemoryBytes,
				MemorySwap: limits.MemorySwap,
				NanoCPUs:   limits.NanoCPUs,
				PidsLimit:  &pids,
				Ulimits: []*container.Ulimit{
					{Name: "core", Soft: 0, Hard: 0},
					{Name: "fsize", Soft: limits.WorkspaceBytes, Hard: limits.WorkspaceBytes},
					{Name: "nofile", Soft: 128, Hard: 128},
					{Name: "nproc", Soft: limits.PIDLimit, Hard: limits.PIDLimit},
				},
			},
			Mounts: []mount.Mount{
				{
					Type:     mount.TypeVolume,
					Source:   volumeName,
					Target:   sandboxWorkspace,
					ReadOnly: workspaceReadOnly,
				},
			},
		},
	}
}

type processResult struct {
	exitCode int64
	stdout   []byte
	stderr   []byte
}

func (e *DockerExecutor) runContainer(
	ctx context.Context,
	containerID string,
	input []byte,
	outputLimit int64,
	cleanupTimeout time.Duration,
) (processResult, error) {
	attached, err := e.engine.ContainerAttach(ctx, containerID, client.ContainerAttachOptions{
		Stream: true,
		Stdin:  true,
		Stdout: true,
		Stderr: true,
	})
	if err != nil {
		return processResult{}, fmt.Errorf("attach sandbox container: %w", err)
	}
	defer attached.Close()

	var stdout, stderr bytes.Buffer
	budget := newOutputBudget(outputLimit)
	outputDone := make(chan error, 1)
	go func() {
		_, copyErr := stdcopy.StdCopy(
			budgetWriter{budget: budget, buffer: &stdout},
			budgetWriter{budget: budget, buffer: &stderr},
			attached.Reader,
		)
		outputDone <- copyErr
	}()

	if _, err := e.engine.ContainerStart(ctx, containerID, client.ContainerStartOptions{}); err != nil {
		return processResult{}, fmt.Errorf("start sandbox container: %w", err)
	}
	go func() {
		if len(input) > 0 {
			_, _ = attached.Conn.Write(input)
		}
		_ = attached.CloseWrite()
	}()
	wait := e.engine.ContainerWait(ctx, containerID, client.ContainerWaitOptions{
		Condition: container.WaitConditionNotRunning,
	})

	var response container.WaitResponse
	select {
	case <-ctx.Done():
		e.kill(containerID, cleanupTimeout)
		return processResult{}, errProcessTimeout
	case copyErr := <-outputDone:
		if copyErr != nil {
			e.kill(containerID, cleanupTimeout)
			if errors.Is(copyErr, errOutputLimit) {
				return processResult{}, errOutputLimit
			}
			return processResult{}, fmt.Errorf("read sandbox output: %w", copyErr)
		}
		select {
		case <-ctx.Done():
			e.kill(containerID, cleanupTimeout)
			return processResult{}, errProcessTimeout
		case waitErr := <-wait.Error:
			if waitErr != nil {
				return processResult{}, fmt.Errorf("wait for sandbox container: %w", waitErr)
			}
		case response = <-wait.Result:
		}
	case waitErr := <-wait.Error:
		if waitErr != nil {
			if ctx.Err() != nil {
				e.kill(containerID, cleanupTimeout)
				return processResult{}, errProcessTimeout
			}
			return processResult{}, fmt.Errorf("wait for sandbox container: %w", waitErr)
		}
	case response = <-wait.Result:
		select {
		case copyErr := <-outputDone:
			if copyErr != nil {
				if errors.Is(copyErr, errOutputLimit) {
					return processResult{}, errOutputLimit
				}
				return processResult{}, fmt.Errorf("read sandbox output: %w", copyErr)
			}
		case <-ctx.Done():
			e.kill(containerID, cleanupTimeout)
			return processResult{}, errProcessTimeout
		}
	}
	if response.Error != nil {
		return processResult{}, fmt.Errorf("sandbox container wait: %s", response.Error.Message)
	}
	return processResult{exitCode: response.StatusCode, stdout: stdout.Bytes(), stderr: stderr.Bytes()}, nil
}

func (e *DockerExecutor) kill(containerID string, timeout time.Duration) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	_, _ = e.engine.ContainerKill(ctx, containerID, client.ContainerKillOptions{Signal: "SIGKILL"})
}

func (e *DockerExecutor) resourceLabels(attemptID string) map[string]string {
	return map[string]string{
		sandboxLabel:  "true",
		attemptLabel:  attemptID,
		instanceLabel: e.instanceID,
	}
}

func labelsWithType(labels map[string]string, resourceType string) map[string]string {
	result := make(map[string]string, len(labels)+1)
	for key, value := range labels {
		result[key] = value
	}
	result[resourceTypeLabel] = resourceType
	return result
}

func sourceArchive(name string, source []byte) ([]byte, error) {
	var archive bytes.Buffer
	writer := tar.NewWriter(&archive)
	if err := writer.WriteHeader(&tar.Header{
		Name: name,
		Mode: 0o400,
		Uid:  10001,
		Gid:  10001,
		Size: int64(len(source)),
	}); err != nil {
		return nil, err
	}
	if _, err := writer.Write(source); err != nil {
		return nil, err
	}
	if err := writer.Close(); err != nil {
		return nil, err
	}
	return archive.Bytes(), nil
}

func (e *DockerExecutor) cleanupStaleResources(ctx context.Context) error {
	filters := make(client.Filters).Add("label", sandboxLabel+"=true")
	containers, err := e.engine.ContainerList(ctx, client.ContainerListOptions{All: true, Filters: filters})
	if err != nil {
		return fmt.Errorf("list containers: %w", err)
	}
	cutoff := time.Now().Add(-e.staleResourceAge)
	for _, item := range containers.Items {
		if time.Unix(item.Created, 0).After(cutoff) {
			continue
		}
		if _, err := e.engine.ContainerRemove(ctx, item.ID, client.ContainerRemoveOptions{Force: true, RemoveVolumes: true}); err != nil {
			return fmt.Errorf("remove stale container %s: %w", item.ID, err)
		}
	}
	volumes, err := e.engine.VolumeList(ctx, client.VolumeListOptions{Filters: filters})
	if err != nil {
		return fmt.Errorf("list volumes: %w", err)
	}
	for _, item := range volumes.Items {
		created, parseErr := time.Parse(time.RFC3339Nano, item.CreatedAt)
		if parseErr != nil || created.After(cutoff) {
			continue
		}
		if _, err := e.engine.VolumeRemove(ctx, item.Name, client.VolumeRemoveOptions{Force: true}); err != nil {
			return fmt.Errorf("remove stale volume %s: %w", item.Name, err)
		}
	}
	return nil
}

type attemptResources struct {
	engine         resourceCleaner
	containers     []string
	volume         string
	cleanupTimeout time.Duration
}

type resourceCleaner interface {
	ContainerRemove(context.Context, string, client.ContainerRemoveOptions) (client.ContainerRemoveResult, error)
	VolumeRemove(context.Context, string, client.VolumeRemoveOptions) (client.VolumeRemoveResult, error)
}

func (r *attemptResources) addContainer(containerID string) {
	if containerID == "" {
		return
	}
	r.containers = append(r.containers, containerID)
}

func (r *attemptResources) removeContainer(containerID string) error {
	ctx, cancel := context.WithTimeout(context.Background(), r.cleanupTimeout)
	defer cancel()
	if _, err := r.engine.ContainerRemove(ctx, containerID, client.ContainerRemoveOptions{Force: true, RemoveVolumes: true}); err != nil {
		return err
	}
	for index, candidate := range r.containers {
		if candidate == containerID {
			r.containers = slices.Delete(r.containers, index, index+1)
			break
		}
	}
	return nil
}

func (r *attemptResources) cleanup() error {
	if r == nil || r.engine == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), r.cleanupTimeout)
	defer cancel()
	var cleanupErrors []error
	for index := len(r.containers) - 1; index >= 0; index-- {
		containerID := r.containers[index]
		if _, err := r.engine.ContainerRemove(ctx, containerID, client.ContainerRemoveOptions{
			Force:         true,
			RemoveVolumes: true,
		}); err != nil && !strings.Contains(strings.ToLower(err.Error()), "no such container") {
			cleanupErrors = append(cleanupErrors, fmt.Errorf("remove container %s: %w", containerID, err))
		}
	}
	if r.volume != "" {
		if _, err := r.engine.VolumeRemove(ctx, r.volume, client.VolumeRemoveOptions{Force: true}); err != nil &&
			!strings.Contains(strings.ToLower(err.Error()), "no such volume") {
			cleanupErrors = append(cleanupErrors, fmt.Errorf("remove volume %s: %w", r.volume, err))
		}
	}
	if len(cleanupErrors) > 0 {
		return fmt.Errorf("clean sandbox resources: %w", errors.Join(cleanupErrors...))
	}
	return nil
}

var _ Executor = (*DockerExecutor)(nil)
