# Example Plans

This directory contains canonical plan YAML for the orchestration adapter.

Each file uses the supported schema:

```yaml
id: example-plan
service: api
team: platform
environment: production
title: Example Plan
description: Example description
version: "1.0"
tags:
  type: example

steps:
  - id: first
    title: First Step
    type: manual
    reasoning: Operator instructions

  - id: second
    title: Automated Step
    type: automated
    dependsOn: [first]
    exec:
      image: ghcr.io/org/tool:latest
      args: ["run"]
```

## Notes

- `dependsOn` controls step ordering.
- multiple steps may become `ready` at the same time if they share satisfied dependencies.
- manual steps are completed by an operator through the API.
- automated steps are executed by the configured runner.

## Files

- `deployment-checklist.yaml`: simple production deployment runbook
- `deployment-with-rollback.yaml`: larger deployment flow with rollback-oriented dependencies
- `incident-response.yaml`: standard incident response checklist
- `incident-triage.yaml`: deeper incident investigation flow
- `maintenance-procedure.yaml`: scheduled maintenance checklist
- `multi-environment-release.yaml`: multi-stage release workflow
