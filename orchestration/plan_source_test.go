package orchestration

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestGitPlanSourceClonesRepository(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}

	repoDir := t.TempDir()
	runGit(t, repoDir, "init")
	runGit(t, repoDir, "config", "user.email", "test@example.com")
	runGit(t, repoDir, "config", "user.name", "Test User")

	planYAML := `id: git-plan
title: Git Plan
tags:
  type: git
steps:
  - id: first
    title: First
    type: manual
`
	if err := os.WriteFile(filepath.Join(repoDir, "git-plan.yaml"), []byte(planYAML), 0644); err != nil {
		t.Fatalf("failed to write plan file: %v", err)
	}
	runGit(t, repoDir, "add", "git-plan.yaml")
	runGit(t, repoDir, "commit", "-m", "add plan")

	cacheDir := filepath.Join(t.TempDir(), "cache")
	source := &GitPlanSource{
		RepoURL:  repoDir,
		Ref:      "HEAD",
		CacheDir: cacheDir,
	}
	planDir, err := source.PlanDir(context.Background())
	if err != nil {
		t.Fatalf("failed to resolve plan dir: %v", err)
	}
	if _, err := os.Stat(filepath.Join(planDir, "git-plan.yaml")); err != nil {
		t.Fatalf("expected cloned plan file: %v", err)
	}
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v failed: %v: %s", args, err, string(out))
	}
}
