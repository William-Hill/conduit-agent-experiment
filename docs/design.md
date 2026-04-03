# OSA Community Event Submission + Experiment Implementation Pack

**Prepared for:** William Hill  
**Email:** mjhilldigital@gmail.com  
**Target community:** Open Source Architects (OSA) Community by OpenTeams  
**Session theme:** Agent-assisted maintenance for under-maintained open source systems using Conduit as the experiment  
**Primary project repo:** https://github.com/ConduitIO/conduit

---

## 1. What this pack is for

This document is designed to be used by:
1. **A coding agent** to start implementing the Conduit maintenance experiment
2. **A slide-deck agent** to create a polished conference presentation
3. **You** to complete the OSA Community speaker information form without missing any required fields

This pack intentionally includes:
- form-ready answers
- alternate title and description options
- polling questions
- a short bio
- social links section
- a complete experiment PRD
- a technical architecture and rollout plan
- a slide deck brief with a recommended narrative arc
- talking points, risks, metrics, and demo guidance

---

## 2. Recommended positioning

### Core thesis
AI agents are not ready to replace maintainers, but they may be able to **extend the maintenance half-life** of an open source project when paired with architectural context, tests, CI, and a human approval gate.

### Why this framing works
This framing is:
- ambitious without overclaiming
- technically credible for an architect audience
- grounded in a real codebase rather than a toy demo
- relevant to open source sustainability, governance, and maintainership

### Recommended audience promise
Attendees will leave with:
- a framework for deciding whether a repo is suitable for agent-assisted maintenance
- a concrete multi-agent architecture they can adapt
- a candid view of where AI helps and where it still fails

---

## 3. OSA Community speaker form - completed draft answers

Use the following as the primary version unless you want to make edits for tone or length.

### EMAIL ADDRESS for communication related to the event
mjhilldigital@gmail.com

### Your NAME, as you would like it to appear in the event program
William Hill

### Your PREFERRED PRONOUN
He/Him

### Your TITLE, ROLE in your organization, or AFFILIATIONS
Senior Software Engineer at Zocdoc  
Founder, MJ Hill Digital  
Former Staff Software Engineer at Meroxa

### TITLE of your session
**Can AI Agents Extend the Half-Life of Open Source Software? A Conduit Case Study**

#### Alternate title options
1. **What Happens When AI Becomes the Maintainer of Last Resort?**
2. **Can AI Help Sustain Open Source After Maintainer Bandwidth Drops Off?**
3. **Designing a Human-Governed AI Maintenance Loop for Open Source Systems**
4. **I Let AI Agents Maintain a Streaming System. Here's What Actually Happened.**

### TYPE of your session
Lecture / Technical Talk

### HOW LONG is your session (in min)?
45

### DESCRIPTION of your session (max 300 words)
Open source projects rarely become under-maintained because the architecture stopped mattering. More often, they drift because maintainers run out of time, funding, or energy.

In this talk, I will explore whether a team of AI agents can meaningfully help sustain and evolve a lightly maintained open source system by using Conduit, a Go-based data streaming platform, as the case study. Rather than treating AI as an autonomous replacement for maintainers, I will show how a human-governed maintenance loop can combine architectural retrieval, issue triage, patch generation, verification, and review.

The session will walk through a practical agent design: an Archivist agent that ingests design docs and architecture decisions, a Triage agent that selects feasible tasks, an Implementer agent that proposes narrow changes, a Verifier agent that runs tests and linting, and an Architect agent that checks the proposal against system boundaries and invariants before human approval.

I will also share the scorecard for the experiment: what kinds of tasks agents can realistically handle today, what failure modes appear in real repositories, and what characteristics make an open source codebase more or less suitable for agent-assisted maintenance.

Attendees will leave with a practical framework for evaluating their own repositories, a reference architecture for a human-in-the-loop AI maintenance workflow, and a grounded perspective on where AI can help open source teams without replacing maintainership.

### LINKS to the blog posts, articles, etc. that the proposed presentation will be based on
Use these if the form allows multiple URLs:
- https://github.com/ConduitIO/conduit
- Add a future project repo or writeup for the experiment if available
- Add any public blog post you publish before the event describing the experiment

