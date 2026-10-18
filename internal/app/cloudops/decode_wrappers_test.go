package cloudops

import (
	"context"
	"strings"
	"testing"

	"github.com/sainteye/clawdline/internal/adapters/store"
	"github.com/sainteye/clawdline/internal/domain/cloudvocab"
)

// The table's `wrappers` and `idempotency` against what Bridge.Handle does
// around a decoder: a generated decoder replaces the decode only, so the
// rules around it have to be stated where the decoder is, and stated right.
func TestTheTableStatesTheBridgesWrapperRules(t *testing.T) {
	ops, err := cloudvocab.Operations()
	if err != nil {
		t.Fatal(err)
	}
	wrappers, idempotency, err := cloudvocab.Wrappers()
	if err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	stated := map[string]bool{}
	for _, w := range wrappers {
		stated[w.Special] = true
		for _, row := range ops {
			tagged := false
			for _, tag := range row.Special {
				tagged = tagged || tag == w.Special
			}
			if _, known := catalog[row.Op]; !known || len(row.Shapes) == 0 {
				continue
			}
			shape := row.Shapes[len(row.Shapes)-1]
			object := wrapperBody(row, shape)
			cmd := request(t, ClassCtl, object)
			b := receiptBridge(st, &router{})
			if w.Channel != "" {
				cmd.Channel = w.Channel + "/mac-01"
			}
			answer := b.Handle(context.Background(), cmd)
			if refused := answer.Code == w.StripRefusal; refused != tagged {
				t.Errorf("%s without %s: refused %s=%v, and the row is tagged %s: %v",
					row.Op, w.Strip, w.StripRefusal, refused, w.Special, tagged)
			}
			if !tagged || w.Require == "" {
				continue
			}
			for _, s := range row.Shapes {
				if strings.Contains(","+strings.Join(s.Body, ",")+",", ","+w.Require+",") {
					continue
				}
				object := wrapperBody(row, s)
				object[w.Strip] = testExecution
				answer := receiptBridge(st, &router{}).Handle(context.Background(), request(t, ClassCtl, object))
				if answer.Code != w.RequireRefusal {
					t.Errorf("%s shape %v without %s: code %q, the table says %s", row.Op, s.Body, w.Require,
						answer.Code, w.RequireRefusal)
				}
			}
		}
	}
	for _, special := range []string{"session_receipt", "content_read"} {
		if !stated[special] {
			t.Errorf("the table states no wrapper for %s", special)
		}
	}

	// Every local request that is not a GET carries the key: the body's
	// request when it named one, the bridge's own spelling when not. A hand
	// row names its request its own way (schedule-webhook-bind-v1's is
	// `request_id`), so the field name is tried on the generated rows.
	checked := 0
	for _, row := range ops {
		o, known := catalog[row.Op]
		if _, generated := generatedDecoders[row.Op]; !known || !generated || o.route == nil || row.Local == nil {
			continue
		}
		for _, shape := range row.Shapes {
			r := &router{}
			object := wrapperBody(row, shape)
			open(r).Handle(context.Background(), request(t, ClassCtl, object))
			if len(r.seen) == 0 {
				continue
			}
			checked++
			got, sent := r.last().Header[idempotency.Header]
			want, named := object[idempotency.From].(string)
			if !named {
				want = strings.NewReplacer("{sender}", "viewer-device-01", "{sequence}", "411").Replace(idempotency.Fallback)
			}
			switch {
			case row.Local.Method == idempotency.Except && sent:
				t.Errorf("%s shape %v: a %s carries %s %q", row.Op, shape.Body, idempotency.Except, idempotency.Header, got)
			case row.Local.Method != idempotency.Except && got != want:
				t.Errorf("%s shape %v: %s is %q, the table's rule says %q", row.Op, shape.Body, idempotency.Header, got, want)
			}
		}
	}
	if checked < 150 {
		t.Errorf("only %d bodies reached the router; the idempotency rule is barely tried", checked)
	}
}

func wrapperBody(row cloudvocab.Operation, shape cloudvocab.Shape) map[string]any {
	example := row.Example
	if shape.Example != nil {
		example = shape.Example
	}
	object := map[string]any{"type": row.Op}
	for _, field := range shape.Body {
		object[field] = example[field]
	}
	return object
}
