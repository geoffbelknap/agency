package runtime

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	authzcore "github.com/geoffbelknap/agency/internal/authz"
	agencyconsent "github.com/geoffbelknap/agency/internal/consent"
	"github.com/geoffbelknap/agency/internal/hub"
	instancepkg "github.com/geoffbelknap/agency/internal/instances"
	"github.com/geoffbelknap/agency/internal/models"
	"gopkg.in/yaml.v3"
)

type packageResolver interface {
	GetPackage(kind, name string) (hub.InstalledPackage, bool)
}

type Planner struct {
	Packages packageResolver
}

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
			InstanceRevision:    inst.UpdatedAt,
			ConsentDeploymentID: plannerConsentDeploymentID(inst),
		},
		Status: ManifestStatus{
			ReconcileState: ReconcileStatePending,
		},
	}

	for _, node := range inst.Nodes {
		if strings.TrimSpace(node.Package.Kind) == "" || strings.TrimSpace(node.Package.Name) == "" {
			return nil, fmt.Errorf("node %q missing package reference", node.ID)
		}
		switch node.Kind {
		case "connector.authority":
			executor, err := p.plannerExecutor(node)
			if err != nil {
				return nil, fmt.Errorf("node %q executor: %w", node.ID, err)
			}
			runtimeNode := RuntimeNode{
				NodeID: node.ID,
				Kind:   node.Kind,
				Package: RuntimePackageRef{
					Kind:    node.Package.Kind,
					Name:    node.Package.Name,
					Version: node.Package.Version,
				},
				Tools:               stringList(node.Config["tools"]),
				ResourceWhitelist:   plannerResourceWhitelist(node),
				CredentialBindings:  plannerCredentialBindings(node),
				GrantSubjects:       plannerGrantSubjects(inst, node.ID),
				ConsentActions:      plannerConsentActions(inst, node.ID),
				ConsentRequirements: plannerConsentRequirements(inst, node.ID),
				Executor:            executor,
				Materialization:     "authority/" + node.ID + ".yaml",
			}
			manifest.Runtime.Nodes = append(manifest.Runtime.Nodes, runtimeNode)
			manifest.Runtime.Operations = append(manifest.Runtime.Operations, RuntimeOperation{
				Type:   "materialize_authority",
				NodeID: node.ID,
				Path:   runtimeNode.Materialization,
			})
		case "connector.ingress":
			ingress, err := p.plannerIngress(inst, node)
			if err != nil {
				return nil, fmt.Errorf("node %q ingress: %w", node.ID, err)
			}
			runtimeNode := RuntimeNode{
				NodeID: node.ID,
				Kind:   node.Kind,
				Package: RuntimePackageRef{
					Kind:    node.Package.Kind,
					Name:    node.Package.Name,
					Version: node.Package.Version,
				},
				CredentialBindings: plannerCredentialBindings(node),
				Ingress:            ingress,
				Materialization:    "ingress/" + node.ID + ".yaml",
			}
			manifest.Runtime.Nodes = append(manifest.Runtime.Nodes, runtimeNode)
			manifest.Runtime.Operations = append(manifest.Runtime.Operations, RuntimeOperation{
				Type:   "publish_ingress",
				NodeID: node.ID,
				Path:   runtimeNode.Materialization,
			})
		}
	}

	sort.Slice(manifest.Runtime.Nodes, func(i, j int) bool {
		return manifest.Runtime.Nodes[i].NodeID < manifest.Runtime.Nodes[j].NodeID
	})
	sort.Slice(manifest.Runtime.Operations, func(i, j int) bool {
		return manifest.Runtime.Operations[i].NodeID < manifest.Runtime.Operations[j].NodeID
	})
	for key, binding := range inst.Credentials {
		manifest.Runtime.Bindings = append(manifest.Runtime.Bindings, RuntimeBinding{
			Name:   key,
			Type:   binding.Type,
			Target: binding.Target,
		})
	}
	sort.Slice(manifest.Runtime.Bindings, func(i, j int) bool {
		return manifest.Runtime.Bindings[i].Name < manifest.Runtime.Bindings[j].Name
	})

	return manifest, nil
}

func (p Planner) plannerExecutor(node instancepkg.Node) (*RuntimeExecutor, error) {
	raw, ok := node.Config["executor"]
	if (!ok || raw == nil) && p.Packages != nil {
		if pkg, found := p.Packages.GetPackage(node.Package.Kind, node.Package.Name); found {
			if runtimeSpec, foundRuntime := pkg.Spec["runtime"].(map[string]any); foundRuntime {
				raw = runtimeSpec["executor"]
				ok = raw != nil
			}
		}
	}
	if !ok || raw == nil {
		return nil, nil
	}
	cfg, ok := raw.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("executor must be an object")
	}
	kind, _ := cfg["kind"].(string)
	kind = strings.TrimSpace(kind)
	if kind == "" {
		return nil, fmt.Errorf("executor.kind is required")
	}
	if kind != "http_json" {
		return nil, fmt.Errorf("unsupported executor kind %q", kind)
	}
	baseURL, _ := cfg["base_url"].(string)
	baseURL = strings.TrimSpace(baseURL)
	if baseURL == "" {
		return nil, fmt.Errorf("executor.base_url is required")
	}
	actions, err := plannerHTTPActions(cfg["actions"])
	if err != nil {
		return nil, err
	}
	if len(actions) == 0 {
		return nil, fmt.Errorf("executor.actions is required")
	}
	auth, err := plannerExecutorAuth(cfg["auth"])
	if err != nil {
		return nil, err
	}
	return &RuntimeExecutor{
		Kind:    kind,
		BaseURL: baseURL,
		Actions: actions,
		Auth:    auth,
	}, nil
}

