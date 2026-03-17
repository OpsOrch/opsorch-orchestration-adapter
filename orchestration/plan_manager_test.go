package orchestration

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/opsorch/opsorch-core/schema"
)

func TestPlanManagerSaveAndLoad(t *testing.T) {
	tempDir := t.TempDir()
	pm := NewPlanManager(tempDir)

	plan := &schema.OrchestrationPlan{
		ID:          "test-plan",
		Title:       "Test Plan",
		Description: "A test orchestration plan",
		Version:     "1.0.0",
		Tags:        map[string]string{"type": "test"},
		Fields: map[string]any{
			"service":     "api",
			"team":        "platform",
			"environment": "dev",
		},
		Steps: []schema.OrchestrationStep{
			{
				ID:          "step1",
				Title:       "First Step",
				Type:        "manual",
				Description: "This is the first step",
				Fields:      map[string]any{"reasoning": "Check status"},
			},
			{
				ID:          "step2",
				Title:       "Second Step",
				Type:        "automated",
				Description: "Runs an automated task",
				DependsOn:   []string{"step1"},
				Fields: map[string]any{
					"exec": &StepExec{
						Image: "busybox:latest",
						Args:  []string{"echo", "ok"},
					},
				},
			},
		},
	}

	if err := pm.SavePlan(plan); err != nil {
		t.Fatalf("failed to save plan: %v", err)
	}

	filePath := filepath.Join(tempDir, "test-plan.yaml")
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		t.Fatal("plan file was not created")
	}

	loadedPlan, err := pm.LoadPlan(context.Background(), "test-plan")
	if err != nil {
		t.Fatalf("failed to load plan: %v", err)
	}
	if loadedPlan.ID != plan.ID {
		t.Fatalf("expected ID %s, got %s", plan.ID, loadedPlan.ID)
	}
	if loadedPlan.Steps[1].Type != "automated" {
		t.Fatalf("expected automated step, got %s", loadedPlan.Steps[1].Type)
	}
	if exec := ExtractStepExec(loadedPlan.Steps[1].Fields); exec == nil || exec.Image != "busybox:latest" {
		t.Fatal("expected automated exec to round-trip")
	}
}

func TestPlanManagerUsesFilenameAsFallbackID(t *testing.T) {
	tempDir := t.TempDir()
	pm := NewPlanManager(tempDir)

	yaml := `title: Example
service: api
team: platform
environment: dev
tags:
  type: example
steps:
  - id: one
    title: One
    type: manual
`
	if err := os.WriteFile(filepath.Join(tempDir, "example.yaml"), []byte(yaml), 0644); err != nil {
		t.Fatalf("failed to write plan file: %v", err)
	}

	plan, err := pm.LoadPlan(context.Background(), "example")
	if err != nil {
		t.Fatalf("failed to load plan: %v", err)
	}
	if plan.ID != "example" {
		t.Fatalf("expected fallback id example, got %s", plan.ID)
	}
}

func TestPlanManagerQueryPlans(t *testing.T) {
	tempDir := t.TempDir()
	pm := NewPlanManager(tempDir)

	plans := []*schema.OrchestrationPlan{
		{
			ID:     "deploy-plan",
			Title:  "Deployment Plan",
			Tags:   map[string]string{"type": "deployment"},
			Fields: map[string]any{"service": "api", "team": "platform", "environment": "prod"},
			Steps:  []schema.OrchestrationStep{{ID: "step1", Title: "Deploy", Type: "manual"}},
		},
		{
			ID:     "incident-plan",
			Title:  "Incident Response",
			Tags:   map[string]string{"type": "incident"},
			Fields: map[string]any{"service": "web", "team": "sre", "environment": "prod"},
			Steps:  []schema.OrchestrationStep{{ID: "step1", Title: "Respond", Type: "manual"}},
		},
	}
	for _, plan := range plans {
		if err := pm.SavePlan(plan); err != nil {
			t.Fatalf("failed to save plan %s: %v", plan.ID, err)
		}
	}

	allPlans, err := pm.QueryPlans(context.Background(), schema.OrchestrationPlanQuery{})
	if err != nil {
		t.Fatalf("failed to query plans: %v", err)
	}
	if len(allPlans) != 2 {
		t.Fatalf("expected 2 plans, got %d", len(allPlans))
	}

	deployPlans, err := pm.QueryPlans(context.Background(), schema.OrchestrationPlanQuery{
		Tags: map[string]string{"type": "deployment"},
	})
	if err != nil {
		t.Fatalf("failed to query plans by tag: %v", err)
	}
	if len(deployPlans) != 1 || deployPlans[0].ID != "deploy-plan" {
		t.Fatalf("unexpected deployment query result: %+v", deployPlans)
	}
}

