# ADR 001: Implement the experiment in a separate repository

## Status
Accepted

## Date
2026-04-02

## Context
The agent-assisted maintenance experiment needs a home. The two options are:

1. Build inside the Conduit repo (`github.com/ConduitIO/conduit`)
2. Build in a standalone repo (`github.com/mjhilldigital/conduit-agent-experiment`)

Key factors:

- **Ownership.** The Conduit repo is owned by `@ConduitIO/conduit-core`. We are not current maintainers and do not have merge access. Embedding experiment code in a repo we cannot merge to creates a hard dependency on external approval for every iteration.

- **Relationship.** The experiment operates ON Conduit (clones it, reads its files, generates patches, runs its tests). That is the relationship of a tool to a target, not a module to its parent. Embedding it would be like putting a code review tool inside the repo it reviews.

- **Lifecycle.** Conduit is a production streaming platform with CI, releases, dependabot, and strict code guidelines. The experiment is research software with a different stability bar, different dependencies (LLM client libraries), and a different audience. Coupling their lifecycles would create drag in both directions.

- **Talk credibility.** A separate tool that any maintainer could point at their own repo is a stronger demonstration than a bespoke integration wired into one specific codebase.

## Decision
The experiment lives in its own repository. Conduit is referenced as a read-only target via a configurable path. The experiment has no Go module dependency on `github.com/conduitio/conduit`. It interacts with Conduit by:

- Reading files from a local checkout
- Running shell commands (make, go test, golangci-lint) in an isolated worktree
- Never pushing or merging without explicit human action

## Consequences

### Positive
- We can iterate independently without gating on Conduit maintainer approval
- The experiment tooling is portable to other repos in future milestones
- Clear separation makes the governance model of the experiment self-contained
- Strengthens the talk narrative: this is an external tool, not a custom fork

### Negative
- No shared CI with Conduit; we must set up our own
- If Conduit's build or test commands change, we discover it at runtime rather than at compile time
- Two repos to manage instead of one

### Mitigations
- The experiment config file (`configs/experiment.yaml`) points to the Conduit checkout path, making it easy to update
- Worktree isolation per run means we never dirty the Conduit working tree
- Phase 1 scope (L1/L2 tasks) is unlikely to be affected by Conduit build changes
