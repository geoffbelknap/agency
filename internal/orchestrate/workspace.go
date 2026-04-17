package orchestrate

import (
	agentruntime "github.com/geoffbelknap/agency/internal/hostadapter/agentruntime"
)

type Workspace = agentruntime.Workspace
type StartOptions = agentruntime.StartOptions

const (
	bodyImage = "agency-body:latest"
	agencyUID = "61000"
	agencyGID = "61000"
)

var (
	NewWorkspace           = agentruntime.NewWorkspace
	NewWorkspaceWithClient = agentruntime.NewWorkspaceWithClient
)
