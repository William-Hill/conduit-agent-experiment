# Pipeline Dashboard Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build a static GitHub Pages dashboard that displays pipeline run history, cost trends, token usage, and live run status, with the ability to trigger new runs.

**Architecture:** Single HTML file with inline CSS/JS served via GitHub Pages. Data comes from a `data.json` file committed by an aggregation workflow that collects CI artifacts. Live features (trigger, status polling) use the GitHub API with a user-provided PAT stored in localStorage.

**Tech Stack:** HTML, vanilla JS, Chart.js (CDN), GitHub Actions, GitHub Pages, GitHub REST API.

**Spec:** `docs/specs/2026-04-09-pipeline-dashboard-design.md`

---

## File Structure

```
docs/dashboard/
  index.html                      # Single-file dashboard (HTML + CSS + JS inline)
  data.json                       # Aggregated run data (seeded, then updated by CI)
.github/workflows/
  implement.yml                   # Existing — already uploads run-summary.json artifact
  aggregate-dashboard.yml         # New — collects artifacts, enriches, writes data.json
```

---

### Task 1: Seed data.json with real run data

**Files:**
- Create: `docs/dashboard/data.json`

We need initial data so the dashboard has something to render. Use the real artifact from CI run 24167191811.

- [ ] **Step 1: Create the dashboard directory**

```bash
mkdir -p docs/dashboard
```

- [ ] **Step 2: Write data.json seeded with the real CI run**

Create `docs/dashboard/data.json`:

```json
{
  "runs": [
    {
      "run_id": "24167191811",
      "issue_number": 576,
      "issue_title": "Error codes needs to be documented in Swagger",
      "model": "claude-haiku-4-5-20251001",
      "iterations": 15,
      "input_tokens": 205097,
      "output_tokens": 15184,
      "cache_creation_tokens": 8582,
      "cache_read_tokens": 128730,
      "estimated_cost_usd": 0.281017,
      "budget_exceeded": false,
      "pr_url": "https://github.com/ConduitIO/conduit/pull/2455",
      "summary": "Implemented error code documentation in Swagger for Conduit API endpoints",
      "plan_chars": 24215,
      "timestamp": "2026-04-09T01:20:01Z",
      "status": "success",
      "duration_seconds": 464
    }
  ],
  "updated_at": "2026-04-09T01:30:00Z"
}
```

- [ ] **Step 3: Verify JSON is valid**

```bash
python3 -c "import json; json.load(open('docs/dashboard/data.json')); print('OK')"
```

Expected: `OK`

- [ ] **Step 4: Commit**

```bash
git add docs/dashboard/data.json
git commit -m "feat(dashboard): seed data.json with first CI run data"
```

---

### Task 2: Build the dashboard HTML — static sections (Header, KPIs, Cost Chart, Token Usage, Run History)

**Files:**
- Create: `docs/dashboard/index.html`

This is the main deliverable — a single HTML file with all CSS and JS inline. This task covers the five static, data-driven sections. The Pipeline Control section (live features) is Task 3.

- [ ] **Step 1: Create index.html with full static dashboard**

Create `docs/dashboard/index.html` with the following content. This is a large file — all CSS, HTML structure, and JS are inline per the spec's "single HTML file" requirement.

