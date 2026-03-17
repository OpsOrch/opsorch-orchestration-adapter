package orchestration

import (
	"context"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
)

func TestLocalRunnerRunBuildsDockerCommand(t *testing.T) {
	orig := localCommandContext
	t.Cleanup(func() { localCommandContext = orig })

	localCommandContext = fakeCommandContext(
		t,
		map[string]fakeExecResponse{
			"docker run --rm --label opsorch.run-id=run-123 --label opsorch.step-id=step-456 -e REGION=eu-west-1 -e VERSION=v1 ghcr.io/opsorch/tool:latest deploy --force": {
				output: "docker-ok",
			},
		},
	)

	runner := &LocalRunner{}
	out, err := runner.Run(context.Background(), "run-123", "step-456", &StepExec{
		Image:   "ghcr.io/opsorch/tool:latest",
		Command: []string{"deploy"},
		Args:    []string{"--force"},
		Env: map[string]string{
			"REGION":  "eu-west-1",
			"VERSION": "v1",
		},
	})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if out != "docker-ok" {
		t.Fatalf("unexpected output: %q", out)
	}
}

func TestLocalRunnerRunReturnsDockerFailure(t *testing.T) {
	orig := localCommandContext
	t.Cleanup(func() { localCommandContext = orig })

	localCommandContext = fakeCommandContext(
		t,
		map[string]fakeExecResponse{
			"docker run --rm --label opsorch.run-id=run-1 --label opsorch.step-id=step-1 ghcr.io/opsorch/tool:latest": {
				output:   "docker-failed",
				exitCode: 3,
			},
		},
	)

	runner := &LocalRunner{}
	out, err := runner.Run(context.Background(), "run-1", "step-1", &StepExec{
		Image: "ghcr.io/opsorch/tool:latest",
	})
	if err == nil {
		t.Fatal("expected docker failure")
	}
	if out != "docker-failed" {
		t.Fatalf("unexpected output: %q", out)
	}
}

type fakeExecResponse struct {
	output   string
	exitCode int
}

func fakeCommandContext(t *testing.T, responses map[string]fakeExecResponse) func(context.Context, string, ...string) *exec.Cmd {
	t.Helper()

	return func(ctx context.Context, name string, args ...string) *exec.Cmd {
		t.Helper()

		key := strings.Join(append([]string{name}, args...), " ")
		resp, ok := responses[key]
		if !ok {
			t.Fatalf("unexpected command: %s", key)
		}

		cmd := exec.CommandContext(
			ctx,
			os.Args[0],
			"-test.run=TestFakeExecHelperProcess",
			"--",
			resp.output,
			strconv.Itoa(resp.exitCode),
		)
		cmd.Env = append(os.Environ(), "GO_WANT_HELPER_PROCESS=1")
		return cmd
	}
}

func TestFakeExecHelperProcess(t *testing.T) {
	if os.Getenv("GO_WANT_HELPER_PROCESS") != "1" {
		return
	}

	args := os.Args
	i := 0
	for i < len(args) && args[i] != "--" {
		i++
	}
	if i >= len(args)-2 {
		os.Exit(2)
	}

	output := args[i+1]
	exitCode, err := strconv.Atoi(args[i+2])
	if err != nil {
		os.Exit(2)
	}

	_, _ = os.Stdout.WriteString(output)
	os.Exit(exitCode)
}
