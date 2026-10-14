package orchestrator

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"

	"github.com/sainteye/clawdline/internal/adapters/store"
	"github.com/sainteye/clawdline/internal/productcopy"
)

const (
	EffectSessionDeliveryPush = "session.delivery.push"
	EffectFanoutCompletedPush = "task.batch.completed.push"
)

type completionPush struct {
	Terminal string `json:"terminal"`
	Language string `json:"language"`
	Tag      string `json:"tag"`
	Summary  string `json:"summary,omitempty"`
	Count    int    `json:"count,omitempty"`
	Failed   int    `json:"failed,omitempty"`
	Titles   string `json:"titles,omitempty"`
}

func (b *Broker) notificationLanguage() string {
	return productcopy.Resolve(b.ProductLanguage())
}

func runCompletionPush(ctx context.Context, b *Broker, e store.Effect) effectResult {
	var p completionPush
	if err := json.Unmarshal(e.Payload, &p); err != nil || p.Terminal == "" || p.Tag == "" {
		return effectResult{state: store.EffectFailed, outcome: "the effect carries no completion"}
	}
	key := "push_on_delivery"
	if e.Kind == EffectFanoutCompletedPush {
		key = "push_on_fanout"
	}
	if b.PushEnabled != nil && !b.PushEnabled(key) {
		return effectResult{state: store.EffectDone, outcome: "disabled_by_user"}
	}
	if b.Push == nil {
		return effectResult{state: store.EffectFailed, outcome: "this daemon cannot push"}
	}
	values := map[string]string{"count": strconv.Itoa(p.Count), "failed": strconv.Itoa(p.Failed)}
	title, body := productcopy.Format(p.Language, "delivery.title", nil), productcopy.Format(p.Language, "delivery.body", nil)
	if e.Kind == EffectFanoutCompletedPush {
		title, body = productcopy.Format(p.Language, "fanout.title", nil), productcopy.Format(p.Language, "fanout.body", values)
	}
	if b.PushEnabled != nil && b.PushEnabled("smart_notifications") {
		if e.Kind == EffectSessionDeliveryPush && p.Summary != "" {
			body = p.Summary
		} else if e.Kind == EffectFanoutCompletedPush && p.Titles != "" {
			body += " " + p.Titles
		}
	}
	sent, failed, err := b.Push(ctx, title, body, p.Terminal, p.Tag)
	result, _ := json.Marshal(map[string]any{"effect": e.ID, "terminal": p.Terminal, "sent": sent, "failed": failed})
	events := []store.Event{{Kind: strings.TrimSuffix(e.Kind, ".push") + ".pushed", Subject: e.Subject, Payload: result}}
	switch {
	case err != nil:
		return effectResult{state: store.EffectFailed, outcome: err.Error(), events: events}
	case sent == 0 && failed == 0:
		return effectResult{state: store.EffectDone, outcome: "not_subscribed", events: events}
	case sent == 0:
		return effectResult{state: store.EffectFailed, outcome: "no push service accepted it", events: events}
	default:
		return effectResult{state: store.EffectDone, outcome: "pushed", events: events}
	}
}

func (b *Broker) batchFinishedEffects(tx *store.Tx, finished Record) ([]store.Effect, error) {
	if finished.BatchID == "" || finished.RootTerminalID == "" {
		return nil, nil
	}
	rows, err := tx.BrokerBatchRows(finished.BatchID)
	if err != nil {
		return nil, err
	}
	count, failed := 0, 0
	var titles []string
	for _, row := range rows {
		r, err := Decode(row.Record)
		if err != nil {
			return nil, err
		}
		if r.BatchID != finished.BatchID || r.RootTerminalID != finished.RootTerminalID {
			continue
		}
		if r.ID == finished.ID {
			r = finished
		}
		if !r.State.Terminal() {
			return nil, nil
		}
		count++
		if r.State != StateSuccess {
			failed++
		}
		if len(titles) < 2 && r.Title != "" {
			titles = append(titles, r.Title)
		}
	}
	if count < 2 {
		return nil, nil
	}
	payload, _ := json.Marshal(completionPush{
		Terminal: finished.RootTerminalID, Language: b.notificationLanguage(),
		Tag: "task-batch-" + finished.BatchID, Count: count, Failed: failed,
		Titles: strings.Join(titles, ", "),
	})
	return []store.Effect{{Kind: EffectFanoutCompletedPush, Subject: finished.BatchID, Payload: payload}}, nil
}