### LINKS to the slides or videos of your previous presentations
Add your strongest prior material here. Suggested categories:
- previous conference talk slides on AI agents
- previous conference talk slides on data streaming / Conduit / architecture
- any recorded talks from prior speaking engagements
- YouTube or speaker profile links if available

**Suggested placeholder text for your own use:**
- [Add talk video URL]
- [Add slide deck URL]
- [Add speaker profile URL]

### POLLING QUESTIONS
Use 3 to 5. Recommended set:

**Poll 1**  
Question: Have you ever depended on an open source project that became lightly maintained or effectively dormant?  
Options:
- Yes, frequently
- Yes, a few times
- Not sure
- No

**Poll 2**  
Question: Which maintenance task would you trust AI to handle first in an OSS repo?  
Options:
- Documentation updates
- Dependency bumps
- Bug fixes
- Architectural changes

**Poll 3**  
Question: True or False: Passing tests alone are enough to trust an AI-generated change in a production OSS system.  
Options:
- True
- False

**Poll 4**  
Question: What is the biggest blocker to agent-assisted OSS maintenance in your view?  
Options:
- Poor documentation
- Weak tests
- Architectural complexity
- Lack of human review capacity

**Poll 5**  
Question: Would you allow AI agents to open draft PRs in a repo you maintain if every change required human approval before merge?  
Options:
- Yes
- Maybe
- No
- Depends on the repo

### Your BIO (max 150 words)
William Hill is a Senior Software Engineer at Zocdoc and founder of MJ Hill Digital. He previously worked as a Staff Software Engineer at Meroxa, where he built data streaming solutions and open source tooling in Go. His work spans software architecture, data engineering, AI agents, and developer platforms, with a focus on turning complex systems into practical workflows teams can use. William has spoken on AI agent workflows, data systems, and modern software architecture, and he is especially interested in how open source projects can remain useful and sustainable as maintainer bandwidth shifts over time. He brings a builder's perspective to emerging AI workflows, combining hands-on experimentation with a grounded view of reliability, governance, and engineering tradeoffs.

### Your social media URLs
Use as many as you want to include:
- LinkedIn: https://www.linkedin.com/in/whill3/
- Instagram: https://www.instagram.com/emjay_hill/
- Website: [Add MJ Hill Digital site URL if live]
- GitHub: [Add your GitHub profile URL]
- Speaker page: [Add if available]

### Upload your PHOTO
Recommended: use a professional headshot, square crop, at least 500x500, JPG or PNG.

### Please recommend one or more people to speak at our future events
Draft suggestions:
- A maintainer or architect working on open source developer infrastructure
- A speaker focused on OSS sustainability and governance
- A practitioner building production AI evaluation or observability systems
- [Add specific names from your network if you want to personalize this]

### Any other info you would like to include
Suggested text:
I am happy to tailor the session toward a more architectural or more implementation-focused audience. If helpful, I can also provide a follow-up resource pack with a PRD, example agent workflow diagrams, and an experiment scorecard template for attendees.

### Recording permission
Recommended answer:
- Yes, I understand and agree.

### Quote and excerpt permission
Recommended answer:
- Yes, I understand and agree.

---

## 4. Short-form versions for forms or promos

### 50-word session summary
Can a team of AI agents help sustain an open source project after human maintainer bandwidth drops off? Using the Conduit streaming platform as a case study, this talk explores a human-governed AI maintenance loop, what tasks agents can handle, where they fail, and which repository qualities make this approach viable.

### 100-word session summary
Open source projects often become under-maintained not because they have lost value, but because maintainers lose time and capacity. This talk explores whether AI agents can extend the maintenance half-life of an open source system by using Conduit, a Go-based streaming platform, as a real case study. I will show a human-governed agent workflow for repository ingestion, issue triage, patch generation, verification, and architectural review, then share the practical scorecard: which tasks are feasible today, where agents create risk, and what architectural conditions make a repo suitable for agent-assisted maintenance.

### Social promo blurb
Can AI agents keep a real open source project moving after human maintainer bandwidth drops off? In this session, William Hill explores a human-in-the-loop AI maintenance workflow using the Conduit streaming codebase as a case study, with a candid look at feasibility, risk, and architectural constraints.

---

## 5. Speaker notes for your own framing

