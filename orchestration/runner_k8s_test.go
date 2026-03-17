package orchestration

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestK8sRunnerManifestIncludesExpectedFields(t *testing.T) {
	runner := &K8sRunner{Namespace: "ops"}

	manifest, err := runner.Manifest("Run_123", "Step.456", &StepExec{
		Image:   "ghcr.io/opsorch/tool:latest",
		Command: []string{"deploy"},
		Args:    []string{"--force"},
		Env: map[string]string{
			"REGION":  "eu-west-1",
			"VERSION": "v1",
		},
	})
	if err != nil {
		t.Fatalf("Manifest returned error: %v", err)
	}

	for _, want := range []string{
		"name: opsorch-run-123-step-456",
		"namespace: ops",
		"image: ghcr.io/opsorch/tool:latest",
		"- deploy",
		"- --force",
		"name: REGION",
		"value: eu-west-1",
		"name: VERSION",
		"value: v1",
		"opsorch/run-id: Run_123",
		"opsorch/step-id: Step.456",
	} {
		if !strings.Contains(manifest, want) {
			t.Fatalf("manifest missing %q:\n%s", want, manifest)
		}
	}
}

func TestK8sRunnerRunExecutesApplyWaitAndLogs(t *testing.T) {
	orig := k8sCommandContext
	t.Cleanup(func() { k8sCommandContext = orig })

	k8sCommandContext = fakeCommandContext(
		t,
		map[string]fakeExecResponse{
			"kubectl --kubeconfig /tmp/test-kubeconfig apply -f -": {
				output: "job.batch/opsorch-run-1-step-1 created",
			},
			"kubectl --kubeconfig /tmp/test-kubeconfig wait --for=condition=complete --timeout 45s job/opsorch-run-1-step-1 -n ops": {
				output: "job.batch/opsorch-run-1-step-1 condition met",
			},
			"kubectl --kubeconfig /tmp/test-kubeconfig logs job/opsorch-run-1-step-1 -n ops": {
				output: "task output",
			},
		},
	)

	runner := &K8sRunner{
		Namespace:      "ops",
		KubeconfigPath: "/tmp/test-kubeconfig",
		WaitTimeout:    45 * time.Second,
	}

	out, err := runner.Run(context.Background(), "run-1", "step-1", &StepExec{
		Image: "ghcr.io/opsorch/tool:latest",
	})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	for _, want := range []string{
		"job.batch/opsorch-run-1-step-1 created",
		"job.batch/opsorch-run-1-step-1 condition met",
		"task output",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("output missing %q:\n%s", want, out)
		}
	}
}

func TestK8sRunnerRunReturnsWaitFailureWithLogs(t *testing.T) {
	orig := k8sCommandContext
	t.Cleanup(func() { k8sCommandContext = orig })

	k8sCommandContext = fakeCommandContext(
		t,
		map[string]fakeExecResponse{
			"kubectl apply -f -": {
				output: "job.batch/opsorch-run-2-step-2 created",
			},
			"kubectl wait --for=condition=complete --timeout 10m0s job/opsorch-run-2-step-2 -n default": {
				output:   "timed out waiting for the condition on jobs/opsorch-run-2-step-2",
				exitCode: 1,
			},
			"kubectl logs job/opsorch-run-2-step-2 -n default": {
				output: "last log line",
			},
		},
	)

	runner := &K8sRunner{}
	out, err := runner.Run(context.Background(), "run-2", "step-2", &StepExec{
		Image: "ghcr.io/opsorch/tool:latest",
	})
	if err == nil {
		t.Fatal("expected wait failure")
	}
	if !strings.Contains(err.Error(), "kubectl wait failed") {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, want := range []string{
		"timed out waiting for the condition",
		"last log line",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("output missing %q:\n%s", want, out)
		}
	}
}