```html
<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<title>conduit-agent-experiment dashboard</title>
<script src="https://cdn.jsdelivr.net/npm/chart.js@4"></script>
<style>
  *, *::before, *::after { box-sizing: border-box; margin: 0; padding: 0; }

  :root {
    --green: #4ecca3;
    --blue: #7aa2f7;
    --purple: #a78bfa;
    --amber: #e8b931;
    --red: #f7768e;
    --bg: #0f0f1a;
    --bg-card: #1a1a2e;
    --border: #2a2a4a;
    --text: #e0e0e0;
    --text-secondary: #888888;
    --text-muted: #555555;
  }

  body {
    font-family: -apple-system, BlinkMacSystemFont, 'SF Mono', 'Fira Code', 'Cascadia Code', monospace;
    background: var(--bg);
    color: var(--text);
    line-height: 1.5;
    padding: 32px;
    max-width: 1200px;
    margin: 0 auto;
  }

  /* Header */
  .header { margin-bottom: 32px; display: flex; justify-content: space-between; align-items: flex-start; }
  .header h1 { font-size: 20px; font-weight: 700; color: #fff; }
  .header .subtitle { font-size: 12px; color: var(--text-muted); margin-top: 4px; }
  .header .settings-btn {
    background: var(--bg-card); border: 1px solid var(--border); border-radius: 6px;
    color: var(--text-secondary); padding: 6px 10px; cursor: pointer; font-size: 14px;
  }
  .header .settings-btn:hover { border-color: var(--text-secondary); }

  /* Section labels */
  .section-label {
    font-size: 11px; text-transform: uppercase; letter-spacing: 1.5px;
    color: var(--green); margin-bottom: 10px; font-weight: 600;
  }

  /* KPI cards */
  .kpi-row { display: flex; gap: 12px; margin-bottom: 32px; }
  .kpi-card {
    flex: 1; background: var(--bg-card); border: 1px solid var(--border);
    border-radius: 8px; padding: 16px;
  }
  .kpi-value { font-size: 28px; font-weight: 700; }
  .kpi-label { font-size: 11px; color: var(--text-secondary); margin-top: 2px; }

  /* Chart container */
  .chart-container {
    background: var(--bg-card); border: 1px solid var(--border);
    border-radius: 8px; padding: 20px; margin-bottom: 32px;
  }
  .chart-container canvas { max-height: 200px; }

  /* Token usage bars */
  .token-section { margin-bottom: 32px; }
  .token-bars {
    background: var(--bg-card); border: 1px solid var(--border);
    border-radius: 8px; padding: 20px; display: flex; gap: 20px;
  }
  .token-bar-item { flex: 1; }
  .token-bar-label { font-size: 10px; color: var(--text-secondary); margin-bottom: 6px; }
  .token-bar-track { background: var(--border); border-radius: 4px; height: 10px; overflow: hidden; }
  .token-bar-fill { height: 100%; border-radius: 4px; transition: width 0.5s ease; }
  .token-bar-value { font-size: 11px; color: var(--text-secondary); margin-top: 4px; }

  /* Run history table */
  .run-table {
    background: var(--bg-card); border: 1px solid var(--border);
    border-radius: 8px; overflow: hidden; margin-bottom: 32px;
  }
  .run-table-header, .run-table-row {
    display: flex; padding: 10px 16px; align-items: center;
  }
  .run-table-header {
    font-size: 10px; color: var(--text-muted); text-transform: uppercase;
    letter-spacing: 0.5px; border-bottom: 1px solid var(--border);
  }
  .run-table-row { font-size: 12px; border-bottom: 1px solid #1f1f35; }
  .run-table-row:last-child { border-bottom: none; }
  .run-table-row:hover { background: #1f1f35; }
  .col-date { flex: 0 0 80px; color: var(--text-secondary); }
  .col-issue { flex: 1; min-width: 0; }
  .col-issue span { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; display: block; }
  .col-status { flex: 0 0 90px; }
  .col-iter { flex: 0 0 70px; text-align: center; }
  .col-cost { flex: 0 0 80px; text-align: right; }
  .col-model { flex: 0 0 90px; color: var(--text-secondary); }
  .col-pr { flex: 0 0 70px; text-align: right; }
  .col-pr a { color: var(--blue); text-decoration: none; }
  .col-pr a:hover { text-decoration: underline; }

  /* Status badges */
  .badge {
    display: inline-block; padding: 2px 10px; border-radius: 10px;
    font-size: 10px; font-weight: 600;
  }
  .badge-success { background: #1a3a2a; color: var(--green); }
  .badge-failed { background: #3a1a1a; color: var(--red); }
  .badge-budget { background: #3a351a; color: var(--amber); }

  /* Pipeline control */
  .pipeline-control {
    background: var(--bg-card); border: 1px solid var(--border);
    border-radius: 8px; padding: 20px; margin-bottom: 32px;
  }
  .control-idle { display: flex; justify-content: space-between; align-items: center; }
  .control-idle-info { }
  .control-idle-info .label { font-size: 13px; color: var(--text-secondary); }
  .control-idle-info .sublabel { font-size: 10px; color: var(--text-muted); margin-top: 4px; }
  .control-form { display: flex; gap: 8px; align-items: center; }
  .control-form input, .control-form select {
    background: #16213e; border: 1px solid var(--border); border-radius: 6px;
    padding: 8px 12px; color: var(--text-secondary); font-size: 11px;
    font-family: inherit;
  }
  .control-form input { width: 100px; }
  .control-form input::placeholder { color: var(--text-muted); }
  .btn-run {
    background: var(--green); color: var(--bg); padding: 8px 16px; border-radius: 6px;
    font-size: 11px; font-weight: 600; cursor: pointer; border: none;
    font-family: inherit;
  }
  .btn-run:hover { opacity: 0.9; }
  .btn-run:disabled { opacity: 0.4; cursor: not-allowed; }

  /* Active run */
  .control-active { }
  .control-active-header { display: flex; align-items: center; gap: 8px; margin-bottom: 16px; }
  .pulse-dot {
    width: 8px; height: 8px; border-radius: 50%;
    animation: pulse 1.5s ease-in-out infinite;
  }
  @keyframes pulse {
    0%, 100% { opacity: 1; }
    50% { opacity: 0.4; }
  }
  .pulse-dot.amber { background: var(--amber); box-shadow: 0 0 8px var(--amber); }
  .pulse-dot.green { background: var(--green); box-shadow: 0 0 8px var(--green); }
  .control-status-text { font-size: 13px; font-weight: 600; }
  .control-meta { font-size: 11px; color: var(--text-muted); }
  .control-logs { margin-left: auto; font-size: 10px; color: var(--text-muted); text-decoration: underline; cursor: pointer; }
  .control-logs:hover { color: var(--text-secondary); }

  /* Step pipeline visualization */
  .pipeline-steps { display: flex; gap: 4px; align-items: center; }
  .pipeline-step {
    flex: 1; text-align: center; border-radius: 6px; padding: 10px 4px;
  }
  .pipeline-step.done { background: #1a3a2a; border: 1px solid rgba(78,204,163,0.4); }
  .pipeline-step.active { background: #2a2a1a; border: 1px solid var(--amber); box-shadow: 0 0 12px rgba(232,185,49,0.15); }
  .pipeline-step.pending { background: #16213e; border: 1px solid var(--border); }
  .pipeline-step .step-icon { font-size: 14px; margin-bottom: 2px; }
  .pipeline-step .step-name { font-size: 9px; }
  .pipeline-step.done .step-name { color: rgba(78,204,163,0.7); }
  .pipeline-step.active .step-name { color: var(--amber); }
  .pipeline-step.pending .step-name { color: var(--text-muted); }
  .pipeline-step .step-detail { font-size: 8px; color: var(--text-muted); }
  .pipeline-arrow { color: var(--border); font-size: 10px; }

  /* Completed run summary */
  .control-completed { }
  .control-completed-header { display: flex; align-items: center; gap: 8px; margin-bottom: 16px; }
  .btn-pr {
    margin-left: auto; background: #16213e; border: 1px solid var(--blue);
    color: var(--blue); padding: 6px 12px; border-radius: 6px;
    font-size: 11px; cursor: pointer; text-decoration: none; font-family: inherit;
  }
  .btn-pr:hover { background: #1a2a4e; }

  /* Token modal */
  .modal-overlay {
    display: none; position: fixed; top: 0; left: 0; right: 0; bottom: 0;
    background: rgba(0,0,0,0.7); z-index: 100; justify-content: center; align-items: center;
  }
  .modal-overlay.visible { display: flex; }
  .modal {
    background: var(--bg-card); border: 1px solid var(--border); border-radius: 12px;
    padding: 24px; width: 420px; max-width: 90vw;
  }
  .modal h2 { font-size: 16px; margin-bottom: 8px; }
  .modal p { font-size: 12px; color: var(--text-secondary); margin-bottom: 16px; line-height: 1.6; }
  .modal input[type="password"] {
    width: 100%; background: #16213e; border: 1px solid var(--border); border-radius: 6px;
    padding: 10px 12px; color: var(--text); font-size: 12px; font-family: inherit;
    margin-bottom: 16px;
  }
  .modal-actions { display: flex; gap: 8px; justify-content: flex-end; }
  .modal-actions button {
    padding: 8px 16px; border-radius: 6px; font-size: 12px; cursor: pointer;
    border: none; font-family: inherit;
  }
  .btn-save { background: var(--green); color: var(--bg); font-weight: 600; }
  .btn-skip { background: var(--border); color: var(--text-secondary); }
  .btn-clear { background: var(--border); color: var(--red); }

  /* No-token notice */
  .no-token-notice {
    font-size: 12px; color: var(--text-muted); text-align: center; padding: 16px;
    cursor: pointer;
  }
  .no-token-notice:hover { color: var(--text-secondary); }
</style>
</head>
<body>

<!-- Header -->
<div class="header">
  <div>
    <h1>conduit-agent-experiment</h1>
    <div class="subtitle">Autonomous pipeline dashboard &middot; Last updated: <span id="last-updated">-</span></div>
  </div>
  <button class="settings-btn" onclick="openModal()" title="Configure GitHub token">&#9881;</button>
</div>

<!-- KPIs -->
<div class="section-label">Overview</div>
<div class="kpi-row">
  <div class="kpi-card"><div class="kpi-value" id="kpi-runs" style="color:var(--green)">-</div><div class="kpi-label">Total Runs</div></div>
  <div class="kpi-card"><div class="kpi-value" id="kpi-cost" style="color:var(--green)">-</div><div class="kpi-label">Total Cost</div></div>
  <div class="kpi-card"><div class="kpi-value" id="kpi-success" style="color:var(--green)">-</div><div class="kpi-label">Success Rate</div></div>
  <div class="kpi-card"><div class="kpi-value" id="kpi-prs" style="color:var(--blue)">-</div><div class="kpi-label">PRs Created</div></div>
  <div class="kpi-card"><div class="kpi-value" id="kpi-iters" style="color:var(--text)">-</div><div class="kpi-label">Avg Iterations</div></div>
</div>

<!-- Pipeline Control -->
<div class="section-label">Pipeline Control</div>
<div class="pipeline-control" id="pipeline-control">
  <!-- Populated by JS -->
</div>

<!-- Cost Chart -->
<div class="section-label">Cost per Run</div>
<div class="chart-container">
  <canvas id="cost-chart"></canvas>
</div>

<!-- Token Usage -->
<div class="section-label">Token Usage (Latest Run)</div>
<div class="token-section">
  <div class="token-bars" id="token-bars">
    <!-- Populated by JS -->
  </div>
</div>

<!-- Run History -->
<div class="section-label">Run History</div>
<div class="run-table">
  <div class="run-table-header">
    <div class="col-date">Date</div>
    <div class="col-issue">Issue</div>
    <div class="col-status">Status</div>
    <div class="col-iter">Iters</div>
    <div class="col-cost">Cost</div>
    <div class="col-model">Model</div>
    <div class="col-pr">PR</div>
  </div>
  <div id="run-table-body">
    <!-- Populated by JS -->
  </div>
</div>

<!-- Token Modal -->
<div class="modal-overlay" id="token-modal">
  <div class="modal">
    <h2>GitHub Personal Access Token</h2>
    <p>
      Enables: trigger pipeline runs, view live run status.<br>
      Token is stored in your browser only &mdash; never sent to any server except api.github.com.
    </p>
    <input type="password" id="token-input" placeholder="ghp_xxxxxxxxxxxx">
    <div class="modal-actions">
      <button class="btn-clear" id="btn-clear-token" onclick="clearToken()" style="display:none">Clear token</button>
      <button class="btn-skip" onclick="closeModal()">Skip &mdash; read-only</button>
      <button class="btn-save" onclick="saveToken()">Save</button>
    </div>
  </div>
</div>

<script>
// ── Config ──────────────────────────────────────────────────────────
const OWNER = 'William-Hill';
const REPO = 'conduit-agent-experiment';
const WORKFLOW_FILE = 'implement.yml';
const POLL_INTERVAL = 15000;

// ── State ───────────────────────────────────────────────────────────
let dashboardData = { runs: [], updated_at: '' };
let costChart = null;
let pollTimer = null;
let activeRun = null;

// ── Init ────────────────────────────────────────────────────────────
async function init() {
  try {
    const resp = await fetch('data.json');
    dashboardData = await resp.json();
  } catch (e) {
    console.warn('Could not load data.json, using empty state:', e);
  }
  renderKPIs();
  renderCostChart();
  renderTokenUsage();
  renderRunHistory();
  renderPipelineControl();
  document.getElementById('last-updated').textContent = dashboardData.updated_at
    ? new Date(dashboardData.updated_at).toLocaleString()
    : '-';

  if (getToken()) startPolling();
}

// ── Token Management ────────────────────────────────────────────────
function getToken() { return localStorage.getItem('gh_token') || ''; }
function openModal() {
  document.getElementById('token-modal').classList.add('visible');
  document.getElementById('token-input').value = '';
  document.getElementById('btn-clear-token').style.display = getToken() ? 'inline-block' : 'none';
}
function closeModal() { document.getElementById('token-modal').classList.remove('visible'); }
function saveToken() {
  const val = document.getElementById('token-input').value.trim();
  if (val) {
    localStorage.setItem('gh_token', val);
    closeModal();
    renderPipelineControl();
    startPolling();
  }
}
function clearToken() {
  localStorage.removeItem('gh_token');
  closeModal();
  stopPolling();
  activeRun = null;
  renderPipelineControl();
}

// ── GitHub API ──────────────────────────────────────────────────────
async function ghApi(path, options = {}) {
  const token = getToken();
  if (!token) return null;
  const resp = await fetch(`https://api.github.com/${path}`, {
    ...options,
    headers: {
      'Authorization': `Bearer ${token}`,
      'Accept': 'application/vnd.github+json',
      ...(options.headers || {}),
    },
  });
  if (!resp.ok) {
    console.error(`GitHub API ${resp.status}:`, await resp.text());
    return null;
  }
  if (resp.status === 204) return {};
  return resp.json();
}