### The most credible version of the talk
Position the talk as:
- an experiment
- a case study
- a framework
- a scorecard

Avoid positioning it as:
- AI replacing maintainers
- autonomous open source stewardship
- a universal recipe for all repos

### The strongest one-sentence hook
What if AI could not replace maintainers, but could still buy an open source project more time?

### The strongest architect-centric question
Under what architectural conditions can AI meaningfully assist with sustaining an open source system after maintainer bandwidth drops off?

### The strongest takeaway
The limiting factor is not whether a model can write code. It is whether the repository provides enough structure, context, boundaries, and safety rails for AI-generated work to be reviewed and trusted.

---

## 6. Slide deck brief for a slide-building agent

### Slide deck goal
Create a polished 45-minute technical conference talk for an architect audience. The tone should be rigorous, grounded, and practical. The presentation should emphasize architecture, governance, repository design, and engineering tradeoffs rather than AI hype.

### Audience
- senior engineers
- staff+ engineers
- architects
- open source maintainers
- technically experienced practitioners evaluating AI workflows

### Tone
- candid
- technically credible
- systems-oriented
- visually clean
- no hype language
- emphasize constraints, scorecards, and failure modes

### Core message
AI agents can likely help extend the maintenance half-life of some open source projects, but only when the repository has strong architecture, tests, documentation, and human governance.

### Recommended deck length
18 to 24 slides

### Recommended visual style
- dark or neutral modern conference aesthetic
- architecture diagrams with clear agent roles
- minimal clutter
- light use of emphasis colors
- code and charts only where they add explanatory value
- use diagrams more than long paragraphs
- avoid novelty visuals that weaken credibility

### Suggested slide-by-slide outline

#### Slide 1 - Title
Title: Can AI Agents Extend the Half-Life of Open Source Software?  
Subtitle: A Conduit case study in human-governed agent-assisted maintenance  
Include your name, role, and affiliations.

#### Slide 2 - The problem
Frame OSS maintenance decline as a bandwidth and sustainability problem, not just a code problem.

#### Slide 3 - The central question
Can a team of AI agents help a real open source system keep moving after maintainer bandwidth drops off?

#### Slide 4 - Why this matters
Show why under-maintained but still valuable OSS is a common and costly reality.

#### Slide 5 - Why Conduit
Explain why Conduit is a credible case study:
- real Go codebase
- plugin architecture
- gRPC interfaces
- tests and CI
- docs and ADRs
- nontrivial correctness concerns

#### Slide 6 - Why AI usually fails in real repos
List common problems:
- no architectural context
- poor tests
- over-broad tasks
- hidden invariants
- false confidence from green checks

#### Slide 7 - The thesis
AI does not replace maintainers. It may extend maintenance capacity under strong constraints.

#### Slide 8 - The agent team
Show the agent roles:
- Archivist
- Triage
- Implementer
- Verifier
- Architect
- Human Governor

#### Slide 9 - Workflow diagram
Show request flow from issue selection through approval.

#### Slide 10 - Repo readiness rubric
Present criteria for "AI-maintainable OSS":
- architecture docs
- clear boundaries
- test coverage
- CI
- contribution rules
- reasonable task granularity

#### Slide 11 - Conduit architecture snapshot
High-level diagram of core runtime, connectors, processors, APIs, config, tests, and docs.

#### Slide 12 - Task ladder
Level 1 to Level 4 difficulty:
- docs drift
- dependency updates
- bug fixes
- runtime semantics

#### Slide 13 - What we let the agents do
List scoped permissions and guardrails.

#### Slide 14 - What we do not let the agents do
No autonomous merge, no roadmap ownership, no blind semantic changes.

#### Slide 15 - Evaluation framework
Metrics:
- pass rate
- time to first patch
- reviewer confidence
- architecture alignment
- failure taxonomy

#### Slide 16 - Example task walkthrough
Choose one narrow task and show:
- context retrieved
- patch proposal
- verification outcome
- architect review outcome

#### Slide 17 - Failure modes
Examples:
- retrieval failure
- hallucinated fix
- semantically unsafe change
- test false confidence
- architecture drift

#### Slide 18 - What worked
Summarize successful task categories.