func TestPlanManagerValidatePlan(t *testing.T) {
	pm := NewPlanManager(t.TempDir())

	validPlan := &schema.OrchestrationPlan{
		ID:     "valid-plan",
		Title:  "Valid",
		Fields: map[string]any{"service": "api", "team": "platform", "environment": "prod"},
		Steps: []schema.OrchestrationStep{
			{ID: "step1", Title: "One", Type: "manual"},
			{ID: "step2", Title: "Two", Type: "automated", DependsOn: []string{"step1"}, Fields: map[string]any{
				"exec": &StepExec{Image: "busybox:latest"},
			}},
		},
	}
	if err := pm.ValidatePlan(validPlan); err != nil {
		t.Fatalf("expected valid plan: %v", err)
	}

	invalid := &schema.OrchestrationPlan{
		ID:     "invalid",
		Title:  "Invalid",
		Fields: map[string]any{"service": "api", "team": "platform", "environment": "prod"},
		Steps:  []schema.OrchestrationStep{{ID: "step1", Title: "One", Type: "automated"}},
	}
	if err := pm.ValidatePlan(invalid); err == nil {
		t.Fatal("expected automated step validation error")
	}
}

func TestPlanManagerRequiresScopeTags(t *testing.T) {
	pm := NewPlanManager(t.TempDir())
	plan := &schema.OrchestrationPlan{
		ID:    "missing-scope",
		Title: "Missing Scope",
		Tags:  map[string]string{"type": "deployment"},
		Steps: []schema.OrchestrationStep{{ID: "step1", Title: "One", Type: "manual"}},
	}
	if err := pm.ValidatePlan(plan); err == nil {
		t.Fatal("expected missing scope tag validation error")
	}
}

func TestExamplePlansLoad(t *testing.T) {
	pm := NewPlanManager(filepath.Join("..", "examples"))
	files, err := filepath.Glob(filepath.Join("..", "examples", "*.yaml"))
	if err != nil {
		t.Fatalf("failed to list example plans: %v", err)
	}
	if len(files) == 0 {
		t.Fatal("expected example plans")
	}

	for _, file := range files {
		planID := strings.TrimSuffix(filepath.Base(file), filepath.Ext(file))
		t.Run(planID, func(t *testing.T) {
			plan, err := pm.LoadPlan(context.Background(), planID)
			if err != nil {
				t.Fatalf("failed to load example plan: %v", err)
			}
			if plan.ID != planID {
				t.Fatalf("expected plan id %s, got %s", planID, plan.ID)
			}
		})
	}
}

func TestMatchesWordSearch(t *testing.T) {
	tests := []struct {
		name     string
		query    string
		fields   []string
		expected bool
	}{
		{name: "empty query", query: "", fields: []string{"deployment checklist"}, expected: true},
		{name: "single word match", query: "deployment", fields: []string{"deployment checklist"}, expected: true},
		{name: "case insensitive", query: "DEPLOYMENT", fields: []string{"deployment checklist"}, expected: true},
		{name: "partial", query: "deploy", fields: []string{"deployment checklist"}, expected: true},
		{name: "no match", query: "database", fields: []string{"deployment checklist"}, expected: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := matchesWordSearch(tt.query, tt.fields); got != tt.expected {
				t.Fatalf("expected %v, got %v", tt.expected, got)
			}
		})
	}
}
