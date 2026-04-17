package orchestrate

import agentruntime "github.com/geoffbelknap/agency/internal/hostadapter/agentruntime"

type Enforcer = agentruntime.Enforcer

const enforcerImage = "agency-enforcer:latest"

var (
	NewEnforcer           = agentruntime.NewEnforcer
	NewEnforcerWithClient = agentruntime.NewEnforcerWithClient
)
