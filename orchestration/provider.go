package orchestration

import (
	"context"
	"fmt"
	"os"
	"sync"

	coreorchestration "github.com/opsorch/opsorch-core/orchestration"
	"github.com/opsorch/opsorch-core/schema"
)

// ProviderName is the registry key for the orchestration adapter.
const ProviderName = "simple-orchestration"

// Provider implements the orchestration.Provider interface.
type Provider struct {
	planManager *PlanManager
	runTracker  *RunTracker
	config      *Config
	runner      Runner

	reconcileCh chan string
	stopCh      chan struct{}
	wg          sync.WaitGroup
	initOnce    sync.Once
	closeOnce   sync.Once

	mu     sync.Mutex
	queued map[string]bool
	active map[string]bool
	rerun  map[string]bool
}

// New constructs the orchestration provider from decrypted config.
func New(config map[string]any) (coreorchestration.Provider, error) {
	cfg := &Config{}
	if storagePath, ok := config["storagePath"].(string); ok {
		cfg.StoragePath = storagePath
	}
	if stateDB, ok := config["stateDB"].(string); ok {
		cfg.StateDB = stateDB
	}
	if sourceType, ok := config["sourceType"].(string); ok {
		cfg.SourceType = SourceKind(sourceType)
	}
	if localPath, ok := config["localPath"].(string); ok {
		cfg.LocalPath = localPath
	}
	if gitRepoURL, ok := config["gitRepoURL"].(string); ok {
		cfg.GitRepoURL = gitRepoURL
	}
	if gitRef, ok := config["gitRef"].(string); ok {
		cfg.GitRef = gitRef
	}
	if gitSubdir, ok := config["gitSubdir"].(string); ok {
		cfg.GitSubdir = gitSubdir
	}
	if gitCacheDir, ok := config["gitCacheDir"].(string); ok {
		cfg.GitCacheDir = gitCacheDir
	}
	if runner, ok := config["runner"].(string); ok {
		cfg.Runner = RunnerKind(runner)
	}
	if ns, ok := config["k8sNamespace"].(string); ok {
		cfg.K8sNamespace = ns
	}
	if kubeconfig, ok := config["kubeconfigPath"].(string); ok {
		cfg.KubeconfigPath = kubeconfig
	}

	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	planManager := NewPlanManagerWithSource(newPlanSource(cfg))
	runTracker, err := NewRunTracker(cfg.GetStateDBPath())
	if err != nil {
		return nil, err
	}

	p := &Provider{
		planManager: planManager,
		runTracker:  runTracker,
		config:      cfg,
		runner:      newRunner(cfg),
	}
	p.ensureRuntime()
	return p, nil
}

func init() {
	_ = coreorchestration.RegisterProvider(ProviderName, New)
}

func newPlanSource(cfg *Config) PlanSource {
	if cfg.SourceType == SourceGit {
		return &GitPlanSource{
			RepoURL:  cfg.GitRepoURL,
			Ref:      cfg.GitRef,
			Subdir:   cfg.GitSubdir,
			CacheDir: cfg.GetGitCachePath(),
		}
	}
	return &LocalPlanSource{Path: cfg.LocalPath}
}

func newRunner(cfg *Config) Runner {
	if cfg.Runner == RunnerK8s {
		return &K8sRunner{
			Namespace:      cfg.K8sNamespace,
			KubeconfigPath: cfg.KubeconfigPath,
		}
	}
	return &LocalRunner{}
}

// Close stops background reconciliation and closes the run tracker.
// Safe to call multiple times.
func (p *Provider) Close() error {
	var closeErr error
	p.closeOnce.Do(func() {
		if p.stopCh != nil {
			close(p.stopCh)
			p.wg.Wait()
		}
		closeErr = p.runTracker.Close()
	})
	return closeErr
}

// QueryPlans returns plans matching the query.
func (p *Provider) QueryPlans(ctx context.Context, query schema.OrchestrationPlanQuery) ([]schema.OrchestrationPlan, error) {
	return p.planManager.QueryPlans(ctx, query)
}

// GetPlan returns a single plan by ID.
func (p *Provider) GetPlan(ctx context.Context, planID string) (*schema.OrchestrationPlan, error) {
	if planID == "" {
		return nil, NewInvalidRequestError("planID cannot be empty")
	}
	plan, err := p.planManager.LoadPlan(ctx, planID)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, NewNotFoundError("plan", planID)
		}
		return nil, NewStorageError("load_plan", err)
	}
	return plan, nil
}

// QueryRuns returns runs matching the query.
func (p *Provider) QueryRuns(ctx context.Context, query schema.OrchestrationRunQuery) ([]schema.OrchestrationRun, error) {
	return p.runTracker.QueryRuns(query)
}

// GetRun returns a single run by ID, enriched with its plan.
func (p *Provider) GetRun(ctx context.Context, runID string) (*schema.OrchestrationRun, error) {
	run, err := p.runTracker.LoadRun(runID)
	if err != nil {
		return nil, err
	}
	plan, err := p.planManager.LoadPlan(ctx, run.PlanID)
	if err == nil {
		run.Plan = plan
	}
	return run, nil
}