async function triggerRun(issueNumber, hitlMode) {
  const body = { ref: 'main', inputs: { hitl_mode: hitlMode } };
  if (issueNumber) body.inputs.issue_number = String(issueNumber);
  const result = await ghApi(`repos/${OWNER}/${REPO}/actions/workflows/${WORKFLOW_FILE}/dispatches`, {
    method: 'POST',
    body: JSON.stringify(body),
  });
  if (result !== null) {
    // Dispatch accepted — start polling for the new run
    setTimeout(() => pollActiveRun(), 3000);
  }
}

async function pollActiveRun() {
  const data = await ghApi(`repos/${OWNER}/${REPO}/actions/workflows/${WORKFLOW_FILE}/runs?status=in_progress&per_page=1`);
  if (data && data.workflow_runs && data.workflow_runs.length > 0) {
    const run = data.workflow_runs[0];
    activeRun = {
      id: run.id,
      status: run.status,
      conclusion: run.conclusion,
      started_at: run.run_started_at || run.created_at,
      html_url: run.html_url,
    };
  } else {
    // Check if the most recent run just completed
    const recent = await ghApi(`repos/${OWNER}/${REPO}/actions/workflows/${WORKFLOW_FILE}/runs?per_page=1`);
    if (recent && recent.workflow_runs && recent.workflow_runs.length > 0) {
      const run = recent.workflow_runs[0];
      if (run.status === 'completed' && activeRun && activeRun.id === run.id) {
        activeRun = {
          id: run.id,
          status: 'completed',
          conclusion: run.conclusion,
          started_at: run.run_started_at || run.created_at,
          completed_at: run.updated_at,
          html_url: run.html_url,
        };
      } else {
        activeRun = null;
      }
    } else {
      activeRun = null;
    }
  }
  renderPipelineControl();
}

