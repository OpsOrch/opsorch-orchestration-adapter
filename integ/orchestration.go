package main

import (
	"context"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/opsorch/opsorch-core/schema"
	adapter "github.com/opsorch/opsorch-orchestration-adapter/orchestration"
)

func main() {
	log.Println("Starting orchestration integration run")

	if _, err := exec.LookPath("docker"); err != nil {
		log.Fatalf("docker is required for this integration run: %v", err)
	}
	if out, err := exec.Command("docker", "info").CombinedOutput(); err != nil {
		log.Fatalf("docker daemon is not available: %v: %s", err, string(out))
	}

	tempDir, err := os.MkdirTemp("", "opsorch-orchestration-integ-*")
	if err != nil {
		log.Fatalf("failed to create temp directory: %v", err)
	}
	defer os.RemoveAll(tempDir)

	plansDir := filepath.Join(tempDir, "plans")
	if err := os.MkdirAll(plansDir, 0755); err != nil {
		log.Fatalf("failed to create plans directory: %v", err)
	}

	config := map[string]any{
		"storagePath": tempDir,
		"sourceType":  "local",
		"localPath":   plansDir,
		"runner":      "local",
		"stateDB":     "integ.db",
	}

	provider, err := adapter.New(config)
	if err != nil {
		log.Fatalf("failed to create provider: %v", err)
	}

	testPlan := `id: docker-smoke
service: api
team: platform
environment: production
title: Docker Smoke Runbook
description: Manual approval, branching manual checks, and docker-based automated steps
version: "1.0"
tags:
  type: integration
  runner: local
steps:
  - id: approve
    title: Approve Execution
    type: manual
    reasoning: Confirm the integration workflow should continue.

  - id: run-docker
    title: Run Docker Smoke Step
    type: automated
    dependsOn: [approve]
    exec:
      image: busybox:latest
      args: ["echo", "opsorch-integration-ok"]

  - id: manual-branch-a
    title: Verify Branch A
    type: manual
    dependsOn: [approve]
    reasoning: Confirm branch A can proceed.

  - id: manual-branch-b
    title: Verify Branch B
    type: manual
    dependsOn: [approve]
    reasoning: Confirm branch B can proceed.

  - id: post-branch-check
    title: Run Post-Branch Check
    type: automated
    dependsOn: [manual-branch-a, manual-branch-b]
    exec:
      image: busybox:latest
      args: ["echo", "opsorch-branch-ok"]

  - id: finalize
    title: Finalize Runbook
    type: manual
    dependsOn: [run-docker, post-branch-check]
    reasoning: Confirm both automated branches completed successfully.
`

	planPath := filepath.Join(plansDir, "docker-smoke.yaml")
	if err := os.WriteFile(planPath, []byte(testPlan), 0644); err != nil {
		log.Fatalf("failed to write test plan: %v", err)
	}

	ctx := context.Background()

	plans, err := provider.QueryPlans(ctx, schema.OrchestrationPlanQuery{})
	if err != nil {
		log.Fatalf("QueryPlans failed: %v", err)
	}
	if len(plans) != 1 {
		log.Fatalf("expected 1 plan, got %d", len(plans))
	}

	scopedPlans, err := provider.QueryPlans(ctx, schema.OrchestrationPlanQuery{
		Scope: schema.QueryScope{
			Service:     "api",
			Team:        "platform",
			Environment: "production",
		},
	})
	if err != nil {
		log.Fatalf("QueryPlans with scope failed: %v", err)
	}
	if len(scopedPlans) != 1 || scopedPlans[0].ID != "docker-smoke" {
		log.Fatalf("expected scoped plan query to find docker-smoke, got %+v", scopedPlans)
	}

	nonMatchingPlans, err := provider.QueryPlans(ctx, schema.OrchestrationPlanQuery{
		Scope: schema.QueryScope{
			Service: "payments",
		},
	})
	if err != nil {
		log.Fatalf("QueryPlans with non-matching scope failed: %v", err)
	}
	if len(nonMatchingPlans) != 0 {
		log.Fatalf("expected no plans for non-matching scope, got %+v", nonMatchingPlans)
	}

	plan, err := provider.GetPlan(ctx, "docker-smoke")
	if err != nil {
		log.Fatalf("GetPlan failed: %v", err)
	}
	if plan.ID != "docker-smoke" {
		log.Fatalf("expected plan ID docker-smoke, got %s", plan.ID)
	}

	run, err := provider.StartRun(ctx, "docker-smoke")
	if err != nil {
		log.Fatalf("StartRun failed: %v", err)
	}
	log.Printf("Started run %s", run.ID)

	scopedRuns, err := provider.QueryRuns(ctx, schema.OrchestrationRunQuery{
		Scope: schema.QueryScope{
			Service:     "api",
			Team:        "platform",
			Environment: "production",
		},
	})
	if err != nil {
		log.Fatalf("QueryRuns with scope failed: %v", err)
	}
	if len(scopedRuns) != 1 || scopedRuns[0].ID != run.ID {
		log.Fatalf("expected scoped run query to find %s, got %+v", run.ID, scopedRuns)
	}

	nonMatchingRuns, err := provider.QueryRuns(ctx, schema.OrchestrationRunQuery{
		Scope: schema.QueryScope{
			Team: "payments",
		},
	})
	if err != nil {
		log.Fatalf("QueryRuns with non-matching scope failed: %v", err)
	}
	if len(nonMatchingRuns) != 0 {
		log.Fatalf("expected no runs for non-matching scope, got %+v", nonMatchingRuns)
	}

	initialRun, err := provider.GetRun(ctx, run.ID)
	if err != nil {
		log.Fatalf("GetRun failed: %v", err)
	}
	assertStepStatus(initialRun, "approve", "ready")
	assertStepStatus(initialRun, "run-docker", "pending")
	assertStepStatus(initialRun, "manual-branch-a", "pending")
	assertStepStatus(initialRun, "manual-branch-b", "pending")
	assertStepStatus(initialRun, "post-branch-check", "pending")
	assertStepStatus(initialRun, "finalize", "pending")

	if err := provider.CompleteStep(ctx, run.ID, "approve", "integ-user", "approved"); err != nil {
		log.Fatalf("CompleteStep failed: %v", err)
	}

	afterApprove, err := waitForRun(ctx, provider, run.ID, func(run *schema.OrchestrationRun) bool {
		return stepStatus(run, "run-docker") == "succeeded"
	})
	if err != nil {
		log.Fatalf("waiting for first automated step failed: %v", err)
	}
	autoStep := assertStepStatus(afterApprove, "run-docker", "succeeded")
	output, _ := autoStep.Fields["output"].(string)
	if output == "" {
		log.Fatalf("expected automated step output, got %+v", autoStep.Fields)
	}
	if !strings.Contains(output, "opsorch-integration-ok") {
		log.Fatalf("expected automated output to contain marker, got %q", output)
	}
	assertStepStatus(afterApprove, "manual-branch-a", "ready")
	assertStepStatus(afterApprove, "manual-branch-b", "ready")
	assertStepStatus(afterApprove, "post-branch-check", "pending")
	assertStepStatus(afterApprove, "finalize", "pending")

	if err := provider.CompleteStep(ctx, run.ID, "manual-branch-a", "integ-user", "branch a approved"); err != nil {
		log.Fatalf("failed to complete branch A: %v", err)
	}
	afterBranchA, err := provider.GetRun(ctx, run.ID)
	if err != nil {
		log.Fatalf("GetRun after branch A failed: %v", err)
	}
	assertStepStatus(afterBranchA, "manual-branch-a", "succeeded")
	assertStepStatus(afterBranchA, "manual-branch-b", "ready")
	assertStepStatus(afterBranchA, "post-branch-check", "pending")

	if err := provider.CompleteStep(ctx, run.ID, "manual-branch-b", "integ-user", "branch b approved"); err != nil {
		log.Fatalf("failed to complete branch B: %v", err)
	}
	afterBranchB, err := waitForRun(ctx, provider, run.ID, func(run *schema.OrchestrationRun) bool {
		return stepStatus(run, "post-branch-check") == "succeeded"
	})
	if err != nil {
		log.Fatalf("waiting for branch automated step failed: %v", err)
	}
	branchStep := assertStepStatus(afterBranchB, "post-branch-check", "succeeded")
	branchOutput, _ := branchStep.Fields["output"].(string)
	if !strings.Contains(branchOutput, "opsorch-branch-ok") {
		log.Fatalf("expected branch automated output to contain marker, got %q", branchOutput)
	}
	assertStepStatus(afterBranchB, "finalize", "ready")

	if err := provider.CompleteStep(ctx, run.ID, "finalize", "integ-user", "finalized"); err != nil {
		log.Fatalf("failed to complete finalize step: %v", err)
	}
	finalRun, err := provider.GetRun(ctx, run.ID)
	if err != nil {
		log.Fatalf("GetRun after finalize failed: %v", err)
	}
	if finalRun.Status != "completed" {
		log.Fatalf("expected run to be completed, got %s", finalRun.Status)
	}
	assertStepStatus(finalRun, "finalize", "succeeded")

	log.Println("Integration run completed successfully")
}

func assertStepStatus(run *schema.OrchestrationRun, stepID, expected string) schema.OrchestrationStepState {
	for _, step := range run.Steps {
		if step.StepID == stepID {
			if step.Status != expected {
				log.Fatalf("expected step %s status %s, got %s", stepID, expected, step.Status)
			}
			return step
		}
	}
	log.Fatalf("step %s not found in run %s", stepID, run.ID)
	return schema.OrchestrationStepState{}
}

func stepStatus(run *schema.OrchestrationRun, stepID string) string {
	for _, step := range run.Steps {
		if step.StepID == stepID {
			return step.Status
		}
	}
	return ""
}

func waitForRun(ctx context.Context, provider schemaCompatibleProvider, runID string, predicate func(*schema.OrchestrationRun) bool) (*schema.OrchestrationRun, error) {
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		run, err := provider.GetRun(ctx, runID)
		if err != nil {
			return nil, err
		}
		if predicate(run) {
			return run, nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	return nil, context.DeadlineExceeded
}

type schemaCompatibleProvider interface {
	GetRun(ctx context.Context, runID string) (*schema.OrchestrationRun, error)
}
