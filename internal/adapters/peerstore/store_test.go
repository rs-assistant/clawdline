package peerstore

import (
	"context"
	"crypto/ecdh"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/domain/agenthandoff"
	"github.com/sainteye/clawdline/internal/domain/cloud"
)

func TestPeerPinsGrantsAndReceiptsSurviveRestartAndLocalRevoke(t *testing.T) {
	ctx := context.Background()
	dir := filepath.Join(t.TempDir(), "private-peers")
	st, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	source := ed25519.NewKeyFromSeed(fill(1))
	target := ed25519.NewKeyFromSeed(fill(2))
	sourceX, _ := ecdh.X25519().NewPrivateKey(fill(3))
	targetX, _ := ecdh.X25519().NewPrivateKey(fill(4))
	pair := Pair{ID: "pair-1", AccountID: "account-1", SourceMachineID: "machine-a", TargetMachineID: "machine-b",
		SourcePublicKey:     base64.StdEncoding.EncodeToString(source.Public().(ed25519.PublicKey)),
		TargetPublicKey:     base64.StdEncoding.EncodeToString(target.Public().(ed25519.PublicKey)),
		SourceFingerprint:   cloud.Fingerprint(source.Public().(ed25519.PublicKey)),
		TargetFingerprint:   cloud.Fingerprint(target.Public().(ed25519.PublicKey)),
		SourceEncryptionKey: base64.StdEncoding.EncodeToString(sourceX.PublicKey().Bytes()),
		TargetEncryptionKey: base64.StdEncoding.EncodeToString(targetX.PublicKey().Bytes()),
		LocalPrivateKey:     sourceX.Bytes(), ExpiresAt: time.Now().Add(2 * time.Hour)}
	transcript := agenthandoff.PairTranscript{PairID: pair.ID, SourceMachineID: pair.SourceMachineID,
		TargetMachineID: pair.TargetMachineID, SourcePublicKey: pair.SourcePublicKey, TargetPublicKey: pair.TargetPublicKey,
		SourceFingerprint: pair.SourceFingerprint, TargetFingerprint: pair.TargetFingerprint,
		SourceEncryptionKey: pair.SourceEncryptionKey, TargetEncryptionKey: pair.TargetEncryptionKey}
	pair.SourceSignature = agenthandoff.SignOffer(transcript, source)
	transcript.SourceOfferSignature = pair.SourceSignature
	pair.TargetSignature = agenthandoff.SignAcceptance(transcript, target)
	pending := pair
	pending.TargetPublicKey, pending.TargetEncryptionKey, pending.TargetSignature = "", "", ""
	if err := st.SavePending(ctx, pending); err != nil {
		t.Fatal(err)
	}
	if err := st.PinPair(ctx, pair, "machine-a", pair.TargetFingerprint); err != nil {
		t.Fatal(err)
	}
	if err := st.PinPair(ctx, pair, "machine-a", pair.TargetFingerprint); err != nil {
		t.Fatalf("idempotent pair pin: %v", err)
	}
	grant := Grant{ID: "grant-1", PairID: pair.ID,
		Source: agenthandoff.Endpoint{MachineID: "machine-a", SessionID: "session-a",
			ExecutionGeneration: "11111111111111111111111111111111"},
		Target: agenthandoff.Endpoint{MachineID: "machine-b", SessionID: "session-b",
			ExecutionGeneration: "22222222222222222222222222222222"},
		SourceKeyFingerprint: pair.SourceFingerprint, AllowMessage: true,
		ExpiresAt: time.Now().Add(time.Hour)}
	if err := st.PinGrant(ctx, grant); err != nil {
		t.Fatal(err)
	}
	body := []byte("hello")
	sum := sha256.Sum256(body)
	request := agenthandoff.Request{RequestID: "request-1", Kind: agenthandoff.Message,
		Source: grant.Source, Target: grant.Target, GrantID: grant.ID, BodyDigest: hex.EncodeToString(sum[:])}
	if err := st.SaveOutbox(ctx, OutboxItem{Request: request, RelayAccepted: "unknown", RelayDelivered: "unknown", At: time.Now()}); err != nil {
		t.Fatal(err)
	}
	if saved, err := st.Outbox(ctx, request.RequestID); err != nil || saved.RelayAccepted != "unknown" || saved.RelayDelivered != "unknown" {
		t.Fatalf("a socket write cannot prove relay acceptance: %+v, %v", saved, err)
	}
	if err := st.MarkOutbox(ctx, request.RequestID, "accepted", "delivered"); err != nil {
		t.Fatal(err)
	}
	if err := st.MarkOutbox(ctx, request.RequestID, "accepted", "machine_offline"); err != nil {
		t.Fatal(err)
	}
	if err := st.MarkOutbox(ctx, request.RequestID, "accepted", "delivered"); err != nil {
		t.Fatal(err)
	}
	if err := st.PutInbox(ctx, InboxItem{Request: request, Body: string(body), At: time.Now()}); err != nil {
		t.Fatal(err)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	st, err = Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if saved, err := st.Outbox(ctx, request.RequestID); err != nil || saved.RelayDelivered != "unknown" || !saved.RelayConflict {
		t.Fatalf("relay evidence after restart: %+v, %v", saved, err)
	}
	if saved, err := st.Inbox(ctx, request.RequestID); err != nil || saved.Body != "hello" {
		t.Fatalf("inbox after restart: %+v, %v", saved, err)
	}
	if usage, err := ReadUsage(ctx, dir); err != nil || usage != (Usage{Pairs: 1, Grants: 1, Inbox: 1, Outbox: 1}) {
		t.Fatalf("peer capacity after restart: %+v, %v", usage, err)
	}
	if err := st.RevokeGrant(ctx, grant.ID, time.Now()); err != nil {
		t.Fatal(err)
	}
	if saved, err := st.Grant(ctx, grant.ID); err != nil || saved.RevokedAt.IsZero() {
		t.Fatalf("local grant revocation was not durable: %+v, %v", saved, err)
	}
	if err := st.RevokePair(ctx, pair.ID, time.Now()); err != nil {
		t.Fatal(err)
	}
	if saved, err := st.Pair(ctx, pair.ID); err != nil || saved.RevokedAt.IsZero() {
		t.Fatalf("local pair revocation was not durable: %+v, %v", saved, err)
	}
}

func TestTaskStartOutboxKeepsRelayEvidenceSeparateFromTargetExecution(t *testing.T) {
	ctx := context.Background()
	dir := filepath.Join(t.TempDir(), "private-peers")
	st, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	start := agenthandoff.TaskStart{TaskID: "11111111-1111-4111-8111-111111111111", RequestID: "request-1",
		Source: agenthandoff.Endpoint{MachineID: "machine-a", SessionID: "session-a",
			ExecutionGeneration: "11111111111111111111111111111111"},
		TargetMachineID: "machine-b", TargetProjectID: "project-b", PairID: "pair-1",
		BodyDigest: "a" + "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}
	item := TaskStartOutboxItem{Start: start, Frame: []byte(`{"sealed":"first"}`),
		RelayAccepted: "unknown", RelayDelivered: "unknown", At: time.Now()}
	if err := st.SaveTaskStartOutbox(ctx, item); err != nil {
		t.Fatal(err)
	}
	if err := st.SaveTaskStartOutbox(ctx, item); err != nil {
		t.Fatalf("identical replay: %v", err)
	}
	changed := item
	changed.Start.TargetProjectID = "project-c"
	if err := st.SaveTaskStartOutbox(ctx, changed); err == nil {
		t.Fatal("same task ID changed its Project")
	}
	changed = item
	changed.Frame = []byte(`{"sealed":"different"}`)
	if err := st.SaveTaskStartOutbox(ctx, changed); err == nil {
		t.Fatal("same task ID changed its signed frame")
	}
	changed = item
	changed.Start.TaskID = "22222222-2222-4222-8222-222222222222"
	if err := st.SaveTaskStartOutbox(ctx, changed); err == nil {
		t.Fatal("request ID was reused for another task")
	}
	if err := st.MarkTaskStartOutbox(ctx, start.TaskID, "delivered"); err != nil {
		t.Fatal(err)
	}
	if err := st.MarkTaskStartOutbox(ctx, start.TaskID, "machine_offline"); err != nil {
		t.Fatal(err)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	st, err = Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	saved, err := st.TaskStartOutboxByRequest(ctx, start.RequestID)
	if err != nil || saved.Start != start || string(saved.Frame) != string(item.Frame) ||
		saved.RelayDelivered != "delivered" || saved.RelayConflict {
		t.Fatalf("outbox after restart: %+v, %v", saved, err)
	}
	if usage, err := ReadUsage(ctx, dir); err != nil || usage.Outbox != 1 {
		t.Fatalf("task start outbox usage: %+v, %v", usage, err)
	}
}

func TestTaskEventsSurviveRestartAndCannotSwitchTheTargetExecution(t *testing.T) {
	ctx := context.Background()
	dir := filepath.Join(t.TempDir(), "private-peers")
	st, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	start := agenthandoff.TaskStart{TaskID: "11111111-1111-4111-8111-111111111111", RequestID: "request-1",
		Source: agenthandoff.Endpoint{MachineID: "source", SessionID: "source-session",
			ExecutionGeneration: "11111111111111111111111111111111"},
		TargetMachineID: "target", TargetProjectID: "project", PairID: "pair",
		BodyDigest: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}
	item := TaskStartOutboxItem{Start: start, Frame: []byte(`{"sealed":"start"}`),
		RelayAccepted: "unknown", RelayDelivered: "unknown", At: time.Now()}
	if err := st.SaveTaskStartOutbox(ctx, item); err != nil {
		t.Fatal(err)
	}
	if err := st.MarkTaskStartOpened(ctx, start.TaskID, "target-session", "22222222222222222222222222222222"); err != nil {
		t.Fatal(err)
	}
	if err := st.MarkTaskStartOpened(ctx, start.TaskID, "target-session", "33333333333333333333333333333333"); err == nil {
		t.Fatal("a second target execution replaced the first")
	}
	body := "Inventory complete."
	sum := sha256.Sum256([]byte(body))
	event := agenthandoff.TaskEvent{Start: start, RequestID: "finish-1", Kind: agenthandoff.TaskEventFinish,
		Sender: agenthandoff.Endpoint{MachineID: "target", SessionID: "target-session",
			ExecutionGeneration: "22222222222222222222222222222222"},
		Recipient: start.Source, BodyDigest: hex.EncodeToString(sum[:])}
	record := TaskEventRecord{Event: event, Direction: "in", Body: body, At: time.Now()}
	if err := st.SaveTaskEvent(ctx, record); err != nil {
		t.Fatal(err)
	}
	if err := st.SaveTaskEvent(ctx, record); err != nil {
		t.Fatalf("identical retry: %v", err)
	}
	changed := record
	changed.Body = "different result"
	if err := st.SaveTaskEvent(ctx, changed); err == nil {
		t.Fatal("one request ID changed its result")
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	st, err = Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	rows, err := st.TaskEvents(ctx, start.TaskID)
	if err != nil || len(rows) != 1 || rows[0].Body != body {
		t.Fatalf("task result after restart: %+v, %v", rows, err)
	}
	if usage, err := ReadUsage(ctx, dir); err != nil || usage.Inbox != 1 || usage.Outbox != 1 {
		t.Fatalf("event capacity usage: %+v, %v", usage, err)
	}
}

func TestRemoteTaskClaimSurvivesRestartAndCannotSwitchExecution(t *testing.T) {
	ctx := context.Background()
	dir := filepath.Join(t.TempDir(), "private-peers")
	st, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	instructions := `{"assistant":"claude","title":"Disk inventory","instructions":"Inspect disk usage without deleting files."}`
	digest := sha256.Sum256([]byte(instructions))
	start := agenthandoff.TaskStart{TaskID: "11111111-1111-4111-8111-111111111111", RequestID: "request-1",
		Source: agenthandoff.Endpoint{MachineID: "machine-a", SessionID: "session-a",
			ExecutionGeneration: "11111111111111111111111111111111"},
		TargetMachineID: "machine-b", TargetProjectID: "project-b", PairID: "pair-1",
		BodyDigest: hex.EncodeToString(digest[:])}
	task := RemoteTask{Start: start, Body: instructions, State: "accepted", At: time.Now()}
	if err := st.SaveRemoteTask(ctx, task); err != nil {
		t.Fatal(err)
	}
	if err := st.SaveRemoteTask(ctx, task); err != nil {
		t.Fatalf("identical replay: %v", err)
	}
	changed := task
	changed.Start.TaskID = "22222222-2222-4222-8222-222222222222"
	if err := st.SaveRemoteTask(ctx, changed); err == nil {
		t.Fatal("same request ID started another task")
	}
	if err := st.MarkRemoteTaskOpened(ctx, start.TaskID, "target-session", "22222222222222222222222222222222"); err != nil {
		t.Fatal(err)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	st, err = Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := st.MarkRemoteTaskOpened(ctx, start.TaskID, "target-session", "22222222222222222222222222222222"); err != nil {
		t.Fatalf("same opening replay: %v", err)
	}
	if err := st.MarkRemoteTaskOpened(ctx, start.TaskID, "other-session", "33333333333333333333333333333333"); err == nil {
		t.Fatal("replay switched the opened Session")
	}
	saved, err := st.RemoteTask(ctx, start.TaskID)
	if err != nil || saved.TargetSessionID != "target-session" || saved.TargetGeneration != "22222222222222222222222222222222" {
		t.Fatalf("target execution after restart: %+v, %v", saved, err)
	}
	if usage, err := ReadUsage(ctx, dir); err != nil || usage.Inbox != 1 {
		t.Fatalf("target task usage: %+v, %v", usage, err)
	}
}

func TestPeerInboxPagePinsExecutionAndBoundsTheCloudReply(t *testing.T) {
	ctx := context.Background()
	st, err := Open(filepath.Join(t.TempDir(), "private-peers"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	target := agenthandoff.Endpoint{MachineID: "machine-b", SessionID: "session-b",
		ExecutionGeneration: "22222222222222222222222222222222"}
	for _, row := range []struct{ id, generation string }{
		{"first", target.ExecutionGeneration},
		{"other-generation", "33333333333333333333333333333333"},
		{"last", target.ExecutionGeneration},
	} {
		request := agenthandoff.Request{RequestID: row.id, Target: target}
		request.Target.ExecutionGeneration = row.generation
		if err := st.PutInbox(ctx, InboxItem{Request: request, Body: row.id, At: time.Now()}); err != nil {
			t.Fatal(err)
		}
	}
	first, cursor, err := st.InboxPage(ctx, target, "")
	if err != nil || len(first) != 1 || first[0].Request.RequestID != "last" || cursor != "last" {
		t.Fatalf("first page: %+v, %q, %v", first, cursor, err)
	}
	second, cursor, err := st.InboxPage(ctx, target, cursor)
	if err != nil || len(second) != 1 || second[0].Request.RequestID != "first" || cursor != "" {
		t.Fatalf("second page: %+v, %q, %v", second, cursor, err)
	}
	if _, _, err := st.InboxPage(ctx, target, "other-generation"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("foreign generation cursor accepted: %v", err)
	}
}

func TestPeerAuthorityListsRefuseAnOversizedStore(t *testing.T) {
	ctx := context.Background()
	st, err := Open(filepath.Join(t.TempDir(), "private-peers"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	// One transaction for each fill: a commit per row synced 640 times and
	// took 18 s, and the bound under test is the list's, not the insert's.
	fill := func(n int, query string, args func(i int) []any) {
		t.Helper()
		tx, err := st.db.BeginTx(ctx, nil)
		if err != nil {
			t.Fatal(err)
		}
		for i := 0; i < n; i++ {
			if _, err := tx.ExecContext(ctx, query, args(i)...); err != nil {
				_ = tx.Rollback()
				t.Fatal(err)
			}
		}
		if err := tx.Commit(); err != nil {
			t.Fatal(err)
		}
	}
	fill(PairLimit+1, "INSERT INTO peer_pairs(id,body) VALUES(?,?)", func(i int) []any {
		id := fmt.Sprintf("pair-%d", i)
		return []any{id, []byte(fmt.Sprintf(`{"id":%q}`, id))}
	})
	if _, err := st.ListPairs(ctx); !errors.Is(err, ErrFull) {
		t.Fatalf("oversized pair authority list: %v", err)
	}
	fill(GrantLimit+1, "INSERT INTO peer_grants(id,pair_id,body) VALUES(?,?,?)", func(i int) []any {
		id := fmt.Sprintf("grant-%d", i)
		return []any{id, "pair", []byte(fmt.Sprintf(`{"id":%q,"pair_id":"pair"}`, id))}
	})
	if _, err := st.ListGrants(ctx); !errors.Is(err, ErrFull) {
		t.Fatalf("oversized grant authority list: %v", err)
	}
}

func TestPeerAuthorityListRejectsARecordWhoseBodyNamesAnotherRevocationID(t *testing.T) {
	ctx := context.Background()
	st, err := Open(filepath.Join(t.TempDir(), "private-peers"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if _, err := st.db.ExecContext(ctx, "INSERT INTO peer_pairs(id,body) VALUES(?,?)",
		"actual-pair", []byte(`{"id":"wrong-pair"}`)); err != nil {
		t.Fatal(err)
	}
	if _, err := st.ListPairs(ctx); err == nil {
		t.Fatal("pair list presented an ID that could not revoke its row")
	}
	if _, err := st.db.ExecContext(ctx, "INSERT INTO peer_grants(id,pair_id,body) VALUES(?,?,?)",
		"actual-grant", "actual-pair", []byte(`{"id":"wrong-grant","pair_id":"actual-pair"}`)); err != nil {
		t.Fatal(err)
	}
	if _, err := st.ListGrants(ctx); err == nil {
		t.Fatal("grant list presented an ID that could not revoke its row")
	}
}

func fill(value byte) []byte {
	bytes := make([]byte, 32)
	for i := range bytes {
		bytes[i] = value
	}
	return bytes
}
