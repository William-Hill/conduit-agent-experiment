package models

import "time"

// RunStatus represents the outcome of a task run.
type RunStatus string

const (
	RunStatusRunning  RunStatus = "running"
	RunStatusSuccess  RunStatus = "success"
	RunStatusFailed   RunStatus = "failed"
	RunStatusRejected RunStatus = "rejected"
)

// HumanDecision captures the human governor's final call.
type HumanDecision string

const (
	HumanDecisionPending  HumanDecision = "pending"
	HumanDecisionApproved HumanDecision = "approved"
	HumanDecisionRejected HumanDecision = "rejected"
	HumanDecisionDeferred HumanDecision = "deferred"
)

// Run captures all artifacts and metadata for a single task execution.
type Run struct {
	ID               string        `json:"id"`
	TaskID           string        `json:"task_id"`
	StartedAt        time.Time     `json:"started_at"`
	EndedAt          time.Time     `json:"ended_at,omitempty"`
	AgentsInvoked    []string      `json:"agents_invoked"`
	RetrievedContext []string      `json:"retrieved_context,omitempty"`
	Prompts          []string      `json:"prompts,omitempty"`
	CommandsRun      []CommandLog  `json:"commands_run,omitempty"`
	Outputs          []string      `json:"outputs,omitempty"`
	FinalStatus      RunStatus     `json:"final_status"`
	HumanDecision    HumanDecision `json:"human_decision"`
}

// CommandLog records a single command execution during a run.
type CommandLog struct {
	Command  string    `json:"command"`
	ExitCode int       `json:"exit_code"`
	Stdout   string    `json:"stdout"`
	Stderr   string    `json:"stderr"`
	RunAt    time.Time `json:"run_at"`
}