function startPolling() {
  stopPolling();
  pollActiveRun();
  pollTimer = setInterval(pollActiveRun, POLL_INTERVAL);
}
function stopPolling() {
  if (pollTimer) { clearInterval(pollTimer); pollTimer = null; }
}

// ── KPIs ────────────────────────────────────────────────────────────
function renderKPIs() {
  const runs = dashboardData.runs;
  const total = runs.length;
  const totalCost = runs.reduce((s, r) => s + r.estimated_cost_usd, 0);
  const successes = runs.filter(r => r.status === 'success').length;
  const rate = total > 0 ? Math.round((successes / total) * 100) : 0;
  const prs = runs.filter(r => r.pr_url).length;
  const avgIter = total > 0 ? Math.round(runs.reduce((s, r) => s + r.iterations, 0) / total) : 0;

  document.getElementById('kpi-runs').textContent = total;
  document.getElementById('kpi-cost').textContent = '$' + totalCost.toFixed(2);

  const successEl = document.getElementById('kpi-success');
  successEl.textContent = rate + '%';
  if (rate >= 80) successEl.style.color = 'var(--green)';
  else if (rate >= 50) successEl.style.color = 'var(--amber)';
  else successEl.style.color = 'var(--red)';

  document.getElementById('kpi-prs').textContent = prs;
  document.getElementById('kpi-iters').textContent = avgIter;
}

