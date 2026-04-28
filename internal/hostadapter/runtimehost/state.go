package runtimehost

type RuntimeState string

const (
	RuntimeStateMissing RuntimeState = "missing"
	RuntimeStateRunning RuntimeState = "running"
	RuntimeStatePaused  RuntimeState = "paused"
	RuntimeStateStopped RuntimeState = "stopped"
)