func plannerHTTPActions(raw any) (map[string]RuntimeHTTPAction, error) {
	if raw == nil {
		return nil, nil
	}
	items, ok := raw.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("executor.actions must be an object")
	}
	out := make(map[string]RuntimeHTTPAction, len(items))
	for name, entry := range items {
		cfg, ok := entry.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("executor.actions.%s must be an object", name)
		}
		path, _ := cfg["path"].(string)
		path = strings.TrimSpace(path)
		if path == "" {
			return nil, fmt.Errorf("executor.actions.%s.path is required", name)
		}
		method, _ := cfg["method"].(string)
		headers, err := stringMap(cfg["headers"])
		if err != nil {
			return nil, fmt.Errorf("executor.actions.%s.headers: %w", name, err)
		}
		query, err := stringMap(cfg["query"])
		if err != nil {
			return nil, fmt.Errorf("executor.actions.%s.query: %w", name, err)
		}
		body, err := stringMap(cfg["body"])
		if err != nil {
			return nil, fmt.Errorf("executor.actions.%s.body: %w", name, err)
		}
		whitelistField := strings.TrimSpace(stringValue(cfg["whitelist_field"]))
		whitelistKind := strings.TrimSpace(stringValue(cfg["whitelist_kind"]))
		out[name] = RuntimeHTTPAction{
			Method:         strings.ToUpper(strings.TrimSpace(method)),
			Path:           path,
			Headers:        headers,
			Query:          query,
			Body:           body,
			WhitelistField: whitelistField,
			WhitelistKind:  whitelistKind,
		}
	}
	return out, nil
}

func plannerExecutorAuth(raw any) (*RuntimeExecutorAuth, error) {
	if raw == nil {
		return nil, nil
	}
	cfg, ok := raw.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("executor.auth must be an object")
	}
	authType, _ := cfg["type"].(string)
	authType = strings.TrimSpace(authType)
	if authType == "" {
		return nil, fmt.Errorf("executor.auth.type is required")
	}
	auth := &RuntimeExecutorAuth{
		Type:    authType,
		Binding: strings.TrimSpace(stringValue(cfg["binding"])),
		Header:  strings.TrimSpace(stringValue(cfg["header"])),
		Prefix:  stringValue(cfg["prefix"]),
		Scopes:  stringList(cfg["scopes"]),
	}
	if auth.Binding == "" {
		return nil, fmt.Errorf("executor.auth.binding is required")
	}
	switch auth.Type {
	case "bearer":
		if auth.Header == "" {
			auth.Header = "Authorization"
		}
		if auth.Prefix == "" {
			auth.Prefix = "Bearer "
		}
	case "header":
		if auth.Header == "" {
			return nil, fmt.Errorf("executor.auth.header is required for header auth")
		}
	case "google_service_account":
		if auth.Header == "" {
			auth.Header = "Authorization"
		}
		if auth.Prefix == "" {
			auth.Prefix = "Bearer "
		}
		if len(auth.Scopes) == 0 {
			return nil, fmt.Errorf("executor.auth.scopes is required for google_service_account auth")
		}
	default:
		return nil, fmt.Errorf("unsupported executor.auth.type %q", auth.Type)
	}
	return auth, nil
}

func plannerCredentialBindings(node instancepkg.Node) []string {
	keys := stringList(node.Config["credential_bindings"])
	sort.Strings(keys)
	return dedupe(keys)
}

