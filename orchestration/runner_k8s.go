package orchestration

import (
	"context"
	"fmt"
	"os/exec"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

var k8sCommandContext = exec.CommandContext

// K8sRunner wraps the container in a Kubernetes Job and waits for completion.
type K8sRunner struct {
	// Namespace to create Jobs in. Defaults to "default".
	Namespace string
	// KubeconfigPath is optional; empty means kubectl uses its default.
	KubeconfigPath string
	// WaitTimeout controls how long kubectl waits for job completion.
	WaitTimeout time.Duration
}

func (r *K8sRunner) Run(ctx context.Context, runID, stepID string, e *StepExec) (string, error) {
	manifest, err := r.Manifest(runID, stepID, e)
	if err != nil {
		return "", err
	}

	jobName := k8sName(fmt.Sprintf("opsorch-%s-%s", runID, stepID))
	ns := r.Namespace
	if ns == "" {
		ns = "default"
	}

	args := []string{"apply", "-f", "-"}
	if r.KubeconfigPath != "" {
		args = append([]string{"--kubeconfig", r.KubeconfigPath}, args...)
	}
	cmd := k8sCommandContext(ctx, "kubectl", args...)
	cmd.Stdin = strings.NewReader(manifest)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return manifest + "\n---\n" + string(out), fmt.Errorf("kubectl apply failed: %w", err)
	}

	waitTimeout := r.WaitTimeout
	if waitTimeout <= 0 {
		waitTimeout = 10 * time.Minute
	}
	waitArgs := []string{"wait", "--for=condition=complete", "--timeout", waitTimeout.String(), "job/" + jobName, "-n", ns}
	if r.KubeconfigPath != "" {
		waitArgs = append([]string{"--kubeconfig", r.KubeconfigPath}, waitArgs...)
	}
	waitOut, waitErr := k8sCommandContext(ctx, "kubectl", waitArgs...).CombinedOutput()

	logArgs := []string{"logs", "job/" + jobName, "-n", ns}
	if r.KubeconfigPath != "" {
		logArgs = append([]string{"--kubeconfig", r.KubeconfigPath}, logArgs...)
	}
	logOut, _ := k8sCommandContext(ctx, "kubectl", logArgs...).CombinedOutput()

	output := manifest + "\n---\n" + string(out) + "\n---\n" + string(waitOut) + "\n---\n" + string(logOut)
	if waitErr != nil {
		return output, fmt.Errorf("kubectl wait failed: %w", waitErr)
	}
	return output, nil
}

// Manifest generates the K8s Job YAML without applying it.
// Useful for dry-runs, auditing, or when kubectl isn't available.
func (r *K8sRunner) Manifest(runID, stepID string, e *StepExec) (string, error) {
	ns := r.Namespace
	if ns == "" {
		ns = "default"
	}

	labels := map[string]string{
		"app.kubernetes.io/managed-by": "opsorch",
		"opsorch/run-id":               runID,
		"opsorch/step-id":              stepID,
	}

	var envVars []map[string]string
	envKeys := make([]string, 0, len(e.Env))
	for k := range e.Env {
		envKeys = append(envKeys, k)
	}
	sort.Strings(envKeys)
	for _, k := range envKeys {
		v := e.Env[k]
		envVars = append(envVars, map[string]string{"name": k, "value": v})
	}

	container := map[string]any{"name": "step", "image": e.Image}
	if len(e.Command) > 0 {
		container["command"] = e.Command
	}
	if len(e.Args) > 0 {
		container["args"] = e.Args
	}
	if len(envVars) > 0 {
		container["env"] = envVars
	}

	job := map[string]any{
		"apiVersion": "batch/v1",
		"kind":       "Job",
		"metadata": map[string]any{
			"name":      jobName(runID, stepID),
			"namespace": ns,
			"labels":    labels,
			"annotations": map[string]string{
				"opsorch/run-id":  runID,
				"opsorch/step-id": stepID,
			},
		},
		"spec": map[string]any{
			"backoffLimit":            0,
			"ttlSecondsAfterFinished": 300,
			"template": map[string]any{
				"metadata": map[string]any{"labels": labels},
				"spec": map[string]any{
					"restartPolicy": "Never",
					"containers":    []any{container},
				},
			},
		},
	}

	out, err := yaml.Marshal(job)
	if err != nil {
		return "", fmt.Errorf("failed to marshal K8s Job manifest: %w", err)
	}
	return string(out), nil
}

func jobName(runID, stepID string) string {
	return k8sName(fmt.Sprintf("opsorch-%s-%s", runID, stepID))
}

// k8sName converts a string to a valid K8s resource name (lowercase, max 63 chars).
func k8sName(s string) string {
	s = strings.ToLower(s)
	var b strings.Builder
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' {
			b.WriteRune(r)
		} else {
			b.WriteRune('-')
		}
	}
	result := strings.Trim(b.String(), "-")
	for strings.Contains(result, "--") {
		result = strings.ReplaceAll(result, "--", "-")
	}
	if len(result) > 63 {
		result = result[:63]
	}
	return result
}
