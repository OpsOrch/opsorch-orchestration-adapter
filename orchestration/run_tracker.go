package orchestration

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/opsorch/opsorch-core/schema"
	_ "modernc.org/sqlite"
)

// RunTracker manages orchestration runs and step state in SQLite.
type RunTracker struct {
	db *sql.DB
}

// NewRunTracker creates a new run tracker with SQLite backend.
func NewRunTracker(dbPath string) (*RunTracker, error) {
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open SQLite database: %w", err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)

	rt := &RunTracker{db: db}
	if err := rt.initializeDatabase(); err != nil {
		return nil, fmt.Errorf("failed to initialize database: %w", err)
	}
	return rt, nil
}

// CreateRun creates a new orchestration run from a plan.
func (rt *RunTracker) CreateRun(plan *schema.OrchestrationPlan, createdBy string) (*schema.OrchestrationRun, error) {
	now := time.Now()
	runID := "run-" + uuid.New().String()
	scope := planScope(plan)

	tx, err := rt.db.Begin()
	if err != nil {
		return nil, fmt.Errorf("failed to start transaction: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	_, err = tx.Exec(`
		INSERT INTO orchestration_runs (id, plan_id, plan_title, plan_description, plan_version, status, created_at, updated_at, created_by, service, team, environment)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, runID, plan.ID, plan.Title, plan.Description, plan.Version, "created", now, now, createdBy, nullableString(scope.Service), nullableString(scope.Team), nullableString(scope.Environment))
	if err != nil {
		return nil, fmt.Errorf("failed to insert run: %w", err)
	}

	for _, step := range plan.Steps {
		initialStatus := "pending"
		if len(step.DependsOn) == 0 {
			initialStatus = "ready"
		}
		fieldsJSON, err := marshalJSON(step.Fields)
		if err != nil {
			return nil, fmt.Errorf("failed to encode step fields: %w", err)
		}
		metadataJSON, err := marshalJSON(step.Metadata)
		if err != nil {
			return nil, fmt.Errorf("failed to encode step metadata: %w", err)
		}
		_, err = tx.Exec(`
			INSERT INTO step_states (run_id, step_id, status, actor, note, started_at, finished_at, updated_at, step_type, fields_json, metadata_json)
			VALUES (?, ?, ?, '', '', NULL, NULL, ?, ?, ?, ?)
		`, runID, step.ID, initialStatus, now, step.Type, fieldsJSON, metadataJSON)
		if err != nil {
			return nil, fmt.Errorf("failed to insert step state: %w", err)
		}
		for _, depStepID := range step.DependsOn {
			_, err = tx.Exec(`
				INSERT INTO step_dependencies (run_id, step_id, depends_on_step_id)
				VALUES (?, ?, ?)
			`, runID, step.ID, depStepID)
			if err != nil {
				return nil, fmt.Errorf("failed to insert step dependency: %w", err)
			}
		}
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("failed to commit transaction: %w", err)
	}
	committed = true
	return rt.LoadRun(runID)
}

// LoadRun loads an orchestration run by ID.
func (rt *RunTracker) LoadRun(runID string) (*schema.OrchestrationRun, error) {
	var run schema.OrchestrationRun
	var createdBy string
	var service, team, environment sql.NullString
	err := rt.db.QueryRow(`
		SELECT id, plan_id, status, created_at, updated_at, created_by, service, team, environment
		FROM orchestration_runs WHERE id = ?
	`, runID).Scan(&run.ID, &run.PlanID, &run.Status, &run.CreatedAt, &run.UpdatedAt, &createdBy, &service, &team, &environment)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("run %s not found", runID)
		}
		return nil, fmt.Errorf("failed to load run: %w", err)
	}
	if service.Valid {
		run.Scope.Service = service.String
	}
	if team.Valid {
		run.Scope.Team = team.String
	}
	if environment.Valid {
		run.Scope.Environment = environment.String
	}

	rows, err := rt.db.Query(`
		SELECT step_id, status, actor, note, started_at, finished_at, updated_at, fields_json, metadata_json
		FROM step_states WHERE run_id = ? ORDER BY step_id
	`, runID)
	if err != nil {
		return nil, fmt.Errorf("failed to load step states: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var step schema.OrchestrationStepState
		var actor, note, fieldsJSON, metadataJSON sql.NullString
		var startedAt, finishedAt, updatedAt sql.NullTime
		if err := rows.Scan(&step.StepID, &step.Status, &actor, &note, &startedAt, &finishedAt, &updatedAt, &fieldsJSON, &metadataJSON); err != nil {
			return nil, fmt.Errorf("failed to scan step state: %w", err)
		}

		if actor.Valid {
			step.Actor = actor.String
		}
		if note.Valid {
			step.Note = note.String
		}
		if startedAt.Valid {
			step.StartedAt = &startedAt.Time
		}
		if finishedAt.Valid {
			step.FinishedAt = &finishedAt.Time
		}
		if updatedAt.Valid {
			step.UpdatedAt = &updatedAt.Time
		}
		if fieldsJSON.Valid {
			fields, err := unmarshalJSON(fieldsJSON.String)
			if err != nil {
				return nil, fmt.Errorf("failed to decode step fields: %w", err)
			}
			step.Fields = fields
		}
		if metadataJSON.Valid {
			metadata, err := unmarshalJSON(metadataJSON.String)
			if err != nil {
				return nil, fmt.Errorf("failed to decode step metadata: %w", err)
			}
			step.Metadata = metadata
		}

		run.Steps = append(run.Steps, step)
	}

	return &run, nil
}

