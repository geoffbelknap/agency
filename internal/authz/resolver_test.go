package authz

import (
	"testing"

	agencysecurity "github.com/geoffbelknap/agency/internal/security"
)

func TestResolver_AllowsGrantedActionWithinInstanceScope(t *testing.T) {
	r := Resolver{}
	decision, err := r.Resolve(Request{
		Subject:  "agent:community-admin/coordinator",
		Target:   "node:community-admin/drive_admin",
		Action:   "add_viewer",
		Instance: "community-admin",
		Grants: []Grant{
			{Subject: "agent:community-admin/coordinator", Target: "node:community-admin/drive_admin", Actions: []string{"add_viewer"}},
		},
	})
	if err != nil {
		t.Fatalf("Resolve(): %v", err)
	}
	if !decision.Allow {
		t.Fatalf("expected allow, got %#v", decision)
	}
	if decision.Outcome != agencysecurity.DecisionAllow {
		t.Fatalf("expected allow outcome, got %#v", decision)
	}
}

func TestResolver_DeniesWhenConsentRequiredButMissing(t *testing.T) {
	r := Resolver{}
	decision, err := r.Resolve(Request{
		Subject:  "agent:community-admin/coordinator",
		Target:   "node:community-admin/drive_admin",
		Action:   "add_viewer",
		Instance: "community-admin",
		Grants: []Grant{
			{
				Subject: "agent:community-admin/coordinator",
				Target:  "node:community-admin/drive_admin",
				Actions: []string{"add_viewer"},
				Consent: &ConsentRequirement{RequiredFor: []string{"add_viewer"}},
			},
		},
	})
	if err != nil {
		t.Fatalf("Resolve(): %v", err)
	}
	if decision.Allow {
		t.Fatalf("expected deny, got %#v", decision)
	}
	if !decision.ConsentNeeded {
		t.Fatalf("expected consent_needed, got %#v", decision)
	}
	if decision.Outcome != agencysecurity.DecisionConsentRequired {
		t.Fatalf("expected consent_required outcome, got %#v", decision)
	}
}

func TestResolver_AllowsWhenConsentProvided(t *testing.T) {
	r := Resolver{}
	decision, err := r.Resolve(Request{
		Subject:         "agent:community-admin/coordinator",
		Target:          "node:community-admin/drive_admin",
		Action:          "add_viewer",
		Instance:        "community-admin",
		ConsentProvided: true,
		Grants: []Grant{
			{
				Subject: "agent:community-admin/coordinator",
				Target:  "node:community-admin/drive_admin",
				Actions: []string{"add_viewer"},
				Consent: &ConsentRequirement{RequiredFor: []string{"add_viewer"}},
			},
		},
	})
	if err != nil {
		t.Fatalf("Resolve(): %v", err)
	}
	if !decision.Allow {
		t.Fatalf("expected allow, got %#v", decision)
	}
	if decision.Outcome != agencysecurity.DecisionAllow {
		t.Fatalf("expected allow outcome, got %#v", decision)
	}
}