#### Slide 19 - What did not
Summarize failed or risky categories.

#### Slide 20 - Practical guidance
How attendees can evaluate their own repos.

#### Slide 21 - The broader implication
AI as a maintainer amplifier, not maintainer replacement.

#### Slide 22 - Close
Repeat the central message and end with a memorable line.

#### Slide 23 - Q&A
Optional polling recap or final discussion prompt.

### Speaker notes instructions for slide agent
Each slide should include concise speaker notes that:
- explain the main point in plain language
- connect back to the thesis
- avoid overclaiming
- give you one memorable phrase or transition line

### Recommended diagrams
1. OSS maintenance problem frame
2. Multi-agent architecture diagram
3. Human-in-the-loop workflow
4. Repo readiness rubric
5. Task risk ladder
6. Evaluation and failure taxonomy matrix

### Recommended charts
1. Task categories by risk
2. Success likelihood by task level
3. Failure modes distribution
4. Human review time versus task complexity

### Demo guidance
If a live demo is included, keep it narrow:
- repository ingest
- task brief generation
- one patch proposal
- verifier result
Do not demo autonomous merge or anything requiring broad runtime changes.

### Slide deck constraints
- no overuse of text
- no em dashes
- architecture-first framing
- show both successes and failures
- keep each slide focused on one idea
- include clear takeaways on the final slides

---

## 7. Product Requirements Document (PRD)

# PRD: Agent-Assisted Maintenance Experiment for Conduit

## 7.1 Document status
Draft v1

## 7.2 Product name
Conduit Agent Maintenance Experiment

## 7.3 Executive summary
Build a human-governed multi-agent system that attempts bounded maintenance work on the Conduit open source codebase. The goal is not autonomous stewardship. The goal is to determine whether AI agents can materially improve maintenance throughput, context retrieval, and reviewability for low-risk to medium-risk maintenance tasks in a real repository.

The system will ingest the Conduit repository, retrieve relevant architectural context, classify candidate tasks, propose changes, run validation, and require human approval before any repository write action. The experiment should produce both implementation outcomes and a research scorecard suitable for conference material, blog content, and future open source discussion.

## 7.4 Problem statement
Many open source projects become lightly maintained or inconsistently maintained because maintainers lose time, attention, or funding. Existing AI coding workflows can generate patches, but they are weak at architectural reasoning, semantic safety, and governance. We need a structured way to test whether a multi-agent workflow can assist with open source maintenance in a credible, measurable, and reviewable way.

## 7.5 Primary question
Can a human-governed team of AI agents successfully complete bounded maintenance tasks in the Conduit codebase with acceptable quality, safety, and review effort?

## 7.6 Goals
- Determine whether AI agents can complete bounded maintenance tasks end-to-end
- Measure patch quality, correctness, reviewability, and architectural alignment
- Identify which classes of tasks are feasible, risky, or infeasible
- Produce a reusable workflow pattern for agent-assisted OSS maintenance
- Generate concrete assets for talks, articles, and future experimentation

## 7.7 Non-goals
- Full autonomy
- Auto-merge without human approval
- Replacing maintainers or project governance
- Phase 1 support for large features or major redesigns
- Managing the entire connector ecosystem in the first milestone

## 7.8 Users

### Primary user
A human maintainer, architect, or experiment runner overseeing the process

### Secondary users
- OSS maintainers evaluating AI workflows
- engineers contributing to a complex codebase
- researchers or speakers documenting AI-assisted software maintenance

## 7.9 Hypotheses
1. Repositories with ADRs, design docs, tests, CI, and contribution conventions are better candidates for agent-assisted maintenance.
2. AI agents perform best on low-risk, narrow-blast-radius tasks.
3. A dedicated architectural review role improves quality over a single coding-agent workflow.
4. Human approval remains essential for semantic, compatibility, and governance-sensitive changes.

## 7.10 Success criteria
The experiment is successful if:
- at least 60 percent of low-risk tasks are completed with acceptable human review effort
- the system reduces time to first useful patch draft relative to manual exploration
- successful tasks include enough context and rationale for a reviewer to trust the change
- failures are clearly classified and reproducible
- the output yields a clear rubric for repository suitability

