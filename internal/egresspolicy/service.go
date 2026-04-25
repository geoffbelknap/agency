package egresspolicy

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	agencysecurity "github.com/geoffbelknap/agency/internal/security"
	"gopkg.in/yaml.v3"
)

const (
	ModeDenylist             = "denylist"
	ModeAllowlist            = "allowlist"
	ModeSupervisedStrict     = "supervised-strict"
	ModeSupervisedPermissive = "supervised-permissive"
	EventDomainApproved      = "egress_domain_approved"
	EventDomainRevoked       = "egress_domain_revoked"
	EventModeChanged         = "egress_mode_changed"
)

var (
	ErrInvalidDomain = errors.New("invalid domain")
	ErrInvalidMode   = errors.New("invalid egress mode")
	ErrAuditRequired = errors.New("audit writer required")
)

// AuditWriter is the gateway-side audit boundary used by policy mutations.
type AuditWriter interface {
	Write(agent, event string, detail map[string]interface{}) error
}

// Service mutates per-agent egress policy files and records gateway audit events.
type Service struct {
	Home  string
	Audit AuditWriter
	Now   func() time.Time
}

type MutationResult struct {
	Change agencysecurity.Mutation `json:"change"`
	Egress map[string]interface{}  `json:"egress"`
}

func (s Service) List(agent string) (map[string]interface{}, error) {
	return s.load(agent)
}

func (s Service) ApproveDomain(agent, domain, reason string) (*MutationResult, error) {
	domain, err := NormalizeDomain(domain)
	if err != nil {
		return nil, err
	}
	egress, err := s.load(agent)
	if err != nil {
		return nil, err
	}
	domains, _ := egress["domains"].([]interface{})
	domains = append(domains, map[string]interface{}{
		"domain":      domain,
		"approved_by": "operator",
		"reason":      reason,
		"approved_at": s.now().UTC().Format(time.RFC3339),
	})
	egress["domains"] = domains
	if err := s.saveAndAudit(agent, egress, EventDomainApproved, map[string]interface{}{"domain": domain, "reason": reason}); err != nil {
		return nil, err
	}
	return &MutationResult{
		Change: mutation("approve_domain", agent, domain, "approved egress domain"),
		Egress: egress,
	}, nil
}

func (s Service) RevokeDomain(agent, domain string) (*MutationResult, error) {
	domain, err := NormalizeDomain(domain)
	if err != nil {
		return nil, err
	}
	egress, err := s.load(agent)
	if err != nil {
		return nil, err
	}
	domains, _ := egress["domains"].([]interface{})
	filtered := make([]interface{}, 0, len(domains))
	for _, entry := range domains {
		switch d := entry.(type) {
		case map[string]interface{}:
			if NormalizeDomainNoError(mapStr(d, "domain")) != domain {
				filtered = append(filtered, entry)
			}
		case string:
			if NormalizeDomainNoError(d) != domain {
				filtered = append(filtered, entry)
			}
		default:
			filtered = append(filtered, entry)
		}
	}
	egress["domains"] = filtered
	if err := s.saveAndAudit(agent, egress, EventDomainRevoked, map[string]interface{}{"domain": domain}); err != nil {
		return nil, err
	}
	return &MutationResult{
		Change: mutation("revoke_domain", agent, domain, "revoked egress domain"),
		Egress: egress,
	}, nil
}

func (s Service) SetMode(agent, mode string) (*MutationResult, error) {
	mode = strings.TrimSpace(strings.ToLower(mode))
	if !ValidMode(mode) {
		return nil, ErrInvalidMode
	}
	egress, err := s.load(agent)
	if err != nil {
		return nil, err
	}
	egress["mode"] = mode
	if err := s.saveAndAudit(agent, egress, EventModeChanged, map[string]interface{}{"mode": mode}); err != nil {
		return nil, err
	}
	return &MutationResult{
		Change: mutation("set_mode", agent, mode, "updated egress mode"),
		Egress: egress,
	}, nil
}

func ValidMode(mode string) bool {
	switch mode {
	case ModeDenylist, ModeAllowlist, ModeSupervisedStrict, ModeSupervisedPermissive:
		return true
	default:
		return false
	}
}

func NormalizeDomain(raw string) (string, error) {
	domain := strings.TrimSpace(strings.ToLower(raw))
	domain = strings.TrimSuffix(domain, ".")
	if domain == "" || strings.Contains(domain, "://") || strings.ContainsAny(domain, "/\\:") {
		return "", ErrInvalidDomain
	}
	if strings.HasPrefix(domain, "*.") {
		rest := strings.TrimPrefix(domain, "*.")
		if validDomainLabels(rest) {
			return domain, nil
		}
		return "", ErrInvalidDomain
	}
	if !validDomainLabels(domain) {
		return "", ErrInvalidDomain
	}
	return domain, nil
}

func NormalizeDomainNoError(raw string) string {
	domain, err := NormalizeDomain(raw)
	if err != nil {
		return strings.TrimSuffix(strings.TrimSpace(strings.ToLower(raw)), ".")
	}
	return domain
}

func (s Service) load(agent string) (map[string]interface{}, error) {
	var egress map[string]interface{}
	if data, err := os.ReadFile(s.path(agent)); err == nil {
		if err := yaml.Unmarshal(data, &egress); err != nil {
			return nil, fmt.Errorf("read egress policy: %w", err)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("read egress policy: %w", err)
	}
	if egress == nil {
		egress = map[string]interface{}{"agent": agent, "mode": ModeAllowlist, "domains": []interface{}{}}
	}
	if egress["agent"] == nil {
		egress["agent"] = agent
	}
	if egress["mode"] == nil {
		egress["mode"] = ModeAllowlist
	}
	if egress["domains"] == nil {
		egress["domains"] = []interface{}{}
	}
	return egress, nil
}

func (s Service) saveAndAudit(agent string, egress map[string]interface{}, event string, detail map[string]interface{}) error {
	if s.Audit == nil {
		return ErrAuditRequired
	}
	if err := os.MkdirAll(filepath.Dir(s.path(agent)), 0700); err != nil {
		return fmt.Errorf("create egress policy dir: %w", err)
	}
	data, err := yaml.Marshal(egress)
	if err != nil {
		return fmt.Errorf("marshal egress policy: %w", err)
	}
	if err := os.WriteFile(s.path(agent), data, 0600); err != nil {
		return fmt.Errorf("write egress policy: %w", err)
	}
	if err := s.Audit.Write(agent, event, detail); err != nil {
		return fmt.Errorf("write audit event: %w", err)
	}
	return nil
}

func (s Service) path(agent string) string {
	return filepath.Join(s.Home, "agents", agent, "egress.yaml")
}

func (s Service) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

func validDomainLabels(domain string) bool {
	if len(domain) == 0 || len(domain) > 253 || strings.Contains(domain, "..") {
		return false
	}
	labels := strings.Split(domain, ".")
	for _, label := range labels {
		if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, r := range label {
			if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' {
				continue
			}
			return false
		}
	}
	return true
}

func mapStr(m map[string]interface{}, key string) string {
	if m == nil {
		return ""
	}
	v, ok := m[key]
	if !ok {
		return ""
	}
	s, ok := v.(string)
	if !ok {
		return fmt.Sprintf("%v", v)
	}
	return s
}

func mutation(action, agent, target, detail string) agencysecurity.Mutation {
	return agencysecurity.Mutation{
		Action: action,
		Agent:  agent,
		Scope:  "egress",
		Target: target,
		Status: agencysecurity.MutationApplied,
		Detail: detail,
	}
}
