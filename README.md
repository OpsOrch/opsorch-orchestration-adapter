# OpsOrch Orchestration Adapter

[![Version](https://img.shields.io/github/v/release/opsorch/opsorch-orchestration-adapter)](https://github.com/opsorch/opsorch-orchestration-adapter/releases)
[![Go Version](https://img.shields.io/github/go-mod/go-version/opsorch/opsorch-orchestration-adapter)](https://github.com/opsorch/opsorch-orchestration-adapter/blob/main/go.mod)
[![License](https://img.shields.io/github/license/opsorch/opsorch-orchestration-adapter)](https://github.com/opsorch/opsorch-orchestration-adapter/blob/main/LICENSE)
[![CI](https://github.com/opsorch/opsorch-orchestration-adapter/workflows/CI/badge.svg)](https://github.com/opsorch/opsorch-orchestration-adapter/actions)

This adapter gives OpsOrch a built-in orchestration provider for teams that do not already have a runbook or playbook system. It loads YAML plans from a local directory or git repository, stores run state in SQLite, and executes mixed manual and automated steps through a lightweight reconciler.

## Quick Start

1. **Build the plugin**
   ```bash
   git clone https://github.com/opsorch/opsorch-orchestration-adapter.git
   cd opsorch-orchestration-adapter
   make plugin
   ```

2. **Create a local plan directory**
   ```bash
   mkdir -p /opt/opsorch/runbooks
   ```

3. **Configure OpsOrch Core**
   ```bash
   export OPSORCH_ORCHESTRATION_PLUGIN=/path/to/bin/orchestrationplugin
   export OPSORCH_ORCHESTRATION_CONFIG='{"storagePath":"/opt/opsorch/data","sourceType":"local","localPath":"/opt/opsorch/runbooks","runner":"local"}'
   ```

## Capabilities

This adapter provides the orchestration capability:

1. **Plan Catalog**: Query and retrieve runbooks/playbooks from local files or git
2. **Run Tracking**: Persist orchestration runs and step state in SQLite
3. **Mixed Execution**: Support manual and automated steps in the same plan
4. **Reactive Reconciliation**: Advance automated work when runs change without introducing a scheduler

## Features

- **YAML-backed plans**: Plans come from a local directory or a git-backed cache
- **Explicit scope**: `service`, `team`, and `environment` are first-class plan fields
- **Container-based automated steps**: Automated work is defined as image, command, args, and env
- **Runner choice**: Execute automated steps locally with Docker or on Kubernetes as Jobs
- **SQLite-backed run state**: Runs, step transitions, outputs, and scope are persisted locally
- **In-process reconciler**: A lightweight background reconciler advances ready automated steps

### Version Compatibility

- **Adapter Version**: 0.1.0
- **Requires OpsOrch Core**: >=0.4.0
- **Go Version**: 1.24+

## Configuration

The orchestration adapter accepts the following configuration:

| Field | Type | Required | Description | Default |
|-------|------|----------|-------------|---------|
| `storagePath` | string | Yes | Writable base directory for SQLite state and git cache | - |
| `stateDB` | string | No | SQLite filename stored under `storagePath` | `state.db` |
| `sourceType` | string | No | `local` or `git` | `local` |
| `localPath` | string | No | Directory containing YAML plans when `sourceType=local` | `{storagePath}/plans` |
| `gitRepoURL` | string | Yes for git | Git repository containing plans | - |
| `gitRef` | string | No | Git ref to reset the local cache to | `HEAD` |
| `gitSubdir` | string | No | Subdirectory in the git repo containing plans | `""` |
| `gitCacheDir` | string | No | Local cache directory under `storagePath` | `plan-cache` |
| `runner` | string | No | `local` or `k8s` | `local` |
| `k8sNamespace` | string | No | Namespace used by the Kubernetes runner | `default` |
| `kubeconfigPath` | string | No | Optional kubeconfig path for the Kubernetes runner | `""` |

### Example Configuration

**JSON format:**
```json
{
  "storagePath": "/opt/opsorch/data",
  "stateDB": "orchestration.db",
  "sourceType": "local",
  "localPath": "/opt/opsorch/runbooks",
  "runner": "local"
}
```

**Environment variables:**
```bash
export OPSORCH_ORCHESTRATION_PLUGIN=/path/to/bin/orchestrationplugin
export OPSORCH_ORCHESTRATION_CONFIG='{"storagePath":"/opt/opsorch/data","sourceType":"local","localPath":"/opt/opsorch/runbooks","runner":"local"}'
```

## Plan Format

Plans use canonical YAML:

```yaml
id: production-deploy
service: api
team: platform
environment: production
title: Production Deployment
description: Standard production deployment runbook
version: "1.0"
tags:
  type: deployment

steps:
  - id: prechecks
    title: Verify Preconditions
    type: manual
    reasoning: Confirm approvals, rollback owner, and maintenance window.

  - id: run-migrations
    title: Run Database Migrations
    type: automated
    dependsOn: [prechecks]
    exec:
      image: ghcr.io/example/migrations:latest
      args: ["up"]

  - id: verify
    title: Verify Result
    type: manual
    dependsOn: [run-migrations]
    reasoning: Confirm health checks and smoke tests are green.
```

### Rules

- `service`, `team`, and `environment` are required top-level plan fields
- `id` is recommended; if omitted, the adapter derives it from the filename
- `tags` are for categorization, not scope
- `dependsOn` defines execution ordering
- `type` must be `manual` or `automated`
- automated steps must define `exec.image`
- `author`, `created`, and `updated` should not be stored in YAML; derive them from the file source or git if needed

## Execution Model

The adapter uses a lightweight reconciler, not a scheduler.

- `StartRun` creates a run and enqueues it for reconciliation
- `CompleteStep` marks a manual step complete and re-enqueues the run
- the reconciler finds `ready` automated steps and executes them in the background
- when an automated step completes, the reconciler updates state and triggers downstream work

This keeps the adapter reactive without adding cron, queue infrastructure, or another workflow engine.

## Automated Steps

Automated steps are container workloads:

```yaml
exec:
  image: ghcr.io/org/tool:tag
  command: ["tool"]
  args: ["run", "--env", "prod"]
  env:
    REGION: eu-west-1
```

Runner selection is deployment configuration:

- **Local runner**: executes `docker run --rm ...`
- **Kubernetes runner**: creates a Job and waits for completion in the background reconciler

## Usage

### In-Process Mode

Import the adapter for side effects to register it with OpsOrch Core:

```go
import (
    _ "github.com/opsorch/opsorch-orchestration-adapter"
)
```

Configure via environment variables:

```bash
export OPSORCH_ORCHESTRATION_PROVIDER=simple-orchestration
export OPSORCH_ORCHESTRATION_CONFIG='{"storagePath":"/opt/opsorch/data","sourceType":"local","localPath":"/opt/opsorch/runbooks","runner":"local"}'
```

### Plugin Mode

Build the plugin binary:

```bash
make plugin
```

Configure OpsOrch Core to use the plugin:

```bash
export OPSORCH_ORCHESTRATION_PLUGIN=/path/to/bin/orchestrationplugin
export OPSORCH_ORCHESTRATION_CONFIG='{"storagePath":"/opt/opsorch/data","sourceType":"local","localPath":"/opt/opsorch/runbooks","runner":"local"}'
```

### Docker Deployment

Download pre-built plugin binaries from [GitHub Releases](https://github.com/opsorch/opsorch-orchestration-adapter/releases):

```dockerfile
FROM ghcr.io/opsorch/opsorch-core:latest
WORKDIR /opt/opsorch

ADD https://github.com/opsorch/opsorch-orchestration-adapter/releases/download/v0.1.0/orchestrationplugin-linux-amd64 ./plugins/orchestrationplugin
RUN chmod +x ./plugins/orchestrationplugin

ENV OPSORCH_ORCHESTRATION_PLUGIN=/opt/opsorch/plugins/orchestrationplugin
ENV OPSORCH_ORCHESTRATION_CONFIG='{"storagePath":"/opt/opsorch/data","sourceType":"local","localPath":"/opt/opsorch/runbooks","runner":"local"}'
```

## Development

### Prerequisites

- Go 1.24+
- Docker for local automated-step integration tests
- `kubectl` if you want to exercise the Kubernetes runner

### Building

```bash
make test
make build
make plugin
make integ
```

### Testing

**Unit tests:**
```bash
make test
```

**Integration tests:**

The integration harness runs a real end-to-end orchestration flow with:

- scoped plan retrieval
- scoped run retrieval
- manual step completion
- branching dependencies
- Docker-backed automated execution

Run it with:

```bash
make integ
```

## Examples

See [examples/README.md](/Users/yusufaytas/dev/OpsOrch/opsorch-orchestration-adapter/examples/README.md) and the YAML files in [examples](/Users/yusufaytas/dev/OpsOrch/opsorch-orchestration-adapter/examples).
