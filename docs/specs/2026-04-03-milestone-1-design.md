# Milestone 1: Low-Risk Task Loop

## Goal

Extend the Milestone 0 CLI to execute a 4-stage agent pipeline: Archivist (LLM-enhanced dossier) -> Triage (policy-based) -> Verifier (command execution) -> Report. This validates the orchestration loop and command execution infrastructure without attempting code generation.

## Architecture

The `run` command currently loads a task, builds a keyword-based dossier, and writes a report. Milestone 1 inserts three agent stages between dossier generation and reporting:

1. **Archivist** (LLM) enhances the keyword-based dossier with smarter ranking and summarization
2. **Triage** (policy engine) accepts, rejects, or defers the task based on difficulty/blast radius
3. **Verifier** (command execution) runs validation commands against the target repo and captures results

The orchestrator chains these stages, updating the Run record at each step. If Triage rejects, the pipeline stops early. If the LLM call fails, the Archivist falls back to the M0 keyword-based dossier.

## LLM Integration

Use the `openai-go` SDK pointed at Gemini's OpenAI-compatible endpoint.

- Base URL: `https://generativelanguage.googleapis.com/v1beta/openai/`
- Auth: `GEMINI_API_KEY` env var
- Model: `gemini-2.5-flash` (configured per role in `configs/models.yaml`)
- Single completion method: `Complete(ctx, systemPrompt, userPrompt) (string, error)`

The LLM client reads provider config from `configs/models.yaml` and the API key from the environment. No provider abstraction interface needed -- the OpenAI SDK is the abstraction layer. Swapping providers later is a config change (base URL + key + model).

### Config format

```yaml
provider:
  base_url: "https://generativelanguage.googleapis.com/v1beta/openai/"

roles:
  archivist:
    model: "gemini-2.5-flash"
  triage:
    model: "gemini-2.5-flash"
  implementer:
    model: "gemini-2.5-flash"
  verifier:
    model: "gemini-2.5-flash"
  architect:
    model: "gemini-2.5-flash"
```

Only `archivist` is used in this milestone. Other roles remain configured for future use.

## Component Details

### LLM Client (`internal/llm/client.go`)

Thin wrapper around `openai-go` that loads config and provides a simple completion interface.

```go
type Client struct {
    client *openai.Client
    model  string
}

func NewClient(baseURL, apiKey, model string) *Client

func (c *Client) Complete(ctx context.Context, systemPrompt, userPrompt string) (string, error)
```

Returns the assistant message content as a string. Wraps errors with context. Tracks token usage in the response for logging.

### Models Config (`internal/config/config.go`)

Extend Config to load models.yaml:

```go
type ModelsConfig struct {
    Provider ProviderConfig         `mapstructure:"provider"`
    Roles    map[string]RoleConfig  `mapstructure:"roles"`
}

type ProviderConfig struct {
    BaseURL string `mapstructure:"base_url"`
}

type RoleConfig struct {
    Model string `mapstructure:"model"`
}
```

Add `LoadModels(path string) (ModelsConfig, error)` that reads `configs/models.yaml` and applies `GEMINI_API_KEY` from the environment.

### Archivist Agent (`internal/agents/archivist.go`)

Takes the keyword-based Dossier from M0 and enhances it via LLM.

**Input:** Task + keyword-based Dossier (from `retrieval.BuildDossier`)

**LLM prompt:** Sends the task description, the list of matched file paths (grouped by category), and the list of matched docs. Asks the LLM to return a JSON object with:
- `summary`: concise task summary (1-2 sentences)
- `relevant_files`: ranked list of the most relevant file paths (top 20)
- `relevant_docs`: ranked list of the most relevant doc paths
- `suggested_commands`: commands to validate the task
- `risks`: potential risks
- `open_questions`: unresolved questions

**Output:** Updated Dossier with LLM-enhanced fields replacing the keyword-based ones.

**Fallback:** If the LLM call fails (network error, parse error, timeout), log the error and return the original keyword-based dossier unchanged. The run continues.

### Triage Agent (`internal/agents/triage.go`)

No LLM. Uses the existing `Policy.CheckTask()` plus additional heuristic checks.

```go
type TriageDecision struct {
    Decision string `json:"decision"` // "accept", "reject", "defer"
    Reason   string `json:"reason"`
}

func Triage(task models.Task, dossier models.Dossier, policy orchestrator.Policy) TriageDecision
```

Decision logic:
- **Reject** if `policy.CheckTask(task)` returns an error (difficulty or blast radius exceeded)
- **Defer** if the dossier has open questions and no related files were found
- **Accept** otherwise

### Verifier Agent (`internal/agents/verifier.go`)

Runs commands from the dossier's `LikelyCommands` list against the target repo.

```go
type VerifierReport struct {
    Commands    []models.CommandLog `json:"commands"`
    OverallPass bool                `json:"overall_pass"`
    Summary     string              `json:"summary"`
}

func Verify(ctx context.Context, runner *execution.CommandRunner, dossier models.Dossier) VerifierReport
```

Iterates through `dossier.LikelyCommands`, runs each via the CommandRunner, collects results. `OverallPass` is true only if all commands exit 0. Summary is a one-line status like "3/3 commands passed" or "1/3 commands failed: golangci-lint".

### Command Runner (`internal/execution/command_runner.go`)

Executes shell commands with timeout and output capture.

```go
type CommandRunner struct {
    WorkDir        string
    TimeoutSeconds int
    UseWorktree    bool
    RepoPath       string
}

func NewCommandRunner(cfg config.Config) *CommandRunner

func (r *CommandRunner) Run(ctx context.Context, command string) models.CommandLog

func (r *CommandRunner) Setup() error    // creates worktree if configured
func (r *CommandRunner) Cleanup() error  // removes worktree if created
```

