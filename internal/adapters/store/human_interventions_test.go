package store

import (
	"context"
	"database/sql"
	"github.com/sainteye/clawdline/internal/domain/work"
	"path/filepath"
	"testing"
	"time"
)

func TestOlderNotesAcquireWithdrawalStateAndRetainItAcrossReopen(t *testing.T) {
	dir := t.TempDir()
	db, err := sql.Open("sqlite", filepath.Join(dir, DBFile))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(humanInterventionSchema); err != nil {
		t.Fatal(err)
	}
	if err = db.Close(); err != nil {
		t.Fatal(err)
	}
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	at := time.Now().UTC().Truncate(time.Second)
	note := work.HumanIntervention{ID: "note", SourceConversation: "source", TargetConversation: "target", CreatedAt: at, Version: 1}
	if err = s.WriteWorkV2(context.Background(), func(tx *WorkV2Tx) error {
		if err := tx.AddHumanIntervention(note, at); err != nil {
			return err
		}
		next := note
		next.ResolvedAt = &at
		next.Withdrawn = true
		next.Resolution = "No longer needed"
		return tx.PutHumanIntervention(note, next)
	}); err != nil {
		t.Fatal(err)
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	got, err := s.HumanIntervention(context.Background(), note.ID)
	if err != nil || !got.Withdrawn || got.Resolution != "No longer needed" || got.ResolvedAt == nil || got.ReadAt != nil {
		t.Fatalf("persisted withdrawal: %+v %v", got, err)
	}
	rows, err := s.SourceHumanInterventions(context.Background(), "source")
	if err != nil || len(rows) != 1 || !rows[0].Withdrawn {
		t.Fatalf("source list: %+v %v", rows, err)
	}
}
