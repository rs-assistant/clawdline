package board

// The card's wire types, as board.schema.json declares them. Cards came only
// from the retired Swift app's board, which this daemon no longer reads
// (docs/cutover.md §7), so every list it answers is empty; the types stay so
// the envelope keeps its declared shape.

// Progress is what a card carries about where the work stands.
type Progress struct {
	State               string         `json:"state"`
	Label               string         `json:"label"`
	Reason              string         `json:"reason"`
	Group               string         `json:"group"`
	BasisCodes          []string       `json:"basisCodes"`
	WarningCodes        []string       `json:"warningCodes"`
	LifecycleApplicable bool           `json:"lifecycleApplicable"`
	Active              bool           `json:"active"`
	Historical          bool           `json:"historical"`
	AttemptCounts       map[string]int `json:"attemptCounts"`
	EvidenceCounts      map[string]int `json:"evidenceCounts"`
}

// ListSummary is the bounded attention fact a list row carries.
type ListSummary struct {
	Group     string         `json:"group"`
	Coverage  string         `json:"coverage"`
	Attention map[string]int `json:"attention"`
	View      AudienceView   `json:"view"`
}

// AudienceView is who a row is for and what part it plays.
type AudienceView struct {
	Audience       string   `json:"audience"`
	Role           string   `json:"role"`
	DefaultVisible bool     `json:"defaultVisible"`
	ReasonCodes    []string `json:"reasonCodes"`
	Outcome        string   `json:"outcome,omitempty"`
	AuditID        string   `json:"auditId,omitempty"`
}

// CardSummary is the exact checklist and milestone count.
type CardSummary struct {
	Checklist  map[string]any `json:"checklist"`
	Milestones map[string]any `json:"milestones"`
}
