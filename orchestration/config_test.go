package orchestration

import (
	"os"
	"path/filepath"
	"testing"
)

func TestConfigValidateLocalSourceDefaults(t *testing.T) {
	tempDir := t.TempDir()
	cfg := &Config{StoragePath: tempDir}

	if err := cfg.Validate(); err != nil {
		t.Fatalf("expected valid config: %v", err)
	}
	if cfg.SourceType != SourceLocal {
		t.Fatalf("expected default source type %q, got %q", SourceLocal, cfg.SourceType)
	}
	if cfg.Runner != RunnerLocal {
		t.Fatalf("expected default runner %q, got %q", RunnerLocal, cfg.Runner)
	}
	if cfg.LocalPath == "" {
		t.Fatal("expected default local plan path")
	}
	if _, err := os.Stat(cfg.LocalPath); err != nil {
		t.Fatalf("expected local plan path to exist: %v", err)
	}
	if cfg.GetStateDBPath() != filepath.Join(tempDir, "state.db") {
		t.Fatalf("unexpected state DB path %s", cfg.GetStateDBPath())
	}
}

func TestConfigValidateGitSource(t *testing.T) {
	tempDir := t.TempDir()
	cfg := &Config{
		StoragePath: tempDir,
		SourceType:  SourceGit,
		GitRepoURL:  "https://example.com/org/repo.git",
	}

	if err := cfg.Validate(); err != nil {
		t.Fatalf("expected valid git config: %v", err)
	}
	if cfg.GetGitCachePath() != filepath.Join(tempDir, "plan-cache") {
		t.Fatalf("unexpected git cache path %s", cfg.GetGitCachePath())
	}
}

func TestConfigValidateErrors(t *testing.T) {
	if err := (&Config{}).Validate(); err == nil {
		t.Fatal("expected missing storage path error")
	}

	cfg := &Config{StoragePath: t.TempDir(), SourceType: SourceGit}
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected missing gitRepoURL error")
	}

	cfg = &Config{StoragePath: t.TempDir(), Runner: RunnerKind("bad-runner")}
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected invalid runner error")
	}
}
