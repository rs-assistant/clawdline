package artifacts

import (
	"cmp"
	"os"
	"slices"
	"time"
)

// A picture an agent shows the person used to reach the console only through
// a marker, `<clawdline-image id="…">`, in the agent's reply. Newer models
// keep the prose they write between two tool calls only as a summary of it,
// and the marker goes with the rest: on 2026-10-15 two Sessions stored
// pictures, were answered 200 with their markers, wrote them, and the person
// saw none. Nothing told either agent that the picture never arrived.
//
// So a picture can be stored as shown in a Session. The store keeps which
// conversation record it belongs to and how far that record had been written
// when it was stored; the transcript route finds the first reply written after
// that point and shows the picture under it. The model's text is not involved.

// Shown is where pictures are shown: a Session's conversation record, and the
// start of the first row that will be written after they are stored.
type Shown struct {
	Record string
	Offset int64
}

// ShownImage is one picture shown in a record. Anchor is the start of the row
// of the reply it is shown under, or -1 while that reply has not been found.
type ShownImage struct {
	ID     string
	Offset int64
	Anchor int64
}

// ShownIn is every live picture shown in record, oldest first. It reads the
// directory again only when the directory has changed since its last read.
func (s *Store) ShownIn(record string, now time.Time) []ShownImage {
	if record == "" {
		return nil
	}
	storeLock.Lock()
	defer storeLock.Unlock()
	info, err := os.Stat(s.Dir)
	if err != nil {
		return nil
	}
	if !info.ModTime().Equal(s.shownAt) || s.shown == nil {
		s.shown, s.shownAt = s.allMetadata(), info.ModTime()
	}
	var out []ShownImage
	for _, m := range s.shown {
		if m.Record != record || m.DeletedAt != nil || m.Artifact.ExpiresAt <= now.Unix() {
			continue
		}
		anchor := int64(-1)
		if m.Anchor != nil {
			anchor = *m.Anchor
		}
		out = append(out, ShownImage{ID: m.Artifact.ID, Offset: m.Offset, Anchor: anchor})
	}
	slices.SortFunc(out, func(a, b ShownImage) int { return cmp.Compare(a.Offset, b.Offset) })
	return out
}

// SetAnchor records the reply a shown picture is shown under. A picture whose
// reply is already recorded keeps it: the first answer is the one every page
// was drawn with.
func (s *Store) SetAnchor(id string, anchor int64) {
	storeLock.Lock()
	defer storeLock.Unlock()
	m, ok := s.readMetadata(id)
	if !ok || m.Record == "" || m.Anchor != nil {
		return
	}
	m.Anchor = &anchor
	_ = s.writeMetadata(m)
}
