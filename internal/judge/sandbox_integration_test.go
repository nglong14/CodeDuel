package judge

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/moby/moby/client"
	"github.com/nglong14/CodeDuel/internal/config"
)

func TestSandboxLanguageOutcomesIntegration(t *testing.T) {
	executor, limits := sandboxExecutorForTest(t)
	languages := []struct {
		name         string
		language     Language
		correct      string
		compileError string
		runtimeError string
		timeout      string
		outputFlood  string
	}{
		{
			name:         "python",
			language:     LanguagePython,
			correct:      "import sys\nprint(sys.stdin.read().strip())\n",
			compileError: "if True print('broken')\n",
			runtimeError: "raise RuntimeError('boom')\n",
			timeout:      "while True:\n    pass\n",
			outputFlood:  "print('x' * 100000)\n",
		},
		{
			name:         "cpp",
			language:     LanguageCPP,
			correct:      "#include <iostream>\n#include <string>\nint main(){std::string s;std::getline(std::cin,s);std::cout<<s;}\n",
			compileError: "int main( {\n",
			runtimeError: "int main(){return 3;}\n",
			timeout:      "int main(){for(;;){}}\n",
			outputFlood:  "#include <iostream>\nint main(){for(int i=0;i<100000;i++)std::cout<<'x';}\n",
		},
		{
			name:         "java",
			language:     LanguageJava,
			correct:      "import java.io.*; class Main { public static void main(String[] a) throws Exception { System.out.print(new BufferedReader(new InputStreamReader(System.in)).readLine()); } }\n",
			compileError: "class Main { public static void main(String[] a) { broken } }\n",
			runtimeError: "class Main { public static void main(String[] a) { throw new RuntimeException(\"boom\"); } }\n",
			timeout:      "class Main { public static void main(String[] a) { while (true) {} } }\n",
			outputFlood:  "class Main { public static void main(String[] a) { System.out.print(\"x\".repeat(100000)); } }\n",
		},
	}

	for _, language := range languages {
		t.Run(language.name, func(t *testing.T) {
			assertSandboxOutcome(t, executor, ExecutionRequest{
				Language: language.language,
				Source:   []byte(language.correct),
				Tests: []TestCase{
					{Input: []byte("alpha\n"), Expected: []byte("alpha")},
					{Input: []byte("beta\n"), Expected: []byte("beta\n")},
				},
				Limits: limits,
			}, ExecutionOutcome{Kind: OutcomePass, TestsPassed: 2})

			assertSandboxOutcome(t, executor, ExecutionRequest{
				Language: language.language,
				Source:   []byte(language.correct),
				Tests:    []TestCase{{Input: []byte("actual\n"), Expected: []byte("expected")}},
				Limits:   limits,
			}, ExecutionOutcome{Kind: OutcomeWrongAnswer})

			assertSandboxOutcome(t, executor, ExecutionRequest{
				Language: language.language,
				Source:   []byte(language.correct),
				Tests: []TestCase{
					{Input: []byte("pass\n"), Expected: []byte("pass")},
					{Input: []byte("actual\n"), Expected: []byte("expected")},
				},
				Limits: limits,
			}, ExecutionOutcome{Kind: OutcomeWrongAnswer, TestsPassed: 1})

			assertSandboxOutcome(t, executor, ExecutionRequest{
				Language: language.language,
				Source:   []byte(language.compileError),
				Tests:    []TestCase{{}},
				Limits:   limits,
			}, ExecutionOutcome{Kind: OutcomeCompileError})

			assertSandboxOutcome(t, executor, ExecutionRequest{
				Language: language.language,
				Source:   []byte(language.runtimeError),
				Tests:    []TestCase{{}},
				Limits:   limits,
			}, ExecutionOutcome{Kind: OutcomeRuntimeError})

			timeoutLimits := limits
			timeoutLimits.TestTimeout = 300 * time.Millisecond
			assertSandboxOutcome(t, executor, ExecutionRequest{
				Language: language.language,
				Source:   []byte(language.timeout),
				Tests:    []TestCase{{}},
				Limits:   timeoutLimits,
			}, ExecutionOutcome{Kind: OutcomeTimeout})

			outputLimits := limits
			outputLimits.MaxOutputBytes = 4096
			assertSandboxOutcome(t, executor, ExecutionRequest{
				Language: language.language,
				Source:   []byte(language.outputFlood),
				Tests:    []TestCase{{}},
				Limits:   outputLimits,
			}, ExecutionOutcome{Kind: OutcomeOutputLimit})
		})
	}
}

