package http

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"net/http"
	"strconv"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/store"
	"github.com/sainteye/clawdline/internal/app"
	"github.com/sainteye/clawdline/internal/domain/work"
)

func (s *Server) secretHandoffNote(ctx context.Context, id string) (work.HumanIntervention, bool) {
	note, err := s.store.HumanIntervention(ctx, id)
	if err != nil || note.Kind != "secret" {
		return work.HumanIntervention{}, false
	}
	return note, true
}

// The person-facing response contains only a receipt. In particular the
// Note, work receipts, audit stream and Cloud response never hold the value.
//
// The submission repeats the receiving command the console showed beside the
// field. The Note's command never changes, so this is not what keeps the
// value from another program (the claim's comparison is); it is what keeps a
// console that never displayed the command, such as a hosted console built
// before commands were declared, from handing off a value blind.
func (s *Server) personSubmitSecretHandoff(w http.ResponseWriter, r *http.Request, conversation, id, actor string) {
	// A resolved Note is not refused here: a submission's own replay is
	// answered below, before the completed Note is.
	note, valid := s.secretHandoffNote(r.Context(), id)
	if !valid || note.TargetConversation != conversation {
		writeRefusal(w, http.StatusNotFound, "secret_handoff_unavailable", "This Secret Handoff is no longer available.")
		return
	}
	var body struct {
		Value   string   `json:"value"`
		Command []string `json:"command"`
	}
	raw, ok := readWorkV2Body(w, r, &body)
	if !ok {
		return
	}
	if len(note.Command) == 0 {
		writeRefusal(w, http.StatusConflict, "secret_handoff_command_missing", "This Secret Handoff Note does not name the command that would receive the value. Ask the Session for a new request.")
		return
	}
	if !app.SameSecretHandoffCommand(note.Command, body.Command) {
		writeRefusal(w, http.StatusConflict, "secret_handoff_command_unconfirmed", "The page did not confirm the command that will receive this value. Reload it, check the command, and hand off again.")
		return
	}
	if len(body.Value) == 0 {
		writeRefusal(w, http.StatusUnprocessableEntity, "secret_empty", "Paste a value before submitting.")
		return
	}
	if len(body.Value) > app.MaxSecretHandoffBytes {
		writeRefusal(w, http.StatusRequestEntityTooLarge, "secret_too_large", "This value is too large for Secret Handoff.")
		return
	}
	k, ok := s.beginWorkV2Write(w, r, actor, raw)
	if !ok {
		return
	}
	// Replay a successful submission before checking the now-completed Note.
	if note.ResolvedAt != nil {
		_ = s.store.ReleaseReceipt(context.WithoutCancel(r.Context()), k)
		writeRefusal(w, http.StatusNotFound, "secret_handoff_unavailable", "This Secret Handoff is no longer available.")
		return
	}
	answer := []byte(`{"ok":true,"received":true}`)
	// Completion acknowledges the person's handoff, not the tool's later
	// claim. Commit the Note and receipt together before publishing the value.
	if err := s.secretHandoffs.PutAndCommit(id, conversation, []byte(body.Value), func() error {
		_, _, err := s.humanInterventions().Update(r.Context(), id, conversation, "resolve", "", note.Version, k,
			func(work.HumanIntervention) []byte { return answer })
		return err
	}); err != nil {
		switch {
		case errors.Is(err, app.ErrSecretFull):
			_ = s.store.ReleaseReceipt(context.WithoutCancel(r.Context()), k)
			writeRefusal(w, http.StatusServiceUnavailable, "secret_handoffs_full", "Too many handoffs are pending. Try again later.")
		case errors.Is(err, app.ErrSecretUnavailable):
			_ = s.store.ReleaseReceipt(context.WithoutCancel(r.Context()), k)
			writeRefusal(w, http.StatusConflict, "secret_handoff_unavailable", "This Secret Handoff cannot accept another value.")
		default:
			writeRefusal(w, http.StatusServiceUnavailable, "receipt_unavailable", "The handoff receipt could not be recorded. Check the Session before trying again.")
		}
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_, _ = w.Write(answer)
}

// This machine-only route is used by the local CLI. It checks that the
// calling conversation still owns the exact target terminal, and that the
// argv it will feed the value to is exactly the one the Note declared and the
// person saw. The value is delivered over a one-use loopback connection,
// never in an HTTP response.
func (s *Server) agentClaimSecretHandoff(w http.ResponseWriter, r *http.Request, id string) {
	var body struct {
		ConversationID string   `json:"conversation_id"`
		CallbackPort   int      `json:"callback_port"`
		Nonce          string   `json:"nonce"`
		Command        []string `json:"command"`
	}
	if _, ok := readWorkV2Body(w, r, &body); !ok {
		return
	}
	nonce, nonceErr := base64.RawURLEncoding.DecodeString(body.Nonce)
	if body.CallbackPort < 1 || body.CallbackPort > 65535 || nonceErr != nil || len(nonce) != 32 {
		writeRefusal(w, http.StatusBadRequest, "secret_handoff_callback_invalid", "The local handoff receiver is invalid.")
		return
	}
	declared, valid := s.secretHandoffNote(r.Context(), id)
	target := declared.TargetConversation
	if !valid || target != body.ConversationID || s.broker == nil {
		writeRefusal(w, http.StatusNotFound, "secret_handoff_unavailable", "This Secret Handoff is not available to this Session.")
		return
	}
	live, err := s.broker.LiveRootSession(r.Context(), body.ConversationID)
	if err != nil || live.ID != declared.TargetSession {
		writeRefusal(w, http.StatusConflict, "secret_handoff_target_changed", "The target Session is no longer live at this terminal.")
		return
	}
	// The whole argv, exactly. Both ends name argv[0] as the absolute path
	// exec.LookPath resolved in the Agent's environment (note create before
	// the Note is written, secret run before it claims), and the daemon
	// refuses a relative one at creation, so `gh` and `/usr/bin/gh` compare
	// equal only when they are the same path, and a PATH that sends `gh`
	// somewhere else is a mismatch.
	//
	// A mismatch discards any waiting value rather than leaving it for a
	// second try. The target Session has just asked to give the value to a
	// program the person did not see, which is what a prompt-injected Agent
	// does; keeping the value in memory for that Session only lengthens the
	// time it has to find another way. The Note is closed too, so the person
	// sees that the request ended, and handing off again is their choice.
	// A submission already resolved the Note as handed off, so the resolution
	// is rewritten whatever its state: left alone, the Note would read as
	// delivered when the value was thrown away.
	if !app.SameSecretHandoffCommand(declared.Command, body.Command) {
		s.secretHandoffs.Discard(id, target)
		ctx := context.WithoutCancel(r.Context())
		if note, err := s.store.HumanIntervention(ctx, id); err == nil {
			_, _, _ = s.humanInterventions().Update(ctx, id, target, "resolve", "Secret Handoff refused: command mismatch", note.Version, store.ReceiptKey{}, nil)
		}
		writeRefusal(w, http.StatusForbidden, "secret_handoff_command_mismatch", "This is not the command the Secret Handoff Note named. Nothing was handed off; any waiting value was discarded and the Note was closed.")
		return
	}
	value, err := s.secretHandoffs.Take(id, target)
	if err != nil {
		writeRefusal(w, http.StatusConflict, "secret_handoff_unavailable", "The value was already claimed, expired, or has not arrived.")
		return
	}
	if note, err := s.store.HumanIntervention(r.Context(), id); err == nil {
		if _, _, err = s.humanInterventions().Update(r.Context(), id, target, "resolve", "Secret Handoff used", note.Version, store.ReceiptKey{}, nil); err == nil {
			s.secretHandoffs.ForgetClaim(id)
		}
	}
	defer func() {
		for i := range value {
			value[i] = 0
		}
	}()
	if err := deliverSecretHandoff(body.CallbackPort, nonce, value); err != nil {
		writeRefusal(w, http.StatusBadGateway, "secret_handoff_delivery_failed", "The one-time value could not reach the local tool receiver. It was consumed.")
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, map[string]bool{"ok": true, "delivered": true})
}

func deliverSecretHandoff(port int, nonce, value []byte) error {
	address := net.JoinHostPort("127.0.0.1", strconv.Itoa(port))
	conn, err := net.DialTimeout("tcp4", address, time.Duration(app.MaxSecretHandoffDeliverySeconds)*time.Second)
	if err != nil {
		return err
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(time.Duration(app.MaxSecretHandoffDeliverySeconds) * time.Second))
	frame := make([]byte, 4)
	binary.BigEndian.PutUint32(frame, uint32(len(value)))
	for _, part := range [][]byte{nonce, frame, value} {
		if _, err := io.Copy(conn, bytes.NewReader(part)); err != nil {
			return err
		}
	}
	var ack [1]byte
	if _, err := io.ReadFull(conn, ack[:]); err != nil {
		return err
	}
	if ack[0] != 1 {
		return errors.New("the local receiver did not acknowledge")
	}
	return nil
}