// ── Cost Chart ──────────────────────────────────────────────────────
function renderCostChart() {
  const runs = [...dashboardData.runs].sort((a, b) => new Date(a.timestamp) - new Date(b.timestamp));
  const labels = runs.map(r => {
    const d = new Date(r.timestamp);
    return d.toLocaleDateString('en-US', { month: 'short', day: 'numeric' });
  });
  const data = runs.map(r => r.estimated_cost_usd);
  const colors = runs.map(r =>
    r.status === 'success' ? '#4ecca3' :
    r.budget_exceeded ? '#e8b931' : '#f7768e'
  );

  const ctx = document.getElementById('cost-chart').getContext('2d');
  if (costChart) costChart.destroy();
  costChart = new Chart(ctx, {
    type: 'bar',
    data: {
      labels,
      datasets: [{
        label: 'Cost (USD)',
        data,
        backgroundColor: colors,
        borderRadius: 4,
        barPercentage: 0.6,
      }],
    },
    options: {
      responsive: true,
      maintainAspectRatio: false,
      plugins: {
        legend: { display: false },
        tooltip: {
          callbacks: {
            title: (items) => {
              const run = runs[items[0].dataIndex];
              return `#${run.issue_number}: ${run.issue_title}`;
            },
            label: (item) => {
              const run = runs[item.dataIndex];
              return `$${run.estimated_cost_usd.toFixed(4)} · ${run.iterations} iters`;
            },
          },
        },
        annotation: {
          annotations: {
            baseline: {
              type: 'line', yMin: 0.06, yMax: 0.06,
              borderColor: 'rgba(136,136,136,0.4)', borderDash: [6, 4], borderWidth: 1,
              label: { display: true, content: '$0.06 baseline', position: 'end',
                       color: '#888', font: { size: 10 }, backgroundColor: 'transparent' },
            },
          },
        },
      },
      scales: {
        x: {
          ticks: { color: '#888', font: { size: 10 } },
          grid: { display: false },
        },
        y: {
          ticks: { color: '#888', font: { size: 10 }, callback: v => '$' + v.toFixed(2) },
          grid: { color: '#1f1f35' },
        },
      },
    },
  });
}

// ── Token Usage ─────────────────────────────────────────────────────
function renderTokenUsage() {
  const runs = dashboardData.runs;
  if (runs.length === 0) {
    document.getElementById('token-bars').innerHTML = '<div style="color:var(--text-muted);padding:8px;">No run data</div>';
    return;
  }
  const latest = runs[runs.length - 1];
  const maxVal = Math.max(latest.input_tokens, latest.output_tokens, latest.cache_read_tokens, latest.cache_creation_tokens);
  const items = [
    { label: 'Input', value: latest.input_tokens, color: 'var(--blue)' },
    { label: 'Output', value: latest.output_tokens, color: 'var(--purple)' },
    { label: 'Cache Read', value: latest.cache_read_tokens, color: 'var(--green)' },
    { label: 'Cache Create', value: latest.cache_creation_tokens, color: 'var(--amber)' },
  ];
  document.getElementById('token-bars').innerHTML = items.map(item => `
    <div class="token-bar-item">
      <div class="token-bar-label">${item.label}: ${item.value.toLocaleString()}</div>
      <div class="token-bar-track">
        <div class="token-bar-fill" style="width:${(item.value / maxVal * 100).toFixed(1)}%;background:${item.color}"></div>
      </div>
    </div>
  `).join('');
}