// QueryRuns searches orchestration runs based on query parameters.
func (rt *RunTracker) QueryRuns(query schema.OrchestrationRunQuery) ([]schema.OrchestrationRun, error) {
	var conditions []string
	var args []interface{}

	if query.Query != "" {
		wordConditions, wordArgs := buildWordSearchConditions(query.Query, []string{"plan_title", "plan_description"})
		if len(wordConditions) > 0 {
			conditions = append(conditions, fmt.Sprintf("(%s)", strings.Join(wordConditions, " OR ")))
			args = append(args, wordArgs...)
		}
	}
	if len(query.Statuses) > 0 {
		placeholders := make([]string, len(query.Statuses))
		for i, status := range query.Statuses {
			placeholders[i] = "?"
			args = append(args, status)
		}
		conditions = append(conditions, fmt.Sprintf("status IN (%s)", strings.Join(placeholders, ",")))
	}
	if len(query.PlanIDs) > 0 {
		placeholders := make([]string, len(query.PlanIDs))
		for i, planID := range query.PlanIDs {
			placeholders[i] = "?"
			args = append(args, planID)
		}
		conditions = append(conditions, fmt.Sprintf("plan_id IN (%s)", strings.Join(placeholders, ",")))
	}
	if query.Scope.Service != "" {
		conditions = append(conditions, "service = ?")
		args = append(args, query.Scope.Service)
	}
	if query.Scope.Team != "" {
		conditions = append(conditions, "team = ?")
		args = append(args, query.Scope.Team)
	}
	if query.Scope.Environment != "" {
		conditions = append(conditions, "environment = ?")
		args = append(args, query.Scope.Environment)
	}

	querySQL := "SELECT id, plan_id, status, created_at, updated_at, created_by, service, team, environment FROM orchestration_runs"
	if len(conditions) > 0 {
		querySQL += " WHERE " + strings.Join(conditions, " AND ")
	}
	querySQL += " ORDER BY created_at DESC"
	if query.Limit > 0 {
		querySQL += " LIMIT ?"
		args = append(args, query.Limit)
	}

	rows, err := rt.db.Query(querySQL, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query runs: %w", err)
	}
	defer rows.Close()

	var runs []schema.OrchestrationRun
	for rows.Next() {
		var run schema.OrchestrationRun
		var createdBy string
		var service, team, environment sql.NullString
		if err := rows.Scan(&run.ID, &run.PlanID, &run.Status, &run.CreatedAt, &run.UpdatedAt, &createdBy, &service, &team, &environment); err != nil {
			return nil, fmt.Errorf("failed to scan run: %w", err)
		}
		if service.Valid {
			run.Scope.Service = service.String
		}
		if team.Valid {
			run.Scope.Team = team.String
		}
		if environment.Valid {
			run.Scope.Environment = environment.String
		}
		runs = append(runs, run)
	}
	return runs, nil
}