## 7.11 Why Conduit is a good experiment target
Conduit is an effective experiment target because:
- it is a real Go-based streaming system
- it uses connectors and processors as explicit extension boundaries
- it has API and runtime surfaces that matter
- it includes tests, linting, CI workflows, docs, ADRs, and design docs
- it is complex enough to be meaningful, but structured enough to support bounded experimentation

## 7.12 Scope

### In scope for phase 1
- repository ingest and structural indexing
- task intake from seeded tasks or GitHub issues
- architecture-aware retrieval
- low-risk and medium-low-risk task classification
- docs updates
- dependency bumps
- narrow bug fixes
- failing test diagnosis
- verifier reports
- human review packet generation

### Out of scope for phase 1
- automatic merge to main
- broad runtime redesigns
- release ownership
- sweeping multi-repo changes
- autonomous issue prioritization without human oversight
- connector ecosystem-wide changes

## 7.13 Task ladder

### Level 1
- docs drift correction
- README updates
- minor dependency bumps
- lint cleanup
- simple test additions

### Level 2
- failing test diagnosis
- narrow bug fixes
- config/docs alignment
- error messaging improvement
- limited CLI quality-of-life fixes

### Level 3
- contained feature additions
- validation changes
- minor behavior adjustments with bounded blast radius

### Level 4
- pipeline runtime semantics
- acknowledgement behavior
- concurrency-sensitive logic
- changes affecting broader compatibility expectations

**Phase 1 recommendation:** Only Levels 1 and 2 should be attempted.

## 7.14 Product requirements

### Requirement 1 - Repository ingestion and indexing
The system must:
- clone or mount the Conduit repository locally
- identify code, docs, workflows, tests, and ADRs
- parse package structure and build targets
- create searchable artifacts for both semantic retrieval and structured lookup
- preserve links from files to summaries, symbols, and task-relevant concepts

### Requirement 2 - Task intake
The system must:
- accept tasks from a local backlog, GitHub issues, or manually entered prompts
- classify task difficulty and blast radius
- mark tasks as accept, reject, or defer
- output a structured task brief before implementation begins

### Requirement 3 - Context dossier generation
The Archivist agent must produce a task dossier containing:
- task summary
- relevant files
- related docs and ADRs
- relevant commands and tests
- probable invariants
- likely risks
- open questions

### Requirement 4 - Patch planning
The Implementer agent must:
- propose a narrow plan before editing
- identify files to touch
- explain design choices
- state assumptions explicitly
- prefer minimal diffs
- propose tests or docs updates where needed

### Requirement 5 - Verification
The Verifier agent must:
- run linting, tests, and build commands
- record outputs and failures
- distinguish environment failures from patch failures
- summarize whether the change is reviewable and what remains unresolved

### Requirement 6 - Architectural review
The Architect agent must:
- compare the patch against known system boundaries and ADR guidance
- identify architecture drift
- assess whether the patch seems semantically safe
- explicitly state why the patch should be approved, revised, or rejected

### Requirement 7 - Human governance
The system must:
- require human signoff before any push or PR creation
- allow a human to stop the run at any point
- preserve artifacts for auditability
- track why a human accepted or rejected a patch

### Requirement 8 - Experiment logging
The system must:
- capture prompts, retrieved context, commands, outputs, and review decisions
- store task-level and run-level metadata
- support later analysis and reporting

## 7.15 Functional flow
1. Select task
2. Generate task brief
3. Archivist retrieves context and produces dossier
4. Triage decides accept, reject, or defer
5. Implementer proposes patch plan
6. Implementer edits or generates draft patch
7. Verifier runs validation
8. Architect reviews alignment and risk
9. Human reviews final packet
10. Result is logged and categorized

## 7.16 Acceptance criteria for a successful task
A task is successful when:
- a patch or document change is generated
- relevant checks pass or failures are understood and isolated
- rationale is understandable to a reviewer
- no major unresolved architectural concerns remain
- the human reviewer considers the output useful and safe enough to progress

## 7.17 Evaluation framework

### Quantitative metrics
- percent of tasks completed successfully
- pass rate for lint, tests, and build
- average iterations per task
- time to first patch draft
- human review time
- acceptance rate by task level
- rejection rate by failure mode

