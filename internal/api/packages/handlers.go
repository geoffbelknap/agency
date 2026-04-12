package packages

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/geoffbelknap/agency/internal/hub"
	"github.com/go-chi/chi/v5"
)

func (h *handler) registry() (*hub.Registry, error) {
	if h.deps.Registry != nil {
		return h.deps.Registry, nil
	}
	if h.deps.Config == nil || strings.TrimSpace(h.deps.Config.Home) == "" {
		return nil, fmt.Errorf("hub registry not available")
	}
	return hub.NewManager(h.deps.Config.Home).Registry, nil
}

func (h *handler) listPackages(w http.ResponseWriter, r *http.Request) {
	reg, err := h.registry()
	if err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": err.Error()})
		return
	}

	items, err := reg.ListPackages(r.URL.Query().Get("kind"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if items == nil {
		items = []hub.InstalledPackage{}
	}

	writeJSON(w, http.StatusOK, map[string]any{"packages": items})
}

func (h *handler) showPackage(w http.ResponseWriter, r *http.Request) {
	reg, err := h.registry()
	if err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": err.Error()})
		return
	}

	kind := chi.URLParam(r, "kind")
	name := chi.URLParam(r, "name")
	if strings.TrimSpace(kind) == "" || strings.TrimSpace(name) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "kind and name are required"})
		return
	}

	item, ok := reg.GetPackage(kind, name)
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "package not found"})
		return
	}
	writeJSON(w, http.StatusOK, item)
}
