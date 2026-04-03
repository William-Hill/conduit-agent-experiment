package models

// Difficulty represents the risk level of a maintenance task.
type Difficulty string

const (
	DifficultyL1 Difficulty = "L1" // docs, deps, lint
	DifficultyL2 Difficulty = "L2" // narrow bug fixes, config alignment
	DifficultyL3 Difficulty = "L3" // contained features, validation changes
	DifficultyL4 Difficulty = "L4" // runtime semantics, concurrency
)

// BlastRadius indicates how broadly a change could affect the system.
type BlastRadius string

const (
	BlastRadiusLow    BlastRadius = "low"
	BlastRadiusMedium BlastRadius = "medium"
	BlastRadiusHigh   BlastRadius = "high"
)

// TaskStatus tracks the lifecycle of a task.
type TaskStatus string

const (
	TaskStatusPending  TaskStatus = "pending"
	TaskStatusAccepted TaskStatus = "accepted"
	TaskStatusRejected TaskStatus = "rejected"
	TaskStatusDeferred TaskStatus = "deferred"
	TaskStatusRunning  TaskStatus = "running"
	TaskStatusDone     TaskStatus = "done"
	TaskStatusFailed   TaskStatus = "failed"
)

// Task represents a maintenance task to be attempted by the agent system.
type Task struct {
	ID                 string      `json:"id"`
	Title              string      `json:"title"`
	Source             string      `json:"source"`
	Description        string      `json:"description"`
	Labels             []string    `json:"labels,omitempty"`
	Difficulty         Difficulty  `json:"difficulty"`
	BlastRadius        BlastRadius `json:"blast_radius"`
	AcceptanceCriteria []string    `json:"acceptance_criteria"`
	SelectedFiles      []string    `json:"selected_files,omitempty"`
	RelatedDocs        []string    `json:"related_docs,omitempty"`
	Status             TaskStatus  `json:"status"`
}