### Qualitative metrics
- architectural alignment
- clarity of rationale
- retrieval usefulness
- reviewer confidence
- patch readability
- usefulness of produced artifacts
- severity of hallucination or overreach

## 7.18 Failure taxonomy
Track failures as:
- retrieval failure
- task misclassification
- implementation hallucination
- semantically incorrect fix
- test false confidence
- architecture drift
- environment/setup failure
- insufficient repository context
- excessive iteration cost
- human rejection

## 7.19 Safety and governance requirements
- no auto-merge in phase 1
- no automatic broad-scoped tasks
- explicit uncertainty is required
- stop when semantics are ambiguous
- preserve logs and artifacts
- keep patches narrow
- include architecture review before human review

## 7.20 Technical architecture

### High-level services
- repo-ingestor
- knowledge-indexer
- task-triage-service
- agent-orchestrator
- execution-runner
- evaluation-service
- github-adapter
- review-dashboard

### Recommended stack
- **Orchestrator:** Go, consistent with the Conduit codebase and target audience
- **Repository execution:** local checkout and sandboxed command runner
- **Knowledge store:** vector index plus structured metadata store
- **LLM interface:** provider abstraction with pluggable models
- **Logging and traces:** structured JSON plus optional observability UI
- **UI:** minimal local dashboard or markdown report generation
- **GitHub integration:** read-only in milestone 1, optional draft PR write support later

### Recommended local-first principle
The system should work fully on a local machine before any hosted or public integration is added.

### Repository separation
The experiment lives in its own repository (`conduit-agent-experiment`), separate from the Conduit codebase. Conduit is a read-only target referenced via a configurable path. The experiment has no Go module dependency on `github.com/conduitio/conduit`. It interacts with Conduit by reading files from a local checkout, running shell commands in an isolated worktree, and never pushing or merging without explicit human action. See `docs/adr/001-separate-repo.md` in the experiment repo for full rationale.

## 7.21 Data model

### Task
- id
- title
- source
- description
- labels
- difficulty
- blast_radius
- acceptance_criteria
- selected_files
- related_docs
- status

### Run
- id
- task_id
- started_at
- ended_at
- agents_invoked
- retrieved_context
- prompts
- commands_run
- outputs
- final_status
- human_decision

### Evaluation
- run_id
- lint_pass
- build_pass
- tests_pass
- review_score
- architecture_score
- notes
- failure_mode

## 7.22 Suggested repository layout for implementation

```text
conduit-agent-experiment/
  README.md
  go.mod
  go.sum
  .env.example
  Makefile
  configs/
    models.yaml
    experiment.yaml
  docs/
    architecture.md
    repo-readiness-rubric.md
    evaluation-rubric.md
    runbook.md
  data/
    runs/
    tasks/
    indexes/
    cache/
  cmd/
    experiment/
      main.go
  internal/
    orchestrator/
      workflow.go
      state.go
      policies.go
    agents/
      archivist.go
      triage.go
      implementer.go
      verifier.go
      architect.go
    ingest/
      repo_loader.go
      symbol_extractor.go
      doc_indexer.go
      issue_loader.go
    retrieval/
      vector_store.go
      search.go
      dossier_builder.go
    execution/
      command_runner.go
      sandbox.go
      test_runner.go
    github/
      adapter.go
    evaluation/
      metrics.go
      taxonomy.go
      scorecard.go
    models/
      task.go
      run.go
      evaluation.go
    reporting/
      markdown_report.go
      json_export.go
      slide_notes_export.go
  scripts/
    bootstrap_repo.sh
```

## 7.23 Agent definitions

### Archivist agent
Purpose: Understand the repository and gather task-specific context

Inputs:
- task brief
- repository index
- docs and ADRs
- file map
- issue body if applicable

Outputs:
- task dossier
- relevant file list
- relevant architecture notes
- likely commands and tests
- uncertainty list

### Triage agent
Purpose: Decide whether the task is appropriate for the current phase

Inputs:
- task brief
- dossier
- policy constraints

Outputs:
- accept/reject/defer decision
- difficulty estimate
- blast radius estimate
- reviewer notes

### Implementer agent
Purpose: Propose and optionally produce a patch

Inputs:
- task brief
- dossier
- policy constraints
- selected files