func (p Planner) plannerIngress(inst *instancepkg.Instance, node instancepkg.Node) (*RuntimeIngressSpec, error) {
	if p.Packages == nil {
		return nil, fmt.Errorf("package registry required")
	}
	pkg, found := p.Packages.GetPackage(node.Package.Kind, node.Package.Name)
	if !found {
		return nil, fmt.Errorf("package %s/%s not found", node.Package.Kind, node.Package.Name)
	}
	var cfg models.ConnectorConfig
	if err := models.Load(pkg.Path, &cfg); err != nil {
		return nil, fmt.Errorf("load connector package: %w", err)
	}
	if cfg.Source.Type == "" || cfg.Source.Type == "none" {
		return nil, fmt.Errorf("package %q does not define an ingress source", node.Package.Name)
	}
	rendered, err := renderIngressConnectorYAML(pkg.Path, ingressTemplateValues(inst, node))
	if err != nil {
		return nil, err
	}
	var renderedCfg models.ConnectorConfig
	if err := yaml.Unmarshal(rendered, &renderedCfg); err != nil {
		return nil, fmt.Errorf("parse rendered connector yaml: %w", err)
	}
	publishedName := strings.TrimSpace(stringValue(node.Config["published_name"]))
	if publishedName == "" {
		publishedName = inst.Name
	}
	renderedCfg.Name = publishedName
	if renderedCfg.Source.Type == "webhook" {
		path := "/webhooks/" + publishedName
		renderedCfg.Source.Path = &path
	}
	finalYAML, err := yaml.Marshal(renderedCfg)
	if err != nil {
		return nil, fmt.Errorf("marshal rendered connector yaml: %w", err)
	}
	return &RuntimeIngressSpec{
		PublishedName: publishedName,
		ConnectorYAML: string(finalYAML),
	}, nil
}

func renderIngressConnectorYAML(path string, values map[string]string) ([]byte, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read connector package: %w", err)
	}
	return []byte(hub.ResolvePlaceholders(string(data), values)), nil
}

func ingressTemplateValues(inst *instancepkg.Instance, node instancepkg.Node) map[string]string {
	values := map[string]string{}
	for key, value := range inst.Config {
		if s, ok := value.(string); ok {
			values[key] = s
		}
	}
	for key, value := range node.Config {
		if s, ok := value.(string); ok {
			values[key] = s
		}
	}
	return values
}

func plannerResourceWhitelist(node instancepkg.Node) []RuntimeResourceWhitelistEntry {
	raw, ok := node.Config["resource_whitelist"]
	if !ok || raw == nil {
		return nil
	}
	items, ok := raw.([]any)
	if !ok {
		return nil
	}
	out := make([]RuntimeResourceWhitelistEntry, 0, len(items))
	for _, item := range items {
		entry, ok := item.(map[string]any)
		if !ok {
			continue
		}
		id := strings.TrimSpace(firstString(entry["drive_id"], entry["id"], entry["resource_id"]))
		if id == "" {
			continue
		}
		out = append(out, RuntimeResourceWhitelistEntry{
			Kind: strings.TrimSpace(stringValue(entry["kind"])),
			ID:   id,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Kind != out[j].Kind {
			return out[i].Kind < out[j].Kind
		}
		return out[i].ID < out[j].ID
	})
	return out
}

func firstString(values ...any) string {
	for _, value := range values {
		s := strings.TrimSpace(stringValue(value))
		if s != "" {
			return s
		}
	}
	return ""
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

func plannerConsentRequirements(inst *instancepkg.Instance, nodeID string) map[string]agencyconsent.Requirement {
	out := map[string]agencyconsent.Requirement{}
	for _, grant := range inst.Grants {
		if grant.Resource != nodeID && grant.Resource != "node:"+inst.Name+"/"+nodeID {
			continue
		}
		requirement, ok := consentRequirementFromGrant(grant.Config)
		if !ok {
			continue
		}
		action := strings.TrimSpace(grant.Action)
		if action == "" {
			continue
		}
		out[action] = requirement.Normalize()
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func consentRequirementFromGrant(cfg map[string]any) (agencyconsent.Requirement, bool) {
	if len(cfg) == 0 {
		return agencyconsent.Requirement{}, false
	}
	raw := map[string]any(nil)
	if nested, ok := cfg["requires_consent_token"].(map[string]any); ok {
		raw = nested
	} else {
		raw = cfg
	}
	req := agencyconsent.Requirement{
		OperationKind:    strings.TrimSpace(stringValue(raw["operation_kind"])),
		TokenInputField:  strings.TrimSpace(stringValue(raw["token_input_field"])),
		TargetInputField: strings.TrimSpace(stringValue(raw["target_input_field"])),
	}
	switch v := raw["min_witnesses"].(type) {
	case int:
		req.MinWitnesses = v
	case int64:
		req.MinWitnesses = int(v)
	case float64:
		req.MinWitnesses = int(v)
	}
	if err := req.Validate(); err != nil {
		return agencyconsent.Requirement{}, false
	}
	return req.Normalize(), true
}

func plannerConsentDeploymentID(inst *instancepkg.Instance) string {
	for _, key := range []string{"consent_deployment_id", "deployment_id"} {
		if value := strings.TrimSpace(stringValue(inst.Config[key])); value != "" {
			return value
		}
	}
	return ""
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

func stringMap(raw any) (map[string]string, error) {
	if raw == nil {
		return nil, nil
	}
	items, ok := raw.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("must be an object")
	}
	out := make(map[string]string, len(items))
	for key, value := range items {
		s, ok := value.(string)
		if !ok {
			return nil, fmt.Errorf("value for %q must be a string", key)
		}
		out[key] = s
	}
	return out, nil
}

func stringValue(raw any) string {
	if s, ok := raw.(string); ok {
		return s
	}
	return ""
}
