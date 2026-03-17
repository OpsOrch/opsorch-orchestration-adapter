package orchestration

import (
	"context"
	"encoding/json"
)

// StepExec describes the container workload for an automated step.
// It is runner-agnostic — the step has no knowledge of where it runs.
type StepExec struct {
	Image   string            `yaml:"image"             json:"image"`
	Command []string          `yaml:"command,omitempty" json:"command,omitempty"`
	Args    []string          `yaml:"args,omitempty"    json:"args,omitempty"`
	Env     map[string]string `yaml:"env,omitempty"     json:"env,omitempty"`
}

// Runner executes a StepExec and returns combined output.
type Runner interface {
	Run(ctx context.Context, runID, stepID string, e *StepExec) (output string, err error)
}

// ExtractStepExec pulls StepExec from a step's Fields map.
func ExtractStepExec(fields map[string]any) *StepExec {
	if fields == nil {
		return nil
	}
	raw, ok := fields["exec"]
	if !ok {
		return nil
	}
	if se, ok := raw.(*StepExec); ok {
		return se
	}
	// Re-hydrate from JSON round-trip (e.g. loaded from DB)
	data, err := json.Marshal(raw)
	if err != nil {
		return nil
	}
	var se StepExec
	if err := json.Unmarshal(data, &se); err != nil {
		return nil
	}
	return &se
}
