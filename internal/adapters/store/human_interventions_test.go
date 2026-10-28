package store

import (
	"context"
	"database/sql"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/domain/work"
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

const humanInterventionsBeforeCommand = `
CREATE TABLE human_interventions (
 id TEXT PRIMARY KEY,
 source_conversation TEXT NOT NULL,
 source_label TEXT NOT NULL,
 target_conversation TEXT NOT NULL,
 target_session TEXT NOT NULL,
 kind TEXT NOT NULL,
 title TEXT NOT NULL,
 summary TEXT NOT NULL,
 action TEXT NOT NULL,
 reason TEXT NOT NULL,
 detail TEXT NOT NULL,
 options TEXT NOT NULL,
 document_url TEXT NOT NULL,
 created_at INTEGER NOT NULL,
 read_at INTEGER,
 resolved_at INTEGER,
 resolution TEXT NOT NULL DEFAULT '',
 version INTEGER NOT NULL DEFAULT 1
);
INSERT INTO human_interventions (id,source_conversation,source_label,target_conversation,target_session,kind,title,summary,action,reason,detail,options,document_url,created_at)
  VALUES ('old-secret','c-source','Manager','c-target','pane-target','secret','Key','API key','Paste it.','A tool needs it.','','[]','',100);
`

func TestAnOlderSecretHandoffNoteHasNoReceivingCommand(t *testing.T) {
	dir := t.TempDir()
	db, err := sql.Open("sqlite", filepath.Join(dir, DBFile))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(humanInterventionsBeforeCommand); err != nil {
		t.Fatal(err)
	}
	db.Close()

	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	old, err := s.HumanIntervention(ctx, "old-secret")
	if err != nil || old.Kind != "secret" || old.Command != nil {
		t.Fatalf("migrated Note = %+v, %v", old, err)
	}
	command := []string{"/usr/bin/gh", "auth", "login", "--with-token"}
	at := time.Unix(200, 0).UTC()
	note := work.HumanIntervention{ID: "new-secret", SourceConversation: "c-source", SourceLabel: "Manager",
		TargetConversation: "c-target", TargetSession: "pane-target", Kind: "secret", Title: "Key", Summary: "API key",
		Action: "Paste it.", Reason: "A tool needs it.", Command: command, CreatedAt: at, Version: 1}
	if err := s.WriteWorkV2(ctx, func(tx *WorkV2Tx) error { return tx.AddHumanIntervention(note, at) }); err != nil {
		t.Fatal(err)
	}
	got, err := s.HumanIntervention(ctx, "new-secret")
	if err != nil || !slices.Equal(got.Command, command) {
		t.Fatalf("new Note command = %q, %v", got.Command, err)
	}
	s.Close()
	again, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	again.Close()
}
