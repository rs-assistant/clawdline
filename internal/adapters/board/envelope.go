package board

import "time"

// Envelope is `GET /v1/board`'s `board` object: the 19 keys the Swift app
// publishes, plus `truncation` when one project is selected.
type Envelope struct {
	SchemaVersion        int            `json:"schemaVersion"`
	Revision             int64          `json:"revision"`
	Enabled              bool           `json:"enabled"`
	Mode                 string         `json:"mode"`
	UpdatedAt            float64        `json:"updatedAt"`
	Available            bool           `json:"available"`
	Entitlement          Entitlement    `json:"entitlement"`
	NarrativeConsent     *string        `json:"narrativeConsent"`
	Projects             []ProjectRow   `json:"projects"`
	Items                []Card         `json:"items"`
	Item                 *Card          `json:"item"`
	Truncated            bool           `json:"truncated"`
	Truncation           *Truncation    `json:"truncation,omitempty"`
	SnapshotBudgetBytes  int            `json:"snapshotBudgetBytes"`
	AutomaticMutation    map[string]any `json:"automaticMutation"`
	SourceIngestion      map[string]any `json:"sourceIngestion"`
	Source               map[string]any `json:"source"`
	ReadState            ReadState      `json:"readState"`
	ResponsibilitySource map[string]any `json:"responsibilitySource"`
	Viewer               Viewer         `json:"viewer"`
}

// Truncation says what this list left out, and why. It describes this list —
// never a different one the byte budget happened to cut.
type Truncation struct {
	Reason            *string `json:"reason"`
	ItemsOmittedCount int     `json:"itemsOmittedCount"`
}

// ReadState is how fresh this answer is.
type ReadState struct {
	Status      string         `json:"status"`
	Revision    int64          `json:"revision"`
	ObservedAt  *float64       `json:"observedAt"`
	AttemptedAt *float64       `json:"attemptedAt"`
	Refreshing  bool           `json:"refreshing"`
	Error       map[string]any `json:"error"`
}

// ProjectRow is one catalog entry.
type ProjectRow struct {
	ID                    string         `json:"id"`
	Name                  string         `json:"name"`
	Label                 string         `json:"label"`
	DisplayPath           string         `json:"displayPath,omitempty"`
	Icon                  any            `json:"icon,omitempty"`
	IsStartPoint          bool           `json:"isStartPoint"`
	ItemCount             int            `json:"itemCount"`
	ActiveItemCount       int            `json:"activeItemCount"`
	HumanItemCount        int            `json:"humanItemCount"`
	CurrentHumanItemCount int            `json:"currentHumanItemCount"`
	HumanLandedItemCount  int            `json:"humanLandedItemCount"`
	AgentRecordCount      int            `json:"agentRecordCount"`
	ArchivedRecordCount   int            `json:"archivedRecordCount"`
	Revision              int64          `json:"revision"`
	UpdatedAt             float64        `json:"updatedAt"`
	ObservedAt            float64        `json:"observedAt"`
	Summary               map[string]any `json:"summary"`
	SummaryCoverage       map[string]any `json:"summaryCoverage"`
}

// Card is one list row, built through `BoardCardContract`'s allow-list: the
// identity fields as they are, `progress` narrowed, and nothing else. A field
// that is not here does not reach the console, which is the point — the Swift
// app learned that by measuring 62,801 bytes a read nobody was reading.
type Card struct {
	ID             string         `json:"id"`
	Key            string         `json:"key"`
	KeyStatus      string         `json:"keyStatus"`
	ProjectID      string         `json:"projectId"`
	Title          string         `json:"title"`
	Type           string         `json:"type"`
	State          string         `json:"state"`
	Summary        string         `json:"summary"`
	Owner          string         `json:"owner"`
	ParentID       *string        `json:"parentId"`
	CreatedAt      float64        `json:"createdAt"`
	UpdatedAt      float64        `json:"updatedAt"`
	ScopeRevision  int64          `json:"scopeRevision"`
	Presentation   map[string]any `json:"presentation"`
	ListSummary    ListSummary    `json:"listSummary"`
	CardSummary    CardSummary    `json:"cardSummary"`
	Progress       Progress       `json:"progress"`
	Responsibility map[string]any `json:"responsibility"`
}

// Presence is the live session inventory `responsibilitySource` describes. It
// is request-time data and never enters a store.
type Presence struct {
	Complete   bool
	Fresh      bool
	ObservedAt time.Time
	Generation int64
}

// Query is what a read asked for.
type Query struct {
	Project  string
	Item     string
	Audience string
	Cursor   int
	Limit    int
}

// Inputs is everything one read is built from.
type Inputs struct {
	Settings SettingsState
	Viewer   Viewer
	Presence Presence
}

// Build answers one read.
//
// The cards were the retired Swift app's, and this daemon no longer reads its
// board (docs/cutover.md §7): the catalog is empty, which is the truth of what
// is read, and the answer is the one that app's board answered when it was
// switched off (cutover B1). A selected project is therefore not one this
// board has.
func Build(in Inputs, q Query) (Envelope, int) {
	now := float64(time.Now().UnixNano()) / 1e9
	env := Envelope{
		SchemaVersion:       WireSchemaVersion,
		Revision:            in.Settings.Revision,
		Enabled:             in.Settings.Enabled,
		Mode:                Mode(in.Settings.Enabled),
		UpdatedAt:           in.Settings.UpdatedAt,
		Available:           true,
		Entitlement:         FreePreview(),
		NarrativeConsent:    in.Settings.NarrativeConsent,
		Projects:            []ProjectRow{},
		Items:               []Card{},
		SnapshotBudgetBytes: MaximumSnapshotBytes,
		AutomaticMutation:   map[string]any{"status": "available", "persisted": true, "reason": nil},
		Viewer:              in.Viewer,
	}
	env.ResponsibilitySource = map[string]any{
		"provenance": "session_watch", "complete": in.Presence.Complete,
		"fresh": in.Presence.Fresh, "observedAt": unixOrNil(in.Presence.ObservedAt),
		"generation": in.Presence.Generation,
	}
	env.ReadState = ReadState{Status: "ready", Revision: in.Settings.Revision, AttemptedAt: &now}
	env.SourceIngestion = map[string]any{"status": "complete", "droppedCount": 0, "reasons": []string{},
		"issues": []map[string]any{}}
	env.Source = map[string]any{
		"observedAt": nil, "attemptedAt": now, "truncated": false,
		"ingestion": map[string]any{
			"status": "complete", "issueCount": 0, "reasons": []string{}, "historicalDroppedCount": 0,
		},
	}
	if q.Project != "" {
		env.ReadState.Status = "error"
		env.ReadState.Error = map[string]any{"code": "project_not_found",
			"message": "This board has no such project."}
	}
	return env, 200
}

func unixOrNil(t time.Time) any {
	if t.IsZero() {
		return nil
	}
	return t.Unix()
}
