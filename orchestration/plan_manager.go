package orchestration

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/opsorch/opsorch-core/schema"
	"gopkg.in/yaml.v3"
)

// YAMLPlan represents the YAML structure for orchestration plans.
type YAMLPlan struct {
	ID          string            `yaml:"id"`
	Title       string            `yaml:"title"`
	Description string            `yaml:"description,omitempty"`
	Version     string            `yaml:"version,omitempty"`
	Service     string            `yaml:"service"`
	Team        string            `yaml:"team"`
	Environment string            `yaml:"environment"`
	Tags        map[string]string `yaml:"tags,omitempty"`
	Steps       []YAMLStep        `yaml:"steps"`
}

// YAMLStep represents a single plan step.
type YAMLStep struct {
	ID          string         `yaml:"id"`
	Title       string         `yaml:"title"`
	Type        string         `yaml:"type,omitempty"`
	Description string         `yaml:"description,omitempty"`
	DependsOn   []string       `yaml:"dependsOn,omitempty"`
	Reasoning   string         `yaml:"reasoning,omitempty"`
	Exec        *StepExec      `yaml:"exec,omitempty"`
	Fields      map[string]any `yaml:"fields,omitempty"`
	Metadata    map[string]any `yaml:"metadata,omitempty"`
}

// PlanManager handles plan storage and retrieval from a source.
type PlanManager struct {
	source      PlanSource
	localSaveTo string
}

// NewPlanManager creates a manager backed by a local directory.
func NewPlanManager(storagePath string) *PlanManager {
	return &PlanManager{
		source:      &LocalPlanSource{Path: storagePath},
		localSaveTo: storagePath,
	}
}

// NewPlanManagerWithSource creates a manager backed by an arbitrary source.
func NewPlanManagerWithSource(source PlanSource) *PlanManager {
	return &PlanManager{source: source}
}

// LoadPlan loads and validates a single plan by ID.
func (pm *PlanManager) LoadPlan(ctx context.Context, planID string) (*schema.OrchestrationPlan, error) {
	planDir, err := pm.source.PlanDir(ctx)
	if err != nil {
		return nil, err
	}
	filePath := filepath.Join(planDir, planID+".yaml")
	return pm.loadPlanFile(filePath)
}

// QueryPlans loads plans from the source and applies query filters.
func (pm *PlanManager) QueryPlans(ctx context.Context, query schema.OrchestrationPlanQuery) ([]schema.OrchestrationPlan, error) {
	planDir, err := pm.source.PlanDir(ctx)
	if err != nil {
		return nil, err
	}

	files, err := filepath.Glob(filepath.Join(planDir, "*.yaml"))
	if err != nil {
		return nil, fmt.Errorf("failed to list plan files: %w", err)
	}

	plans := make([]schema.OrchestrationPlan, 0, len(files))
	for _, file := range files {
		plan, err := pm.loadPlanFile(file)
		if err != nil {
			continue
		}
		if pm.matchesQuery(plan, query) {
			plans = append(plans, *plan)
		}
		if query.Limit > 0 && len(plans) >= query.Limit {
			break
		}
	}

	return plans, nil
}

// SavePlan saves a plan as YAML to the local directory.
func (pm *PlanManager) SavePlan(plan *schema.OrchestrationPlan) error {
	if pm.localSaveTo == "" {
		return fmt.Errorf("plan source is read-only")
	}
	if err := pm.ValidatePlan(plan); err != nil {
		return err
	}

	yamlPlan := pm.schemaToYAML(plan)
	data, err := yaml.Marshal(yamlPlan)
	if err != nil {
		return fmt.Errorf("failed to marshal plan to YAML: %w", err)
	}

	filePath := filepath.Join(pm.localSaveTo, plan.ID+".yaml")
	if err := os.WriteFile(filePath, data, 0644); err != nil {
		return fmt.Errorf("failed to write plan file %s: %w", filePath, err)
	}
	return nil
}