Outputs:
- patch plan
- code diff or draft patch
- test recommendations
- assumptions list

### Verifier agent
Purpose: Validate the patch and summarize confidence

Inputs:
- patch
- repo checkout
- test targets
- command policy

Outputs:
- command log
- pass/fail summary
- issue list
- residual risk notes

### Architect agent
Purpose: Assess architectural and semantic safety

Inputs:
- patch
- dossier
- verifier report
- ADRs and design docs

Outputs:
- alignment review
- risk review
- approval/revise/reject recommendation

### Human governor
Purpose: Final approval and experiment supervision

Inputs:
- all prior artifacts

Outputs:
- decision
- final notes
- accept/reject/defer label
- merge or no-merge decision

## 7.24 Policies and constraints

### Hard safety rules
- no push without explicit human action
- no merge without explicit human action
- no Level 3 or 4 tasks in milestone 1
- no broad refactors
- no touching generated files unless clearly required
- no silent assumptions
- stop if acceptance criteria are unclear

### Diff discipline
- prefer the smallest reviewable patch
- one logical task per run
- require rationale for each changed file
- attach test intent to every code change

## 7.25 Knowledge and retrieval design

### Corpus to ingest
- README
- CONTRIBUTING if present
- ADRs
- design docs
- workflow files
- Makefile and task entrypoints
- package tree
- tests
- issue templates
- selected issues and PRs
- release notes if useful

### Indexing strategy
Build both:
1. a semantic retrieval layer for natural language task understanding
2. a structured layer for file paths, packages, symbols, tests, commands, and docs

### Dossier structure
Each dossier should include:
- task objective
- likely subsystem
- likely files
- related architecture docs
- existing tests
- likely build commands
- compatibility concerns
- unresolved questions

## 7.26 Execution environment

### Milestone 1 execution requirements
- local git checkout of Conduit
- Go environment for orchestrator
- Go toolchain compatible with repo
- make
- golangci-lint if repo uses it
- Docker if required for integration tests
- isolated temp worktree per run

### Execution runner responsibilities
- create isolated workspace
- apply or generate patch
- run allowed commands
- capture stdout/stderr
- timebox long-running tasks
- clean up or preserve artifacts based on config

## 7.27 Reporting outputs
Every run should produce:
- task brief
- dossier
- patch plan
- patch diff
- verifier log
- architect review
- final decision
- machine-readable JSON
- human-readable markdown summary

## 7.28 MVP definition
The MVP should include:
- local repository ingestion
- manual task seeding
- Archivist, Implementer, Verifier, Architect roles
- markdown reporting
- JSON logging
- policy guards
- no GitHub write actions

## 7.29 Milestones

### Milestone 0 - Setup
- clone repo
- create local orchestrator project
- define schemas
- build repo ingest pipeline
- produce first dossier from a seeded task

### Milestone 1 - Low-risk task loop
- docs drift tasks
- dependency bumps
- lint fixes
- simple failing tests

### Milestone 2 - Narrow bug-fix pilot
- select 3 to 5 bounded issues
- run full workflow
- measure acceptance and review effort

### Milestone 3 - Extended reporting
- scorecards
- aggregate failure taxonomy
- public experiment writeup assets
- optional draft PR generation

## 7.30 Open questions
- Should issue intake begin from GitHub or a manually curated backlog?
- Should the Architect agent use a stronger model or just a stricter review policy?
- How much repository history should be indexed?
- Should PR history be part of context retrieval in milestone 1?
- What is the right threshold for human review rejection?

## 7.31 Recommended first task batch
1. docs drift correction
2. small dependency update
3. narrow lint issue
4. config/docs mismatch
5. one small failing or missing test

## 7.32 Exit criteria for phase 1
Phase 1 is complete when:
- 5 to 10 low-risk tasks have been attempted
- task outcomes are logged consistently
- failure taxonomy is populated
- human review effort is measured
- enough evidence exists to update the talk with real results

---

## 8. Build plan for a coding agent

This section is intentionally explicit so a coding agent can begin implementation without guessing the project shape.

