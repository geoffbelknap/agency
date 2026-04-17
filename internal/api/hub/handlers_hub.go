package hub

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"gopkg.in/yaml.v3"

	"github.com/geoffbelknap/agency/internal/credstore"
	"github.com/geoffbelknap/agency/internal/hostadapter"
	"github.com/geoffbelknap/agency/internal/hostadapter/runtimehost"
	hubpkg "github.com/geoffbelknap/agency/internal/hub"
	deploymentspkg "github.com/geoffbelknap/agency/internal/hub/deployments"
	"github.com/geoffbelknap/agency/internal/models"
	"github.com/geoffbelknap/agency/internal/orchestrate"
)

// -- Hub --

func (h *handler) hubUpdate(w http.ResponseWriter, r *http.Request) {
	mgr := hubpkg.NewManager(h.deps.Config.Home)
	report, err := mgr.Update()
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, 200, report)
}

func (h *handler) hubOutdated(w http.ResponseWriter, r *http.Request) {
	mgr := hubpkg.NewManager(h.deps.Config.Home)
	upgrades := mgr.Outdated()
	if upgrades == nil {
		upgrades = []hubpkg.AvailableUpgrade{}
	}
	writeJSON(w, 200, upgrades)
}

func (h *handler) hubUpgrade(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Components []string `json:"components"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil && err.Error() != "EOF" {
		writeJSON(w, 400, map[string]string{"error": "invalid JSON"})
		return
	}
	// Normalize empty slice to nil so Upgrade() treats it as "upgrade all"
	if len(body.Components) == 0 {
		body.Components = nil
	}

	mgr := hubpkg.NewManager(h.deps.Config.Home)
	report, err := mgr.Upgrade(body.Components)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, 200, report)
}

func (h *handler) hubSearch(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query().Get("q")
	kind := r.URL.Query().Get("kind")
	mgr := hubpkg.NewManager(h.deps.Config.Home)
	results := mgr.Search(query, kind)
	if results == nil {
		results = []hubpkg.Component{}
	}
	writeJSON(w, 200, results)
}

func (h *handler) hubInstall(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Component string `json:"component"`
		Kind      string `json:"kind"`
		As        string `json:"as"`
		// Legacy fields — kept for backward compatibility
		Name   string `json:"name"`
		Source string `json:"source"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, 400, map[string]string{"error": "invalid JSON"})
		return
	}
	// Support legacy {name, kind} format
	if body.Component == "" {
		body.Component = body.Name
	}
	if body.Component == "" {
		writeJSON(w, 400, map[string]string{"error": "component name required"})
		return
	}
	mgr := hubpkg.NewManager(h.deps.Config.Home)
	if body.Kind == "pack" && h.isDeploymentEnabledPack(body.Component, body.Source) {
		writeJSON(w, 400, map[string]string{
			"error": fmt.Sprintf("pack %q is deployment-enabled; use 'agency hub deployment create %s' instead", body.Component, body.Component),
		})
		return
	}

	// Resolve dependencies: if the component has requires.services or
	// requires.connectors, install those first.
	comp := mgr.FindInCache(body.Component, body.Kind, body.Source)
	if comp != nil {
		parentName := body.Component
		if body.As != "" {
			parentName = body.As
		}
		h.installDependencies(mgr, parentName, comp)
	}

	inst, err := mgr.Install(body.Component, body.Kind, body.Source, body.As)
	if err != nil {
		writeJSON(w, 400, map[string]string{"error": err.Error()})
		return
	}

	// Auto-activate: provision egress domains, JWT swap, and publish resolved YAML.
	// This merges the old install + activate two-step into a single operation.
	h.autoActivate(mgr, inst)
	h.signalInfraComponent("egress")
	if inst.Kind == "connector" {
		h.signalInfraComponent("intake")
	}

	writeJSON(w, 200, map[string]interface{}{
		"name":   inst.Name,
		"id":     inst.ID,
		"status": inst.State,
	})
}

