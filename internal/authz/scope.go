package authz

import "github.com/geoffbelknap/agency/internal/registry"

// Scope captures the set of platform entities a principal is authorized to
// observe: which agents' events they can see, which channels, and whether
// they can see platform-wide infrastructure events.
//
// Scope is computed once when a client connects (see internal/ws/hub.go)
// and evaluated on every outbound event before delivery. This enforces
// ASK tenets 7 (least privilege) and 27 (knowledge access bounded by
// authorization scope) on the WebSocket receive path.
//
// If All is true, every other field is ignored and all events are allowed.
// Operators default to All=true today.
//
// An unresolved principal (nil) also yields All=true — a transitional
// compatibility mode for callers that validated a token but did not resolve
// a Principal from the registry. TASK-ios-relay-fwd-003 will close this
// window by ensuring every connection carries a real principal.
type Scope struct {
	All      bool
	Agents   map[string]bool
	Channels map[string]bool
	Infra    bool
}

// AllowAll returns a Scope with no restrictions.
func AllowAll() *Scope {
	return &Scope{All: true}
}

// DenyAll returns a Scope that allows nothing.
func DenyAll() *Scope {
	return &Scope{}
}

// Resolve computes the Scope for the given principal, using reg to look up
// sibling agents under the same team parent when applicable.
//
// Resolution rules:
//   - nil principal → All=true (transitional; see Scope doc).
//   - reg == nil   → All=true (tests and pre-wired-deps callers).
//   - operator     → All=true.
//   - agent        → self + siblings under same parent; own DM channel;
//                    no infra visibility.
//   - team         → every agent whose parent is this team's UUID;
//                    channels empty (team channel enumeration lives in
//                    the comms service, not the registry); no infra.
//   - anything else → empty scope (deny).
func Resolve(p *registry.Principal, reg *registry.Registry) *Scope {
	if p == nil || reg == nil {
		return AllowAll()
	}

	switch p.Type {
	case "operator":
		return AllowAll()

	case "agent":
		s := &Scope{
			Agents:   map[string]bool{p.Name: true},
			Channels: map[string]bool{},
		}
		// Siblings under the same team parent.
		if p.Parent != "" {
			if agents, err := reg.List("agent"); err == nil {
				for _, a := range agents {
					if a.Parent == p.Parent {
						s.Agents[a.Name] = true
					}
				}
			}
		}
		// DM channel convention is "dm-<agent-name>".
		s.Channels["dm-"+p.Name] = true
		return s

	case "team":
		s := &Scope{
			Agents:   map[string]bool{},
			Channels: map[string]bool{},
		}
		if agents, err := reg.List("agent"); err == nil {
			for _, a := range agents {
				if a.Parent == p.UUID {
					s.Agents[a.Name] = true
				}
			}
		}
		return s

	default:
		// role, channel, or unknown — no WS visibility.
		return DenyAll()
	}
}

// AllowsAgent reports whether the scope admits events keyed to the given
// agent name. An empty name is allowed (the event is not agent-scoped).
func (s *Scope) AllowsAgent(name string) bool {
	if s == nil || s.All {
		return true
	}
	if name == "" {
		return true
	}
	return s.Agents[name]
}

// AllowsChannel reports whether the scope admits events keyed to the given
// channel name. An empty name is allowed (the event is not channel-scoped).
func (s *Scope) AllowsChannel(name string) bool {
	if s == nil || s.All {
		return true
	}
	if name == "" {
		return true
	}
	return s.Channels[name]
}

// AllowsInfra reports whether the scope admits platform infrastructure events
// (infra_status, deploy_progress).
func (s *Scope) AllowsInfra() bool {
	if s == nil || s.All {
		return true
	}
	return s.Infra
}
