package orchestrator

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
)

// CallbackOutput is a bounded view of the daemon-owned command log. Reading
// never acknowledges completion or changes a callback's lifecycle.
type CallbackOutput struct {
	Text      string
	Truncated bool
	Signature string
}

func CallbackCommand(r Record) string {
	if r.Callback == nil {
		return ""
	}
	return shellWords(r.Callback.Argv)
}

// ReadCallbackOutput requires the current Session's proven conversation, not
// a caller-provided path or the terminal id a different conversation reused.
func (b *Broker) ReadCallbackOutput(ctx context.Context, id, conversation string) (Record, CallbackOutput, error) {
	r, _, err := b.Record(ctx, id)
	if err != nil {
		return Record{}, CallbackOutput{}, err
	}
	if r.Callback == nil || r.Kind != TaskKindCallback || r.Root == nil || conversation == "" || r.Root.SessionID != conversation {
		return Record{}, CallbackOutput{}, refuse(http.StatusNotFound, "not_found", "That background command is not part of this Session.")
	}
	path := callbackFiles{r.Dir}.log()
	before, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) && !r.State.Terminal() && r.Callback.StartedAt.IsZero() {
		return r, CallbackOutput{Signature: string(r.State)}, nil
	}
	if err != nil || !before.Mode().IsRegular() {
		return r, CallbackOutput{}, refuse(http.StatusNotFound, "background_output_unavailable", "The background command output is unavailable or has expired.")
	}
	f, err := os.Open(path)
	if err != nil {
		return r, CallbackOutput{}, refuse(http.StatusServiceUnavailable, "background_output_unreadable", "The background command output could not be read.")
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil || !os.SameFile(before, st) {
		return r, CallbackOutput{}, refuse(http.StatusServiceUnavailable, "background_output_unreadable", "The background command output changed while it was opened.")
	}
	from := max(int64(0), st.Size()-CallbackTailLimit)
	buf := make([]byte, st.Size()-from)
	n, err := f.ReadAt(buf, from)
	if err != nil && !errors.Is(err, io.EOF) {
		return r, CallbackOutput{}, refuse(http.StatusServiceUnavailable, "background_output_unreadable", "The background command output could not be read.")
	}
	text := string(buf[:n])
	if from > 0 {
		if i := strings.IndexByte(text, '\n'); i >= 0 {
			text = text[i+1:]
		}
	}
	return r, CallbackOutput{Text: strings.ToValidUTF8(text, "?"), Truncated: from > 0, Signature: fmt.Sprintf("%s:%d:%d", r.State, st.ModTime().UnixNano(), st.Size())}, nil
}