func (h *handler) signalInfraComponent(component string) {
	if h.deps.Signal == nil {
		return
	}
	backend := runtimehost.BackendDocker
	if h.deps.Config != nil && strings.TrimSpace(h.deps.Config.Hub.DeploymentBackend) != "" {
		backend = strings.TrimSpace(h.deps.Config.Hub.DeploymentBackend)
	}
	if !runtimehost.IsContainerBackend(backend) {
		if h.deps.Logger != nil {
			h.deps.Logger.Debug("hub: skip infra signal for non-container backend", "component", component, "backend", backend)
		}
		return
	}
	name := "agency-infra-" + component
	if instance := strings.TrimSpace(os.Getenv("AGENCY_INFRA_INSTANCE")); instance != "" {
		name += "-" + instance
	}
	if err := h.deps.Signal.SignalContainer(context.Background(), name, "SIGHUP"); err != nil && h.deps.Logger != nil {
		h.deps.Logger.Debug("hub: infra component SIGHUP failed", "component", component, "container", name, "err", err)
	}
}

func (h *handler) backendRequiresDocker(w http.ResponseWriter, operation string) bool {
	backend := runtimehost.BackendDocker
	if h.deps.Config != nil && strings.TrimSpace(h.deps.Config.Hub.DeploymentBackend) != "" {
		backend = strings.TrimSpace(h.deps.Config.Hub.DeploymentBackend)
	}
	if !runtimehost.IsContainerBackend(backend) {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{
			"error":   fmt.Sprintf("%s is only available for container backends (current: %s)", operation, backend),
			"backend": backend,
		})
		return false
	}
	if h.deps.DC == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{
			"error": fmt.Sprintf("%s is unavailable: %s client is not initialized", operation, runtimehost.NormalizeContainerBackend(backend)),
		})
		return false
	}
	return true
}

func (h *handler) ensureDependencyInstalled(mgr *hubpkg.Manager, parentName string, dep hubpkg.DependencyRef) {
	if existing := mgr.Registry.Resolve(dep.Name); existing != nil {
		_ = mgr.Registry.AddRequiredBy(existing.Name, parentName)
		return
	}

	inst, err := mgr.Install(dep.Name, dep.Kind, "", "")
	if err != nil {
		log.Printf("[hub] auto-install dependency %s (%s): %s", dep.Name, dep.Kind, err)
		return
	}
	_ = mgr.Registry.MarkAutoInstalled(inst.Name, true)
	_ = mgr.Registry.AddRequiredBy(inst.Name, parentName)
	log.Printf("[hub] auto-installed dependency: %s (%s)", dep.Name, dep.Kind)
	if dep.Kind == "connector" {
		h.autoActivate(mgr, inst)
		h.signalInfraComponent("intake")
	}
}

// installDependencies resolves and installs required services and connectors.
func (h *handler) installDependencies(mgr *hubpkg.Manager, parentName string, comp *hubpkg.Component) {
	data, err := os.ReadFile(comp.Path)
	if err != nil {
		return
	}

	for _, dep := range hubpkg.DependencyRefsFromYAML(data) {
		h.ensureDependencyInstalled(mgr, parentName, dep)
	}
}

// autoActivate provisions egress domains, JWT swap, and publishes resolved YAML.
func (h *handler) autoActivate(mgr *hubpkg.Manager, inst *hubpkg.Instance) {
	if !requireNameStr(inst.Name) || !requireNameStr(inst.Kind) {
		h.deps.Logger.Warn("invalid hub instance name or kind", "name", inst.Name, "kind", inst.Kind)
		return
	}
	instDir := mgr.Registry.InstanceDir(inst.Name)
	templatePath := instDir + "/" + inst.Kind + ".yaml"
	templateData, err := os.ReadFile(templatePath)
	if err != nil {
		return
	}

	// Auto-provision egress domains and JWT swap
	if inst.Kind == "connector" {
		var doc map[string]interface{}
		if yaml.Unmarshal(templateData, &doc) == nil {
			if requires, ok := doc["requires"].(map[string]interface{}); ok {
				if domains, ok := requires["egress_domains"].([]interface{}); ok {
					for _, d := range domains {
						if domain, ok := d.(string); ok {
							h.addEgressDomainProvenance(domain, "connector", inst.Name)
						}
					}
				}
				if auth, ok := requires["auth"].(map[string]interface{}); ok {
					if authType, _ := auth["type"].(string); authType == "jwt-exchange" {
						authData, _ := yaml.Marshal(auth)
						var connAuth models.ConnectorAuth
						if yaml.Unmarshal(authData, &connAuth) == nil {
							reqData, _ := yaml.Marshal(requires)
							var connReq models.ConnectorRequires
							if yaml.Unmarshal(reqData, &connReq) == nil {
								h.writeJWTSwap(&connAuth, &connReq) //nolint:errcheck
							}
						}
					}
				}
			}
		}

		// Publish resolved YAML for intake
		if resolved, err := mgr.Registry.ResolvedYAML(inst.Name); err == nil && resolved != nil {
			os.WriteFile(instDir+"/resolved.yaml", resolved, 0644)
			connectorsDir := h.deps.Config.Home + "/connectors"
			os.MkdirAll(connectorsDir, 0755)
			os.WriteFile(connectorsDir+"/"+inst.Name+".yaml", resolved, 0644)
		}
	}

	// Set state to active
	mgr.Registry.SetState(inst.Name, "active")
	inst.State = "active"

	// Regenerate swap config
	h.regenerateSwapConfig()
}

