package backendhealth

// Availability reports whether the active runtime/backend control plane is
// currently reachable for lifecycle and infrastructure operations.
type Availability interface {
	Available() bool
}

// Recorder tracks backend availability based on successful or failed backend
// operations. Concrete implementations may use this to surface reconnects or
// degrade operator diagnostics.
type Recorder interface {
	Availability
	RecordSuccess()
	RecordError(error)
}