// CompleteStep marks a step as completed by an actor.
func (rt *RunTracker) CompleteStep(runID, stepID, actor, note string) error {
	return rt.finishStep(runID, stepID, actor, note, "succeeded", nil)
}

// MarkStepRunning moves a ready step to running.
func (rt *RunTracker) MarkStepRunning(runID, stepID, actor string) error {
	now := time.Now()
	tx, err := rt.db.Begin()
	if err != nil {
		return fmt.Errorf("failed to start transaction: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	var status string
	if err := tx.QueryRow(`SELECT status FROM step_states WHERE run_id = ? AND step_id = ?`, runID, stepID).Scan(&status); err != nil {
		if err == sql.ErrNoRows {
			return fmt.Errorf("step %s not found in run %s", stepID, runID)
		}
		return fmt.Errorf("failed to load step status: %w", err)
	}
	if status != "ready" {
		return fmt.Errorf("step %s cannot start from status %s", stepID, status)
	}
	if _, err := tx.Exec(`
		UPDATE step_states
		SET status = ?, actor = ?, started_at = COALESCE(started_at, ?), updated_at = ?
		WHERE run_id = ? AND step_id = ?
	`, "running", actor, now, now, runID, stepID); err != nil {
		return fmt.Errorf("failed to mark step running: %w", err)
	}
	if err := rt.updateRunStatus(tx, runID, "running"); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}
	committed = true
	return nil
}

// SucceedStep marks a step as succeeded and unlocks dependents.
func (rt *RunTracker) SucceedStep(runID, stepID, actor, note string, fields map[string]any) error {
	return rt.finishStep(runID, stepID, actor, note, "succeeded", fields)
}

// FailStep marks a step and its run as failed.
func (rt *RunTracker) FailStep(runID, stepID, actor, note string, fields map[string]any) error {
	return rt.finishStep(runID, stepID, actor, note, "failed", fields)
}

func (rt *RunTracker) finishStep(runID, stepID, actor, note, finalStatus string, fields map[string]any) error {
	now := time.Now()
	tx, err := rt.db.Begin()
	if err != nil {
		return fmt.Errorf("failed to start transaction: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	var currentStatus string
	err = tx.QueryRow(`SELECT status FROM step_states WHERE run_id = ? AND step_id = ?`, runID, stepID).Scan(&currentStatus)
	if err != nil {
		if err == sql.ErrNoRows {
			return fmt.Errorf("step %s not found in run %s", stepID, runID)
		}
		return fmt.Errorf("failed to check step status: %w", err)
	}

	if currentStatus == "succeeded" {
		return fmt.Errorf("step %s is already completed", stepID)
	}
	switch currentStatus {
	case "pending", "ready", "blocked", "running":
	default:
		return fmt.Errorf("step %s cannot be completed from status %s", stepID, currentStatus)
	}

	if finalStatus == "succeeded" {
		if err := rt.checkStepDependencies(tx, runID, stepID); err != nil {
			return fmt.Errorf("dependencies not satisfied: %w", err)
		}
	}

	fieldsJSON, err := marshalJSON(fields)
	if err != nil {
		return fmt.Errorf("failed to encode step fields: %w", err)
	}
	_, err = tx.Exec(`
		UPDATE step_states
		SET status = ?, actor = ?, note = ?, finished_at = ?, updated_at = ?, fields_json = COALESCE(?, fields_json)
		WHERE run_id = ? AND step_id = ?
	`, finalStatus, actor, note, now, now, nullableString(fieldsJSON), runID, stepID)
	if err != nil {
		return fmt.Errorf("failed to update step state: %w", err)
	}

	if finalStatus == "succeeded" {
		if err := rt.updateDependentSteps(tx, runID, stepID); err != nil {
			return fmt.Errorf("failed to update dependent steps: %w", err)
		}
	}

	if err := rt.recomputeRunStatus(tx, runID, finalStatus); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}
	committed = true
	return nil
}

func (rt *RunTracker) checkStepDependencies(tx *sql.Tx, runID, stepID string) error {
	rows, err := tx.Query(`
		SELECT sd.depends_on_step_id, ss.status
		FROM step_dependencies sd
		JOIN step_states ss ON sd.run_id = ss.run_id AND sd.depends_on_step_id = ss.step_id
		WHERE sd.run_id = ? AND sd.step_id = ?
	`, runID, stepID)
	if err != nil {
		return fmt.Errorf("failed to query dependencies: %w", err)
	}
	defer rows.Close()

	var unsatisfiedDeps []string
	for rows.Next() {
		var depStepID, depStatus string
		if err := rows.Scan(&depStepID, &depStatus); err != nil {
			return fmt.Errorf("failed to scan dependency: %w", err)
		}
		if depStatus != "succeeded" {
			unsatisfiedDeps = append(unsatisfiedDeps, depStepID)
		}
	}
	if len(unsatisfiedDeps) > 0 {
		return fmt.Errorf("step %s has unsatisfied dependencies: %v", stepID, unsatisfiedDeps)
	}
	return nil
}

func (rt *RunTracker) updateDependentSteps(tx *sql.Tx, runID, completedStepID string) error {
	now := time.Now()
	rows, err := tx.Query(`
		SELECT DISTINCT step_id FROM step_dependencies
		WHERE run_id = ? AND depends_on_step_id = ?
	`, runID, completedStepID)
	if err != nil {
		return fmt.Errorf("failed to query dependent steps: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var dependentStepID string
		if err := rows.Scan(&dependentStepID); err != nil {
			return fmt.Errorf("failed to scan dependent step: %w", err)
		}

		var currentStatus string
		if err := tx.QueryRow(`SELECT status FROM step_states WHERE run_id = ? AND step_id = ?`, runID, dependentStepID).Scan(&currentStatus); err != nil {
			continue
		}
		if currentStatus != "pending" {
			continue
		}

		var unsatisfiedCount int
		if err := tx.QueryRow(`
			SELECT COUNT(*)
			FROM step_dependencies sd
			JOIN step_states ss ON sd.run_id = ss.run_id AND sd.depends_on_step_id = ss.step_id
			WHERE sd.run_id = ? AND sd.step_id = ? AND ss.status != 'succeeded'
		`, runID, dependentStepID).Scan(&unsatisfiedCount); err != nil {
			continue
		}
		if unsatisfiedCount == 0 {
			if _, err := tx.Exec(`
				UPDATE step_states SET status = ?, updated_at = ?
				WHERE run_id = ? AND step_id = ?
			`, "ready", now, runID, dependentStepID); err != nil {
				return fmt.Errorf("failed to update dependent step %s: %w", dependentStepID, err)
			}
		}
	}
	return nil
}

func (rt *RunTracker) initializeDatabase() error {
	schemaSQL := `
	CREATE TABLE IF NOT EXISTS orchestration_runs (
		id TEXT PRIMARY KEY,
		plan_id TEXT NOT NULL,
		plan_title TEXT,
		plan_description TEXT,
		plan_version TEXT,
		service TEXT,
		team TEXT,
		environment TEXT,
		status TEXT NOT NULL,
		created_at DATETIME NOT NULL,
		updated_at DATETIME NOT NULL,
		created_by TEXT NOT NULL
	);

	CREATE TABLE IF NOT EXISTS step_states (
		run_id TEXT NOT NULL,
		step_id TEXT NOT NULL,
		status TEXT NOT NULL,
		actor TEXT,
		note TEXT,
		started_at DATETIME,
		finished_at DATETIME,
		updated_at DATETIME,
		step_type TEXT,
		fields_json TEXT,
		metadata_json TEXT,
		PRIMARY KEY (run_id, step_id),
		FOREIGN KEY (run_id) REFERENCES orchestration_runs(id)
	);

	CREATE TABLE IF NOT EXISTS step_dependencies (
		run_id TEXT NOT NULL,
		step_id TEXT NOT NULL,
		depends_on_step_id TEXT NOT NULL,
		PRIMARY KEY (run_id, step_id, depends_on_step_id),
		FOREIGN KEY (run_id, step_id) REFERENCES step_states(run_id, step_id),
		FOREIGN KEY (run_id, depends_on_step_id) REFERENCES step_states(run_id, step_id)
	);

	CREATE INDEX IF NOT EXISTS idx_runs_status ON orchestration_runs(status);
	CREATE INDEX IF NOT EXISTS idx_runs_plan_id ON orchestration_runs(plan_id);
	CREATE INDEX IF NOT EXISTS idx_runs_created_by ON orchestration_runs(created_by);
	CREATE INDEX IF NOT EXISTS idx_runs_created_at ON orchestration_runs(created_at);
	CREATE INDEX IF NOT EXISTS idx_runs_plan_title ON orchestration_runs(plan_title);
	CREATE INDEX IF NOT EXISTS idx_runs_plan_description ON orchestration_runs(plan_description);
	CREATE INDEX IF NOT EXISTS idx_steps_status ON step_states(status);
	CREATE INDEX IF NOT EXISTS idx_step_deps_step ON step_dependencies(run_id, step_id);
	CREATE INDEX IF NOT EXISTS idx_step_deps_depends ON step_dependencies(run_id, depends_on_step_id);
	`

	if _, err := rt.db.Exec(schemaSQL); err != nil {
		return fmt.Errorf("failed to create database schema: %w", err)
	}
	return nil
}

func (rt *RunTracker) recomputeRunStatus(tx *sql.Tx, runID, finalStepStatus string) error {
	now := time.Now()

	// Check if any step in the run has failed (including previously failed steps).
	var failedCount int
	if err := tx.QueryRow(`
		SELECT COUNT(*) FROM step_states
		WHERE run_id = ? AND status = 'failed'
	`, runID).Scan(&failedCount); err != nil {
		return fmt.Errorf("failed to count failed steps: %w", err)
	}
	if failedCount > 0 {
		return rt.updateRunStatus(tx, runID, "failed")
	}

	var remaining int
	if err := tx.QueryRow(`
		SELECT COUNT(*) FROM step_states
		WHERE run_id = ? AND status NOT IN ('succeeded', 'skipped')
	`, runID).Scan(&remaining); err != nil {
		return fmt.Errorf("failed to count remaining steps: %w", err)
	}

	status := "running"
	if remaining == 0 {
		status = "completed"
	}
	if _, err := tx.Exec(`UPDATE orchestration_runs SET status = ?, updated_at = ? WHERE id = ?`, status, now, runID); err != nil {
		return fmt.Errorf("failed to update run status: %w", err)
	}
	return nil
}

func (rt *RunTracker) updateRunStatus(tx *sql.Tx, runID, status string) error {
	if _, err := tx.Exec(`UPDATE orchestration_runs SET status = ?, updated_at = ? WHERE id = ?`, status, time.Now(), runID); err != nil {
		return fmt.Errorf("failed to update run timestamp: %w", err)
	}
	return nil
}

// Close closes the database connection.
func (rt *RunTracker) Close() error {
	return rt.db.Close()
}

// buildWordSearchConditions creates SQL conditions for word-based search.
func buildWordSearchConditions(query string, fields []string) ([]string, []interface{}) {
	if query == "" {
		return nil, nil
	}
	words := strings.Fields(strings.ToLower(strings.TrimSpace(query)))
	if len(words) == 0 {
		return nil, nil
	}
	var conditions []string
	var args []interface{}
	for _, word := range words {
		for _, field := range fields {
			conditions = append(conditions, fmt.Sprintf("LOWER(%s) LIKE ?", field))
			args = append(args, "%"+word+"%")
		}
	}
	return conditions, args
}

func marshalJSON(v map[string]any) (string, error) {
	if len(v) == 0 {
		return "", nil
	}
	data, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func unmarshalJSON(raw string) (map[string]any, error) {
	if raw == "" {
		return nil, nil
	}
	var out map[string]any
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return nil, err
	}
	return out, nil
}

func nullableString(v string) any {
	if v == "" {
		return nil
	}
	return v
}

func planScope(plan *schema.OrchestrationPlan) schema.QueryScope {
	return schema.QueryScope{
		Service:     stringField(plan.Fields, "service"),
		Team:        stringField(plan.Fields, "team"),
		Environment: stringField(plan.Fields, "environment"),
	}
}