`Setup()` creates a git worktree from `RepoPath` into a temp directory if `UseWorktree` is true. `WorkDir` is set to the worktree path (or `RepoPath` if worktrees are disabled). `Cleanup()` removes the worktree.

Commands are executed via `exec.CommandContext` with a deadline derived from `TimeoutSeconds`. Stdout and stderr are captured separately. If the command times out, `ExitCode` is set to -1 and stderr includes a timeout message.

### Orchestrator Workflow (`internal/orchestrator/workflow.go`)

Chains the stages together. This is the main entry point called by the `run` CLI command.

```go
type WorkflowResult struct {
    Run            models.Run
    Dossier        models.Dossier
    TriageDecision agents.TriageDecision
    VerifierReport agents.VerifierReport
    LLMCalls       []LLMCall
}

type LLMCall struct {
    Agent    string        `json:"agent"`
    Model    string        `json:"model"`
    Prompt   string        `json:"prompt"`
    Response string        `json:"response"`
    Duration time.Duration `json:"duration"`
}

func RunWorkflow(ctx context.Context, task models.Task, cfg config.Config, modelsCfg config.ModelsConfig) (*WorkflowResult, error)
```

Flow:
1. Build keyword-based dossier (M0 code)
2. Archivist: enhance dossier via LLM
3. Triage: accept/reject/defer
4. If rejected/deferred: set run status, skip to reporting
5. Verifier: set up command runner, run commands, collect results
6. Clean up command runner
7. Return WorkflowResult with all artifacts

### Data Model Extensions

Add to `internal/models/run.go` or a new file:

```go
type LLMCall struct {
    Agent    string `json:"agent"`
    Model    string `json:"model"`
    Prompt   string `json:"prompt"`
    Response string `json:"response"`
    Duration string `json:"duration"`
}
```

Extend `Run`:
```go
type Run struct {
    // ... existing fields ...
    TriageDecision string   `json:"triage_decision,omitempty"`
    TriageReason   string   `json:"triage_reason,omitempty"`
    LLMCalls       []LLMCall `json:"llm_calls,omitempty"`
}
```

The `VerifierReport` is stored as part of `CommandsRun` (which already exists on `Run`) plus a new `VerifierSummary` field.

### Updated Reporting

Extend the markdown report template to include:

- **Triage Decision** section: decision + reason
- **Verification Results** section: per-command pass/fail table with exit codes, a summary line
- **LLM-Enhanced Summary**: the Archivist's summary replaces the mechanical M0 summary
- **LLM Calls** section: which agents called the LLM, which model, duration (prompts omitted from markdown for readability, but present in JSON)

### CLI Changes

The `run` command in `cmd/experiment/main.go` is refactored to call `orchestrator.RunWorkflow()` instead of inlining the pipeline. The CLI handles:
- Loading both config files (experiment.yaml + models.yaml)
- Checking for `GEMINI_API_KEY` and erroring early if missing
- Printing progress to stdout
- Writing output artifacts via the reporting package

### Error Handling

| Failure | Behavior |
|---------|----------|
| LLM call fails (network, timeout, parse) | Log error, fall back to M0 keyword dossier, continue run |
| Command times out | Record timeout in CommandLog (exit code -1), continue remaining commands |
| Worktree creation fails | Fall back to direct execution if `use_worktree` is true but fails; log warning |
| API key missing | Fail fast at CLI startup with clear error message |
| Triage rejects | Stop pipeline, record rejection reason, write report with what was collected |

### Testing Strategy

| Component | Test approach |
|-----------|--------------|
| LLM client | Mock HTTP server returning canned OpenAI-format responses |
| Archivist | Test prompt construction and JSON response parsing; mock LLM client |
| Triage | Test accept/reject/defer against various task + policy combinations |
| Verifier | Run simple commands (`echo hello`, `true`, `false`) in temp dirs |
| Command runner | Test timeout, output capture, exit codes; test worktree create/cleanup with a temp git repo |
| Orchestrator | Integration test: mock LLM server + temp repo, verify full pipeline produces expected artifacts |

### Files to Create or Modify

| File | Action |
|------|--------|
| `internal/llm/client.go` | Create |
| `internal/llm/client_test.go` | Create |
| `internal/agents/archivist.go` | Rewrite (currently stub) |
| `internal/agents/archivist_test.go` | Create |
| `internal/agents/triage.go` | Rewrite (currently stub) |
| `internal/agents/triage_test.go` | Create |
| `internal/agents/verifier.go` | Rewrite (currently stub) |
| `internal/agents/verifier_test.go` | Create |
| `internal/execution/command_runner.go` | Rewrite (currently stub) |
| `internal/execution/command_runner_test.go` | Create |
| `internal/orchestrator/workflow.go` | Rewrite (currently stub) |
| `internal/orchestrator/workflow_test.go` | Create |
| `internal/config/config.go` | Modify (add ModelsConfig) |
| `internal/config/config_test.go` | Modify (add ModelsConfig tests) |
| `internal/models/run.go` | Modify (add LLMCall, triage fields) |
| `internal/reporting/markdown_report.go` | Modify (new sections) |
| `internal/reporting/markdown_report_test.go` | Modify (test new sections) |
| `cmd/experiment/main.go` | Modify (use orchestrator workflow) |
| `configs/models.yaml` | Modify (add provider.base_url) |
| `.env.example` | Modify (add GEMINI_API_KEY) |
