package hub

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"gopkg.in/yaml.v3"

	hubpkg "github.com/geoffbelknap/agency/internal/hub"
	deploymentspkg "github.com/geoffbelknap/agency/internal/hub/deployments"
)

func (h *handler) deploymentStore() *deploymentspkg.FilesystemStore {
	root := filepath.Join(h.deps.Config.Home, "hub", "deployments")
	if cfgRoot := strings.TrimSpace(h.deps.Config.Hub.DeploymentBackendConfig["root"]); cfgRoot != "" {
		root = os.ExpandEnv(cfgRoot)
	}
	return deploymentspkg.NewFilesystemStore(root)
}

func (h *handler) deploymentAgencyOwner() deploymentspkg.OwnerRef {
	host, _ := os.Hostname()
	if host == "" {
		host = "local"
	}
	return deploymentspkg.OwnerRef{
		AgencyID:   fmt.Sprintf("%s:%x", host, len(h.deps.Config.Home)),
		AgencyName: host,
	}
}

func (h *handler) deploymentList(w http.ResponseWriter, r *http.Request) {
	items, err := h.deploymentStore().List(r.Context())
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, 200, items)
}

func (h *handler) deploymentShow(w http.ResponseWriter, r *http.Request) {
	dep, schema, err := h.getDeployment(r.Context(), chi.URLParam(r, "nameOrID"))
	if err != nil {
		writeJSON(w, 404, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, 200, map[string]interface{}{"deployment": dep, "schema": schema})
}

func (h *handler) deploymentSchema(w http.ResponseWriter, r *http.Request) {
	schema, packRef, _, err := h.loadPackSchema(chi.URLParam(r, "pack"))
	if err != nil {
		writeJSON(w, 404, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, 200, map[string]interface{}{"pack": packRef, "schema": schema})
}

func (h *handler) deploymentCreate(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Pack     string                 `json:"pack"`
		Name     string                 `json:"name"`
		Config   map[string]interface{} `json:"config"`
		CredRefs map[string]string      `json:"credrefs"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, 400, map[string]string{"error": "invalid JSON"})
		return
	}
	if strings.TrimSpace(body.Pack) == "" {
		writeJSON(w, 400, map[string]string{"error": "pack is required"})
		return
	}
	schema, packRef, packComp, err := h.loadPackSchema(body.Pack)
	if err != nil {
		writeJSON(w, 400, map[string]string{"error": err.Error()})
		return
	}
	config := schema.ApplyDefaults(body.Config)
	if err := schema.ValidateConfig(config); err != nil {
		writeJSON(w, 400, map[string]string{"error": err.Error()})
		return
	}
	credrefs := make(map[string]deploymentspkg.CredRef, len(body.CredRefs))
	for key, credID := range body.CredRefs {
		credrefs[key] = deploymentspkg.CredRef{Key: key, CredstoreID: credID, ExportPolicy: "ref_only"}
	}
	name := strings.TrimSpace(body.Name)
	if name == "" {
		name = schema.Deployment.Name
	}
	dep := &deploymentspkg.Deployment{
		Name:          name,
		Pack:          packRef,
		SchemaVersion: schema.SchemaVersion,
		Config:        config,
		CredRefs:      credrefs,
		Owner:         h.deploymentAgencyOwner(),
	}
	dep.Owner.ClaimedAt = time.Now().UTC()
	dep.Owner.Heartbeat = dep.Owner.ClaimedAt
	if err := h.deploymentStore().Create(r.Context(), dep, schema); err != nil {
		writeJSON(w, 400, map[string]string{"error": err.Error()})
		return
	}
	if err := h.instantiateDeploymentInstances(r.Context(), dep, schema, packComp); err != nil {
		_ = h.deploymentStore().Delete(context.Background(), dep.ID)
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	if err := h.applyDeployment(r.Context(), dep.ID); err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	_ = h.deploymentStore().AppendAudit(r.Context(), dep.ID, deploymentspkg.AuditEntry{
		Action:       "create",
		DeploymentID: dep.ID,
		Result:       "ok",
	})
	h.deps.Audit.WriteSystem("hub_deployment_created", map[string]interface{}{
		"deployment_id": dep.ID,
		"pack":          dep.Pack.Name,
	})
	writeJSON(w, 200, dep)
}

func (h *handler) deploymentConfigure(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Config   map[string]interface{} `json:"config"`
		CredRefs map[string]string      `json:"credrefs"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, 400, map[string]string{"error": "invalid JSON"})
		return
	}
	dep, schema, err := h.getDeployment(r.Context(), chi.URLParam(r, "nameOrID"))
	if err != nil {
		writeJSON(w, 404, map[string]string{"error": err.Error()})
		return
	}
	if err := h.requireDeploymentOwner(dep); err != nil {
		writeJSON(w, 409, map[string]string{"error": err.Error()})
		return
	}
	nextConfig := dep.Config
	if body.Config != nil {
		nextConfig = schema.ApplyDefaults(body.Config)
	}
	if err := schema.ValidateConfig(nextConfig); err != nil {
		writeJSON(w, 400, map[string]string{"error": err.Error()})
		return
	}
	nextCredRefs := dep.CredRefs
	if body.CredRefs != nil {
		nextCredRefs = make(map[string]deploymentspkg.CredRef, len(body.CredRefs))
		for key, credID := range body.CredRefs {
			nextCredRefs[key] = deploymentspkg.CredRef{Key: key, CredstoreID: credID, ExportPolicy: "ref_only"}
		}
	}
	if err := h.deploymentStore().Update(r.Context(), dep.ID, func(stored *deploymentspkg.Deployment, _ *deploymentspkg.Schema) error {
		stored.Config = nextConfig
		stored.CredRefs = nextCredRefs
		if stored.Owner.AgencyID != "" {
			stored.Owner.Heartbeat = time.Now().UTC()
		}
		return nil
	}); err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	_ = h.deploymentStore().AppendAudit(r.Context(), dep.ID, deploymentspkg.AuditEntry{
		Action:       "configure",
		DeploymentID: dep.ID,
		Result:       "ok",
	})
	writeJSON(w, 200, map[string]string{"status": "configured", "deployment_id": dep.ID})
}

