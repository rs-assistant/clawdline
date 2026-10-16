package app

import (
	"context"
	"testing"

	"github.com/sainteye/clawdline/internal/domain/work"
)

// A schedule's title is the title of every task it dispatches, and a dispatch
// refuses a title that does not read as a result. A schedule saved with such a
// title failed every run with only `bad_task` on the phone; the save is where
// the person can still read why and change it.
func TestAScheduleTitleADispatchWouldRefuseIsRefusedWhenSaved(t *testing.T) {
	f, _ := permissionFixture(t)
	ctx := context.Background()
	device := ScheduleAuthority{}
	colon := "Token delta: trial readout"
	want := work.OutcomeTitleRefusal(colon)
	if want == "" {
		t.Fatal("the fixture title is no longer one a dispatch refuses")
	}

	made := f.book.Create(ctx, formBody(map[string]any{"title": colon}), device)
	if made.Code != "bad_request" || made.Message != want || made.RawMessage {
		t.Fatalf("create with a refused title: %+v", made)
	}

	id := madeID(t, f.book.Create(ctx, formBody(nil), device))
	saved := f.book.Update(ctx, id, formBody(map[string]any{"title": colon}), device)
	if saved.Code != "bad_request" || saved.Message != want {
		t.Fatalf("save with a refused title: %+v", saved)
	}
	record, _, _ := f.book.Detail(ctx, id)
	if record["title"] != "nightly" {
		t.Fatalf("a refused save changed the title: %v", record["title"])
	}
}
