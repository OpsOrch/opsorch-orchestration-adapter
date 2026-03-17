package orchestration

import (
	"path/filepath"
	"testing"

	"github.com/opsorch/opsorch-core/schema"
)

func TestRunTrackerCreateAndLoad(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "test.db")
	rt, err := NewRunTracker(dbPath)
	if err != nil {
		t.Fatalf("failed to create run tracker: %v", err)
	}
	defer rt.Close()

	// Create a test plan
	plan := &schema.OrchestrationPlan{
		ID:      "test-plan",
		Title:   "Test Plan",
		Version: "1.0.0",
		Fields:  map[string]any{"service": "api", "team": "platform", "environment": "dev"},
		Steps: []schema.OrchestrationStep{
			{ID: "step1", Title: "First Step"},
			{ID: "step2", Title: "Second Step"},
		},
	}

	// Create a run
	run, err := rt.CreateRun(plan, "test-user")
	if err != nil {
		t.Fatalf("failed to create run: %v", err)
	}

	// Verify run properties
	if run.PlanID != plan.ID {
		t.Errorf("expected plan ID %s, got %s", plan.ID, run.PlanID)
	}
	if run.Status != "created" {
		t.Errorf("expected status 'created', got %s", run.Status)
	}
	if len(run.Steps) != 2 {
		t.Errorf("expected 2 steps, got %d", len(run.Steps))
	}

	// Verify all steps start with correct initial status
	for _, step := range run.Steps {
		// Steps with no dependencies should be ready, others pending
		expectedStatus := "ready" // Both steps have no dependencies in this test
		if step.Status != expectedStatus {
			t.Errorf("expected step %s to be %s, got %s", step.StepID, expectedStatus, step.Status)
		}
	}

	// Load the run back
	loadedRun, err := rt.LoadRun(run.ID)
	if err != nil {
		t.Fatalf("failed to load run: %v", err)
	}

	if loadedRun.ID != run.ID {
		t.Errorf("expected run ID %s, got %s", run.ID, loadedRun.ID)
	}
}

func TestRunTrackerCompleteStep(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "test.db")
	rt, err := NewRunTracker(dbPath)
	if err != nil {
		t.Fatalf("failed to create run tracker: %v", err)
	}
	defer rt.Close()

	// Create a test plan
	plan := &schema.OrchestrationPlan{
		ID:     "test-plan",
		Fields: map[string]any{"service": "api", "team": "platform", "environment": "dev"},
		Steps: []schema.OrchestrationStep{
			{ID: "step1", Title: "First Step"},
			{ID: "step2", Title: "Second Step"},
		},
	}

	// Create a run
	run, err := rt.CreateRun(plan, "test-user")
	if err != nil {
		t.Fatalf("failed to create run: %v", err)
	}

	// Complete first step
	err = rt.CompleteStep(run.ID, "step1", "test-actor", "Step completed successfully")
	if err != nil {
		t.Fatalf("failed to complete step: %v", err)
	}

	// Load run and verify step completion
	updatedRun, err := rt.LoadRun(run.ID)
	if err != nil {
		t.Fatalf("failed to load updated run: %v", err)
	}

	// Find step1 and verify it's completed
	var step1 *schema.OrchestrationStepState
	for i := range updatedRun.Steps {
		if updatedRun.Steps[i].StepID == "step1" {
			step1 = &updatedRun.Steps[i]
			break
		}
	}

	if step1 == nil {
		t.Fatal("step1 not found")
	}
	if step1.Status != "succeeded" {
		t.Errorf("expected step1 status 'succeeded', got %s", step1.Status)
	}
	if step1.Actor != "test-actor" {
		t.Errorf("expected actor 'test-actor', got %s", step1.Actor)
	}
	if step1.Note != "Step completed successfully" {
		t.Errorf("expected note 'Step completed successfully', got %s", step1.Note)
	}

	// Run should now be in running status
	if updatedRun.Status != "running" {
		t.Errorf("expected run status 'running', got %s", updatedRun.Status)
	}

	// Complete second step
	err = rt.CompleteStep(run.ID, "step2", "test-actor", "All done")
	if err != nil {
		t.Fatalf("failed to complete step2: %v", err)
	}

	// Load run and verify it's completed
	finalRun, err := rt.LoadRun(run.ID)
	if err != nil {
		t.Fatalf("failed to load final run: %v", err)
	}

	if finalRun.Status != "completed" {
		t.Errorf("expected run status 'completed', got %s", finalRun.Status)
	}
}

