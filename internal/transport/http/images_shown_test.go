package http

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/artifacts"
	"github.com/sainteye/clawdline/internal/adapters/transcript"
	"github.com/sainteye/clawdline/internal/contract"
	"github.com/sainteye/clawdline/internal/domain/session"
)

func claudeRow(at, text string) string {
	return `{"type":"assistant","timestamp":"` + at + `","message":{"role":"assistant","content":[{"type":"text","text":"` + text + `"}]}}` + "\n"
}

func toolRow(at string) string {
	return `{"type":"assistant","timestamp":"` + at + `","message":{"role":"assistant","content":[{"type":"tool_use","id":"t1","name":"Bash","input":{"command":"ls"}}]}}` + "\n"
}

func appendRows(t *testing.T, path string, rows ...string) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	for _, r := range rows {
		if _, err := f.WriteString(r); err != nil {
			t.Fatal(err)
		}
	}
}

func shownPicture(t *testing.T, s *Server, record string) string {
	t.Helper()
	var buf bytes.Buffer
	_ = png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 4, 3)))
	src := filepath.Join(t.TempDir(), "shot.png")
	if err := os.WriteFile(src, buf.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	offset, ok := recordEnd(record)
	if !ok {
		t.Fatal("no record end")
	}
	stored, err := s.pictures.store.ImportShown(context.Background(), []string{src}, time.Now(), artifacts.Shown{Record: record, Offset: offset})
	if err != nil {
		t.Fatal(err)
	}
	return stored[0].Artifact.ID
}

func picturesIn(rows []contract.TranscriptEntry) map[string][]string {
	out := map[string][]string{}
	for _, r := range rows {
		for _, a := range r.Artifacts {
			out[r.Text] = append(out[r.Text], a.ID)
		}
	}
	return out
}

// A picture stored as shown in a Session appears under the first reply the
// Session writes after it — with no marker anywhere, the way a reply whose
// prose a model kept only as a summary reaches the record — and only there,
// on the newest page, on an older page, and on the rows read after a cursor.
func TestAPictureShownInASessionAppearsUnderItsNextReply(t *testing.T) {
	record := filepath.Join(t.TempDir(), "conversation.jsonl")
	appendRows(t, record,
		claudeRow("2026-10-15T10:00:00Z", "before the picture"),
		toolRow("2026-10-15T10:00:01Z"))
	s := &Server{pictures: pictures{store: artifacts.NewStore(t.TempDir())}}
	id := shownPicture(t, s, record)
	item := session.Session{Assistant: session.AssistantClaude}

	read := func() []contract.TranscriptEntry {
		page, err := transcript.ReadClaudeBefore(record, 200, 0)
		if err != nil {
			t.Fatal(err)
		}
		rows := make([]contract.TranscriptEntry, len(page.Entries))
		for i, e := range page.Entries {
			rows[i] = transcriptEntry(e)
		}
		s.showPictures(item, record, page.Entries, rows, time.Now())
		return rows
	}
	if got := picturesIn(read()); len(got) != 0 {
		t.Fatalf("before any reply the picture was drawn under %v", got)
	}

	// The same second as the upload, then a later reply: only the first is it.
	cursor, _ := recordEnd(record)
	appendRows(t, record,
		claudeRow("2026-10-15T10:00:01Z", "here are the designs"),
		claudeRow("2026-10-15T10:00:05Z", "anything else"))
	got := picturesIn(read())
	if len(got) != 1 || len(got["here are the designs"]) != 1 || got["here are the designs"][0] != id {
		t.Fatalf("pictures drawn under %v, want only %s under the reply after it", got, id)
	}
	if again := picturesIn(read()); len(again["here are the designs"]) != 1 {
		t.Fatalf("a second read drew it %d times", len(again["here are the designs"]))
	}

	// A reader asking for the rows after its cursor sees it under the same entry.
	page, err := transcript.ReadClaudeAfter(record, 200, cursor)
	if err != nil {
		t.Fatal(err)
	}
	rows := make([]contract.TranscriptEntry, len(page.Entries))
	for i, e := range page.Entries {
		rows[i] = transcriptEntry(e)
	}
	s.showPictures(item, record, page.Entries, rows, time.Now())
	if got := picturesIn(rows); len(got) != 1 || len(got["here are the designs"]) != 1 {
		t.Fatalf("rows after the cursor drew %v", got)
	}

	// Another conversation's record shows none of it.
	other := filepath.Join(t.TempDir(), "other.jsonl")
	appendRows(t, other, claudeRow("2026-10-15T10:00:09Z", "elsewhere"))
	otherPage, _ := transcript.ReadClaudeBefore(other, 200, 0)
	otherRows := []contract.TranscriptEntry{transcriptEntry(otherPage.Entries[0])}
	s.showPictures(item, other, otherPage.Entries, otherRows, time.Now())
	if got := picturesIn(otherRows); len(got) != 0 {
		t.Fatalf("another record drew %v", got)
	}
}