func (h *handler) deploymentValidate(w http.ResponseWriter, r *http.Request) {
	dep, schema, err := h.getDeployment(r.Context(), chi.URLParam(r, "nameOrID"))
	if err != nil {
		writeJSON(w, 404, map[string]string{"error": err.Error()})
		return
	}
	if err := h.requireDeploymentOwner(dep); err != nil {
		writeJSON(w, 409, map[string]string{"error": err.Error()})
		return
	}
	if err := schema.ValidateConfig(dep.Config); err != nil {
		writeJSON(w, 400, map[string]string{"error": err.Error()})
		return
	}
	_ = h.deploymentStore().Update(r.Context(), dep.ID, func(stored *deploymentspkg.Deployment, _ *deploymentspkg.Schema) error {
		if stored.Owner.AgencyID != "" {
			stored.Owner.Heartbeat = time.Now().UTC()
		}
		return nil
	})
	_ = h.deploymentStore().AppendAudit(r.Context(), dep.ID, deploymentspkg.AuditEntry{
		Action:       "validate",
		DeploymentID: dep.ID,
		Result:       "ok",
	})
	writeJSON(w, 200, map[string]string{"status": "valid"})
}

func (h *handler) deploymentApply(w http.ResponseWriter, r *http.Request) {
	if err := h.applyDeployment(r.Context(), chi.URLParam(r, "nameOrID")); err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, 200, map[string]string{"status": "applied"})
}

func (h *handler) deploymentExport(w http.ResponseWriter, r *http.Request) {
	dep, _, err := h.getDeployment(r.Context(), chi.URLParam(r, "nameOrID"))
	if err != nil {
		writeJSON(w, 404, map[string]string{"error": err.Error()})
		return
	}
	reader, err := h.deploymentStore().Export(r.Context(), dep.ID)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	defer reader.Close()
	w.Header().Set("Content-Type", "application/gzip")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=deployment-%s-%s.tar.gz", dep.Name, dep.ID))
	_, _ = io.Copy(w, reader)
}

func (h *handler) deploymentImport(w http.ResponseWriter, r *http.Request) {
	dep, schema, err := h.deploymentStore().Import(r.Context(), r.Body)
	if err != nil {
		writeJSON(w, 400, map[string]string{"error": err.Error()})
		return
	}
	if override := strings.TrimSpace(r.URL.Query().Get("name")); override != "" {
		if err := h.deploymentStore().Update(r.Context(), dep.ID, func(stored *deploymentspkg.Deployment, _ *deploymentspkg.Schema) error {
			stored.Name = override
			return nil
		}); err != nil {
			writeJSON(w, 400, map[string]string{"error": err.Error()})
			return
		}
		dep.Name = override
	}
	if err := schema.ValidateConfig(dep.Config); err != nil {
		writeJSON(w, 400, map[string]string{"error": err.Error()})
		return
	}
	packComp, err := h.findPackComponent(dep.Pack.Name, dep.Pack.HubSource)
	if err != nil {
		writeJSON(w, 400, map[string]string{"error": err.Error()})
		return
	}
	if err := h.instantiateDeploymentInstances(r.Context(), dep, schema, packComp); err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	if err := h.applyDeployment(r.Context(), dep.ID); err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	_ = h.deploymentStore().AppendAudit(r.Context(), dep.ID, deploymentspkg.AuditEntry{
		Action:       "import",
		DeploymentID: dep.ID,
		Result:       "ok",
		Metadata: map[string]interface{}{
			"imported_from": dep.Pack.HubSource,
		},
	})
	writeJSON(w, 200, dep)
}

