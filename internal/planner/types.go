package planner

// PlannedChange is a single file to write.
type PlannedChange struct {
	Path        string `json:"path"`
	Description string `json:"description"`
	Content     string `json:"content"` // complete file content
}

// ImplementationPlan is the planner's output.
type ImplementationPlan struct {
	Summary      string          `json:"summary"`
	Changes      []PlannedChange `json:"changes"`
	Verification []string        `json:"verification"` // commands to run after writing
}

// ReviewResult is the reviewer's verdict.
type ReviewResult struct {
	Approved bool   `json:"approved"`
	Feedback string `json:"feedback"`
}