// ── Run History ─────────────────────────────────────────────────────
function renderRunHistory() {
  const runs = [...dashboardData.runs].sort((a, b) => new Date(b.timestamp) - new Date(a.timestamp));
  const body = document.getElementById('run-table-body');
  if (runs.length === 0) {
    body.innerHTML = '<div style="padding:16px;color:var(--text-muted);text-align:center;">No runs yet</div>';
    return;
  }
  body.innerHTML = runs.map(r => {
    const date = new Date(r.timestamp).toLocaleDateString('en-US', { month: 'short', day: 'numeric' });
    const badgeClass = r.budget_exceeded ? 'badge-budget' : r.status === 'success' ? 'badge-success' : 'badge-failed';
    const badgeText = r.budget_exceeded ? 'budget' : r.status;
    const model = r.model.includes('haiku') ? 'Haiku 4.5' : r.model.includes('sonnet') ? 'Sonnet' : r.model;
    const prLink = r.pr_url
      ? `<a href="${r.pr_url}" target="_blank">#${r.pr_url.split('/').pop()}</a>`
      : '-';
    return `<div class="run-table-row">
      <div class="col-date">${date}</div>
      <div class="col-issue"><span>#${r.issue_number} ${r.issue_title}</span></div>
      <div class="col-status"><span class="badge ${badgeClass}">${badgeText}</span></div>
      <div class="col-iter">${r.iterations}</div>
      <div class="col-cost">$${r.estimated_cost_usd.toFixed(2)}</div>
      <div class="col-model">${model}</div>
      <div class="col-pr">${prLink}</div>
    </div>`;
  }).join('');
}

// ── Pipeline Control ────────────────────────────────────────────────
function renderPipelineControl() {
  const el = document.getElementById('pipeline-control');
  const token = getToken();

  if (!token) {
    el.innerHTML = '<div class="no-token-notice" onclick="openModal()">Configure GitHub token to enable pipeline control and live status</div>';
    return;
  }

  if (activeRun && activeRun.status === 'in_progress') {
    const elapsed = Math.round((Date.now() - new Date(activeRun.started_at).getTime()) / 1000);
    const mins = Math.floor(elapsed / 60);
    const secs = elapsed % 60;
    el.innerHTML = `
      <div class="control-active">
        <div class="control-active-header">
          <div class="pulse-dot amber"></div>
          <div class="control-status-text" style="color:var(--amber)">Running</div>
          <div class="control-meta">${mins}m ${secs}s elapsed</div>
          <a class="control-logs" href="${activeRun.html_url}" target="_blank">View logs &rarr;</a>
        </div>
        <div class="pipeline-steps">
          ${renderSteps('running')}
        </div>
      </div>`;
    return;
  }

  if (activeRun && activeRun.status === 'completed') {
    const icon = activeRun.conclusion === 'success' ? 'green' : 'red';
    const label = activeRun.conclusion === 'success' ? 'Completed' : 'Failed';
    const color = activeRun.conclusion === 'success' ? 'var(--green)' : 'var(--red)';
    el.innerHTML = `
      <div class="control-completed">
        <div class="control-completed-header">
          <div class="pulse-dot ${icon}" style="animation:none"></div>
          <div class="control-status-text" style="color:${color}">${label}</div>
          <a class="control-logs" href="${activeRun.html_url}" target="_blank">View logs &rarr;</a>
        </div>
        <div class="pipeline-steps">
          ${renderSteps(activeRun.conclusion === 'success' ? 'done' : 'failed')}
        </div>
      </div>`;
    setTimeout(() => { activeRun = null; renderPipelineControl(); }, 30000);
    return;
  }

  // Idle state
  const lastRun = dashboardData.runs.length > 0
    ? dashboardData.runs[dashboardData.runs.length - 1]
    : null;
  const lastInfo = lastRun
    ? `Last run: ${new Date(lastRun.timestamp).toLocaleDateString('en-US', {month:'short',day:'numeric'})} &mdash; #${lastRun.issue_number} &mdash; ${lastRun.status}`
    : 'No previous runs';
  el.innerHTML = `
    <div class="control-idle">
      <div class="control-idle-info">
        <div class="label">No active runs</div>
        <div class="sublabel">${lastInfo}</div>
      </div>
      <div class="control-form">
        <input type="text" id="trigger-issue" placeholder="#issue">
        <select id="trigger-mode">
          <option value="yolo">yolo</option>
          <option value="full">full</option>
        </select>
        <button class="btn-run" id="btn-trigger" onclick="handleTrigger()">Run Pipeline</button>
      </div>
    </div>`;
}