func (h *handler) intakePollHealth(w http.ResponseWriter, r *http.Request) {
	resp, err := http.Get("http://127.0.0.1:8205/poll-health")
	if err != nil {
		writeJSON(w, 502, map[string]string{"error": err.Error()})
		return
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(resp.StatusCode)
	w.Write(data)
}

func (h *handler) intakePollTrigger(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "connector")
	resp, err := http.Post("http://127.0.0.1:8205/poll/"+name, "application/json", nil)
	if err != nil {
		writeJSON(w, 502, map[string]string{"error": err.Error()})
		return
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(resp.StatusCode)
	w.Write(data)
}

func (h *handler) hubCheck(w http.ResponseWriter, r *http.Request) {
	nameOrID := chi.URLParam(r, "nameOrID")
	mgr := hubpkg.NewManager(h.deps.Config.Home)
	inst := mgr.Registry.Resolve(nameOrID)
	if inst == nil {
		writeJSON(w, 404, map[string]string{"error": fmt.Sprintf("component %q not found", nameOrID)})
		return
	}
	checker := &orchestrate.HubHealthChecker{Home: h.deps.Config.Home, CredStore: h.deps.CredStore}
	resp := checker.Check(inst)
	writeJSON(w, 200, resp)
}

func (h *handler) hubDoctor(w http.ResponseWriter, r *http.Request) {
	mgr := hubpkg.NewManager(h.deps.Config.Home)
	checker := &orchestrate.HubHealthChecker{Home: h.deps.Config.Home, CredStore: h.deps.CredStore}
	results := checker.CheckAll(mgr.Registry)
	writeJSON(w, 200, map[string]interface{}{"components": results})
}

func (h *handler) hubRemove(w http.ResponseWriter, r *http.Request) {
	nameOrID := chi.URLParam(r, "nameOrID")
	mgr := hubpkg.NewManager(h.deps.Config.Home)
	inst := mgr.Registry.Resolve(nameOrID)
	if inst == nil {
		writeJSON(w, 404, map[string]string{"error": fmt.Sprintf("component %q not found", nameOrID)})
		return
	}
	if inst.DeploymentManaged {
		writeJSON(w, 409, map[string]string{
			"error": fmt.Sprintf("instance %q is managed by deployment %s; use 'agency hub deployment destroy %s' instead", inst.Name, inst.DeploymentID, inst.DeploymentID),
		})
		return
	}
	if inst.Kind == "provider" {
		if err := hubpkg.RemoveProviderRouting(h.deps.Config.Home, inst.Name); err != nil {
			writeJSON(w, 500, map[string]string{"error": fmt.Sprintf("remove provider routing: %v", err)})
			return
		}
	}
	if inst.Kind == "connector" {
		_ = os.Remove(filepath.Join(h.deps.Config.Home, "connectors", inst.Name+".yaml"))
	}
	removed, err := mgr.RemoveWithDependencies(nameOrID)
	if err != nil {
		writeJSON(w, 404, map[string]string{"error": err.Error()})
		return
	}
	needsIntakeSignal := false
	for _, removedInst := range removed {
		if removedInst.Kind == "connector" {
			_ = os.Remove(filepath.Join(h.deps.Config.Home, "connectors", removedInst.Name+".yaml"))
			needsIntakeSignal = true
		}
	}
	if needsIntakeSignal {
		h.signalInfraComponent("intake")
	}
	writeJSON(w, 200, map[string]string{"status": "removed", "name": inst.Name})
}

func (h *handler) hubInstalled(w http.ResponseWriter, r *http.Request) {
	mgr := hubpkg.NewManager(h.deps.Config.Home)
	installed := mgr.List()
	// Normalize: ensure "name" is always populated for the API response
	type normalizedProvenance struct {
		Name        string `json:"name"`
		Kind        string `json:"kind"`
		Source      string `json:"source"`
		InstalledAt string `json:"installed_at"`
	}
	result := make([]normalizedProvenance, 0, len(installed))
	for _, p := range installed {
		result = append(result, normalizedProvenance{
			Name:        p.DisplayName(),
			Kind:        p.Kind,
			Source:      p.Source,
			InstalledAt: p.InstalledAt,
		})
	}
	writeJSON(w, 200, result)
}

func (h *handler) hubInfo(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	kind := r.URL.Query().Get("kind")
	mgr := hubpkg.NewManager(h.deps.Config.Home)
	info, err := mgr.Info(name, kind)
	if err != nil {
		writeJSON(w, 404, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, 200, info)
}

func (h *handler) hubInstances(w http.ResponseWriter, r *http.Request) {
	kind := r.URL.Query().Get("kind")
	mgr := hubpkg.NewManager(h.deps.Config.Home)
	instances := mgr.Registry.List(kind)
	if instances == nil {
		instances = []hubpkg.Instance{}
	}
	writeJSON(w, 200, instances)
}

func (h *handler) hubShow(w http.ResponseWriter, r *http.Request) {
	nameOrID := chi.URLParam(r, "nameOrID")
	mgr := hubpkg.NewManager(h.deps.Config.Home)
	inst := mgr.Registry.Resolve(nameOrID)
	if inst == nil {
		writeJSON(w, 404, map[string]string{"error": fmt.Sprintf("instance %q not found", nameOrID)})
		return
	}

	instDir := mgr.Registry.InstanceDir(nameOrID)
	resp := map[string]interface{}{
		"id":      inst.ID,
		"name":    inst.Name,
		"kind":    inst.Kind,
		"source":  inst.Source,
		"state":   inst.State,
		"created": inst.Created,
	}
	cv, _ := hubpkg.ReadConfig(instDir)
	if cv != nil {
		masked := make(map[string]string)
		for k, v := range cv.Values {
			if strings.HasPrefix(v, "@scoped:") {
				masked[k] = "****"
			} else {
				masked[k] = v
			}
		}
		resp["config"] = masked
		resp["configured_at"] = cv.ConfiguredAt
	}
	writeJSON(w, 200, resp)
}

func (h *handler) hubActivate(w http.ResponseWriter, r *http.Request) {
	nameOrID := chi.URLParam(r, "nameOrID")
	mgr := hubpkg.NewManager(h.deps.Config.Home)
	inst := mgr.Registry.Resolve(nameOrID)
	if inst == nil {
		writeJSON(w, 404, map[string]string{"error": "instance not found"})
		return
	}
	if inst.DeploymentManaged {
		writeJSON(w, 409, map[string]string{
			"error": fmt.Sprintf("instance %q is managed by deployment %s; use 'agency hub deployment apply %s' instead", inst.Name, inst.DeploymentID, inst.DeploymentID),
		})
		return
	}
	if inst.Kind == "pack" && h.isDeploymentEnabledInstance(inst) {
		writeJSON(w, 400, map[string]string{
			"error": fmt.Sprintf("pack %q is deployment-enabled; use 'agency hub deployment create %s' instead", inst.Name, componentNameFromSource(inst.Source)),
		})
		return
	}

	// Parse config from request body
	var body struct {
		Config map[string]string `json:"config"`
	}
	json.NewDecoder(r.Body).Decode(&body)
	if body.Config == nil {
		body.Config = map[string]string{}
	}

	// Read component template and parse config schema
	instDir := mgr.Registry.InstanceDir(nameOrID)
	templatePath := filepath.Join(instDir, inst.Kind+".yaml")
	templateData, err := os.ReadFile(templatePath)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": "cannot read component template"})
		return
	}

	// Auto-provision egress domains and JWT swap from connector requires block
	if inst.Kind == "connector" {
		var doc map[string]interface{}
		if yaml.Unmarshal(templateData, &doc) == nil {
			if requires, ok := doc["requires"].(map[string]interface{}); ok {
				if domains, ok := requires["egress_domains"].([]interface{}); ok {
					for _, d := range domains {
						if domain, ok := d.(string); ok {
							h.addEgressDomainProvenance(domain, "connector", inst.Name)
						}
					}
				}
				// Auto-provision JWT swap config if auth type is jwt-exchange
				if auth, ok := requires["auth"].(map[string]interface{}); ok {
					if authType, _ := auth["type"].(string); authType == "jwt-exchange" {
						// Parse auth into the model and call writeJWTSwap
						authData, _ := yaml.Marshal(auth)
						var connAuth models.ConnectorAuth
						if yaml.Unmarshal(authData, &connAuth) == nil {
							reqData, _ := yaml.Marshal(requires)
							var connReq models.ConnectorRequires
							if yaml.Unmarshal(reqData, &connReq) == nil {
								h.writeJWTSwap(&connAuth, &connReq) //nolint:errcheck
							}
						}
					}
				}
			}
		}
	}

	schema, err := hubpkg.ParseConfigSchema(templateData)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": "cannot parse config schema: " + err.Error()})
		return
	}

	// If no config schema, just activate (no config needed)
	if schema == nil || len(schema.Fields) == 0 {
		// Publish resolved YAML for intake
		if inst.Kind == "connector" {
			if resolved, err := mgr.Registry.ResolvedYAML(nameOrID); err == nil && resolved != nil {
				os.WriteFile(filepath.Join(instDir, "resolved.yaml"), resolved, 0644)
				connectorsDir := filepath.Join(h.deps.Config.Home, "connectors")
				os.MkdirAll(connectorsDir, 0755)
				os.WriteFile(filepath.Join(connectorsDir, inst.Name+".yaml"), resolved, 0644)
			}
		}
		mgr.Registry.SetState(nameOrID, "active")
		writeJSON(w, 200, map[string]interface{}{"status": "active", "name": inst.Name, "id": inst.ID})
		return
	}

	// Apply defaults
	for _, f := range schema.Fields {
		if _, ok := body.Config[f.Name]; !ok && f.Default != "" {
			body.Config[f.Name] = f.Default
		}
	}

	// Validate — return missing fields if incomplete
	missing := schema.Validate(body.Config)
	if len(missing) > 0 {
		writeJSON(w, 200, map[string]interface{}{
			"status":  "config_required",
			"missing": missing,
		})
		return
	}

	// Split secrets from config
	configValues, secrets := schema.SplitSecrets(body.Config, inst.Name)

	// Write config.yaml
	cv := &hubpkg.ConfigValues{
		Instance:        inst.Name,
		ID:              inst.ID,
		SourceComponent: inst.Source,
		ConfiguredAt:    time.Now().UTC().Format(time.RFC3339),
		Values:          configValues,
	}
	if err := hubpkg.WriteConfig(instDir, cv); err != nil {
		writeJSON(w, 500, map[string]string{"error": "failed to write config: " + err.Error()})
		return
	}

	// Write resolved.yaml for intake
	resolved, _ := mgr.Registry.ResolvedYAML(nameOrID)
	if resolved != nil {
		os.WriteFile(filepath.Join(instDir, "resolved.yaml"), resolved, 0644)
		// Publish to ~/.agency/connectors/ where the intake container reads from
		if inst.Kind == "connector" {
			connectorsDir := filepath.Join(h.deps.Config.Home, "connectors")
			os.MkdirAll(connectorsDir, 0755)
			os.WriteFile(filepath.Join(connectorsDir, inst.Name+".yaml"), resolved, 0644)
		}
	}

	// Write secrets to credential store
	if len(secrets) > 0 {
		hubpkg.WriteSecrets(h.deps.Config.Home, inst.Name, secrets, h.hubSecretPutter(inst.Name))
	}

	// Regenerate credential-swaps.yaml
	h.regenerateSwapConfig()

	// Set state to active
	mgr.Registry.SetState(nameOrID, "active")

	// Audit
	configKeys := make([]string, 0, len(configValues))
	for k := range configValues {
		configKeys = append(configKeys, k)
	}
	h.deps.Audit.Write(inst.Name, "hub_activate", map[string]interface{}{
		"instance_id":     inst.ID,
		"config_keys":     configKeys,
		"secrets_updated": len(secrets) > 0,
	})

	writeJSON(w, 200, map[string]interface{}{
		"status": "active",
		"name":   inst.Name,
		"id":     inst.ID,
	})
}