func TestRunTrackerQueryRuns(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "test.db")
	rt, err := NewRunTracker(dbPath)
	if err != nil {
		t.Fatalf("failed to create run tracker: %v", err)
	}
	defer rt.Close()

	// Create test plans and runs
	plan1 := &schema.OrchestrationPlan{
		ID:     "deploy-plan",
		Fields: map[string]any{"service": "api", "team": "platform", "environment": "prod"},
		Steps:  []schema.OrchestrationStep{{ID: "step1", Title: "Deploy"}},
	}
	plan2 := &schema.OrchestrationPlan{
		ID:     "test-plan",
		Fields: map[string]any{"service": "web", "team": "sre", "environment": "staging"},
		Steps:  []schema.OrchestrationStep{{ID: "step1", Title: "Test"}},
	}

	run1, err := rt.CreateRun(plan1, "user1")
	if err != nil {
		t.Fatalf("failed to create run1: %v", err)
	}

	_, err = rt.CreateRun(plan2, "user2")
	if err != nil {
		t.Fatalf("failed to create run2: %v", err)
	}

	// Complete run1 to change its status
	rt.CompleteStep(run1.ID, "step1", "user1", "done")

	// Query all runs
	allRuns, err := rt.QueryRuns(schema.OrchestrationRunQuery{})
	if err != nil {
		t.Fatalf("failed to query all runs: %v", err)
	}
	if len(allRuns) != 2 {
		t.Errorf("expected 2 runs, got %d", len(allRuns))
	}

	// Query by status
	createdRuns, err := rt.QueryRuns(schema.OrchestrationRunQuery{
		Statuses: []string{"created"},
	})
	if err != nil {
		t.Fatalf("failed to query created runs: %v", err)
	}
	if len(createdRuns) != 1 {
		t.Errorf("expected 1 created run, got %d", len(createdRuns))
	}

	// Query by plan ID
	deployRuns, err := rt.QueryRuns(schema.OrchestrationRunQuery{
		PlanIDs: []string{"deploy-plan"},
	})
	if err != nil {
		t.Fatalf("failed to query deploy runs: %v", err)
	}
	if len(deployRuns) != 1 {
		t.Errorf("expected 1 deploy run, got %d", len(deployRuns))
	}

	// Query with limit
	limitedRuns, err := rt.QueryRuns(schema.OrchestrationRunQuery{
		Limit: 1,
	})
	if err != nil {
		t.Fatalf("failed to query with limit: %v", err)
	}
	if len(limitedRuns) != 1 {
		t.Errorf("expected 1 run with limit, got %d", len(limitedRuns))
	}
}

func TestRunTrackerCompleteStepValidation(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "test.db")
	rt, err := NewRunTracker(dbPath)
	if err != nil {
		t.Fatalf("failed to create run tracker: %v", err)
	}
	defer rt.Close()

	// Create a test plan
	plan := &schema.OrchestrationPlan{
		ID:     "test-plan",
		Fields: map[string]any{"service": "api", "team": "platform", "environment": "dev"},
		Steps:  []schema.OrchestrationStep{{ID: "step1", Title: "Test Step"}},
	}

	run, err := rt.CreateRun(plan, "test-user")
	if err != nil {
		t.Fatalf("failed to create run: %v", err)
	}

	// Try to complete non-existent step
	err = rt.CompleteStep(run.ID, "nonexistent", "actor", "note")
	if err == nil {
		t.Error("expected error for non-existent step")
	}

	// Complete the step
	err = rt.CompleteStep(run.ID, "step1", "actor", "note")
	if err != nil {
		t.Fatalf("failed to complete step: %v", err)
	}

	// Try to complete already completed step
	err = rt.CompleteStep(run.ID, "step1", "actor", "note")
	if err == nil {
		t.Error("expected error for already completed step")
	}
}