function renderSteps(state) {
  const steps = ['Clone', 'Archivist', 'Planner', 'Reviewer', 'Implementer', 'Push', 'Draft PR'];
  return steps.map((name, i) => {
    let cls, icon;
    if (state === 'done') { cls = 'done'; icon = '&#10003;'; }
    else if (state === 'failed') { cls = i < steps.length - 1 ? 'done' : 'pending'; icon = i < steps.length - 1 ? '&#10003;' : '&#10007;'; }
    else if (state === 'running') { cls = 'active'; icon = '&#9679;'; }
    else { cls = 'pending'; icon = '&#9711;'; }
    return `${i > 0 ? '<div class="pipeline-arrow">&rarr;</div>' : ''}
      <div class="pipeline-step ${cls}">
        <div class="step-icon">${icon}</div>
        <div class="step-name">${name}</div>
      </div>`;
  }).join('');
}

async function handleTrigger() {
  const btn = document.getElementById('btn-trigger');
  const issue = document.getElementById('trigger-issue').value.replace('#', '').trim();
  const mode = document.getElementById('trigger-mode').value;
  btn.disabled = true;
  btn.textContent = 'Dispatching...';
  await triggerRun(issue, mode);
  btn.textContent = 'Dispatched!';
  setTimeout(() => {
    btn.disabled = false;
    btn.textContent = 'Run Pipeline';
  }, 5000);
}

// ── Boot ────────────────────────────────────────────────────────────
init();
</script>
</body>
</html>
```

- [ ] **Step 2: Open in a browser to verify it renders with seed data**

```bash
open docs/dashboard/index.html
```

Verify: dark theme loads, KPIs show "1 run / $0.28 / 100% / 1 PR / 15 iters", cost chart shows one green bar, token bars render, run history table shows the #576 row, pipeline control shows "Configure GitHub token" notice.

- [ ] **Step 3: Commit**

```bash
git add docs/dashboard/index.html
git commit -m "feat(dashboard): add static dashboard with all 6 sections"
```

---

### Task 3: Create the aggregation workflow

**Files:**
- Create: `.github/workflows/aggregate-dashboard.yml`

This workflow runs after each `implement.yml` completion, downloads all run artifacts, enriches them with run metadata, and writes `docs/dashboard/data.json`.

- [ ] **Step 1: Create the aggregation workflow**

Create `.github/workflows/aggregate-dashboard.yml`:

```yaml
name: Update dashboard data

on:
  workflow_run:
    workflows: ["Run pipeline"]
    types: [completed]

  workflow_dispatch:

permissions:
  contents: write
  actions: read

jobs:
  aggregate:
    runs-on: ubuntu-latest
    steps:
      - name: Checkout
        uses: actions/checkout@v4

      - name: Collect artifacts and build data.json
        env:
          GH_TOKEN: ${{ secrets.GITHUB_TOKEN }}
        run: |
          set -euo pipefail

          REPO="${{ github.repository }}"
          DATA_FILE="docs/dashboard/data.json"

          # Start building the runs array
          echo '{"runs":[' > /tmp/runs.json
          FIRST=true

          # List all workflow runs for implement.yml
          RUNS=$(gh api "repos/${REPO}/actions/workflows/implement.yml/runs?per_page=100" \
            --jq '.workflow_runs[] | {id: .id, status: .status, conclusion: .conclusion, started: .run_started_at, updated: .updated_at, html_url: .html_url}' \
            | jq -s '.')

          for ROW in $(echo "$RUNS" | jq -c '.[]'); do
            RUN_ID=$(echo "$ROW" | jq -r '.id')
            CONCLUSION=$(echo "$ROW" | jq -r '.conclusion')
            STARTED=$(echo "$ROW" | jq -r '.started')
            UPDATED=$(echo "$ROW" | jq -r '.updated')

            # Calculate duration
            if [ "$STARTED" != "null" ] && [ "$UPDATED" != "null" ]; then
              START_EPOCH=$(date -d "$STARTED" +%s 2>/dev/null || date -j -f "%Y-%m-%dT%H:%M:%SZ" "$STARTED" +%s 2>/dev/null || echo 0)
              END_EPOCH=$(date -d "$UPDATED" +%s 2>/dev/null || date -j -f "%Y-%m-%dT%H:%M:%SZ" "$UPDATED" +%s 2>/dev/null || echo 0)
              DURATION=$((END_EPOCH - START_EPOCH))
            else
              DURATION=0
            fi

            # Try to download the artifact for this run
            ARTIFACT_NAME="pipeline-run-${RUN_ID}"
            ARTIFACT_DIR="/tmp/artifacts/${RUN_ID}"
            mkdir -p "$ARTIFACT_DIR"

            if gh run download "$RUN_ID" --repo "$REPO" --name "$ARTIFACT_NAME" --dir "$ARTIFACT_DIR" 2>/dev/null; then
              # Find the run-summary.json
              SUMMARY_FILE=$(find "$ARTIFACT_DIR" -name "run-summary.json" -print -quit)
              if [ -n "$SUMMARY_FILE" ]; then
                # Enrich with run metadata
                ENRICHED=$(jq --arg rid "$RUN_ID" \
                              --arg status "$CONCLUSION" \
                              --argjson dur "$DURATION" \
                              '. + {run_id: $rid, status: $status, duration_seconds: $dur}' \
                              "$SUMMARY_FILE")

                if [ "$FIRST" = true ]; then
                  FIRST=false
                else
                  echo ',' >> /tmp/runs.json
                fi
                echo "$ENRICHED" >> /tmp/runs.json
              fi
            fi
          done

          # Close the JSON
          UPDATED_AT=$(date -u +"%Y-%m-%dT%H:%M:%SZ")
          echo "]," >> /tmp/runs.json
          echo "\"updated_at\": \"${UPDATED_AT}\"}" >> /tmp/runs.json

          # Validate and format
          mkdir -p docs/dashboard
          jq '.' /tmp/runs.json > "$DATA_FILE"

          echo "Dashboard data written with $(jq '.runs | length' "$DATA_FILE") runs"

      - name: Commit and push updated data
        run: |
          git config user.name "conduit-agent[bot]"
          git config user.email "conduit-agent[bot]@users.noreply.github.com"
          git add docs/dashboard/data.json
          if git diff --cached --quiet; then
            echo "No changes to data.json"
          else
            git commit -m "chore(dashboard): update run data [skip ci]"
            git push
          fi