func TestSandboxBoundaryIntegration(t *testing.T) {
	executor, limits := sandboxExecutorForTest(t)
	languages := []struct {
		name     string
		language Language
		source   string
	}{
		{"python", LanguagePython, `import os
import socket

uid_ok = os.getuid() != 0 and os.getgid() != 0
with open("/proc/self/status", encoding="utf-8") as status:
    status_lines = status.readlines()
caps_ok = int(next(line for line in status_lines if line.startswith("CapEff:")).split()[1], 16) == 0
no_new_privs = next(line for line in status_lines if line.startswith("NoNewPrivs:")).split()[1] == "1"
seccomp = next(line for line in status_lines if line.startswith("Seccomp:")).split()[1] == "2"

root_read_only = False
try:
    with open("/codeduel-root-write", "w", encoding="utf-8") as output:
        output.write("bad")
except OSError:
    root_read_only = True

workspace_read_only = False
try:
    with open("/workspace/leak", "w", encoding="utf-8") as output:
        output.write("bad")
except OSError:
    workspace_read_only = True

network_blocked = False
sock = socket.socket()
sock.settimeout(0.2)
try:
    sock.connect(("1.1.1.1", 53))
except OSError:
    network_blocked = True
finally:
    sock.close()

fresh = not os.path.exists("/tmp/leak")
with open("/tmp/leak", "w", encoding="utf-8") as output:
    output.write("test-local")

print("secure" if all((uid_ok, caps_ok, no_new_privs, seccomp, root_read_only, workspace_read_only, network_blocked, fresh)) else "unsafe")
`},
		{"cpp", LanguageCPP, `#include <arpa/inet.h>
#include <fstream>
#include <iostream>
#include <netinet/in.h>
#include <string>
#include <sys/socket.h>
#include <unistd.h>

int main() {
    bool uid_ok = getuid() != 0 && getgid() != 0;
    std::ifstream status("/proc/self/status");
    std::string line;
    bool caps_ok = false;
	bool no_new_privs = false;
	bool seccomp = false;
    while (std::getline(status, line)) {
        if (line.rfind("CapEff:", 0) == 0) {
            caps_ok = std::stoull(line.substr(7), nullptr, 16) == 0;
        }
		if (line.rfind("NoNewPrivs:", 0) == 0) no_new_privs = line.find('1') != std::string::npos;
		if (line.rfind("Seccomp:", 0) == 0) seccomp = line.find('2') != std::string::npos;
    }
    std::ofstream root("/codeduel-root-write");
    bool root_read_only = !root;
    std::ofstream workspace("/workspace/leak");
    bool workspace_read_only = !workspace;
    int sock = socket(AF_INET, SOCK_STREAM, 0);
    sockaddr_in address{};
    address.sin_family = AF_INET;
    address.sin_port = htons(53);
    inet_pton(AF_INET, "1.1.1.1", &address.sin_addr);
    bool network_blocked = sock < 0 || connect(sock, reinterpret_cast<sockaddr*>(&address), sizeof(address)) != 0;
    if (sock >= 0) close(sock);
    bool fresh = access("/tmp/leak", F_OK) != 0;
    std::ofstream("/tmp/leak") << "test-local";
    std::cout << (uid_ok && caps_ok && no_new_privs && seccomp && root_read_only && workspace_read_only && network_blocked && fresh ? "secure" : "unsafe");
}
`},
		{"java", LanguageJava, `import java.io.IOException;
import java.math.BigInteger;
import java.net.InetSocketAddress;
import java.net.Socket;
import java.nio.file.Files;
import java.nio.file.Path;

class Main {
    public static void main(String[] args) throws Exception {
        boolean uidOk = false;
        boolean capsOk = false;
		boolean noNewPrivs = false;
		boolean seccomp = false;
        for (String line : Files.readAllLines(Path.of("/proc/self/status"))) {
            if (line.startsWith("Uid:")) uidOk = !line.split("\\s+")[1].equals("0");
            if (line.startsWith("CapEff:")) capsOk = new BigInteger(line.split("\\s+")[1], 16).equals(BigInteger.ZERO);
			if (line.startsWith("NoNewPrivs:")) noNewPrivs = line.split("\\s+")[1].equals("1");
			if (line.startsWith("Seccomp:")) seccomp = line.split("\\s+")[1].equals("2");
        }
        boolean rootReadOnly = writeFails(Path.of("/codeduel-root-write"));
        boolean workspaceReadOnly = writeFails(Path.of("/workspace/leak"));
        boolean networkBlocked = false;
        try (Socket socket = new Socket()) {
            socket.connect(new InetSocketAddress("1.1.1.1", 53), 200);
        } catch (IOException error) {
            networkBlocked = true;
        }
        Path leak = Path.of("/tmp/leak");
        boolean fresh = !Files.exists(leak);
        Files.writeString(leak, "test-local");
		System.out.print(uidOk && capsOk && noNewPrivs && seccomp && rootReadOnly && workspaceReadOnly && networkBlocked && fresh ? "secure" : "unsafe");
    }

    private static boolean writeFails(Path path) {
        try {
            Files.writeString(path, "bad");
            return false;
        } catch (IOException error) {
            return true;
        }
    }
}
`},
	}
	for _, language := range languages {
		t.Run(language.name, func(t *testing.T) {
			assertSandboxOutcome(t, executor, ExecutionRequest{
				Language: language.language,
				Source:   []byte(language.source),
				Tests: []TestCase{
					{Expected: []byte("secure")},
					{Expected: []byte("secure")},
				},
				Limits: limits,
			}, ExecutionOutcome{Kind: OutcomePass, TestsPassed: 2})
		})
	}
}