func (h *handler) deploymentClaim(w http.ResponseWriter, r *http.Request) {
	dep, _, err := h.getDeployment(r.Context(), chi.URLParam(r, "nameOrID"))
	if err != nil {
		writeJSON(w, 404, map[string]string{"error": err.Error()})
		return
	}
	force, _ := strconv.ParseBool(r.URL.Query().Get("force"))
	if err := h.deploymentStore().Claim(r.Context(), dep.ID, h.deploymentAgencyOwner(), force); err != nil {
		writeJSON(w, 409, map[string]string{"error": err.Error()})
		return
	}
	_ = h.deploymentStore().AppendAudit(r.Context(), dep.ID, deploymentspkg.AuditEntry{
		Action:       "claim",
		DeploymentID: dep.ID,
		Result:       "ok",
		Metadata:     map[string]interface{}{"force": force},
	})
	writeJSON(w, 200, map[string]string{"status": "claimed", "deployment_id": dep.ID})
}

func (h *handler) deploymentRelease(w http.ResponseWriter, r *http.Request) {
	dep, _, err := h.getDeployment(r.Context(), chi.URLParam(r, "nameOrID"))
	if err != nil {
		writeJSON(w, 404, map[string]string{"error": err.Error()})
		return
	}
	if err := h.requireDeploymentOwner(dep); err != nil {
		writeJSON(w, 409, map[string]string{"error": err.Error()})
		return
	}
	if err := h.deploymentStore().Release(r.Context(), dep.ID); err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	_ = h.deploymentStore().AppendAudit(r.Context(), dep.ID, deploymentspkg.AuditEntry{
		Action:       "release",
		DeploymentID: dep.ID,
		Result:       "ok",
	})
	writeJSON(w, 200, map[string]string{"status": "released", "deployment_id": dep.ID})
}

func (h *handler) deploymentDestroy(w http.ResponseWriter, r *http.Request) {
	dep, _, err := h.getDeployment(r.Context(), chi.URLParam(r, "nameOrID"))
	if err != nil {
		writeJSON(w, 404, map[string]string{"error": err.Error()})
		return
	}
	if err := h.requireDeploymentOwner(dep); err != nil {
		writeJSON(w, 409, map[string]string{"error": err.Error()})
		return
	}
	keepInstances, _ := strconv.ParseBool(r.URL.Query().Get("keep_instances"))
	mgr := hubpkg.NewManager(h.deps.Config.Home)
	for _, binding := range dep.Instances {
		inst := mgr.Registry.Resolve(binding.InstanceID)
		if inst == nil {
			continue
		}
		if keepInstances {
			_ = mgr.Registry.SetDeploymentBinding(inst.ID, "", "")
			continue
		}
		_ = mgr.Registry.Remove(inst.ID)
		if inst.Kind == "connector" {
			_ = os.Remove(filepath.Join(h.deps.Config.Home, "connectors", inst.Name+".yaml"))
		}
	}
	_ = h.deploymentStore().AppendAudit(r.Context(), dep.ID, deploymentspkg.AuditEntry{
		Action:       "destroy",
		DeploymentID: dep.ID,
		Result:       "ok",
		Metadata:     map[string]interface{}{"keep_instances": keepInstances},
	})
	if err := h.deploymentStore().Delete(r.Context(), dep.ID); err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, 200, map[string]string{"status": "destroyed", "deployment_id": dep.ID})
}

func (h *handler) getDeployment(ctx context.Context, nameOrID string) (*deploymentspkg.Deployment, *deploymentspkg.Schema, error) {
	items, err := h.deploymentStore().List(ctx)
	if err != nil {
		return nil, nil, err
	}
	for _, item := range items {
		if item.ID == nameOrID || item.Name == nameOrID {
			return h.deploymentStore().Get(ctx, item.ID)
		}
	}
	return nil, nil, fmt.Errorf("deployment %q not found", nameOrID)
}

