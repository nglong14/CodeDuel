package judge

import (
	"archive/tar"
	"bytes"
	"context"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/moby/moby/api/types/mount"
	"github.com/moby/moby/api/types/system"
	"github.com/moby/moby/client"
)

func TestExecutionRequestValidate(t *testing.T) {
	request := ExecutionRequest{
		Language: LanguagePython,
		Source:   []byte("print('ok')"),
		Tests:    []TestCase{{Expected: []byte("ok")}},
		Limits:   testLimits(),
	}
	if err := request.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}

	tests := []struct {
		name   string
		mutate func(*ExecutionRequest)
	}{
		{"unknown language", func(r *ExecutionRequest) { r.Language = "ruby" }},
		{"empty source", func(r *ExecutionRequest) { r.Source = nil }},
		{"invalid UTF-8 source", func(r *ExecutionRequest) { r.Source = []byte{0xff} }},
		{"no tests", func(r *ExecutionRequest) { r.Tests = nil }},
		{"oversized source", func(r *ExecutionRequest) { r.Source = bytes.Repeat([]byte("x"), 65) }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			invalid := request
			test.mutate(&invalid)
			if err := invalid.Validate(); err == nil {
				t.Fatal("Validate returned nil error")
			}
		})
	}
}

func TestNormalizeOutput(t *testing.T) {
	tests := []struct {
		name     string
		actual   string
		expected string
		matches  bool
	}{
		{"one final newline", "42\n", "42", true},
		{"CRLF", "42\r\n", "42\n", true},
		{"remove exactly one", "42\n\n", "42\n", false},
		{"preserve spaces", "42 ", "42", false},
		{"preserve bare carriage return", "42\r", "42", false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := outputMatches([]byte(test.actual), []byte(test.expected)); got != test.matches {
				t.Fatalf("outputMatches() = %v, want %v", got, test.matches)
			}
		})
	}
}

func TestOutputBudgetIsCombined(t *testing.T) {
	budget := newOutputBudget(5)
	var stdout, stderr bytes.Buffer
	stdoutWriter := budgetWriter{budget: budget, buffer: &stdout}
	stderrWriter := budgetWriter{budget: budget, buffer: &stderr}
	if _, err := stdoutWriter.Write([]byte("abc")); err != nil {
		t.Fatalf("write stdout: %v", err)
	}
	n, err := stderrWriter.Write([]byte("def"))
	if !errors.Is(err, errOutputLimit) {
		t.Fatalf("write stderr error = %v, want output limit", err)
	}
	if n != 2 || stdout.String() != "abc" || stderr.String() != "de" {
		t.Fatalf("writes = (%d, %q, %q), want (2, abc, de)", n, stdout.String(), stderr.String())
	}
}

func TestSourceArchiveUsesFixedFile(t *testing.T) {
	raw, err := sourceArchive("main.py", []byte("print('ok')"))
	if err != nil {
		t.Fatalf("sourceArchive: %v", err)
	}
	reader := tar.NewReader(bytes.NewReader(raw))
	header, err := reader.Next()
	if err != nil {
		t.Fatalf("read archive header: %v", err)
	}
	if header.Name != "main.py" || header.Uid != 10001 || header.Gid != 10001 || header.Mode != 0o400 {
		t.Fatalf("archive header = %#v", header)
	}
	contents, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("read archive contents: %v", err)
	}
	if string(contents) != "print('ok')" {
		t.Fatalf("archive contents = %q", contents)
	}
	if _, err := reader.Next(); !errors.Is(err, io.EOF) {
		t.Fatalf("second archive entry error = %v, want EOF", err)
	}
}

func TestLanguageCommandsDoNotUseShell(t *testing.T) {
	for language, spec := range languageSpecs {
		for _, command := range [][]string{spec.compile, spec.run} {
			if len(command) == 0 {
				t.Fatalf("%s has empty command", language)
			}
			if command[0] == "sh" || command[0] == "/bin/sh" || command[0] == "bash" || command[0] == "/bin/bash" {
				t.Fatalf("%s command uses a shell: %v", language, command)
			}
		}
	}
}