// A picture whose reply also carries its marker is drawn once.
func TestAShownPictureWithItsMarkerIsDrawnOnce(t *testing.T) {
	record := filepath.Join(t.TempDir(), "conversation.jsonl")
	appendRows(t, record, toolRow("2026-10-15T10:00:01Z"))
	s := &Server{pictures: pictures{store: artifacts.NewStore(t.TempDir())}}
	id := shownPicture(t, s, record)
	appendRows(t, record, claudeRow("2026-10-15T10:00:02Z", `see <clawdline-image id=\"`+id+`\">`))
	page, err := transcript.ReadClaudeBefore(record, 200, 0)
	if err != nil {
		t.Fatal(err)
	}
	rows := make([]contract.TranscriptEntry, len(page.Entries))
	for i, e := range page.Entries {
		rows[i] = transcriptEntry(e)
		rows[i].Artifacts = s.pictures.wireArtifacts(e, time.Now())
	}
	s.showPictures(session.Session{Assistant: session.AssistantClaude}, record, page.Entries, rows, time.Now())
	n := 0
	for _, r := range rows {
		n += len(r.Artifacts)
	}
	if n != 1 {
		t.Fatalf("the picture was drawn %d times, want once", n)
	}
}

// The cursor a picture is kept with always meets a row's start: a row still
// being written is left out of it.
func TestRecordEndStopsAtTheLastCompleteRow(t *testing.T) {
	record := filepath.Join(t.TempDir(), "conversation.jsonl")
	appendRows(t, record, claudeRow("2026-10-15T10:00:00Z", "one"), `{"type":"assist`)
	end, ok := recordEnd(record)
	whole := int64(len(claudeRow("2026-10-15T10:00:00Z", "one")))
	if !ok || end != whole {
		t.Fatalf("recordEnd = %d, %v; want %d", end, ok, whole)
	}
	if _, ok := recordEnd(filepath.Join(t.TempDir(), "missing.jsonl")); ok {
		t.Fatal("a missing record has an end")
	}
}

// The body takes images and, besides them, only a terminal named by a string.
func TestTheImagesBodyTakesASessionAndNothingElse(t *testing.T) {
	s := &Server{pictures: pictures{store: artifacts.NewStore(t.TempDir())}}
	for _, body := range []string{
		`{"images":[{"path":"/tmp/a.png"}],"other":1}`,
		`{"images":[{"path":"/tmp/a.png"}],"terminal":""}`,
		`{"images":[{"path":"/tmp/a.png"}],"terminal":7}`,
		`{"terminal":"%1"}`,
	} {
		req := httptest.NewRequest(http.MethodPost, "/v1/artifacts/images", strings.NewReader(body))
		req = req.WithContext(context.WithValue(req.Context(), accessKey{}, access{machine: true}))
		w := httptest.NewRecorder()
		s.imagesRoute(w, req)
		if w.Code != http.StatusBadRequest {
			t.Errorf("%s: %d %s, want 400", body, w.Code, w.Body.String())
		}
	}
}

// An upload that names no Session is told, in the answer it reads, that the
// pictures are shown nowhere yet and how to show them: a 200 used to be the
// only thing an agent saw, and it said nothing of the sort.
func TestAnUploadThatNamesNoSessionIsToldItShowedNothing(t *testing.T) {
	s := &Server{pictures: pictures{store: artifacts.NewStore(t.TempDir())}}
	var buf bytes.Buffer
	_ = png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 4, 3)))
	src := filepath.Join(t.TempDir(), "shot.png")
	if err := os.WriteFile(src, buf.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/v1/artifacts/images", strings.NewReader(`{"images":[{"path":"`+src+`"}]}`))
	req = req.WithContext(context.WithValue(req.Context(), accessKey{}, access{machine: true}))
	w := httptest.NewRecorder()
	s.imagesRoute(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
	var out contract.ImageStoreResult
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if out.Terminal != "" || !strings.Contains(out.Note, "shown nowhere yet") || !strings.Contains(out.Note, "clawdline image send") {
		t.Fatalf("answer %+v; want a note that nothing is shown and how to show it", out)
	}
}
