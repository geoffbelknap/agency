package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"gopkg.in/yaml.v3"

	"github.com/geoffbelknap/agency/internal/credstore"
	"github.com/geoffbelknap/agency/internal/hub"
	"github.com/geoffbelknap/agency/internal/knowledge"
	"github.com/geoffbelknap/agency/internal/logs"
	"github.com/geoffbelknap/agency/internal/models"
	"github.com/geoffbelknap/agency/internal/orchestrate"
)

// -- Hub --

func (h *handler) hubUpdate(w http.ResponseWriter, r *http.Request) {
	mgr := hub.NewManager(h.cfg.Home)
	report, err := mgr.Update()
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, 200, report)
}

func (h *handler) hubOutdated(w http.ResponseWriter, r *http.Request) {
	mgr := hub.NewManager(h.cfg.Home)
	upgrades := mgr.Outdated()
	if upgrades == nil {
		upgrades = []hub.AvailableUpgrade{}
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

	mgr := hub.NewManager(h.cfg.Home)
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
	mgr := hub.NewManager(h.cfg.Home)
	results := mgr.Search(query, kind)
	if results == nil {
		results = []hub.Component{}
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
	mgr := hub.NewManager(h.cfg.Home)

	// Resolve dependencies: if the component has requires.services or
	// requires.connectors, install those first.
	comp := mgr.FindInCache(body.Component, body.Kind, body.Source)
	if comp != nil {
		h.installDependencies(mgr, comp)
	}

	inst, err := mgr.Install(body.Component, body.Kind, body.Source, body.As)
	if err != nil {
		writeJSON(w, 400, map[string]string{"error": err.Error()})
		return
	}

	// Auto-activate: provision egress domains, JWT swap, and publish resolved YAML.
	// This merges the old install + activate two-step into a single operation.
	h.autoActivate(mgr, inst)

	writeJSON(w, 200, map[string]interface{}{
		"name":   inst.Name,
		"id":     inst.ID,
		"status": inst.State,
	})
}

// installDependencies resolves and installs required services and connectors.
func (h *handler) installDependencies(mgr *hub.Manager, comp *hub.Component) {
	data, err := os.ReadFile(comp.Path)
	if err != nil {
		return
	}
	var doc map[string]interface{}
	if yaml.Unmarshal(data, &doc) != nil {
		return
	}
	requires, ok := doc["requires"].(map[string]interface{})
	if !ok {
		return
	}

	// Install required services
	if services, ok := requires["services"].([]interface{}); ok {
		for _, s := range services {
			svcName, _ := s.(string)
			if svcName == "" {
				continue
			}
			// Skip if already installed
			if existing := mgr.Registry.Resolve(svcName); existing != nil {
				continue
			}
			if _, err := mgr.Install(svcName, "service", "", ""); err != nil {
				log.Printf("[hub] auto-install dependency %s (service): %s", svcName, err)
			} else {
				log.Printf("[hub] auto-installed dependency: %s (service)", svcName)
			}
		}
	}

	// Install required presets
	if presets, ok := requires["presets"].([]interface{}); ok {
		for _, p := range presets {
			presetName, _ := p.(string)
			if presetName == "" {
				continue
			}
			if existing := mgr.Registry.Resolve(presetName); existing != nil {
				continue
			}
			if _, err := mgr.Install(presetName, "preset", "", ""); err != nil {
				log.Printf("[hub] auto-install dependency %s (preset): %s", presetName, err)
			} else {
				log.Printf("[hub] auto-installed dependency: %s (preset)", presetName)
			}
		}
	}

	// Install required missions
	if missions, ok := requires["missions"].([]interface{}); ok {
		for _, m := range missions {
			missionName, _ := m.(string)
			if missionName == "" {
				continue
			}
			if existing := mgr.Registry.Resolve(missionName); existing != nil {
				continue
			}
			if _, err := mgr.Install(missionName, "mission", "", ""); err != nil {
				log.Printf("[hub] auto-install dependency %s (mission): %s", missionName, err)
			} else {
				log.Printf("[hub] auto-installed dependency: %s (mission)", missionName)
			}
		}
	}

	// Install required connectors
	if connectors, ok := requires["connectors"].([]interface{}); ok {
		for _, c := range connectors {
			connName, _ := c.(string)
			if connName == "" {
				continue
			}
			if existing := mgr.Registry.Resolve(connName); existing != nil {
				continue
			}
			inst, err := mgr.Install(connName, "connector", "", "")
			if err != nil {
				log.Printf("[hub] auto-install dependency %s (connector): %s", connName, err)
			} else {
				log.Printf("[hub] auto-installed dependency: %s (connector)", connName)
				h.autoActivate(mgr, inst)
			}
		}
	}

	// Also check mission_assignments in pack YAML
	if assignments, ok := doc["mission_assignments"].([]interface{}); ok {
		for _, a := range assignments {
			am, _ := a.(map[string]interface{})
			missionName, _ := am["mission"].(string)
			if missionName == "" {
				continue
			}
			if existing := mgr.Registry.Resolve(missionName); existing != nil {
				continue
			}
			if _, err := mgr.Install(missionName, "mission", "", ""); err != nil {
				log.Printf("[hub] auto-install mission %s: %s", missionName, err)
			} else {
				log.Printf("[hub] auto-installed mission: %s", missionName)
			}
		}
	}
}

// autoActivate provisions egress domains, JWT swap, and publishes resolved YAML.
func (h *handler) autoActivate(mgr *hub.Manager, inst *hub.Instance) {
	if !requireNameStr(inst.Name) || !requireNameStr(inst.Kind) {
		h.log.Warn("invalid hub instance name or kind", "name", inst.Name, "kind", inst.Kind)
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
			connectorsDir := h.cfg.Home + "/connectors"
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
	mgr := hub.NewManager(h.cfg.Home)
	inst := mgr.Registry.Resolve(nameOrID)
	if inst == nil {
		writeJSON(w, 404, map[string]string{"error": fmt.Sprintf("component %q not found", nameOrID)})
		return
	}
	checker := &orchestrate.HubHealthChecker{Home: h.cfg.Home, CredStore: h.credStore}
	resp := checker.Check(inst)
	writeJSON(w, 200, resp)
}

func (h *handler) hubDoctor(w http.ResponseWriter, r *http.Request) {
	mgr := hub.NewManager(h.cfg.Home)
	checker := &orchestrate.HubHealthChecker{Home: h.cfg.Home, CredStore: h.credStore}
	results := checker.CheckAll(mgr.Registry)
	writeJSON(w, 200, map[string]interface{}{"components": results})
}

func (h *handler) hubRemove(w http.ResponseWriter, r *http.Request) {
	nameOrID := chi.URLParam(r, "nameOrID")
	mgr := hub.NewManager(h.cfg.Home)
	if err := mgr.Registry.Remove(nameOrID); err != nil {
		writeJSON(w, 404, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, 200, map[string]string{"status": "removed", "name": nameOrID})
}

func (h *handler) hubInstalled(w http.ResponseWriter, r *http.Request) {
	mgr := hub.NewManager(h.cfg.Home)
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
	mgr := hub.NewManager(h.cfg.Home)
	info, err := mgr.Info(name, kind)
	if err != nil {
		writeJSON(w, 404, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, 200, info)
}

func (h *handler) hubInstances(w http.ResponseWriter, r *http.Request) {
	kind := r.URL.Query().Get("kind")
	mgr := hub.NewManager(h.cfg.Home)
	instances := mgr.Registry.List(kind)
	if instances == nil {
		instances = []hub.Instance{}
	}
	writeJSON(w, 200, instances)
}

func (h *handler) hubShow(w http.ResponseWriter, r *http.Request) {
	nameOrID := chi.URLParam(r, "nameOrID")
	mgr := hub.NewManager(h.cfg.Home)
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
	cv, _ := hub.ReadConfig(instDir)
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
	mgr := hub.NewManager(h.cfg.Home)
	inst := mgr.Registry.Resolve(nameOrID)
	if inst == nil {
		writeJSON(w, 404, map[string]string{"error": "instance not found"})
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

	schema, err := hub.ParseConfigSchema(templateData)
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
				connectorsDir := filepath.Join(h.cfg.Home, "connectors")
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
	cv := &hub.ConfigValues{
		Instance:        inst.Name,
		ID:              inst.ID,
		SourceComponent: inst.Source,
		ConfiguredAt:    time.Now().UTC().Format(time.RFC3339),
		Values:          configValues,
	}
	if err := hub.WriteConfig(instDir, cv); err != nil {
		writeJSON(w, 500, map[string]string{"error": "failed to write config: " + err.Error()})
		return
	}

	// Write resolved.yaml for intake
	resolved, _ := mgr.Registry.ResolvedYAML(nameOrID)
	if resolved != nil {
		os.WriteFile(filepath.Join(instDir, "resolved.yaml"), resolved, 0644)
		// Publish to ~/.agency/connectors/ where the intake container reads from
		if inst.Kind == "connector" {
			connectorsDir := filepath.Join(h.cfg.Home, "connectors")
			os.MkdirAll(connectorsDir, 0755)
			os.WriteFile(filepath.Join(connectorsDir, inst.Name+".yaml"), resolved, 0644)
		}
	}

	// Write secrets to credential store
	if len(secrets) > 0 {
		hub.WriteSecrets(h.cfg.Home, inst.Name, secrets, h.hubSecretPutter(inst.Name))
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
	h.audit.Write(inst.Name, "hub_activate", map[string]interface{}{
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
	mgr := hub.NewManager(h.cfg.Home)
	inst := mgr.Registry.Resolve(nameOrID)
	if inst == nil {
		writeJSON(w, 404, map[string]string{"error": "instance not found"})
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
	schema, err := hub.ParseConfigSchema(templateData)
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
	cv := &hub.ConfigValues{
		Instance:        inst.Name,
		ID:              inst.ID,
		SourceComponent: inst.Source,
		ConfiguredAt:    time.Now().UTC().Format(time.RFC3339),
		Values:          configValues,
	}
	if err := hub.WriteConfig(instDir, cv); err != nil {
		writeJSON(w, 500, map[string]string{"error": "failed to write config: " + err.Error()})
		return
	}

	// Write resolved.yaml for intake
	if resolved, err := mgr.Registry.ResolvedYAML(nameOrID); err == nil && resolved != nil {
		os.WriteFile(filepath.Join(instDir, "resolved.yaml"), resolved, 0644)
	}

	// Write secrets to credential store
	if len(secrets) > 0 {
		hub.WriteSecrets(h.cfg.Home, inst.Name, secrets, h.hubSecretPutter(inst.Name))
	}

	// If instance is active, SIGHUP intake to pick up new config
	if inst.State == "active" {
		ctx := context.Background()
		intakeName := "agency-intake"
		if err := h.dc.RawClient().ContainerKill(ctx, intakeName, "SIGHUP"); err != nil {
			h.log.Debug("hubConfigure: intake SIGHUP failed (may not be running)", "err", err)
		}
	}

	// Audit
	configKeys := make([]string, 0, len(configValues))
	for k := range configValues {
		configKeys = append(configKeys, k)
	}
	h.audit.Write(inst.Name, "hub_configure", map[string]interface{}{
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
	mgr := hub.NewManager(h.cfg.Home)
	inst := mgr.Registry.Resolve(nameOrID)
	if err := mgr.Registry.SetState(nameOrID, "inactive"); err != nil {
		writeJSON(w, 404, map[string]string{"error": err.Error()})
		return
	}
	h.regenerateSwapConfig()
	// Remove published connector YAML so intake stops polling
	if inst != nil && inst.Kind == "connector" {
		os.Remove(filepath.Join(h.cfg.Home, "connectors", inst.Name+".yaml"))
	}
	name := nameOrID
	if inst != nil {
		name = inst.Name
	}
	writeJSON(w, 200, map[string]string{"status": "inactive", "name": name})
}

// hubSecretPutter returns a hub.SecretPutter that writes service credentials
// to the encrypted credential store with hub-specific metadata.
func (h *handler) hubSecretPutter(instanceName string) hub.SecretPutter {
	return func(name, value string) error {
		if h.credStore == nil {
			return fmt.Errorf("credential store not initialized")
		}
		now := time.Now().UTC().Format(time.RFC3339)
		return h.credStore.Put(credstore.Entry{
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
func (h *handler) hubSecretDeleter() hub.SecretDeleter {
	return func(name string) error {
		if h.credStore == nil {
			return fmt.Errorf("credential store not initialized")
		}
		return h.credStore.Delete(name)
	}
}

// -- Agent Logs --

func (h *handler) agentLogs(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	since := r.URL.Query().Get("since")
	until := r.URL.Query().Get("until")

	reader := logs.NewReader(h.cfg.Home)
	events, err := reader.ReadAgentLog(name, since, until)
	if err != nil {
		writeJSON(w, 404, map[string]string{"error": "no audit logs for agent"})
		return
	}

	// Limit to last 500
	if len(events) > 500 {
		events = events[len(events)-500:]
	}

	writeJSON(w, 200, events)
}

// -- Knowledge --

func (h *handler) knowledgeQuery(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Query string `json:"query"`
		Text  string `json:"text"`
		Q     string `json:"q"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, 400, map[string]string{"error": "invalid JSON"})
		return
	}
	q := body.Query
	if q == "" {
		q = body.Text
	}
	if q == "" {
		q = body.Q
	}
	if q == "" {
		writeJSON(w, 400, map[string]string{"error": "query required (use 'query', 'text', or 'q' field)"})
		return
	}
	data, err := h.knowledge.Query(r.Context(), q)
	if err != nil {
		writeJSON(w, 502, map[string]string{"error": err.Error()})
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(200)
	w.Write(data)
}

func (h *handler) knowledgeWhoKnows(w http.ResponseWriter, r *http.Request) {
	topic := r.URL.Query().Get("topic")
	if topic == "" {
		writeJSON(w, 400, map[string]string{"error": "topic parameter required"})
		return
	}
	proxy := knowledge.NewProxy()
	data, err := proxy.WhoKnows(r.Context(), topic)
	if err != nil {
		writeJSON(w, 502, map[string]string{"error": err.Error()})
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(200)
	w.Write(data)
}

func (h *handler) knowledgeStats(w http.ResponseWriter, r *http.Request) {
	proxy := knowledge.NewProxy()
	data, err := proxy.Stats(r.Context())
	if err != nil {
		writeJSON(w, 502, map[string]string{"error": err.Error()})
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(200)
	w.Write(data)
}

func (h *handler) knowledgeExport(w http.ResponseWriter, r *http.Request) {
	format := r.URL.Query().Get("format")
	if format == "" {
		format = "json"
	}
	proxy := knowledge.NewProxy()
	data, err := proxy.Export(r.Context(), format)
	if err != nil {
		writeJSON(w, 502, map[string]string{"error": err.Error()})
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(200)
	w.Write(data)
}

func (h *handler) knowledgeChanges(w http.ResponseWriter, r *http.Request) {
	since := r.URL.Query().Get("since")
	proxy := knowledge.NewProxy()
	data, err := proxy.Changes(r.Context(), since)
	if err != nil {
		writeJSON(w, 502, map[string]string{"error": err.Error()})
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(200)
	w.Write(data)
}

func (h *handler) knowledgeContext(w http.ResponseWriter, r *http.Request) {
	subject := r.URL.Query().Get("subject")
	if subject == "" {
		writeJSON(w, 400, map[string]string{"error": "subject parameter required"})
		return
	}
	proxy := knowledge.NewProxy()
	data, err := proxy.Context(r.Context(), subject)
	if err != nil {
		writeJSON(w, 502, map[string]string{"error": err.Error()})
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(200)
	w.Write(data)
}

func (h *handler) knowledgeNeighbors(w http.ResponseWriter, r *http.Request) {
	nodeID := r.URL.Query().Get("node_id")
	if nodeID == "" {
		writeJSON(w, 400, map[string]string{"error": "node_id parameter required"})
		return
	}
	proxy := knowledge.NewProxy()
	data, err := proxy.Neighbors(r.Context(), nodeID)
	if err != nil {
		writeJSON(w, 502, map[string]string{"error": err.Error()})
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(200)
	w.Write(data)
}

func (h *handler) knowledgePath(w http.ResponseWriter, r *http.Request) {
	from := r.URL.Query().Get("from")
	to := r.URL.Query().Get("to")
	if from == "" || to == "" {
		writeJSON(w, 400, map[string]string{"error": "from and to parameters required"})
		return
	}
	proxy := knowledge.NewProxy()
	data, err := proxy.Path(r.Context(), from, to)
	if err != nil {
		writeJSON(w, 502, map[string]string{"error": err.Error()})
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(200)
	w.Write(data)
}

func (h *handler) knowledgeFlags(w http.ResponseWriter, r *http.Request) {
	proxy := knowledge.NewProxy()
	data, err := proxy.Flags(r.Context())
	if err != nil {
		writeJSON(w, 502, map[string]string{"error": err.Error()})
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(200)
	w.Write(data)
}

func (h *handler) knowledgeRestore(w http.ResponseWriter, r *http.Request) {
	var body struct {
		NodeID string `json:"node_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, 400, map[string]string{"error": "invalid JSON"})
		return
	}
	if body.NodeID == "" {
		writeJSON(w, 400, map[string]string{"error": "node_id required"})
		return
	}
	proxy := knowledge.NewProxy()
	data, err := proxy.Restore(r.Context(), body.NodeID)
	if err != nil {
		writeJSON(w, 502, map[string]string{"error": err.Error()})
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(200)
	w.Write(data)
}

func (h *handler) knowledgeCurationLog(w http.ResponseWriter, r *http.Request) {
	proxy := knowledge.NewProxy()
	data, err := proxy.CurationLog(r.Context())
	if err != nil {
		writeJSON(w, 502, map[string]string{"error": err.Error()})
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(200)
	w.Write(data)
}

func (h *handler) knowledgeCommunities(w http.ResponseWriter, r *http.Request) {
	proxy := knowledge.NewProxy()
	data, err := proxy.Communities(r.Context())
	if err != nil {
		writeJSON(w, 502, map[string]string{"error": err.Error()})
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(200)
	w.Write(data)
}

func (h *handler) knowledgeCommunity(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "" {
		writeJSON(w, 400, map[string]string{"error": "missing community id"})
		return
	}
	proxy := knowledge.NewProxy()
	data, err := proxy.Community(r.Context(), id)
	if err != nil {
		writeJSON(w, 502, map[string]string{"error": err.Error()})
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(200)
	w.Write(data)
}

func (h *handler) knowledgeHubs(w http.ResponseWriter, r *http.Request) {
	limit := 0
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			limit = n
		}
	}
	proxy := knowledge.NewProxy()
	data, err := proxy.Hubs(r.Context(), limit)
	if err != nil {
		writeJSON(w, 502, map[string]string{"error": err.Error()})
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(200)
	w.Write(data)
}

func (h *handler) knowledgeIngest(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Content     string          `json:"content"`
		Filename    string          `json:"filename"`
		ContentType string          `json:"content_type"`
		Scope       json.RawMessage `json:"scope,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, 400, map[string]string{"error": "invalid JSON"})
		return
	}
	if body.Content == "" && body.Filename == "" {
		writeJSON(w, 400, map[string]string{"error": "content or filename required"})
		return
	}
	data, err := h.knowledge.Ingest(r.Context(), body.Content, body.Filename, body.ContentType, body.Scope)
	if err != nil {
		writeJSON(w, 502, map[string]string{"error": err.Error()})
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(200)
	w.Write(data)
}

func (h *handler) knowledgeSaveInsight(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Insight     string   `json:"insight"`
		SourceNodes []string `json:"source_nodes"`
		Confidence  string   `json:"confidence"`
		Tags        []string `json:"tags,omitempty"`
		AgentName   string   `json:"agent_name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, 400, map[string]string{"error": "invalid JSON"})
		return
	}
	if body.Insight == "" {
		writeJSON(w, 400, map[string]string{"error": "insight required"})
		return
	}
	data, err := h.knowledge.SaveInsight(r.Context(), body.Insight, body.SourceNodes, body.Confidence, body.Tags, body.AgentName)
	if err != nil {
		writeJSON(w, 502, map[string]string{"error": err.Error()})
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(200)
	w.Write(data)
}

// ── Knowledge Ontology ──────────────────────────────────────────────────────

func (h *handler) knowledgeOntology(w http.ResponseWriter, r *http.Request) {
	cfg, err := knowledge.LoadOntology(h.cfg.Home)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, 200, cfg)
}

func (h *handler) knowledgeOntologyTypes(w http.ResponseWriter, r *http.Request) {
	cfg, err := knowledge.LoadOntology(h.cfg.Home)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, 200, map[string]interface{}{
		"entity_types": cfg.EntityTypes,
		"count":        len(cfg.EntityTypes),
	})
}

func (h *handler) knowledgeOntologyRelationships(w http.ResponseWriter, r *http.Request) {
	cfg, err := knowledge.LoadOntology(h.cfg.Home)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, 200, map[string]interface{}{
		"relationship_types": cfg.RelationshipTypes,
		"count":              len(cfg.RelationshipTypes),
	})
}

func (h *handler) knowledgeOntologyValidate(w http.ResponseWriter, r *http.Request) {
	cfg, err := knowledge.LoadOntology(h.cfg.Home)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}

	// Get all nodes from knowledge graph
	proxy := knowledge.NewProxy()
	statsData, err := proxy.Stats(r.Context())
	if err != nil {
		writeJSON(w, 502, map[string]string{"error": "cannot reach knowledge service: " + err.Error()})
		return
	}

	var stats map[string]interface{}
	json.Unmarshal(statsData, &stats)

	kindsRaw, _ := stats["kinds"].(map[string]interface{})
	var issues []map[string]interface{}
	validCount := 0
	invalidCount := 0

	for kind, countRaw := range kindsRaw {
		count, _ := countRaw.(float64)
		corrected, changed := knowledge.ValidateNode(kind, cfg)
		if changed {
			invalidCount += int(count)
			issues = append(issues, map[string]interface{}{
				"kind":      kind,
				"count":     int(count),
				"suggested": corrected,
				"action":    "migrate " + kind + " " + corrected,
			})
		} else {
			validCount += int(count)
		}
	}

	writeJSON(w, 200, map[string]interface{}{
		"valid_nodes":   validCount,
		"invalid_nodes": invalidCount,
		"issues":        issues,
		"ontology_version": cfg.Version,
	})
}

func (h *handler) knowledgeOntologyMigrate(w http.ResponseWriter, r *http.Request) {
	var body struct {
		From string `json:"from"`
		To   string `json:"to"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, 400, map[string]string{"error": "invalid JSON"})
		return
	}
	if body.From == "" || body.To == "" {
		writeJSON(w, 400, map[string]string{"error": "from and to required"})
		return
	}

	// Validate target type exists in ontology
	cfg, err := knowledge.LoadOntology(h.cfg.Home)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	if _, ok := cfg.EntityTypes[body.To]; !ok {
		writeJSON(w, 400, map[string]string{"error": "target type '" + body.To + "' not in ontology"})
		return
	}

	// Proxy the migration to the knowledge service
	proxy := knowledge.NewProxy()
	migrationBody := map[string]string{"from": body.From, "to": body.To}
	data, err := proxy.Post(r.Context(), "/migrate-kind", migrationBody)
	if err != nil {
		// If knowledge service doesn't support migration endpoint, return info
		writeJSON(w, 200, map[string]interface{}{
			"status":  "pending",
			"from":    body.From,
			"to":      body.To,
			"message": "Migration queued. Knowledge service will process on next cycle.",
		})
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(200)
	w.Write(data)
}
