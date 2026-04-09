package admin

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/geoffbelknap/agency/internal/config"
)

func TestCreateAndDeleteTeam(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	h := &handler{deps: Deps{
		Config: &config.Config{Home: home},
		Logger: slog.Default(),
	}}

	createBody, err := json.Marshal(map[string]any{
		"name":   "alpha-team",
		"agents": []string{"alice", "bob"},
	})
	if err != nil {
		t.Fatalf("marshal create body: %v", err)
	}

	createReq := httptest.NewRequest(http.MethodPost, "/api/v1/admin/teams", bytes.NewReader(createBody))
	createRec := httptest.NewRecorder()
	h.createTeam(createRec, createReq)
	if createRec.Code != http.StatusCreated {
		t.Fatalf("createTeam status = %d, want %d", createRec.Code, http.StatusCreated)
	}

	teamPath := filepath.Join(home, "teams", "alpha-team", "team.yaml")
	if _, err := os.Stat(teamPath); err != nil {
		t.Fatalf("expected team.yaml to exist: %v", err)
	}

	showReq := withTeamName(httptest.NewRequest(http.MethodGet, "/api/v1/admin/teams/alpha-team", nil), "alpha-team")
	showRec := httptest.NewRecorder()
	h.showTeam(showRec, showReq)
	if showRec.Code != http.StatusOK {
		t.Fatalf("showTeam status = %d, want %d", showRec.Code, http.StatusOK)
	}

	listReq := httptest.NewRequest(http.MethodGet, "/api/v1/admin/teams", nil)
	listRec := httptest.NewRecorder()
	h.listTeams(listRec, listReq)
	if listRec.Code != http.StatusOK {
		t.Fatalf("listTeams status = %d, want %d", listRec.Code, http.StatusOK)
	}

	deleteReq := withTeamName(httptest.NewRequest(http.MethodDelete, "/api/v1/admin/teams/alpha-team", nil), "alpha-team")
	deleteRec := httptest.NewRecorder()
	h.deleteTeam(deleteRec, deleteReq)
	if deleteRec.Code != http.StatusOK {
		t.Fatalf("deleteTeam status = %d, want %d", deleteRec.Code, http.StatusOK)
	}

	if _, err := os.Stat(filepath.Join(home, "teams", "alpha-team")); !os.IsNotExist(err) {
		t.Fatalf("expected team directory to be removed, stat err = %v", err)
	}

	missingDeleteReq := withTeamName(httptest.NewRequest(http.MethodDelete, "/api/v1/admin/teams/alpha-team", nil), "alpha-team")
	missingDeleteRec := httptest.NewRecorder()
	h.deleteTeam(missingDeleteRec, missingDeleteReq)
	if missingDeleteRec.Code != http.StatusNotFound {
		t.Fatalf("missing deleteTeam status = %d, want %d", missingDeleteRec.Code, http.StatusNotFound)
	}
}

func withTeamName(req *http.Request, name string) *http.Request {
	routeCtx := chi.NewRouteContext()
	routeCtx.URLParams.Add("name", name)
	ctx := context.WithValue(req.Context(), chi.RouteCtxKey, routeCtx)
	return req.WithContext(ctx)
}