// StartRun creates a new run from a plan and enqueues reconciliation.
func (p *Provider) StartRun(ctx context.Context, planID string) (*schema.OrchestrationRun, error) {
	p.ensureRuntime()
	plan, err := p.planManager.LoadPlan(ctx, planID)
	if err != nil {
		return nil, err
	}
	run, err := p.runTracker.CreateRun(plan, "system")
	if err != nil {
		return nil, err
	}
	p.enqueueRun(run.ID)
	return p.GetRun(ctx, run.ID)
}

// CompleteStep marks a manual/blocked step as complete and enqueues reconciliation.
func (p *Provider) CompleteStep(ctx context.Context, runID, stepID, actor, note string) error {
	p.ensureRuntime()
	run, err := p.runTracker.LoadRun(runID)
	if err != nil {
		return err
	}
	plan, err := p.planManager.LoadPlan(ctx, run.PlanID)
	if err != nil {
		return err
	}
	step, ok := findPlanStep(plan, stepID)
	if !ok {
		return NewNotFoundError("step", stepID)
	}
	if step.Type == "automated" {
		return NewInvalidRequestError(fmt.Sprintf("step %s is automated and cannot be completed manually", stepID))
	}
	if err := p.runTracker.CompleteStep(runID, stepID, actor, note); err != nil {
		return err
	}
	p.enqueueRun(runID)
	return nil
}

func (p *Provider) enqueueRun(runID string) {
	p.ensureRuntime()
	shouldSend := false

	p.mu.Lock()
	switch {
	case p.active[runID]:
		p.rerun[runID] = true
	case p.queued[runID]:
	default:
		p.queued[runID] = true
		shouldSend = true
	}
	p.mu.Unlock()

	if !shouldSend {
		return
	}

	select {
	case p.reconcileCh <- runID:
	case <-p.stopCh:
	}
}

func (p *Provider) ensureRuntime() {
	p.initOnce.Do(func() {
		p.reconcileCh = make(chan string, 128)
		p.stopCh = make(chan struct{})
		p.queued = make(map[string]bool)
		p.active = make(map[string]bool)
		p.rerun = make(map[string]bool)
		p.wg.Add(1)
		go p.reconcileLoop()
	})
}

func (p *Provider) reconcileLoop() {
	defer p.wg.Done()
	for {
		select {
		case <-p.stopCh:
			return
		case runID := <-p.reconcileCh:
			p.mu.Lock()
			delete(p.queued, runID)
			p.active[runID] = true
			p.mu.Unlock()

			p.reconcileRun(runID)

			shouldReplay := false
			p.mu.Lock()
			delete(p.active, runID)
			if p.rerun[runID] {
				delete(p.rerun, runID)
				if !p.queued[runID] {
					p.queued[runID] = true
					shouldReplay = true
				}
			}
			p.mu.Unlock()

			if shouldReplay {
				select {
				case p.reconcileCh <- runID:
				case <-p.stopCh:
					return
				}
			}
		}
	}
}

func (p *Provider) reconcileRun(runID string) {
	run, err := p.runTracker.LoadRun(runID)
	if err != nil {
		return
	}
	plan, err := p.planManager.LoadPlan(context.Background(), run.PlanID)
	if err != nil {
		return
	}

	for _, state := range run.Steps {
		if state.Status != "ready" {
			continue
		}
		step, ok := findPlanStep(plan, state.StepID)
		if !ok || step.Type != "automated" {
			continue
		}
		execSpec := ExtractStepExec(step.Fields)
		if execSpec == nil {
			_ = p.runTracker.FailStep(runID, step.ID, "system", "automated step is missing exec configuration", map[string]any{
				"error": "missing exec configuration",
			})
			p.enqueueRun(runID)
			continue
		}
		if err := p.runTracker.MarkStepRunning(runID, step.ID, "system"); err != nil {
			continue
		}
		stepCopy := *step
		p.wg.Add(1)
		go func() {
			defer p.wg.Done()
			p.executeAutomatedStep(runID, &stepCopy)
		}()
	}
}

func (p *Provider) executeAutomatedStep(runID string, step *schema.OrchestrationStep) {
	execSpec := ExtractStepExec(step.Fields)
	if execSpec == nil {
		_ = p.runTracker.FailStep(runID, step.ID, "system", "automated step is missing exec configuration", map[string]any{
			"error": "missing exec configuration",
		})
		p.enqueueRun(runID)
		return
	}

	output, runErr := p.runner.Run(context.Background(), runID, step.ID, execSpec)
	fields := map[string]any{
		"output": output,
		"runner": string(p.config.Runner),
	}
	if runErr != nil {
		fields["error"] = runErr.Error()
		_ = p.runTracker.FailStep(runID, step.ID, "system", runErr.Error(), fields)
		p.enqueueRun(runID)
		return
	}

	_ = p.runTracker.SucceedStep(runID, step.ID, "system", "automated step completed", fields)
	p.enqueueRun(runID)
}

func findPlanStep(plan *schema.OrchestrationPlan, stepID string) (*schema.OrchestrationStep, bool) {
	for i := range plan.Steps {
		if plan.Steps[i].ID == stepID {
			return &plan.Steps[i], true
		}
	}
	return nil, false
}
