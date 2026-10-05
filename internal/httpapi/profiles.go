package httpapi

import (
	"net/http"
	"regexp"
	"strconv"
	"strings"
)

var profileTypeID = regexp.MustCompile(`^[a-zA-Z0-9_]+:[a-zA-Z0-9_]+:[a-zA-Z0-9_]+:[a-zA-Z0-9_]+:[a-zA-Z0-9_]+$`)

func (s *Server) explorerProfilesCatalog(w http.ResponseWriter, r *http.Request) {
	r, done, ok := s.explorerRequest(w, r)
	if !ok {
		return
	}
	defer done()
	start, end, _, ok := explorerWindow(w, r)
	if !ok {
		return
	}
	org := getPrincipal(r.Context()).OrganizationID
	types, err := s.telemetry.ProfileTypes(r.Context(), org, start, end)
	typesStatus := discoveryStatus(err, len(types))
	if !s.allowObservabilityQuery(w, r) {
		return
	}
	labels, truncated, err := s.telemetry.ProfileLabels(r.Context(), org, "", "", start, end)
	write(w, 200, map[string]any{"types": types, "labels": labels, "truncated": truncated, "sources": map[string]sourceStatus{"types": typesStatus, "labels": discoveryStatus(err, len(labels))}})
}
func (s *Server) explorerProfilesLabels(w http.ResponseWriter, r *http.Request) {
	r, done, ok := s.explorerRequest(w, r)
	if !ok {
		return
	}
	defer done()
	start, end, _, ok := explorerWindow(w, r)
	if !ok {
		return
	}
	label := r.URL.Query().Get("label")
	if !labelName.MatchString(label) {
		fail(w, r, 400, "invalid_label", "Label inválido")
		return
	}
	items, truncated, err := s.telemetry.ProfileLabels(r.Context(), getPrincipal(r.Context()).OrganizationID, label, "", start, end)
	write(w, 200, map[string]any{"values": items, "truncated": truncated, "source": discoveryStatus(err, len(items))})
}
func (s *Server) explorerProfilesQuery(w http.ResponseWriter, r *http.Request) {
	r, done, ok := s.explorerRequest(w, r)
	if !ok {
		return
	}
	defer done()
	start, end, _, ok := explorerWindow(w, r)
	if !ok {
		return
	}
	id := r.URL.Query().Get("type")
	if len(id) > 200 || !profileTypeID.MatchString(id) {
		fail(w, r, 400, "invalid_profile_type", "Escolha um tipo de perfil")
		return
	}
	filters, err := readFilters(r.URL.Query().Get("filters"))
	if err != nil || filters["service_name"] == "" {
		fail(w, r, 400, "service_required", "Escolha um serviço de perfis")
		return
	}
	// exactMetricSelector validates names and escapes every value; remove only its fixed metric prefix.
	selector, err := exactMetricSelector("profile", filters)
	if err != nil {
		fail(w, r, 400, "invalid_filters", "Filtros inválidos")
		return
	}
	selector = strings.TrimPrefix(selector, "profile")
	graph, err := s.telemetry.ProfileFlamegraph(r.Context(), getPrincipal(r.Context()).OrganizationID, id, selector, start, end)
	count := len(graph.Frames)
	if graph.Total == "0" {
		count = 0
	}
	write(w, 200, map[string]any{"graph": graph, "source": discoveryStatus(err, count), "filters": filters, "selector": selector, "profileType": id, "unit": strings.Split(id, ":")[2], "start": start, "end": end, "maxNodes": 200, "note": "Perfil agregado na janela. A fonte agrupa as funções menores para limitar a árvore; valores não são taxas. Total: " + strconv.Itoa(count) + " nós retornados."})
}
