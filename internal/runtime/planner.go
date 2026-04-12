package runtime

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
	"time"

	authzcore "github.com/geoffbelknap/agency/internal/authz"
	instancepkg "github.com/geoffbelknap/agency/internal/instances"
)

type Planner struct{}

func (p Planner) Compile(inst *instancepkg.Instance) (*Manifest, error) {
	if err := instancepkg.ValidateInstance(inst); err != nil {
		return nil, err
	}

	manifestID, err := generateManifestID()
	if err != nil {
		return nil, err
	}

	manifest := &Manifest{
		APIVersion: ManifestAPIVersion,
		Kind:       ManifestKind,
		Metadata: ManifestMeta{
			ManifestID:   manifestID,
			InstanceID:   inst.ID,
			InstanceName: inst.Name,
			CompiledAt:   time.Now().UTC(),
			Planner:      PlannerVersion,
		},
		Source: ManifestSource{
			InstanceRevision: inst.UpdatedAt,
		},
		Status: ManifestStatus{
			ReconcileState: ReconcileStatePending,
		},
	}

	for _, node := range inst.Nodes {
		if node.Kind != "connector.authority" {
			continue
		}
		if strings.TrimSpace(node.Package.Kind) == "" || strings.TrimSpace(node.Package.Name) == "" {
			return nil, fmt.Errorf("node %q missing package reference", node.ID)
		}
		runtimeNode := RuntimeNode{
			NodeID: node.ID,
			Kind:   node.Kind,
			Package: RuntimePackageRef{
				Kind:    node.Package.Kind,
				Name:    node.Package.Name,
				Version: node.Package.Version,
			},
			Tools:              stringList(node.Config["tools"]),
			CredentialBindings: plannerCredentialBindings(node),
			GrantSubjects:      plannerGrantSubjects(inst, node.ID),
			ConsentActions:     plannerConsentActions(inst, node.ID),
			Materialization:    "authority/" + node.ID + ".yaml",
		}
		manifest.Runtime.Nodes = append(manifest.Runtime.Nodes, runtimeNode)
		manifest.Runtime.Operations = append(manifest.Runtime.Operations, RuntimeOperation{
			Type:   "materialize_authority",
			NodeID: node.ID,
			Path:   runtimeNode.Materialization,
		})
	}

	sort.Slice(manifest.Runtime.Nodes, func(i, j int) bool {
		return manifest.Runtime.Nodes[i].NodeID < manifest.Runtime.Nodes[j].NodeID
	})
	sort.Slice(manifest.Runtime.Operations, func(i, j int) bool {
		return manifest.Runtime.Operations[i].NodeID < manifest.Runtime.Operations[j].NodeID
	})
	for key, binding := range inst.Credentials {
		manifest.Runtime.Bindings = append(manifest.Runtime.Bindings, RuntimeBinding{
			Name: key,
			Type: binding.Type,
		})
	}
	sort.Slice(manifest.Runtime.Bindings, func(i, j int) bool {
		return manifest.Runtime.Bindings[i].Name < manifest.Runtime.Bindings[j].Name
	})

	return manifest, nil
}

func plannerCredentialBindings(node instancepkg.Node) []string {
	keys := stringList(node.Config["credential_bindings"])
	sort.Strings(keys)
	return dedupe(keys)
}

func plannerGrantSubjects(inst *instancepkg.Instance, nodeID string) []string {
	var out []string
	for _, grant := range inst.Grants {
		if grant.Resource == nodeID || grant.Resource == "node:"+inst.Name+"/"+nodeID {
			out = append(out, grant.Principal)
		}
	}
	sort.Strings(out)
	return dedupe(out)
}

func plannerConsentActions(inst *instancepkg.Instance, nodeID string) []string {
	var out []string
	for _, grant := range inst.Grants {
		if grant.Resource != nodeID && grant.Resource != "node:"+inst.Name+"/"+nodeID {
			continue
		}
		if consentRequired(grant.Config) {
			out = append(out, grant.Action)
		}
		out = append(out, stringList(grant.Config["consent_actions"])...)
		if reqs := stringList(grant.Config["required_for"]); len(reqs) > 0 {
			out = append(out, reqs...)
		}
	}
	sort.Strings(out)
	return dedupe(out)
}

func ResolveRequestAgainstManifest(m *Manifest, req authzcore.Request) authzcore.Request {
	req.Grants = append([]authzcore.Grant(nil), req.Grants...)
	for _, node := range m.Runtime.Nodes {
		target := "node:" + m.Metadata.InstanceName + "/" + node.NodeID
		for _, subject := range node.GrantSubjects {
			grant := authzcore.Grant{
				Subject: subject,
				Target:  target,
				Actions: append([]string(nil), node.Tools...),
			}
			if len(node.ConsentActions) > 0 {
				grant.Consent = &authzcore.ConsentRequirement{RequiredFor: append([]string(nil), node.ConsentActions...)}
			}
			req.Grants = append(req.Grants, grant)
		}
	}
	return req
}

func generateManifestID() (string, error) {
	b := make([]byte, 4)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate manifest id: %w", err)
	}
	return "rtm_" + hex.EncodeToString(b), nil
}

func stringList(v any) []string {
	switch val := v.(type) {
	case []string:
		return append([]string(nil), val...)
	case []any:
		out := make([]string, 0, len(val))
		for _, item := range val {
			if s, ok := item.(string); ok && strings.TrimSpace(s) != "" {
				out = append(out, s)
			}
		}
		return out
	default:
		return nil
	}
}

func dedupe(items []string) []string {
	if len(items) == 0 {
		return nil
	}
	out := make([]string, 0, len(items))
	var last string
	for i, item := range items {
		if i == 0 || item != last {
			out = append(out, item)
			last = item
		}
	}
	return out
}

func consentRequired(cfg map[string]any) bool {
	if cfg == nil {
		return false
	}
	for _, key := range []string{"consent_required", "requires_consent"} {
		if v, ok := cfg[key].(bool); ok && v {
			return true
		}
	}
	return false
}
