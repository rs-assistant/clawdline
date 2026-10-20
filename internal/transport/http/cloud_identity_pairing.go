package http

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"

	cloudtransport "github.com/sainteye/clawdline/internal/transport/cloud"
)

// CloudIdentityPairingLine is a Cloud link that can run the content-key
// handover (identity pairing). It is its own interface so that a link built
// before the identity loads, or a test line, still answers the legacy routes.
type CloudIdentityPairingLine interface {
	IdentityPairing() *cloudtransport.IdentityPairing
}

// maxIdentityDecisionBody bounds the decision body: a pairing id and a bool.
const maxIdentityDecisionBody = 1 << 10

// cloudIdentityPairingRoute begins, reads or cancels the identity pairing.
//
//	GET    /v1/cloud/pairing/identity   the run in progress
//	POST   /v1/cloud/pairing/identity   start one and answer its #pairid= link
//	DELETE /v1/cloud/pairing/identity   stop it
//
// This machine's own token only, for the reason cloudPairingRoute gives: the
// link begins a handover of the account master secret.
func (s *Server) cloudIdentityPairingRoute(w http.ResponseWriter, r *http.Request) {
	if !requireLocal(w, r) {
		return
	}
	pairing, ok := s.cloudIdentityPairing(w)
	if !ok {
		return
	}
	switch r.Method {
	case http.MethodGet, http.MethodHead:
		writeJSON(w, pairing.State())
	case http.MethodPost:
		state, err := pairing.Begin(r.Context())
		if err != nil {
			writeCloudIdentityPairingError(w, state, err)
			return
		}
		writeJSON(w, state)
	case http.MethodDelete:
		writeJSON(w, pairing.Cancel())
	default:
		writeAuthRefusal(w, http.StatusMethodNotAllowed, "bad_request",
			"An identity pairing is read with GET, started with POST and stopped with DELETE.")
	}
}

// cloudIdentityPairingDecisionRoute is the person's answer to the code
// comparison: `POST /v1/cloud/pairing/identity/decision` with
// `{"pairing_id":"…","approve":true|false}`. `approve` is required; a body
// without it is refused rather than read as either answer.
func (s *Server) cloudIdentityPairingDecisionRoute(w http.ResponseWriter, r *http.Request) {
	if !requireLocal(w, r) {
		return
	}
	if r.Method != http.MethodPost {
		writeAuthRefusal(w, http.StatusMethodNotAllowed, "bad_request", "A pairing decision is sent with POST.")
		return
	}
	pairing, ok := s.cloudIdentityPairing(w)
	if !ok {
		return
	}
	var body struct {
		PairingID string `json:"pairing_id"`
		Approve   *bool  `json:"approve"`
	}
	decoder := json.NewDecoder(io.LimitReader(r.Body, maxIdentityDecisionBody))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&body); err != nil || body.Approve == nil || body.PairingID == "" || len(body.PairingID) > 128 {
		writeAuthRefusal(w, http.StatusBadRequest, "bad_request",
			"A pairing decision names its pairing_id and says approve true or false.")
		return
	}
	state, err := pairing.Decide(body.PairingID, *body.Approve)
	if err != nil {
		writeCloudIdentityPairingError(w, state, err)
		return
	}
	writeJSON(w, state)
}

func (s *Server) cloudIdentityPairing(w http.ResponseWriter) (*cloudtransport.IdentityPairing, bool) {
	line, ok := cloudLines.Load(s.cfg.Dir)
	if !ok {
		writeAuthRefusal(w, http.StatusConflict, "cloud_off", "The Cloud line is off in this app's settings.")
		return nil, false
	}
	holder, ok := line.(CloudIdentityPairingLine)
	if !ok {
		writeAuthRefusal(w, http.StatusConflict, "cloud_off", "This Cloud line cannot pair.")
		return nil, false
	}
	pairing := holder.IdentityPairing()
	if pairing == nil {
		writeAuthRefusal(w, http.StatusConflict, "cloud_not_signed_in",
			"This machine is not connected to a Clawdline Cloud account. Run `clawdline cloud login` first.")
		return nil, false
	}
	return pairing, true
}

// writeCloudIdentityPairingError answers a typed refusal with the run's state
// beside it, so the page can show the stable error code the run recorded.
func writeCloudIdentityPairingError(w http.ResponseWriter, state cloudtransport.IdentityPairingState, err error) {
	switch {
	case errors.Is(err, cloudtransport.ErrPairingUnavailable):
		writeAuthRefusal(w, http.StatusConflict, "cloud_not_signed_in", err.Error())
	case errors.Is(err, cloudtransport.ErrIdentityPairingUnknown):
		writeAuthRefusal(w, http.StatusNotFound, "unknown_pairing", err.Error())
	case errors.Is(err, cloudtransport.ErrIdentityPairingNotWaiting):
		writeAuthRefusal(w, http.StatusConflict, "pairing_not_waiting", err.Error())
	default:
		code := state.Error
		if code == "" {
			code = "pairing_failed"
		}
		writeAuthRefusal(w, http.StatusBadGateway, code, "The identity pairing could not start.")
	}
}