func (h *handler) hubConfigure(w http.ResponseWriter, r *http.Request) {
	nameOrID := chi.URLParam(r, "nameOrID")
	mgr := hubpkg.NewManager(h.deps.Config.Home)
	inst := mgr.Registry.Resolve(nameOrID)
	if inst == nil {
		writeJSON(w, 404, map[string]string{"error": "instance not found"})
		return
	}
	if inst.DeploymentManaged {
		writeJSON(w, 409, map[string]string{
			"error": fmt.Sprintf("instance %q is managed by deployment %s; use 'agency hub deployment configure %s' instead", inst.Name, inst.DeploymentID, inst.DeploymentID),
		})
		return
	}

	// Parse config from request body
	var body struct {
		Config map[string]string `json:"config"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, 400, map[string]string{"error": "invalid JSON"})
		return
	}
	if body.Config == nil {
		body.Config = map[string]string{}
	}

	// Read component template and parse config schema
	instDir := mgr.Registry.InstanceDir(nameOrID)
	templatePath := filepath.Join(instDir, inst.Kind+".yaml")
	templateData, err := os.ReadFile(templatePath)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": "cannot read component template"})
		return
	}
	schema, err := hubpkg.ParseConfigSchema(templateData)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": "cannot parse config schema: " + err.Error()})
		return
	}

	// Apply defaults
	if schema != nil {
		for _, f := range schema.Fields {
			if _, ok := body.Config[f.Name]; !ok && f.Default != "" {
				body.Config[f.Name] = f.Default
			}
		}
	}

	// Validate — return missing fields if incomplete
	if schema != nil && len(schema.Fields) > 0 {
		missing := schema.Validate(body.Config)
		if len(missing) > 0 {
			writeJSON(w, 200, map[string]interface{}{
				"status":  "config_required",
				"missing": missing,
			})
			return
		}
	}

	// Split secrets from config
	var configValues map[string]string
	var secrets map[string]string
	if schema != nil && len(schema.Fields) > 0 {
		configValues, secrets = schema.SplitSecrets(body.Config, inst.Name)
	} else {
		configValues = body.Config
		secrets = map[string]string{}
	}

	// Write config.yaml
	cv := &hubpkg.ConfigValues{
		Instance:        inst.Name,
		ID:              inst.ID,
		SourceComponent: inst.Source,
		ConfiguredAt:    time.Now().UTC().Format(time.RFC3339),
		Values:          configValues,
	}
	if err := hubpkg.WriteConfig(instDir, cv); err != nil {
		writeJSON(w, 500, map[string]string{"error": "failed to write config: " + err.Error()})
		return
	}

	// Write resolved.yaml for intake
	if resolved, err := mgr.Registry.ResolvedYAML(nameOrID); err == nil && resolved != nil {
		os.WriteFile(filepath.Join(instDir, "resolved.yaml"), resolved, 0644)
		if inst.Kind == "connector" {
			connectorsDir := filepath.Join(h.deps.Config.Home, "connectors")
			os.MkdirAll(connectorsDir, 0755)
			os.WriteFile(filepath.Join(connectorsDir, inst.Name+".yaml"), resolved, 0644)
		}
	}

	// Write secrets to credential store
	if len(secrets) > 0 {
		hubpkg.WriteSecrets(h.deps.Config.Home, inst.Name, secrets, h.hubSecretPutter(inst.Name))
	}

	// If instance is active, SIGHUP intake to pick up new config
	if inst.State == "active" {
		h.signalInfraComponent("intake")
	}

	// Audit
	configKeys := make([]string, 0, len(configValues))
	for k := range configValues {
		configKeys = append(configKeys, k)
	}
	h.deps.Audit.Write(inst.Name, "hub_configure", map[string]interface{}{
		"instance_id":     inst.ID,
		"config_keys":     configKeys,
		"secrets_updated": len(secrets) > 0,
	})

	writeJSON(w, 200, map[string]interface{}{
		"status": "configured",
		"name":   inst.Name,
		"id":     inst.ID,
	})
}

func (h *handler) hubDeactivate(w http.ResponseWriter, r *http.Request) {
	nameOrID := chi.URLParam(r, "nameOrID")
	mgr := hubpkg.NewManager(h.deps.Config.Home)
	inst := mgr.Registry.Resolve(nameOrID)
	if inst != nil && inst.DeploymentManaged {
		writeJSON(w, 409, map[string]string{
			"error": fmt.Sprintf("instance %q is managed by deployment %s; use 'agency hub deployment destroy %s --keep-instances' or 'agency hub deployment apply %s' instead", inst.Name, inst.DeploymentID, inst.DeploymentID, inst.DeploymentID),
		})
		return
	}
	if err := mgr.Registry.SetState(nameOrID, "inactive"); err != nil {
		writeJSON(w, 404, map[string]string{"error": err.Error()})
		return
	}
	h.regenerateSwapConfig()
	// Remove published connector YAML so intake stops polling
	if inst != nil && inst.Kind == "connector" {
		os.Remove(filepath.Join(h.deps.Config.Home, "connectors", inst.Name+".yaml"))
		h.signalInfraComponent("intake")
	}
	name := nameOrID
	if inst != nil {
		name = inst.Name
	}
	writeJSON(w, 200, map[string]string{"status": "inactive", "name": name})
}

func (h *handler) isDeploymentEnabledPack(component, source string) bool {
	mgr := hubpkg.NewManager(h.deps.Config.Home)
	comp := mgr.FindInCache(component, "pack", source)
	if comp == nil {
		return false
	}
	_, err := deploymentspkg.LoadSchema(filepath.Join(filepath.Dir(comp.Path), "deployment_schema.yaml"))
	return err == nil
}

func (h *handler) isDeploymentEnabledInstance(inst *hubpkg.Instance) bool {
	if inst == nil || inst.Kind != "pack" {
		return false
	}
	return h.isDeploymentEnabledPack(componentNameFromSource(inst.Source), "")
}

// hubSecretPutter returns a hub.SecretPutter that writes service credentials
// to the encrypted credential store with hub-specific metadata.
func (h *handler) hubSecretPutter(instanceName string) hubpkg.SecretPutter {
	return func(name, value string) error {
		if h.deps.CredStore == nil {
			return fmt.Errorf("credential store not initialized")
		}
		now := time.Now().UTC().Format(time.RFC3339)
		return h.deps.CredStore.Put(credstore.Entry{
			Name:  name,
			Value: value,
			Metadata: credstore.Metadata{
				Kind:      credstore.KindService,
				Scope:     "platform",
				Service:   instanceName,
				Protocol:  credstore.ProtocolAPIKey,
				Source:    "hub",
				CreatedAt: now,
				RotatedAt: now,
			},
		})
	}
}

// hubSecretDeleter returns a hub.SecretDeleter that removes credentials
// from the encrypted credential store.
func (h *handler) hubSecretDeleter() hubpkg.SecretDeleter {
	return func(name string) error {
		if h.deps.CredStore == nil {
			return fmt.Errorf("credential store not initialized")
		}
		return h.deps.CredStore.Delete(name)
	}
}

// regenerateSwapConfig rebuilds credential-swaps.yaml from the credential
// store. If the store is nil or empty, it falls back to the legacy hub-based
// generation so existing file-based setups keep working.
func (h *handler) regenerateSwapConfig() {
	if h.deps.CredStore == nil {
		hubpkg.WriteSwapConfig(h.deps.Config.Home)
		return
	}
	data, err := h.deps.CredStore.GenerateSwapConfig()
	if err != nil {
		h.deps.Logger.Warn("failed to generate swap config from store", "err", err)
		hubpkg.WriteSwapConfig(h.deps.Config.Home)
		return
	}

	// If the store produced an empty swap map, fall back to legacy so that
	// service-definition / routing-based entries are still generated.
	if len(data) == 0 {
		hubpkg.WriteSwapConfig(h.deps.Config.Home)
		return
	}

	// Merge: generate legacy config, then overlay store entries on top.
	// This ensures service-definition swaps survive while the store is
	// being gradually populated.
	legacyData, legacyErr := hubpkg.GenerateSwapConfig(h.deps.Config.Home)

	swapPath := filepath.Join(h.deps.Config.Home, "infrastructure", "credential-swaps.yaml")
	os.MkdirAll(filepath.Dir(swapPath), 0755)

	if legacyErr == nil && len(legacyData) > 0 {
		// Parse both, merge store entries on top of legacy
		var legacy hubpkg.SwapConfigFile
		var store hubpkg.SwapConfigFile
		if yaml.Unmarshal(legacyData, &legacy) == nil && yaml.Unmarshal(data, &store) == nil {
			if legacy.Swaps == nil {
				legacy.Swaps = map[string]hubpkg.SwapEntry{}
			}
			for k, v := range store.Swaps {
				legacy.Swaps[k] = v
			}
			if merged, err := yaml.Marshal(legacy); err == nil {
				os.WriteFile(swapPath, merged, 0644)
				return
			}
		}
	}

	// If merge failed, write store-only config
	os.WriteFile(swapPath, data, 0644)
}

// deployPack handles POST /api/v1/hub/deploy
func (h *handler) deployPack(w http.ResponseWriter, r *http.Request) {
	if !h.backendRequiresDocker(w, "hub deploy") {
		return
	}
	var body struct {
		PackPath    string               `json:"pack_path"`
		PackName    string               `json:"pack_name"`
		Pack        *orchestrate.PackDef `json:"pack"`
		DryRun      bool                 `json:"dry_run"`
		Credentials map[string]string    `json:"credentials"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, 400, map[string]string{"error": "invalid JSON"})
		return
	}

	var pack *orchestrate.PackDef
	var err error
	if body.PackPath != "" {
		pack, err = orchestrate.LoadPack(body.PackPath)
		if err != nil {
			writeJSON(w, 400, map[string]string{"error": err.Error()})
			return
		}
	} else if body.PackName != "" {
		mgr := hubpkg.NewManager(h.deps.Config.Home)
		inst := mgr.Registry.Resolve(body.PackName)
		if inst == nil || inst.Kind != "pack" {
			writeJSON(w, 404, map[string]string{"error": fmt.Sprintf("pack %q not found", body.PackName)})
			return
		}
		packPath := filepath.Join(mgr.Registry.InstanceDir(inst.Name), "pack.yaml")
		pack, err = orchestrate.LoadPack(packPath)
		if err != nil {
			writeJSON(w, 400, map[string]string{"error": err.Error()})
			return
		}
		body.PackPath = packPath
	} else if body.Pack != nil {
		pack = body.Pack
	} else {
		writeJSON(w, 400, map[string]string{"error": "pack_path, pack_name, or pack required"})
		return
	}

	// Validate required credentials are present.
	if len(pack.Credentials) > 0 {
		var missing []string
		for _, cred := range pack.Credentials {
			if cred.Required {
				if _, ok := body.Credentials[cred.Name]; !ok {
					missing = append(missing, cred.Name)
				}
			}
		}
		if len(missing) > 0 {
			writeJSON(w, 400, map[string]interface{}{
				"status":  "credentials_required",
				"missing": missing,
			})
			return
		}
	}

	if body.DryRun {
		result, err := h.deps.Host.DryRunDeployPack(r.Context(), hostadapter.DeployOptions{
			Home:        h.deps.Config.Home,
			Version:     h.deps.Config.Version,
			SourceDir:   h.deps.Config.SourceDir,
			BuildID:     h.deps.Config.BuildID,
			Credentials: body.Credentials,
			CredStore:   h.deps.CredStore,
		}, pack, func(s string) {
			h.deps.Logger.Info("deploy dry-run", "status", s)
		})
		if err != nil {
			writeJSON(w, 400, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, 200, result)
		return
	}

	result, err := h.deps.Host.DeployPack(r.Context(), hostadapter.DeployOptions{
		Home:        h.deps.Config.Home,
		Version:     h.deps.Config.Version,
		SourceDir:   h.deps.Config.SourceDir,
		BuildID:     h.deps.Config.BuildID,
		Credentials: body.Credentials,
		CredStore:   h.deps.CredStore,
	}, pack, func(s string) {
		h.deps.Logger.Info("deploy", "status", s)
	})
	if err != nil {
		h.deps.Audit.WriteSystem("deploy_failed", map[string]interface{}{"pack": body.PackPath, "error": err.Error()})
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	h.deps.Audit.WriteSystem("pack_deployed", map[string]interface{}{"pack": body.PackPath})
	writeJSON(w, 200, result)
}

// teardownPack handles POST /api/v1/hub/teardown/{pack}
func (h *handler) teardownPack(w http.ResponseWriter, r *http.Request) {
	if !h.backendRequiresDocker(w, "hub teardown") {
		return
	}
	packName := chi.URLParam(r, "pack")
	var body struct {
		Delete bool `json:"delete"`
	}
	json.NewDecoder(r.Body).Decode(&body)

	if err := h.deps.Host.TeardownPack(r.Context(), hostadapter.DeployOptions{
		Home:      h.deps.Config.Home,
		Version:   h.deps.Config.Version,
		CredStore: h.deps.CredStore,
	}, packName, body.Delete); err != nil {
		writeJSON(w, 400, map[string]string{"error": err.Error()})
		return
	}
	h.deps.Audit.WriteSystem("pack_teardown", map[string]interface{}{"pack": packName, "delete": body.Delete})
	writeJSON(w, 200, map[string]string{"status": "torn down", "pack": packName})
}
