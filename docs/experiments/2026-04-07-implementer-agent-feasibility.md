# Experiment: Implementer Agent Feasibility Test

**Date:** 2026-04-07
**Author:** William Hill
**Target:** ConduitIO/conduit

---

## Objective

Validate that the implementer agent can read triage output, clone the target repo, and dispatch a Claude Sonnet coding session via `BetaToolRunner` to produce a draft PR.

## Method

Built a Go implementer using `anthropic-sdk-go` with `BetaToolRunner` and 5 custom tools:
- `read_file`, `write_file`, `list_dir`, `search_files`, `run_command`

CLI orchestration: reads latest triage JSON → picks top issue → fetches full issue via `gh` → clones repo → runs agent → creates draft PR.

## Results

### Orchestration Verified

The full pipeline executed correctly up to the API call:

| Step | Status | Detail |
|------|--------|--------|
| Read triage output | PASS | Selected #1268 (score 63) |
| Fetch issue details | PASS | Retrieved via `gh issue view` |
| Clone repo | PASS | Shallow clone to temp dir |
| Create agent + tools | PASS | 5 tools wired to BetaToolRunner |
| API call | BLOCKED | Insufficient Anthropic credits |
| Draft PR | NOT REACHED | — |

### What Was Validated

- **Build passes**: `go build ./cmd/implementer/` compiles cleanly
- **Tests pass**: 13/13 tool tests pass including path traversal protection
- **Orchestration works**: triage → clone → agent setup runs without errors
- **API integration correct**: Got a clean 400 from Anthropic (auth works, billing issue)
- **Error reporting**: Clean error message surfaced to user

### Estimated Cost Per Run

Using Claude Sonnet 4.6 ($3/$15 per MTok):
- System prompt: ~500 tokens (cached after first call at 90% discount)
- Per iteration: ~2K input + ~1K output tokens
- 10-15 iterations typical: ~$0.15-0.30 per implementation run
- With Haiku 4.5 ($1/$5): ~$0.05-0.10 per run

## Next Steps

1. Add Anthropic credits and re-run the full integration test
2. Test against issue #1268 (docs — embedding guide) and #576 (Swagger error codes)
3. Evaluate output quality and iteration efficiency
4. Consider adding `maxBudgetUsd` parameter to cap cost per run