func TestRunTrackerDatabasePersistence(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "test.db")

	// Create first tracker and add data
	rt1, err := NewRunTracker(dbPath)
	if err != nil {
		t.Fatalf("failed to create first run tracker: %v", err)
	}

	plan := &schema.OrchestrationPlan{
		ID:     "persist-test",
		Fields: map[string]any{"service": "api", "team": "platform", "environment": "dev"},
		Steps:  []schema.OrchestrationStep{{ID: "step1", Title: "Test"}},
	}

	run, err := rt1.CreateRun(plan, "test-user")
	if err != nil {
		t.Fatalf("failed to create run: %v", err)
	}

	rt1.Close()

	// Create second tracker and verify data persists
	rt2, err := NewRunTracker(dbPath)
	if err != nil {
		t.Fatalf("failed to create second run tracker: %v", err)
	}
	defer rt2.Close()

	loadedRun, err := rt2.LoadRun(run.ID)
	if err != nil {
		t.Fatalf("failed to load run from persisted database: %v", err)
	}

	if loadedRun.PlanID != plan.ID {
		t.Errorf("expected plan ID %s, got %s", plan.ID, loadedRun.PlanID)
	}
}
func TestRunTrackerStepDependencies(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "test.db")
	rt, err := NewRunTracker(dbPath)
	if err != nil {
		t.Fatalf("failed to create run tracker: %v", err)
	}
	defer rt.Close()

	// Create a plan with dependencies: step1 -> step2 -> step3
	plan := &schema.OrchestrationPlan{
		ID:     "dependency-test",
		Fields: map[string]any{"service": "api", "team": "platform", "environment": "dev"},
		Steps: []schema.OrchestrationStep{
			{ID: "step1", Title: "First Step", DependsOn: []string{}},
			{ID: "step2", Title: "Second Step", DependsOn: []string{"step1"}},
			{ID: "step3", Title: "Third Step", DependsOn: []string{"step2"}},
		},
	}

	// Create a run
	run, err := rt.CreateRun(plan, "test-user")
	if err != nil {
		t.Fatalf("failed to create run: %v", err)
	}

	// Verify initial states: step1 should be ready, others pending
	loadedRun, err := rt.LoadRun(run.ID)
	if err != nil {
		t.Fatalf("failed to load run: %v", err)
	}

	stepStates := make(map[string]string)
	for _, step := range loadedRun.Steps {
		stepStates[step.StepID] = step.Status
	}

	if stepStates["step1"] != "ready" {
		t.Errorf("expected step1 to be ready, got %s", stepStates["step1"])
	}
	if stepStates["step2"] != "pending" {
		t.Errorf("expected step2 to be pending, got %s", stepStates["step2"])
	}
	if stepStates["step3"] != "pending" {
		t.Errorf("expected step3 to be pending, got %s", stepStates["step3"])
	}

	// Try to complete step2 before step1 - should fail
	err = rt.CompleteStep(run.ID, "step2", "actor", "note")
	if err == nil {
		t.Error("expected error when completing step2 before step1")
	}

	// Complete step1 - should succeed and make step2 ready
	err = rt.CompleteStep(run.ID, "step1", "actor", "completed step1")
	if err != nil {
		t.Fatalf("failed to complete step1: %v", err)
	}

	// Check that step2 is now ready
	updatedRun, err := rt.LoadRun(run.ID)
	if err != nil {
		t.Fatalf("failed to load updated run: %v", err)
	}

	stepStates = make(map[string]string)
	for _, step := range updatedRun.Steps {
		stepStates[step.StepID] = step.Status
	}

	if stepStates["step1"] != "succeeded" {
		t.Errorf("expected step1 to be succeeded, got %s", stepStates["step1"])
	}
	if stepStates["step2"] != "ready" {
		t.Errorf("expected step2 to be ready after step1 completion, got %s", stepStates["step2"])
	}
	if stepStates["step3"] != "pending" {
		t.Errorf("expected step3 to still be pending, got %s", stepStates["step3"])
	}

	// Complete step2 - should succeed and make step3 ready
	err = rt.CompleteStep(run.ID, "step2", "actor", "completed step2")
	if err != nil {
		t.Fatalf("failed to complete step2: %v", err)
	}

	// Check final states
	finalRun, err := rt.LoadRun(run.ID)
	if err != nil {
		t.Fatalf("failed to load final run: %v", err)
	}

	stepStates = make(map[string]string)
	for _, step := range finalRun.Steps {
		stepStates[step.StepID] = step.Status
	}

	if stepStates["step3"] != "ready" {
		t.Errorf("expected step3 to be ready after step2 completion, got %s", stepStates["step3"])
	}
}
