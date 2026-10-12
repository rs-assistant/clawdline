package http

import (
	"net/http"
	"strings"

	"github.com/sainteye/clawdline/internal/contract"
	"github.com/sainteye/clawdline/internal/domain/persona"
)

// personasRoute is GET /v1/personas: the built-in catalog a start, a resume, a
// dispatch or a Root Assignment may name (docs/personas.md). Read-level, like
// the place list it is picked beside. The texts stay on this machine; a page
// needs the names, the summaries and the bot, and nothing it draws is the
// definition itself.
func (s *Server) personasRoute(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeRefusal(w, http.StatusMethodNotAllowed, "method_not_allowed", "Personas are read with GET.")
		return
	}
	out := personaCatalog()
	catalog, err := s.squadCatalog(r.Context())
	if err != nil {
		squadFailure(w, err, true)
		return
	}
	for _, d := range catalog.Definitions {
		if d.Builtin {
			continue
		}
		teams := make([]string, 0, len(d.Teams))
		for _, ref := range d.Teams {
			if strings.HasPrefix(ref.ID, "clawdline.team.") {
				teams = append(teams, strings.TrimPrefix(ref.ID, "clawdline.team."))
			}
		}
		if len(teams) == 0 {
			teams = []string{"custom"}
		}
		out.Personas = append(out.Personas, contract.Persona{
			ID: d.DefinitionID, Teams: teams,
			Name:           contract.PersonaNames{En: d.Name.En, ZhHant: d.Name.ZhHant},
			Summary:        contract.PersonaNames{En: d.Summary.En, ZhHant: d.Summary.ZhHant},
			SuggestedKinds: []string{}, Icon: contract.Icon{Accent: d.Icon.Accent, Cells: d.Icon.Cells}, Source: d.Source,
		})
	}
	writeJSON(w, out)
}

func personaCatalog() contract.PersonaCatalog {
	out := contract.PersonaCatalog{Personas: []contract.Persona{}, License: persona.UpstreamLicense}
	for _, p := range persona.All() {
		out.Personas = append(out.Personas, contract.Persona{
			ID:             p.ID,
			Teams:          p.Teams,
			Name:           contract.PersonaNames{En: p.Name.En, ZhHant: p.Name.ZhHant},
			Summary:        contract.PersonaNames{En: p.Summary.En, ZhHant: p.Summary.ZhHant},
			SuggestedKinds: append([]string{}, p.SuggestedKinds...),
			Icon:           contract.Icon{Accent: p.Icon.Accent, Cells: p.Icon.Cells},
			Source:         p.Source,
		})
	}
	return out
}
