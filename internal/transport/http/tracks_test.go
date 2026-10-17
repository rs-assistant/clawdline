package http

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	boardstore "github.com/sainteye/clawdline/internal/adapters/board"
	"github.com/sainteye/clawdline/internal/adapters/store"
	"github.com/sainteye/clawdline/internal/config"
	"github.com/sainteye/clawdline/internal/domain/work"
)

// tracksFixture is a daemon with its own store, beside a canary: a retired
// Swift board of 251 cards and their logs, where that app kept them and where
// its override pointed. The route must read none of it (docs/cutover.md §7).
type tracksFixture struct {
	s       *Server
	st      *store.Store
	root    string
	next    string
	history string
}

func newTracksFixture(t *testing.T) *tracksFixture {
	t.Helper()
	root := t.TempDir()
	next := filepath.Join(root, "next")
	boardPath := filepath.Join(root, "Library", "Application Support", "Clawdline", "project-board.json")
	history := filepath.Join(filepath.Dir(boardPath), "project-board-history")
	if err := os.MkdirAll(history, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", root)
	t.Setenv("CLAWDLINE_BOARD_STORE", boardPath)
	items := []map[string]any{}
	for i := 0; i < 251; i++ {
		project := "p1"
		if i == 250 {
			project = "p2"
		}
		id := fmt.Sprintf("card-%03d", i)
		items = append(items, map[string]any{"id": id, "key": fmt.Sprintf("CLA-%d", i), "projectId": project,
			"title": "work " + id, "type": "feature", "state": "backlog",
			"createdAt": 1_789_000_000 + i, "updatedAt": 1_789_000_000 + i})
		line := fmt.Sprintf(`{"kind":"item_created","at":%d}`+"\n", 1_789_000_000+i)
		if err := os.WriteFile(filepath.Join(history, id+".jsonl"), []byte(line), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	body, _ := json.Marshal(map[string]any{"schemaVersion": 2, "revision": 42, "items": items,
		"projects": []map[string]any{{"id": "p1", "name": "one"}, {"id": "p2", "name": "two"}}})
	if err := os.WriteFile(boardPath, body, 0o600); err != nil {
		t.Fatal(err)
	}

	st, err := store.Open(next)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	s := &Server{cfg: config.Config{Dir: next}, store: st}
	boardByServer.Store(s, &boardDeps{settings: boardstore.OpenSettings(next)})
	tracksByServer.Store(s, &tracksDeps{
		presence: func(context.Context) (work.Presence, work.PresenceSource) {
			return work.Presence{}, work.PresenceSource{From: "inventory"}
		},
		now: func() time.Time { return time.Unix(1_790_000_000, 0) },
	})
	t.Cleanup(func() { boardByServer.Delete(s); tracksByServer.Delete(s) })
	return &tracksFixture{s: s, st: st, root: root, next: next, history: history}
}

func (f *tracksFixture) get(t *testing.T, method, target string) (*httptest.ResponseRecorder, work.Tracks) {
	t.Helper()
	rec := httptest.NewRecorder()
	f.s.boardTracks(rec, httptest.NewRequest(method, target, nil))
	var body struct {
		Tracks work.Tracks `json:"tracks"`
	}
	if rec.Code == http.StatusOK {
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("%s: %v", target, err)
		}
	}
	return rec, body.Tracks
}

func TestTracksRoute(t *testing.T) {
	f := newTracksFixture(t)

	rec, all := f.get(t, http.MethodGet, "/v1/board/tracks")
	if rec.Code != http.StatusOK || all.Counts.Cards != 0 || len(all.Rows) != 0 || all.NextCursor != nil || all.Project != nil {
		t.Fatalf("the retired board's cards were read: %d %+v", rec.Code, all.Counts)
	}
	if all.Sources.Board != (work.BoardSource{Status: "disabled"}) ||
		all.Sources.History != (work.HistorySource{Status: "disabled"}) || all.Sources.Presence.From != "inventory" {
		t.Fatalf("sources: %+v", all.Sources)
	}
	if all.EvaluatedAt != 1_790_000_000 || all.Rules != work.RulesVersion || all.PageSize != TracksPageSize {
		t.Fatalf("projection: %v %s %d", all.EvaluatedAt, all.Rules, all.PageSize)
	}
	if rec, todo := f.get(t, http.MethodGet, "/v1/board/tracks?track=todo"); rec.Code != http.StatusOK || todo.Counts != all.Counts {
		t.Fatalf("one track: %d %+v", rec.Code, todo.Counts)
	}

	for target, code := range map[string]string{
		// p1 is a project of the canary board; no board this daemon reads has it.
		"/v1/board/tracks?project=p1":            "project_not_found",
		"/v1/board/tracks?audience=human":        "bad_request",
		"/v1/board/tracks?project=p1&project=p2": "bad_request",
		"/v1/board/tracks?project=":              "bad_request",
		"/v1/board/tracks?track=Board":           "invalid_track",
		"/v1/board/tracks?cursor=nine":           "invalid_cursor",
	} {
		rec, _ := f.get(t, http.MethodGet, target)
		var refusal struct{ Error string }
		_ = json.Unmarshal(rec.Body.Bytes(), &refusal)
		if refusal.Error != code || rec.Code < 400 {
			t.Errorf("%s: %d %s, want %s", target, rec.Code, rec.Body, code)
		}
	}
	if rec, _ := f.get(t, http.MethodPost, "/v1/board/tracks"); rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("POST: %d", rec.Code)
	}
}

// stamp is everything a write could change about one path.
type stamp struct {
	mode  fs.FileMode
	size  int64
	mtime time.Time
	sum   string
}

func stampTree(t *testing.T, roots ...string) map[string]stamp {
	t.Helper()
	out := map[string]stamp{}
	for _, root := range roots {
		err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			info, err := d.Info()
			if err != nil {
				return err
			}
			s := stamp{mode: info.Mode(), size: info.Size(), mtime: info.ModTime()}
			if d.Type().IsRegular() {
				body, err := os.ReadFile(path)
				if err != nil {
					return err
				}
				sum := sha256.Sum256(body)
				s.sum = hex.EncodeToString(sum[:])
			}
			out[path] = s
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	return out
}

func changedPaths(before, after map[string]stamp) []string {
	var changed []string
	for path, b := range before {
		if a, ok := after[path]; !ok || a != b {
			changed = append(changed, path)
		}
	}
	for path := range after {
		if _, ok := before[path]; !ok {
			changed = append(changed, path)
		}
	}
	return changed
}

// dataVersion is SQLite's count of commits made by other connections, read
// on one connection held for the whole test.
func dataVersion(t *testing.T, conn *sql.Conn) int64 {
	t.Helper()
	var v int64
	if err := conn.QueryRowContext(context.Background(), "PRAGMA data_version").Scan(&v); err != nil {
		t.Fatal(err)
	}
	return v
}

// Reading the tracks writes nothing: not a file under the board or the
// daemon's own directory, not a row through the daemon's store connection,
// not a commit any other connection could see. Each of the three detectors is
// then shown to see a write when there is one.
func TestTracksWriteNothing(t *testing.T) {
	f := newTracksFixture(t)
	ctx := context.Background()
	other, err := sql.Open("sqlite", filepath.Join(f.next, "clawdline.sqlite3"))
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	conn, err := other.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	// The counters are read before the stamps are taken and after the second
	// set, so their own reads fall outside what the stamps compare.
	changes, writes, version := f.st.Health(ctx).Changes, f.st.Stats().Writes, dataVersion(t, conn)
	before := stampTree(t, f.root)

	var cursor string
	for _, target := range []string{"/v1/board/tracks", "/v1/board/tracks?project=p1",
		"/v1/board/tracks?track=backlog", "/v1/board/tracks?project=nope", "/v1/board/tracks?cursor=nine"} {
		rec, got := f.get(t, http.MethodGet, target)
		if got.NextCursor != nil {
			cursor = *got.NextCursor
		}
		if rec.Code >= 500 {
			t.Fatalf("%s: %d", target, rec.Code)
		}
	}
	f.get(t, http.MethodGet, "/v1/board/tracks?cursor="+cursor)
	f.get(t, http.MethodPost, "/v1/board/tracks")

	after := stampTree(t, f.root)
	if changed := changedPaths(before, after); len(changed) != 0 {
		t.Fatalf("reading the tracks changed %v", changed)
	}
	if c, w, v := f.st.Health(ctx).Changes, f.st.Stats().Writes, dataVersion(t, conn); c != changes || w != writes || v != version {
		t.Fatalf("store moved: total_changes %d→%d, writes %d→%d, data_version %d→%d", changes, c, writes, w, version, v)
	}
	t.Logf("%d paths unchanged; total_changes %d, writes %d, data_version %d, before and after", len(after), changes, writes, version)

	// Controls, same detectors.
	if err := f.st.Append(ctx, store.Event{Kind: "tracks.control", Subject: "t1"}); err != nil {
		t.Fatal(err)
	}
	if c, w, v := f.st.Health(ctx).Changes, f.st.Stats().Writes, dataVersion(t, conn); c != changes+1 || w != writes+1 || v == version {
		t.Fatalf("control: one store write was not seen: total_changes %d→%d, writes %d→%d, data_version %d→%d",
			changes, c, writes, w, version, v)
	}
	log, err := os.OpenFile(filepath.Join(f.history, "card-000.jsonl"), os.O_WRONLY|os.O_APPEND, 0)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = log.WriteString(`{"kind":"span_started","at":1789000001}` + "\n")
	log.Close()
	if changed := changedPaths(after, stampTree(t, f.root)); len(changed) == 0 {
		t.Fatalf("control: an appended log line was not seen")
	}
}
