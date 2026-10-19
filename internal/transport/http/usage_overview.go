package http

import (
	"errors"
	"github.com/sainteye/clawdline/internal/app"
	"net/http"
)

func (s *Server) usageOverview(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	for key, values := range q {
		if (key != "days" && key != "project") || len(values) != 1 {
			writeRefusal(w, 400, "bad_request", "Usage overview accepts one days and one project value.")
			return
		}
	}
	days := 7
	switch q.Get("days") {
	case "", "7":
	case "28":
		days = 28
	default:
		writeRefusal(w, 400, "bad_request", "Usage overview days must be 7 or 28.")
		return
	}
	got, err := s.usageLedger().Overview(r.Context(), days, q.Get("project"))
	if errors.Is(err, app.ErrUsageOverviewProject) {
		writeRefusal(w, 400, "unknown_project", "That project is not in this machine's usage overview.")
		return
	}
	if err != nil {
		usageStoreRefusal(w, err)
		return
	}
	writeJSON(w, got)
}