func (pm *PlanManager) loadPlanFile(filePath string) (*schema.OrchestrationPlan, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read plan file %s: %w", filePath, err)
	}

	var yamlPlan YAMLPlan
	if err := yaml.Unmarshal(data, &yamlPlan); err != nil {
		return nil, fmt.Errorf("failed to parse YAML plan %s: %w", filePath, err)
	}

	if yamlPlan.ID == "" {
		yamlPlan.ID = strings.TrimSuffix(filepath.Base(filePath), filepath.Ext(filePath))
	}

	plan := pm.yamlToSchema(&yamlPlan)
	if err := pm.ValidatePlan(plan); err != nil {
		return nil, err
	}
	return plan, nil
}

// ValidatePlan checks required fields and dependencies.
func (pm *PlanManager) ValidatePlan(plan *schema.OrchestrationPlan) error {
	if plan == nil {
		return fmt.Errorf("plan cannot be nil")
	}
	if strings.TrimSpace(plan.ID) == "" {
		return fmt.Errorf("plan id is required")
	}
	if strings.TrimSpace(plan.Title) == "" {
		return fmt.Errorf("plan %s title is required", plan.ID)
	}
	scope := planScope(plan)
	if strings.TrimSpace(scope.Service) == "" {
		return fmt.Errorf("plan %s requires service", plan.ID)
	}
	if strings.TrimSpace(scope.Team) == "" {
		return fmt.Errorf("plan %s requires team", plan.ID)
	}
	if strings.TrimSpace(scope.Environment) == "" {
		return fmt.Errorf("plan %s requires environment", plan.ID)
	}
	if len(plan.Steps) == 0 {
		return fmt.Errorf("plan %s must define at least one step", plan.ID)
	}

	stepMap := make(map[string]*schema.OrchestrationStep, len(plan.Steps))
	for i := range plan.Steps {
		step := &plan.Steps[i]
		if strings.TrimSpace(step.ID) == "" {
			return fmt.Errorf("plan %s contains a step with empty id", plan.ID)
		}
		if strings.TrimSpace(step.Title) == "" {
			return fmt.Errorf("step %s title is required", step.ID)
		}
		if _, exists := stepMap[step.ID]; exists {
			return fmt.Errorf("plan %s contains duplicate step id %s", plan.ID, step.ID)
		}
		stepMap[step.ID] = step

		switch step.Type {
		case "", "manual":
			step.Type = "manual"
		case "automated":
			exec := ExtractStepExec(step.Fields)
			if exec == nil || strings.TrimSpace(exec.Image) == "" {
				return fmt.Errorf("automated step %s requires exec.image", step.ID)
			}
		default:
			return fmt.Errorf("step %s has unsupported type %s", step.ID, step.Type)
		}
	}

	visited := make(map[string]bool, len(plan.Steps))
	recStack := make(map[string]bool, len(plan.Steps))
	for _, step := range plan.Steps {
		if !visited[step.ID] && pm.hasCycle(step.ID, stepMap, visited, recStack) {
			return fmt.Errorf("circular dependency detected involving step %s", step.ID)
		}
	}

	for _, step := range plan.Steps {
		for _, depID := range step.DependsOn {
			if _, exists := stepMap[depID]; !exists {
				return fmt.Errorf("step %s depends on non-existent step %s", step.ID, depID)
			}
		}
	}

	return nil
}

func (pm *PlanManager) hasCycle(stepID string, stepMap map[string]*schema.OrchestrationStep, visited, recStack map[string]bool) bool {
	visited[stepID] = true
	recStack[stepID] = true
	step := stepMap[stepID]
	for _, depID := range step.DependsOn {
		if !visited[depID] {
			if pm.hasCycle(depID, stepMap, visited, recStack) {
				return true
			}
		} else if recStack[depID] {
			return true
		}
	}
	recStack[stepID] = false
	return false
}

func (pm *PlanManager) matchesQuery(plan *schema.OrchestrationPlan, query schema.OrchestrationPlanQuery) bool {
	if query.Query != "" && !matchesWordSearch(query.Query, []string{plan.Title, plan.Description}) {
		return false
	}
	if !matchesScope(planScope(plan), query.Scope) {
		return false
	}
	if len(query.Tags) > 0 {
		for key, value := range query.Tags {
			if planValue, exists := plan.Tags[key]; !exists || planValue != value {
				return false
			}
		}
	}
	return true
}

