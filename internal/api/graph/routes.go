package graph

import (
	"encoding/json"
	"net/http"

	"log/slog"
	"github.com/go-chi/chi/v5"

	"github.com/geoffbelknap/agency/internal/config"
	"github.com/geoffbelknap/agency/internal/knowledge"
	"github.com/geoffbelknap/agency/internal/logs"
)

// Deps holds the dependencies required by the graph module.
type Deps struct {
	Knowledge *knowledge.Proxy
	Config    *config.Config
	Logger    *slog.Logger
	Audit     *logs.Writer
}

type handler struct {
	deps Deps
}

// RegisterRoutes mounts all knowledge graph and ontology routes onto r.
func RegisterRoutes(r chi.Router, d Deps) {
	h := &handler{deps: d}

	r.Route("/api/v1/graph", func(r chi.Router) {
		r.Post("/query", h.knowledgeQuery)
		r.Get("/who-knows", h.knowledgeWhoKnows)
		r.Get("/stats", h.knowledgeStats)
		r.Get("/export", h.knowledgeExport)
		r.Post("/import", h.knowledgeImport)
		r.Get("/changes", h.knowledgeChanges)
		r.Get("/context", h.knowledgeContext)
		r.Get("/neighbors", h.knowledgeNeighbors)
		r.Get("/path", h.knowledgePath)
		r.Get("/flags", h.knowledgeFlags)
		r.Post("/restore", h.knowledgeRestore)
		r.Get("/curation-log", h.knowledgeCurationLog)

		// Review (operator-only — org-structural contributions)
		r.Get("/pending", h.handleKnowledgePending)
		r.Post("/review/{id}", h.handleKnowledgeReview)

		// Ontology
		r.Get("/ontology", h.knowledgeOntology)
		r.Get("/ontology/types", h.knowledgeOntologyTypes)
		r.Get("/ontology/relationships", h.knowledgeOntologyRelationships)
		r.Post("/ontology/validate", h.knowledgeOntologyValidate)
		r.Post("/ontology/migrate", h.knowledgeOntologyMigrate)

		// Principals (knowledge service proxy)
		r.Get("/principals", h.knowledgePrincipalsList)
		r.Post("/principals", h.knowledgePrincipalsRegister)
		r.Get("/principals/{uuid}", h.knowledgePrincipalsResolve)

		// Quarantine (ASK tenet 16)
		r.Post("/quarantine", h.knowledgeQuarantine)
		r.Post("/quarantine/release", h.knowledgeQuarantineRelease)
		r.Get("/quarantine", h.knowledgeQuarantineList)

		// Classification, communities, hubs
		r.Get("/classification", h.knowledgeClassification)
		r.Get("/communities", h.knowledgeCommunities)
		r.Get("/communities/{id}", h.knowledgeCommunity)
		r.Get("/hubs", h.knowledgeHubs)

		// Ingestion and insights
		r.Post("/ingest", h.knowledgeIngest)
		r.Post("/insight", h.knowledgeSaveInsight)
	})

	r.Route("/api/v1/graph/ontology", func(r chi.Router) {
		r.Get("/candidates", h.listOntologyCandidates)
		r.Post("/promote", h.promoteOntologyCandidate)
		r.Post("/reject", h.rejectOntologyCandidate)
		r.Post("/restore", h.restoreOntologyCandidate)
	})
}

// writeJSON writes a JSON response with the given status code.
func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}
