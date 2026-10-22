package http

import (
	"net/http"
	"os"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/providerquota"
)

func (s *Server) providerQuotaReader() *providerquota.Service {
	s.providerQuotaOnce.Do(func() {
		if s.providerQuotaService == nil {
			home, _ := os.UserHomeDir()
			s.providerQuotaService = providerquota.New(home, quotaReader())
		}
		// Read on every snapshot, so turning it on or off is a file edit and
		// not a restart. An unreadable file keeps it off.
		if s.providerQuotaService.Remote == nil {
			s.providerQuotaService.Remote = func() bool {
				values, err := s.settingsFile().Read()
				if err != nil {
					return false
				}
				on, set := values.Bool("provider_quota_remote")
				return set && on
			}
		}
	})
	return s.providerQuotaService
}
func (s *Server) providerQuotas(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeRefusal(w, http.StatusMethodNotAllowed, "method_not_allowed", "Provider quotas are read with GET.")
		return
	}
	writeJSON(w, s.providerQuotaReader().Snapshot(time.Now()))
}