func (h *handler) requireDeploymentOwner(dep *deploymentspkg.Deployment) error {
	if dep == nil || dep.Owner.AgencyID == "" {
		return nil
	}
	if dep.Owner.AgencyID != h.deploymentAgencyOwner().AgencyID {
		return fmt.Errorf("deployment owned by %s; claim it before mutating", dep.Owner.AgencyName)
	}
	return nil
}

func (h *handler) loadPackSchema(name string) (*deploymentspkg.Schema, deploymentspkg.PackRef, *hubpkg.Component, error) {
	comp, err := h.findPackComponent(name, "")
	if err != nil {
		return nil, deploymentspkg.PackRef{}, nil, err
	}
	schemaPath := filepath.Join(filepath.Dir(comp.Path), "deployment_schema.yaml")
	schema, err := deploymentspkg.LoadSchema(schemaPath)
	if err != nil {
		return nil, deploymentspkg.PackRef{}, nil, fmt.Errorf("pack %q is not deployment-enabled: %w", name, err)
	}
	packRef := deploymentspkg.PackRef{Name: comp.Name, Version: comp.Version, HubSource: comp.Source}
	return schema, packRef, comp, nil
}

func (h *handler) findPackComponent(name, source string) (*hubpkg.Component, error) {
	mgr := hubpkg.NewManager(h.deps.Config.Home)
	comp := mgr.FindInCache(name, "pack", source)
	if comp != nil {
		return comp, nil
	}
	return nil, fmt.Errorf("pack %q not found in hub cache; run 'agency hub update' and install sources first", name)
}

func componentNameFromSource(source string) string {
	parts := strings.Split(strings.TrimSpace(source), "/")
	if len(parts) == 0 {
		return strings.TrimSpace(source)
	}
	return parts[len(parts)-1]
}

func (h *handler) instantiateDeploymentInstances(ctx context.Context, dep *deploymentspkg.Deployment, schema *deploymentspkg.Schema, packComp *hubpkg.Component) error {
	mgr := hubpkg.NewManager(h.deps.Config.Home)
	var bindings []deploymentspkg.InstanceBinding
	items := []struct {
		component string
		role      string
		name      string
	}{
		{component: schema.Instances.Pack.Component, role: "pack", name: dep.Name},
	}
	for _, conn := range schema.Instances.Connectors {
		items = append(items, struct {
			component string
			role      string
			name      string
		}{component: conn.Component, role: "connector", name: dep.Name + "-" + conn.Component})
	}
	for _, preset := range schema.Instances.Presets {
		items = append(items, struct {
			component string
			role      string
			name      string
		}{component: preset.Component, role: "preset", name: dep.Name + "-" + preset.Component})
	}
	for _, item := range items {
		if existing := mgr.Registry.Resolve(item.name); existing != nil {
			if existing.DeploymentID != dep.ID && existing.DeploymentID != "" {
				return fmt.Errorf("instance name %q already owned by deployment %s", existing.Name, existing.DeploymentID)
			}
			if err := mgr.Registry.SetDeploymentBinding(existing.ID, dep.ID, item.role); err != nil {
				return err
			}
			bindings = append(bindings, deploymentspkg.InstanceBinding{Component: item.component, InstanceID: existing.ID, Role: item.role})
			continue
		}
		inst, err := mgr.Install(item.component, item.role, packComp.Source, item.name)
		if err != nil {
			return err
		}
		if err := mgr.Registry.SetDeploymentBinding(inst.ID, dep.ID, item.role); err != nil {
			return err
		}
		bindings = append(bindings, deploymentspkg.InstanceBinding{Component: item.component, InstanceID: inst.ID, Role: item.role})
	}
	return h.deploymentStore().Update(ctx, dep.ID, func(stored *deploymentspkg.Deployment, _ *deploymentspkg.Schema) error {
		stored.Instances = bindings
		return nil
	})
}