func (pm *PlanManager) yamlToSchema(yamlPlan *YAMLPlan) *schema.OrchestrationPlan {
	plan := &schema.OrchestrationPlan{
		ID:          yamlPlan.ID,
		Title:       yamlPlan.Title,
		Description: yamlPlan.Description,
		Version:     yamlPlan.Version,
		Tags:        yamlPlan.Tags,
		Fields: map[string]any{
			"service":     yamlPlan.Service,
			"team":        yamlPlan.Team,
			"environment": yamlPlan.Environment,
		},
		Steps: make([]schema.OrchestrationStep, len(yamlPlan.Steps)),
	}

	for i, yamlStep := range yamlPlan.Steps {
		stepType := yamlStep.Type
		if stepType == "" {
			stepType = "manual"
		}

		fields := cloneMap(yamlStep.Fields)
		if yamlStep.Reasoning != "" {
			if fields == nil {
				fields = make(map[string]any)
			}
			fields["reasoning"] = yamlStep.Reasoning
		}
		if yamlStep.Exec != nil {
			if fields == nil {
				fields = make(map[string]any)
			}
			fields["exec"] = yamlStep.Exec
		}

		plan.Steps[i] = schema.OrchestrationStep{
			ID:          yamlStep.ID,
			Title:       yamlStep.Title,
			Type:        stepType,
			Description: yamlStep.Description,
			DependsOn:   append([]string(nil), yamlStep.DependsOn...),
			Fields:      fields,
			Metadata:    cloneMap(yamlStep.Metadata),
		}
	}

	return plan
}

func (pm *PlanManager) schemaToYAML(plan *schema.OrchestrationPlan) *YAMLPlan {
	yamlPlan := &YAMLPlan{
		ID:          plan.ID,
		Title:       plan.Title,
		Description: plan.Description,
		Version:     plan.Version,
		Service:     stringField(plan.Fields, "service"),
		Team:        stringField(plan.Fields, "team"),
		Environment: stringField(plan.Fields, "environment"),
		Tags:        plan.Tags,
		Steps:       make([]YAMLStep, len(plan.Steps)),
	}

	for i, step := range plan.Steps {
		yamlStep := YAMLStep{
			ID:          step.ID,
			Title:       step.Title,
			Type:        step.Type,
			Description: step.Description,
			DependsOn:   append([]string(nil), step.DependsOn...),
			Metadata:    cloneMap(step.Metadata),
		}
		if step.Fields != nil {
			remaining := make(map[string]any)
			for k, v := range step.Fields {
				switch k {
				case "reasoning":
					if s, ok := v.(string); ok {
						yamlStep.Reasoning = s
					}
				case "exec":
					if se := ExtractStepExec(map[string]any{"exec": v}); se != nil {
						yamlStep.Exec = se
					}
				default:
					remaining[k] = v
				}
			}
			if len(remaining) > 0 {
				yamlStep.Fields = remaining
			}
		}
		yamlPlan.Steps[i] = yamlStep
	}

	return yamlPlan
}

// matchesWordSearch checks if any of the search words appear in any of the target strings.
func matchesWordSearch(query string, fields []string) bool {
	if query == "" {
		return true
	}
	words := strings.Fields(strings.ToLower(strings.TrimSpace(query)))
	if len(words) == 0 {
		return true
	}
	for _, field := range fields {
		fieldLower := strings.ToLower(field)
		for _, word := range words {
			if strings.Contains(fieldLower, word) {
				return true
			}
		}
	}
	return false
}

func cloneMap(in map[string]any) map[string]any {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]any, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func matchesScope(actual, expected schema.QueryScope) bool {
	if expected.Service != "" && actual.Service != expected.Service {
		return false
	}
	if expected.Team != "" && actual.Team != expected.Team {
		return false
	}
	if expected.Environment != "" && actual.Environment != expected.Environment {
		return false
	}
	return true
}

func stringField(fields map[string]any, key string) string {
	if len(fields) == 0 {
		return ""
	}
	if value, ok := fields[key].(string); ok {
		return value
	}
	return ""
}
