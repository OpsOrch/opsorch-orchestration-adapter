package orchestration

import (
	"context"
	"os/exec"
	"sort"
)

var localCommandContext = exec.CommandContext

// LocalRunner runs the container image via `docker run` on the local host.
type LocalRunner struct{}

func (r *LocalRunner) Run(ctx context.Context, runID, stepID string, e *StepExec) (string, error) {
	args := []string{
		"run", "--rm",
		"--label", "opsorch.run-id=" + runID,
		"--label", "opsorch.step-id=" + stepID,
	}
	envKeys := make([]string, 0, len(e.Env))
	for k := range e.Env {
		envKeys = append(envKeys, k)
	}
	sort.Strings(envKeys)
	for _, k := range envKeys {
		v := e.Env[k]
		args = append(args, "-e", k+"="+v)
	}
	args = append(args, e.Image)
	args = append(args, e.Command...)
	args = append(args, e.Args...)

	out, err := localCommandContext(ctx, "docker", args...).CombinedOutput()
	return string(out), err
}