func (h *handler) applyDeployment(ctx context.Context, nameOrID string) error {
	dep, schema, err := h.getDeployment(ctx, nameOrID)
	if err != nil {
		return err
	}
	if err := h.requireDeploymentOwner(dep); err != nil {
		return err
	}
	if err := schema.ValidateConfig(dep.Config); err != nil {
		return err
	}
	mgr := hubpkg.NewManager(h.deps.Config.Home)
	for _, binding := range dep.Instances {
		inst := mgr.Registry.Resolve(binding.InstanceID)
		if inst == nil {
			return fmt.Errorf("instance %s missing", binding.InstanceID)
		}
		instDir := mgr.Registry.InstanceDir(inst.ID)
		templateData, err := os.ReadFile(filepath.Join(instDir, inst.Kind+".yaml"))
		if err != nil {
			return err
		}
		cfgSchema, err := hubpkg.ParseConfigSchema(templateData)
		if err != nil {
			return err
		}
		values := h.resolveInstanceConfigValues(binding.Component, cfgSchema, dep, schema)
		cv := &hubpkg.ConfigValues{
			Instance:        inst.Name,
			ID:              inst.ID,
			SourceComponent: inst.Source,
			ConfiguredAt:    time.Now().UTC().Format(time.RFC3339),
			Values:          values,
		}
		if err := hubpkg.WriteConfig(instDir, cv); err != nil {
			return err
		}
		if resolved, err := mgr.Registry.ResolvedYAML(inst.ID); err == nil && resolved != nil {
			_ = os.WriteFile(filepath.Join(instDir, "resolved.yaml"), resolved, 0o644)
			if inst.Kind == "connector" {
				connectorsDir := filepath.Join(h.deps.Config.Home, "connectors")
				_ = os.MkdirAll(connectorsDir, 0o755)
				_ = os.WriteFile(filepath.Join(connectorsDir, inst.Name+".yaml"), resolved, 0o644)
			}
		}
		h.autoActivate(mgr, inst)
	}
	h.signalInfraComponent("egress")
	if h.deps.Signal != nil {
		_ = h.deps.Signal.SignalContainer(ctx, "agency-intake", "SIGHUP")
	}
	if err := h.deploymentStore().Update(ctx, dep.ID, func(stored *deploymentspkg.Deployment, _ *deploymentspkg.Schema) error {
		if stored.Owner.AgencyID != "" {
			stored.Owner.Heartbeat = time.Now().UTC()
		}
		return nil
	}); err != nil {
		return err
	}
	return h.deploymentStore().AppendAudit(ctx, dep.ID, deploymentspkg.AuditEntry{
		Action:       "apply",
		DeploymentID: dep.ID,
		Result:       "ok",
	})
}

func (h *handler) resolveInstanceConfigValues(component string, schemaFields *hubpkg.ConfigSchema, dep *deploymentspkg.Deployment, depSchema *deploymentspkg.Schema) map[string]string {
	values := map[string]string{}
	if schemaFields == nil {
		return values
	}
	componentMappings := depSchema.ConnectorConfig[component]
	for _, field := range schemaFields.Fields {
		if raw, ok := componentMappings[field.Name]; ok {
			if resolved := resolveMappedValue(raw, dep.Config, dep.CredRefs); resolved != "" {
				values[field.Name] = resolved
				continue
			}
		}
		if cred, ok := dep.CredRefs[field.Name]; ok {
			values[field.Name] = "@scoped:" + cred.CredstoreID
			continue
		}
		if val, ok := dep.Config[field.Name]; ok {
			values[field.Name] = stringifyConfigValue(val)
			continue
		}
		if field.Default != "" {
			values[field.Name] = field.Default
		}
	}
	return values
}

func resolveMappedValue(raw interface{}, config map[string]interface{}, credrefs map[string]deploymentspkg.CredRef) string {
	switch typed := raw.(type) {
	case string:
		if strings.HasPrefix(typed, "${deployment.") && strings.HasSuffix(typed, "}") {
			key := strings.TrimSuffix(strings.TrimPrefix(typed, "${deployment."), "}")
			return stringifyConfigValue(config[key])
		}
		if strings.HasPrefix(typed, "${credential.") && strings.HasSuffix(typed, "}") {
			key := strings.TrimSuffix(strings.TrimPrefix(typed, "${credential."), "}")
			if cred, ok := credrefs[key]; ok {
				return "@scoped:" + cred.CredstoreID
			}
			return ""
		}
		return typed
	case map[string]interface{}:
		if items, ok := typed["derived_from"].([]interface{}); ok {
			parts := make([]string, 0, len(items))
			for _, item := range items {
				key := fmt.Sprintf("%v", item)
				if val, ok := config[key]; ok {
					parts = append(parts, stringifyConfigValue(val))
				}
			}
			return strings.Join(parts, ",")
		}
	}
	return ""
}

func stringifyConfigValue(v interface{}) string {
	switch typed := v.(type) {
	case nil:
		return ""
	case string:
		return typed
	case int:
		return strconv.Itoa(typed)
	case bool:
		if typed {
			return "true"
		}
		return "false"
	default:
		data, _ := yaml.Marshal(typed)
		return strings.TrimSpace(string(data))
	}
}
