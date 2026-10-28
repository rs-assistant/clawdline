package work

import "time"

// HumanIntervention is a request for a person's attention on a target Session.
// Its source and target are deliberately independent: a coordinating Root may
// put a request on another Session's conversation.
type HumanIntervention struct {
	ID                 string
	SourceConversation string
	SourceLabel        string
	TargetConversation string
	TargetSession      string
	Kind               string
	Title              string
	Summary            string
	Action             string
	Reason             string
	Detail             string
	Options            []HumanInterventionOption
	DocumentURL        string
	// Command is the exact argv a Secret Handoff Note declares will receive
	// the value: an absolute executable followed by its arguments. It is not
	// secret; the person reads it before handing off, and the daemon refuses
	// a claim that presents any other argv. Every other kind leaves it empty.
	Command    []string
	CreatedAt  time.Time
	ReadAt     *time.Time
	ResolvedAt *time.Time
	Withdrawn  bool
	Resolution string
	Version    int64
}

type HumanInterventionOption struct {
	Label string `json:"label"`
	Draft string `json:"draft"`
}
