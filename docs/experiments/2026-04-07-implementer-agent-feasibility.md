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

## Live Run: Issue #576 (Error codes in Swagger)

After adding credits and iterating on the pipeline, a successful end-to-end run was completed.

### Pipeline
```text
triage (Gemini Flash) → archivist (Gemini Flash, single call) → planner (Gemini Flash, Markdown) → reviewer (Gemini Flash, JSON) → implementer (Haiku 4.5, 15 iterations)
```

### Results

| Step | Time | Detail |
|------|------|--------|
| Archivist | 21s | Found 7 relevant files via grep + single Gemini call |
| Planner | 64s | Produced 29K char Markdown implementation plan |
| Reviewer | 19s | Approved the plan |
| Implementer | 62s | 15 iterations: 4 write_file, 2 run_command, 9 read/search |
| **Total** | **~3 min** | **Draft PR created** |

### Output
- **PR:** https://github.com/ConduitIO/conduit/pull/2451
- **Files changed:** 3 (status.go, errors.go, service.go)
- **Lines changed:** +73 / -39
- **Estimated cost:** ~$0.06 total (~$0.005 Gemini Flash + ~$0.05 Haiku)

### Key Learnings

1. **Gemini Flash cannot reliably follow tool-calling instructions** — the ADK Go agent loop failed repeatedly (archivist never called save_dossier). Replacing with deterministic Go code (grep) + single LLM call was the fix.
2. **JSON output with code content is unreliable** — even with Gemini's JSON mode, Go source code breaks JSON encoding. Markdown is the right format for plans containing code.
3. **Haiku needs extremely aggressive prompts** — without explicit "write by iteration 3" instructions, it explores endlessly even with pre-provided context.
4. **The archivist→planner→reviewer→implementer pipeline works** — cheap Gemini Flash does all the thinking ($0.005), expensive Anthropic does the mechanical writing ($0.05).
5. **Prompt caching on the implementer is critical** — the plan + system prompt are marked cacheable, so subsequent iterations cost 10% of input price.

## Next Steps

1. Evaluate the quality of PR #2451 against the actual issue requirements
2. Test against more issues (different categories, difficulties)
3. Add cost tracking (token usage per step)
4. Consider Sonnet for the implementer when Haiku's output quality is insufficient
