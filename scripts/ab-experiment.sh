#!/bin/bash
# ab-experiment.sh — Run the same seeded tasks N times under both implementer
# backends (anthropic baseline vs aider+openrouter). Collects run-summary.json
# files into data/ab-runs/<arm>/<task>/run-<iter>/ for later analysis.
#
# Usage:
#   ./scripts/ab-experiment.sh <iterations_per_arm>
#
# Required env vars:
#   ANTHROPIC_API_KEY      — for arm A (anthropic)
#   OPENROUTER_API_KEY     — for arm B (aider)
#   GH_TOKEN               — for gh CLI used by the implementer
#   IMPL_REPO_OWNER        — target repo owner
#   IMPL_REPO_NAME         — target repo name
#   IMPL_FORK_OWNER        — your fork owner
#
# Optional:
#   IMPL_AIDER_MODEL       — override default Qwen3 Coder free model ID

set -euo pipefail

ITERS="${1:-3}"
TASKS=(576 645)  # gh-576 HTTP status codes, gh-645 version constant
BASE_DIR="data/ab-runs"
TIMESTAMP="$(date -u +%Y%m%dT%H%M%SZ)"

mkdir -p "$BASE_DIR"

run_one() {
  local backend="$1"
  local issue="$2"
  local iter="$3"
  local out_dir="$BASE_DIR/$backend/task-gh-$issue/run-$iter-$TIMESTAMP"

  mkdir -p "$out_dir"
  echo "=== $backend / task-gh-$issue / iter $iter ==="

  IMPL_BACKEND="$backend" \
  IMPL_ISSUE_NUMBER="$issue" \
  IMPL_ARTIFACT_DIR="$out_dir" \
    go run ./cmd/implementer 2>&1 | tee "$out_dir/stdout.log" || {
      echo "run failed — continuing"
      echo "{\"error\":\"run failed\"}" > "$out_dir/run-summary.json"
    }
}

for task in "${TASKS[@]}"; do
  for iter in $(seq 1 "$ITERS"); do
    run_one "anthropic" "$task" "$iter"
    run_one "aider" "$task" "$iter"
  done
done

echo
echo "All runs complete. Analyze with: go run ./cmd/ab-analyze $BASE_DIR"
