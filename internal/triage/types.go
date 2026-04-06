package triage

// IssueInfo is the summary returned by the list_issues tool.
type IssueInfo struct {
	Number     int      `json:"number"`
	Title      string   `json:"title"`
	Labels     []string `json:"labels"`
	BodyPrefix string   `json:"body_prefix"` // first 500 chars
	Comments   int      `json:"comments"`
	Assignees  int      `json:"assignees"`
	CreatedAt  string   `json:"created_at"`
}

// IssueDetail is the full issue returned by the get_issue tool.
type IssueDetail struct {
	Number    int      `json:"number"`
	Title     string   `json:"title"`
	Labels    []string `json:"labels"`
	Body      string   `json:"body"`
	Comments  int      `json:"comments"`
	Assignees int      `json:"assignees"`
	CreatedAt string   `json:"created_at"`
}

// RankedIssue is a single classified and scored issue.
type RankedIssue struct {
	Number      int    `json:"number"`
	Title       string `json:"title"`
	Category    string `json:"category"`     // bug, feature, connector, housekeeping, docs
	Difficulty  string `json:"difficulty"`    // L1, L2, L3, L4
	BlastRadius string `json:"blast_radius"` // low, medium, high
	Feasibility int    `json:"feasibility"`  // 1-10
	Demand      int    `json:"demand"`       // 1-10
	Score       int    `json:"score"`        // feasibility * demand
	Rationale   string `json:"rationale"`
	Suitable    bool   `json:"suitable"` // suitable for automated fix
}

// TriageOutput is the complete output of a triage run.
type TriageOutput struct {
	Timestamp   string        `json:"timestamp"`
	Repo        string        `json:"repo"`
	TotalIssues int           `json:"total_issues"`
	Ranked      []RankedIssue `json:"ranked"`
}
