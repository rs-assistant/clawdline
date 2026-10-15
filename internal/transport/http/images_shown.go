package http

import (
	"bytes"
	"io"
	"os"
	"slices"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/transcript"
	"github.com/sainteye/clawdline/internal/contract"
	"github.com/sainteye/clawdline/internal/domain/session"
)

// Pictures stored as shown in a Session (POST /v1/artifacts/images with
// `terminal`) are drawn under the first reply that Session writes after them.
// The reply is found by where it sits in the record, not by its time or its
// text: the store kept the start of the first row written after the pictures,
// and the first assistant entry at or after it is their reply. That row is
// kept once found, so every page — the newest, an older one, or the rows a
// reader asks for after its cursor — draws them under the same entry and no
// other, and a page boundary between the two cannot show them twice.

// shownReadEntries is how many entries after a picture are read to find its
// reply: as many as one page holds.
const shownReadEntries = 200

// recordEnd is where the next row of record will start: just past its last
// complete row. A row still being written is left out, so the cursor the
// transcript readers are given always meets a row's start.
func recordEnd(record string) (int64, bool) {
	if record == "" {
		return 0, false
	}
	f, err := os.Open(record)
	if err != nil {
		return 0, false
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return 0, false
	}
	end := info.Size()
	buf := make([]byte, 64<<10)
	for end > 0 {
		from := max(end-int64(len(buf)), 0)
		chunk := buf[:end-from]
		if _, err := f.ReadAt(chunk, from); err != nil && err != io.EOF {
			return 0, false
		}
		if i := bytes.LastIndexByte(chunk, '\n'); i >= 0 {
			return from + int64(i) + 1, true
		}
		end = from
	}
	return 0, true
}

// showPictures adds to rows the pictures shown in record under the entry each
// one's reply is. rows and entries are one page, index for index.
func (s *Server) showPictures(item session.Session, record string, entries []transcript.Entry, rows []contract.TranscriptEntry, now time.Time) {
	if s.pictures.store == nil || len(entries) == 0 {
		return
	}
	shown := s.pictures.store.ShownIn(record, now)
	for _, picture := range shown {
		anchor := picture.Anchor
		if anchor < 0 {
			anchor = s.findReply(item, record, picture.Offset)
			if anchor < 0 {
				continue
			}
			s.pictures.store.SetAnchor(picture.ID, anchor)
		}
		for i, e := range entries {
			if e.Before != anchor || e.Kind != transcript.KindAssistant {
				continue
			}
			if slices.ContainsFunc(rows[i].Artifacts, func(a contract.ImageArtifact) bool { return a.ID == picture.ID }) {
				break
			}
			rows[i].Artifacts = append(rows[i].Artifacts, s.pictures.resolve(picture.ID, now))
			break
		}
	}
}

// findReply is the start of the row of the first assistant entry at or after
// offset, or -1 while there is none.
func (s *Server) findReply(item session.Session, record string, offset int64) int64 {
	var read transcript.Page
	var err error
	if item.Assistant == session.AssistantCodex {
		read, err = transcript.ReadCodexAfter(record, shownReadEntries, offset)
	} else {
		read, err = transcript.ReadClaudeAfter(record, shownReadEntries, offset)
	}
	if err != nil {
		return -1
	}
	for _, e := range read.Entries {
		if e.Kind == transcript.KindAssistant {
			return e.Before
		}
	}
	return -1
}