```

- [ ] **Step 2: Verify YAML syntax**

```bash
python3 -c "import yaml; yaml.safe_load(open('.github/workflows/aggregate-dashboard.yml')); print('OK')"
```

Expected: `OK`

- [ ] **Step 3: Commit**

```bash
git add .github/workflows/aggregate-dashboard.yml
git commit -m "feat(dashboard): add aggregation workflow for data.json"
```

---

### Task 4: Configure GitHub Pages and update documentation

**Files:**
- Modify: `README.md`
- Modify: `docs/demo-guide.md`

- [ ] **Step 1: Enable GitHub Pages via API**

```bash
gh api repos/William-Hill/conduit-agent-experiment/pages \
  -X POST \
  -f source[branch]=main \
  -f source[path]="/docs/dashboard" \
  --silent 2>/dev/null || echo "Pages may already be configured"
```

If this fails (already configured or insufficient permissions), configure manually:
Repo Settings → Pages → Source: Deploy from branch → Branch: `main` → Folder: `/docs/dashboard`

- [ ] **Step 2: Add dashboard link to README.md**

In `README.md`, add to the Documentation section after the existing entries:

```markdown
- **[Pipeline Dashboard](https://william-hill.github.io/conduit-agent-experiment/)** -- live run history, cost trends, and pipeline control
```

- [ ] **Step 3: Add dashboard section to demo guide**

In `docs/demo-guide.md`, add after the "Running via GitHub Actions (CI)" section:

```markdown
## Dashboard

Live at: **https://william-hill.github.io/conduit-agent-experiment/**

The dashboard updates automatically after each pipeline run. It shows:
- Run history with cost, iterations, and PR links
- Cost trend chart
- Token usage breakdown
- Live pipeline status (requires GitHub token — click the gear icon)

To trigger a run from the dashboard, configure a GitHub PAT with `repo` and `actions` scope via the settings gear icon.
```

- [ ] **Step 4: Commit**

```bash
git add README.md docs/demo-guide.md
git commit -m "docs: add dashboard link to README and demo guide"
```

---

### Task 5: End-to-end verification

- [ ] **Step 1: Push all commits and verify GitHub Pages**

```bash
git push origin main
```

Wait 1-2 minutes for GitHub Pages to deploy, then open:
`https://william-hill.github.io/conduit-agent-experiment/`

Verify the dashboard loads with the seed data.

- [ ] **Step 2: Trigger the aggregation workflow manually**

```bash
gh workflow run aggregate-dashboard.yml --repo William-Hill/conduit-agent-experiment
```

Watch it complete:

```bash
gh run watch --repo William-Hill/conduit-agent-experiment
```

Verify `docs/dashboard/data.json` was updated with the enriched run data (should now include `run_id`, `status`, `duration_seconds`).

- [ ] **Step 3: Verify dashboard reflects updated data**

Refresh `https://william-hill.github.io/conduit-agent-experiment/` after the aggregation workflow pushes.

Verify: KPIs updated, cost chart shows data, run history table populated.

- [ ] **Step 4: Test pipeline trigger from dashboard (optional)**

Open the dashboard, click the gear icon, paste a GitHub PAT with `repo` + `actions` scope. Click "Run Pipeline" with issue #576 and yolo mode. Verify:
- Dispatch accepted (button shows "Dispatched!")
- Status polling starts (amber "Running" indicator appears within ~30s)
- After completion, green "Completed" indicator appears