func TestSandboxContainerOptions(t *testing.T) {
	limits := testLimits()
	options := sandboxContainerOptions(
		"sha256:image",
		"attempt-test-1",
		map[string]string{sandboxLabel: "true"},
		[]string{"python3", "/workspace/main.py"},
		"workspace",
		true,
		limits,
	)
	if options.Image != "sha256:image" || options.Config.Image != "" {
		t.Fatalf("image options = (%q, %q)", options.Image, options.Config.Image)
	}
	if options.Config.User != sandboxUser || !options.Config.NetworkDisabled || options.Config.Tty {
		t.Fatalf("container config = %#v", options.Config)
	}
	if options.HostConfig.NetworkMode != "none" || !options.HostConfig.ReadonlyRootfs || options.HostConfig.Privileged {
		t.Fatalf("host isolation config = %#v", options.HostConfig)
	}
	if len(options.HostConfig.CapDrop) != 1 || options.HostConfig.CapDrop[0] != "ALL" {
		t.Fatalf("CapDrop = %v, want ALL", options.HostConfig.CapDrop)
	}
	if len(options.HostConfig.SecurityOpt) != 1 || options.HostConfig.SecurityOpt[0] != "no-new-privileges=true" {
		t.Fatalf("SecurityOpt = %v", options.HostConfig.SecurityOpt)
	}
	if options.HostConfig.LogConfig.Type != "none" {
		t.Fatalf("log driver = %q, want none", options.HostConfig.LogConfig.Type)
	}
	if options.HostConfig.Memory != limits.MemoryBytes || options.HostConfig.MemorySwap != limits.MemorySwap ||
		options.HostConfig.NanoCPUs != limits.NanoCPUs || *options.HostConfig.PidsLimit != limits.PIDLimit {
		t.Fatalf("resource limits = %#v", options.HostConfig.Resources)
	}
	if len(options.HostConfig.Mounts) != 1 {
		t.Fatalf("mount count = %d, want 1", len(options.HostConfig.Mounts))
	}
	workspace := options.HostConfig.Mounts[0]
	if workspace.Type != mount.TypeVolume || workspace.Source != "workspace" || workspace.Target != sandboxWorkspace || !workspace.ReadOnly {
		t.Fatalf("workspace mount = %#v", workspace)
	}
	if options.HostConfig.Tmpfs["/tmp"] == "" {
		t.Fatal("missing /tmp tmpfs")
	}
}

func TestAttemptResourcesCleanup(t *testing.T) {
	cleaner := &recordingCleaner{}
	resources := &attemptResources{
		engine:         cleaner,
		containers:     []string{"compile", "runtime"},
		volume:         "workspace",
		cleanupTimeout: time.Second,
	}
	if err := resources.cleanup(); err != nil {
		t.Fatalf("cleanup: %v", err)
	}
	want := []string{"container:runtime", "container:compile", "volume:workspace"}
	if !slicesEqual(cleaner.removed, want) {
		t.Fatalf("removed = %v, want %v", cleaner.removed, want)
	}
}

func TestClassifyProcessCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := classifyProcessError(ctx, errProcessTimeout, 0)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context canceled", err)
	}
}

func TestValidateDockerHost(t *testing.T) {
	valid := client.SystemInfoResult{Info: system.Info{
		OSType:          "linux",
		MemoryLimit:     true,
		SwapLimit:       true,
		CPUCfsQuota:     true,
		PidsLimit:       true,
		SecurityOptions: []string{"name=seccomp,profile=builtin"},
	}}
	if err := validateDockerHost(valid); err != nil {
		t.Fatalf("validateDockerHost: %v", err)
	}

	tests := []struct {
		name   string
		mutate func(*system.Info)
	}{
		{"non-Linux", func(info *system.Info) { info.OSType = "windows" }},
		{"no memory limit", func(info *system.Info) { info.MemoryLimit = false }},
		{"no swap limit", func(info *system.Info) { info.SwapLimit = false }},
		{"no CPU quota", func(info *system.Info) { info.CPUCfsQuota = false }},
		{"no PID limit", func(info *system.Info) { info.PidsLimit = false }},
		{"no seccomp", func(info *system.Info) { info.SecurityOptions = nil }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			invalid := valid
			test.mutate(&invalid.Info)
			if err := validateDockerHost(invalid); err == nil {
				t.Fatal("validateDockerHost returned nil error")
			}
		})
	}
}

type recordingCleaner struct {
	removed []string
}

func (c *recordingCleaner) ContainerRemove(
	_ context.Context,
	id string,
	_ client.ContainerRemoveOptions,
) (client.ContainerRemoveResult, error) {
	c.removed = append(c.removed, "container:"+id)
	return client.ContainerRemoveResult{}, nil
}

func (c *recordingCleaner) VolumeRemove(
	_ context.Context,
	id string,
	_ client.VolumeRemoveOptions,
) (client.VolumeRemoveResult, error) {
	c.removed = append(c.removed, "volume:"+id)
	return client.VolumeRemoveResult{}, nil
}

func slicesEqual(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func testLimits() Limits {
	return Limits{
		MaxCodeBytes:   64,
		MaxOutputBytes: 1024,
		CompileTimeout: time.Second,
		TestTimeout:    time.Second,
		TotalTimeout:   2 * time.Second,
		CleanupTimeout: time.Second,
		NanoCPUs:       1_000_000_000,
		MemoryBytes:    128 << 20,
		MemorySwap:     128 << 20,
		PIDLimit:       32,
		WorkspaceBytes: 16 << 20,
		TmpfsBytes:     4 << 20,
	}
}