func TestSandboxMemoryAndPIDLimitsIntegration(t *testing.T) {
	executor, limits := sandboxExecutorForTest(t)
	limits.MemoryBytes = 96 << 20
	limits.MemorySwap = 96 << 20
	limits.TestTimeout = 5 * time.Second
	assertSandboxOutcome(t, executor, ExecutionRequest{
		Language: LanguagePython,
		Source:   []byte("blocks = []\nwhile True:\n    blocks.append(bytearray(10_000_000))\n"),
		Tests:    []TestCase{{}},
		Limits:   limits,
	}, ExecutionOutcome{Kind: OutcomeRuntimeError})

	pidSource := `import os
import signal

children = []
blocked = False
try:
    while True:
        pid = os.fork()
        if pid == 0:
            signal.pause()
            os._exit(0)
        children.append(pid)
except OSError:
    blocked = True
finally:
    for pid in children:
        try:
            os.kill(pid, signal.SIGKILL)
        except OSError:
            pass
    for pid in children:
        try:
            os.waitpid(pid, 0)
        except OSError:
            pass
print("blocked" if blocked else "unlimited")
`
	limits.PIDLimit = 16
	assertSandboxOutcome(t, executor, ExecutionRequest{
		Language: LanguagePython,
		Source:   []byte(pidSource),
		Tests:    []TestCase{{Expected: []byte("blocked")}},
		Limits:   limits,
	}, ExecutionOutcome{Kind: OutcomePass, TestsPassed: 1})
}

func TestSandboxCancellationIntegration(t *testing.T) {
	executor, limits := sandboxExecutorForTest(t)
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(200 * time.Millisecond)
		cancel()
	}()
	_, err := executor.Execute(ctx, ExecutionRequest{
		Language: LanguagePython,
		Source:   []byte("while True:\n    pass\n"),
		Tests:    []TestCase{{}},
		Limits:   limits,
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Execute error = %v, want context canceled", err)
	}
	assertNoSandboxResources(t, executor)
}

