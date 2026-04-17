package orchestrate

import (
	agentruntime "github.com/geoffbelknap/agency/internal/hostadapter/agentruntime"
)

type WorkspaceDeps = agentruntime.WorkspaceDeps

func resolveWorkspaceEnv(declared map[string]string, home, scopedKey string) map[string]string {
	return agentruntime.ResolveWorkspaceEnv(declared, home, scopedKey)
}
