package orchestration

import (
	"fmt"
	"os"
	"path/filepath"
)

// RunnerKind selects the execution backend for automated steps.
type RunnerKind string

const (
	RunnerLocal RunnerKind = "local"
	RunnerK8s   RunnerKind = "k8s"
)

// SourceKind selects where plans are loaded from.
type SourceKind string

const (
	SourceLocal SourceKind = "local"
	SourceGit   SourceKind = "git"
)

// Config holds the provider configuration.
type Config struct {
	StoragePath string
	StateDB     string

	SourceType  SourceKind
	LocalPath   string
	GitRepoURL  string
	GitRef      string
	GitSubdir   string
	GitCacheDir string

	Runner         RunnerKind
	K8sNamespace   string
	KubeconfigPath string
}

// Validate checks configuration, sets defaults, and creates writable paths.
func (c *Config) Validate() error {
	if c.StoragePath == "" {
		return fmt.Errorf("storagePath is required and cannot be empty")
	}

	if c.StateDB == "" {
		c.StateDB = "state.db"
	}
	if c.SourceType == "" {
		c.SourceType = SourceLocal
	}
	if c.Runner == "" {
		c.Runner = RunnerLocal
	}
	if c.GitRef == "" {
		c.GitRef = "HEAD"
	}
	if c.GitCacheDir == "" {
		c.GitCacheDir = "plan-cache"
	}

	if err := validatePathAtom("stateDB", c.StateDB); err != nil {
		return err
	}

	if err := os.MkdirAll(c.StoragePath, 0755); err != nil {
		return fmt.Errorf("failed to create storage directory %s: %w", c.StoragePath, err)
	}

	switch c.SourceType {
	case SourceLocal:
		if c.LocalPath == "" {
			c.LocalPath = filepath.Join(c.StoragePath, "plans")
		}
		if err := os.MkdirAll(c.LocalPath, 0755); err != nil {
			return fmt.Errorf("failed to create local plan directory %s: %w", c.LocalPath, err)
		}
	case SourceGit:
		if c.GitRepoURL == "" {
			return fmt.Errorf("gitRepoURL is required when sourceType is git")
		}
		if err := validatePathAtom("gitCacheDir", c.GitCacheDir); err != nil {
			return err
		}
	default:
		return fmt.Errorf("unsupported sourceType %q", c.SourceType)
	}

	switch c.Runner {
	case RunnerLocal, RunnerK8s:
	default:
		return fmt.Errorf("unsupported runner %q", c.Runner)
	}

	return nil
}

func validatePathAtom(name, value string) error {
	if value == "" || value == "." || value == ".." {
		return fmt.Errorf("%s must be a valid path segment, got: %s", name, value)
	}
	return nil
}

// GetStateDBPath returns the full path to the SQLite database file.
func (c *Config) GetStateDBPath() string {
	return filepath.Join(c.StoragePath, c.StateDB)
}

// GetGitCachePath returns the local repository cache directory.
func (c *Config) GetGitCachePath() string {
	return filepath.Join(c.StoragePath, c.GitCacheDir)
}