func TestSandboxMissingImageIntegration(t *testing.T) {
	requireDockerIntegration(t)
	cfg := sandboxTestConfig()
	cfg.PythonImage = "codeduel/sandbox-python:does-not-exist"
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if executor, err := NewDockerExecutor(ctx, cfg, slog.New(slog.DiscardHandler)); err == nil {
		_ = executor.Close()
		t.Fatal("NewDockerExecutor returned nil error for missing image")
	}
}

func TestSandboxUnavailableDockerIntegration(t *testing.T) {
	requireDockerIntegration(t)
	t.Setenv("DOCKER_HOST", "unix:///tmp/codeduel-missing-docker.sock")
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if executor, err := NewDockerExecutor(ctx, sandboxTestConfig(), slog.New(slog.DiscardHandler)); err == nil {
		_ = executor.Close()
		t.Fatal("NewDockerExecutor returned nil error for unavailable Docker daemon")
	}
}

func sandboxExecutorForTest(t *testing.T) (*DockerExecutor, Limits) {
	t.Helper()
	requireDockerIntegration(t)
	cfg := sandboxTestConfig()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	executor, err := NewDockerExecutor(ctx, cfg, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("NewDockerExecutor: %v", err)
	}
	t.Cleanup(func() {
		assertNoSandboxResources(t, executor)
		if err := executor.Close(); err != nil {
			t.Errorf("Close: %v", err)
		}
	})
	return executor, limitsFromConfig(cfg)
}

func requireDockerIntegration(t *testing.T) {
	t.Helper()
	if os.Getenv("CODEDUEL_DOCKER_INTEGRATION") != "1" {
		t.Skip("set CODEDUEL_DOCKER_INTEGRATION=1 to run Docker sandbox tests")
	}
}

func sandboxTestConfig() config.JudgeConfig {
	return config.JudgeConfig{
		Concurrency:     1,
		MaxCodeBytes:    64 << 10,
		MaxOutputBytes:  1 << 20,
		CompileTimeout:  10 * time.Second,
		TestTimeout:     2 * time.Second,
		TotalTimeout:    20 * time.Second,
		CleanupTimeout:  5 * time.Second,
		AttemptLease:    45 * time.Second,
		NanoCPUs:        1_000_000_000,
		MemoryBytes:     256 << 20,
		MemorySwapBytes: 256 << 20,
		PIDLimit:        64,
		WorkspaceBytes:  64 << 20,
		TmpfsBytes:      16 << 20,
		PythonImage:     "codeduel/sandbox-python:3.13",
		CPPImage:        "codeduel/sandbox-cpp:gcc14",
		JavaImage:       "codeduel/sandbox-java:temurin21",
	}
}

func assertSandboxOutcome(
	t *testing.T,
	executor *DockerExecutor,
	request ExecutionRequest,
	want ExecutionOutcome,
) {
	t.Helper()
	outcome, err := executor.Execute(context.Background(), request)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if outcome != want {
		t.Fatalf("outcome = %#v, want %#v", outcome, want)
	}
	assertNoSandboxResources(t, executor)
}

func assertNoSandboxResources(t *testing.T, executor *DockerExecutor) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	filters := make(client.Filters).Add("label", sandboxLabel+"=true")
	containers, err := executor.engine.ContainerList(ctx, client.ContainerListOptions{All: true, Filters: filters})
	if err != nil {
		t.Fatalf("list sandbox containers: %v", err)
	}
	if len(containers.Items) != 0 {
		t.Fatalf("sandbox containers remain: %#v", containers.Items)
	}
	volumes, err := executor.engine.VolumeList(ctx, client.VolumeListOptions{Filters: filters})
	if err != nil {
		t.Fatalf("list sandbox volumes: %v", err)
	}
	if len(volumes.Items) != 0 {
		t.Fatalf("sandbox volumes remain: %#v", volumes.Items)
	}
}
