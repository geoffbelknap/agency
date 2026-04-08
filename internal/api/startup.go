package api

import (
	"context"
	"fmt"
	"path/filepath"

	"log/slog"

	"github.com/geoffbelknap/agency/internal/config"
	agencyctx "github.com/geoffbelknap/agency/internal/context"
	"github.com/geoffbelknap/agency/internal/credstore"
	"github.com/geoffbelknap/agency/internal/docker"
	"github.com/geoffbelknap/agency/internal/hub"
	"github.com/geoffbelknap/agency/internal/knowledge"
	"github.com/geoffbelknap/agency/internal/logs"
	"github.com/geoffbelknap/agency/internal/orchestrate"
	"github.com/geoffbelknap/agency/internal/profiles"
)

// StartupResult holds the initialized components from a successful Startup call.
// Core fields are guaranteed non-nil. Optional fields may be nil if the feature
// is disabled or initialization failed (non-fatal).
type StartupResult struct {
	// Core — guaranteed non-nil after successful Startup.
	Infra           *orchestrate.Infra
	AgentManager    *orchestrate.AgentManager
	HaltController  *orchestrate.HaltController
	Audit           *logs.Writer
	CtxMgr          *agencyctx.Manager
	MissionManager  *orchestrate.MissionManager
	MeeseeksManager *orchestrate.MeeseeksManager
	Claims          *orchestrate.MissionClaimRegistry
	Knowledge       *knowledge.Proxy
	MCPReg          *MCPToolRegistry

	// Optional — nil means feature disabled.
	CredStore    *credstore.Store
	ProfileStore *profiles.Store
}

// Startup initializes all gateway components and returns a StartupResult.
// Core component failures (Infra, AgentManager, HaltController) are fatal and
// return an error — the gateway will not start. Optional component failures
// log warnings and leave the corresponding field nil.
func Startup(cfg *config.Config, dc *docker.Client, logger *slog.Logger) (*StartupResult, error) {
	if dc == nil {
		return nil, fmt.Errorf("docker client is required")
	}

	infra, err := orchestrate.NewInfra(cfg.Home, cfg.Version, dc, logger, cfg.HMACKey)
	if err != nil {
		return nil, fmt.Errorf("infra init: %w", err)
	}
	if infra == nil {
		return nil, fmt.Errorf("infra init returned nil")
	}
	infra.SourceDir = cfg.SourceDir
	infra.BuildID = cfg.BuildID
	infra.GatewayAddr = cfg.GatewayAddr
	infra.GatewayToken = cfg.Token
	infra.EgressToken = cfg.EgressToken

	agents, err := orchestrate.NewAgentManager(cfg.Home, dc, logger)
	if err != nil {
		return nil, fmt.Errorf("agent manager init: %w", err)
	}
	if agents == nil {
		return nil, fmt.Errorf("agent manager init returned nil")
	}

	halt, err := orchestrate.NewHaltController(cfg.Home, cfg.Version, dc, logger)
	if err != nil {
		return nil, fmt.Errorf("halt controller init: %w", err)
	}
	if halt == nil {
		return nil, fmt.Errorf("halt controller init returned nil")
	}
	halt.SourceDir = cfg.SourceDir
	halt.BuildID = cfg.BuildID
	halt.Comms = dc

	audit := logs.NewWriter(cfg.Home)
	ctxMgr := agencyctx.NewManager(audit)

	// Wire halt function so constraint timeout triggers agent halt.
	// ASK tenet 6: unacknowledged constraint changes are treated as potential compromise.
	ctxMgr.SetHaltFunc(func(agent, changeID, reason string) error {
		return halt.HaltForUnackedConstraint(context.Background(), agent, changeID, reason)
	})

	// Initialize credential store (non-fatal — endpoints return 503 if nil).
	var cs *credstore.Store
	storePath := filepath.Join(cfg.Home, "credentials", "store.enc")
	keyPath := filepath.Join(cfg.Home, "credentials", ".key")
	if fb, err := credstore.NewFileBackend(storePath, keyPath); err != nil {
		if logger != nil {
			logger.Warn("credential store init failed", "err", err)
		}
	} else if fb != nil {
		cs = credstore.NewStore(fb, cfg.Home)
	}
	halt.CredStore = cs

	// Initialize profile store (non-fatal).
	ps := profiles.NewStore(filepath.Join(cfg.Home, "profiles"))

	mcpReg := NewMCPToolRegistry()
	registerMCPTools(mcpReg)

	// Migrate flat-file hub installations to the instance-directory model on startup.
	hubMgr := hub.NewManager(cfg.Home)
	if migrated, err := hubMgr.Registry.MigrateIfNeeded(); err != nil {
		if logger != nil {
			logger.Warn("hub migration failed", "err", err)
		}
	} else if migrated > 0 {
		if logger != nil {
			logger.Info("migrated hub instances from flat files", "count", migrated)
		}
	}

	return &StartupResult{
		Infra:           infra,
		AgentManager:    agents,
		HaltController:  halt,
		Audit:           audit,
		CtxMgr:          ctxMgr,
		MissionManager:  orchestrate.NewMissionManager(cfg.Home),
		MeeseeksManager: orchestrate.NewMeeseeksManager(),
		Claims:          orchestrate.NewMissionClaimRegistry(),
		Knowledge:       knowledge.NewProxy(),
		MCPReg:          mcpReg,
		CredStore:       cs,
		ProfileStore:    ps,
	}, nil
}
