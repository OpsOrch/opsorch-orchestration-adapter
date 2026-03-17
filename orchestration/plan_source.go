package orchestration

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// PlanSource resolves a local directory containing plan YAML files.
type PlanSource interface {
	PlanDir(ctx context.Context) (string, error)
}

type LocalPlanSource struct {
	Path string
}

func (s *LocalPlanSource) PlanDir(_ context.Context) (string, error) {
	if s.Path == "" {
		return "", fmt.Errorf("local plan path is required")
	}
	if err := os.MkdirAll(s.Path, 0755); err != nil {
		return "", fmt.Errorf("failed to create local plan directory %s: %w", s.Path, err)
	}
	return s.Path, nil
}

type GitPlanSource struct {
	RepoURL  string
	Ref      string
	Subdir   string
	CacheDir string
}

func (s *GitPlanSource) PlanDir(ctx context.Context) (string, error) {
	if s.RepoURL == "" {
		return "", fmt.Errorf("git repository URL is required")
	}
	if s.CacheDir == "" {
		return "", fmt.Errorf("git cache directory is required")
	}

	if _, err := os.Stat(filepath.Join(s.CacheDir, ".git")); err != nil {
		if !os.IsNotExist(err) {
			return "", fmt.Errorf("failed to inspect git cache %s: %w", s.CacheDir, err)
		}
		if err := os.RemoveAll(s.CacheDir); err != nil && !os.IsNotExist(err) {
			return "", fmt.Errorf("failed to reset git cache %s: %w", s.CacheDir, err)
		}
		if out, err := exec.CommandContext(ctx, "git", "clone", s.RepoURL, s.CacheDir).CombinedOutput(); err != nil {
			return "", fmt.Errorf("git clone failed: %w: %s", err, string(out))
		}
	} else {
		if out, err := exec.CommandContext(ctx, "git", "-C", s.CacheDir, "fetch", "--all", "--prune").CombinedOutput(); err != nil {
			return "", fmt.Errorf("git fetch failed: %w: %s", err, string(out))
		}
	}

	ref := s.Ref
	if ref == "" {
		ref = "HEAD"
	}
	if out, err := exec.CommandContext(ctx, "git", "-C", s.CacheDir, "reset", "--hard", ref).CombinedOutput(); err != nil {
		return "", fmt.Errorf("git reset failed: %w: %s", err, string(out))
	}

	planDir := s.CacheDir
	if s.Subdir != "" {
		planDir = filepath.Join(planDir, s.Subdir)
	}
	info, err := os.Stat(planDir)
	if err != nil {
		return "", fmt.Errorf("failed to access plan directory %s: %w", planDir, err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("plan directory %s is not a directory", planDir)
	}
	return planDir, nil
}
