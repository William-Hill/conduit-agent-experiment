package planner

// ImplementationPlan is the planner's output — a markdown document
// with detailed instructions for what code to write.
type ImplementationPlan struct {
	Markdown string
}

// ReviewResult is the reviewer's verdict.
type ReviewResult struct {
	Approved bool   `json:"approved"`
	Feedback string `json:"feedback"`
}