### 8.1 Recommended implementation order
1. create project skeleton
2. define schemas for Task, Run, Evaluation, Dossier
3. implement repo ingestion and file inventory
4. implement structured search over files and docs
5. implement dossier generation
6. implement task policy guardrails
7. implement implementer patch-plan mode
8. implement verifier command execution
9. implement architect review output
10. implement markdown and JSON reporting
11. add optional GitHub read integration
12. add aggregate scorecard reporting

### 8.2 First deliverables
The first useful end-to-end milestone should support:
- create a seeded task JSON file
- generate a dossier from the Conduit repo
- generate a patch plan
- run one validation command
- output a full markdown report

### 8.3 Minimal interfaces to define early

#### Task schema
```json
{
  "id": "task-001",
  "title": "Fix docs drift in pipeline config example",
  "source": "seeded",
  "description": "Update docs to match current config behavior",
  "difficulty": "L1",
  "blast_radius": "low",
  "acceptance_criteria": [
    "Docs updated",
    "No code changes required",
    "Links and formatting validated"
  ]
}
```

#### Dossier schema
```json
{
  "task_id": "task-001",
  "summary": "Docs drift likely in config examples",
  "related_files": ["README.md", "docs/..."],
  "related_docs": ["docs/design-documents/..."],
  "likely_commands": ["make test"],
  "risks": ["Possible outdated examples elsewhere"],
  "open_questions": ["Is current config syntax stable?"]
}
```

#### Review schema
```json
{
  "task_id": "task-001",
  "architect_recommendation": "approve_with_review",
  "verifier_status": "pass",
  "human_decision": "pending"
}
```

### 8.4 Suggested commands
The agent should support commands like:
- `go run ./cmd/experiment index`
- `go run ./cmd/experiment run --task data/tasks/task-001.json`
- `go run ./cmd/experiment report --run-id run-001`

### 8.5 Policy engine requirements
Implement a simple policy file that:
- blocks tasks above allowed difficulty
- blocks broad file touches
- blocks push/merge operations
- requires rationale fields
- requires explicit uncertainty section

### 8.6 Testing strategy for the experiment system
Write tests for:
- schema validation
- policy guards
- dossier generation
- path filtering
- report generation
- command execution wrappers

### 8.7 Observability and logs
Store per-run:
- timestamps
- prompts
- retrieved chunks
- command outputs
- changed files
- status transitions
- final scores

### 8.8 Nice-to-have later
- lightweight dashboard
- GitHub issue ingestion
- draft PR generation
- richer symbol graph
- benchmark comparison of different model roles

---

## 9. Deliverables for the slide-deck agent

The slide-deck agent should produce:

### Primary artifact
A 45-minute conference deck in either PowerPoint or Google Slides compatible structure.

### Secondary artifacts
- speaker notes
- one speaker summary slide
- one appendix with extra experiment details
- one diagram export pack if possible

### Must-include content
- title and positioning
- why OSS maintenance is the real problem
- why Conduit is the experiment target
- multi-agent workflow diagram
- repository readiness rubric
- task ladder
- evaluation and failure taxonomy
- practical guidance for attendees
- closing thesis

### Optional appendix content
- fuller PRD summary
- example dossier format
- example verifier report
- experiment metrics table
- future work

---

## 10. Final recommendations

### Best session format choice
Lecture / technical talk is the best fit unless the OSA organizers explicitly prefer workshops.

### Best title choice
Can AI Agents Extend the Half-Life of Open Source Software? A Conduit Case Study

### Best risk-managed promise
This talk shares a real experiment in human-governed AI-assisted maintenance, including both successful task categories and the points where the approach breaks down.

### Best next implementation step
Start with a local-only MVP that can ingest the Conduit repo, generate a task dossier, produce a patch plan, and run verification for one low-risk seeded task.

---

## 11. Final checklist

Before submitting the speaker form:
- confirm your preferred professional headshot
- gather 1 to 3 prior talk links
- add GitHub and website URLs if desired
- decide whether to use the main or alternate title
- confirm 45 minutes is acceptable

Before starting implementation:
- create the project skeleton
- clone the Conduit repo locally
- define schemas
- build ingestion and dossier generation first
- restrict scope to Level 1 and Level 2 tasks

Before creating the slide deck:
- keep the message architect-focused
- avoid hype framing
- show failures as well as successes
- keep visuals clean and systems-oriented
