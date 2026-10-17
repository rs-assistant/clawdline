package http

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"sync"

	"github.com/sainteye/clawdline/internal/app/orchestrator"
)

var dispatchPolicySettingsMu sync.Mutex

// dispatchPolicySettings is local-only: house rules can contain private prose.
// Only the person's additions are editable; the shipped base remains visible.
func (s *Server) dispatchPolicySettings(w http.ResponseWriter, r *http.Request) {
	dispatchPolicySettingsMu.Lock()
	defer dispatchPolicySettingsMu.Unlock()
	path := filepath.Join(s.cfg.Dir, orchestrator.PolicyLocalFile)
	read := func() (string, string, string, string, error) {
		if info, err := os.Lstat(path); err == nil {
			if !info.Mode().IsRegular() {
				return "", "", "", "", os.ErrInvalid
			}
			if _, err := os.ReadFile(path); err != nil {
				return "", "", "", "", err
			}
		} else if !os.IsNotExist(err) {
			return "", "", "", "", err
		}
		base, local, source := orchestrator.ReadPolicy(s.cfg.Dir)
		digest := sha256.Sum256([]byte(source.Local + "\n" + local))
		return base, local, source.Local, hex.EncodeToString(digest[:]), nil
	}
	base, local, source, version, err := read()
	if err != nil {
		writeRefusal(w, http.StatusInternalServerError, "policy_unreadable", "The local dispatch policy could not be read.")
		return
	}
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, map[string]any{"base": base, "local": local, "source": source, "version": version})
	case http.MethodPut:
		if mt, _, err := mime.ParseMediaType(r.Header.Get("Content-Type")); err != nil || mt != "application/json" {
			writeRefusal(w, http.StatusUnsupportedMediaType, "unsupported_media_type", "A policy write is application/json.")
			return
		}
		var body struct {
			Local   *string `json:"local"`
			Version string  `json:"version"`
		}
		dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, settingsRequestBodyLimit))
		if err := dec.Decode(&body); err != nil || body.Local == nil || body.Version == "" {
			writeRefusal(w, http.StatusBadRequest, "bad_request", "The local text and its read version are required.")
			return
		}
		if err := dec.Decode(new(any)); err != io.EOF {
			writeRefusal(w, http.StatusBadRequest, "bad_request", "Only one policy change is accepted.")
			return
		}
		if body.Version != version {
			writeRefusal(w, http.StatusConflict, "policy_changed", "The policy changed since it was opened. Read it again before saving.")
			return
		}
		if *body.Local == local && source == "next" {
			writeJSON(w, map[string]any{"base": base, "local": local, "source": source, "version": version})
			return
		}
		if err := os.MkdirAll(s.cfg.Dir, 0o700); err != nil {
			writeRefusal(w, http.StatusInternalServerError, "policy_write_failed", "The policy directory could not be prepared.")
			return
		}
		tmp, err := os.CreateTemp(s.cfg.Dir, ".dispatch-policy-*")
		if err == nil {
			err = tmp.Chmod(0o600)
			if err == nil {
				_, err = tmp.WriteString(*body.Local)
			}
			if closeErr := tmp.Close(); err == nil {
				err = closeErr
			}
			if err == nil {
				err = os.Rename(tmp.Name(), path)
			}
			if err != nil {
				_ = os.Remove(tmp.Name())
			}
		}
		if err != nil {
			writeRefusal(w, http.StatusInternalServerError, "policy_write_failed", "The local dispatch policy could not be saved.")
			return
		}
		local, source = *body.Local, "next"
		digest := sha256.Sum256([]byte(source + "\n" + local))
		writeJSON(w, map[string]any{"base": base, "local": local, "source": source, "version": hex.EncodeToString(digest[:])})
	default:
		writeRefusal(w, http.StatusMethodNotAllowed, "method_not_allowed", "GET or PUT")
	}
}
