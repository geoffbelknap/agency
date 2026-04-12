package instances

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"

	"github.com/geoffbelknap/agency/internal/events"
	"github.com/geoffbelknap/agency/internal/hub"
	"github.com/geoffbelknap/agency/internal/hubpolicy"
	instancepkg "github.com/geoffbelknap/agency/internal/instances"
	"github.com/geoffbelknap/agency/internal/manifestgen"
	runpkg "github.com/geoffbelknap/agency/internal/runtime"
	"github.com/go-chi/chi/v5"
)

func (h *handler) listInstances(w http.ResponseWriter, r *http.Request) {
	store := h.store()
	if store == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "instance store not available"})
		return
	}

	items, err := store.List(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if items == nil {
		items = []*instancepkg.Instance{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"instances": items})
}

func (h *handler) createInstance(w http.ResponseWriter, r *http.Request) {
	store := h.store()
	if store == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "instance store not available"})
		return
	}

	var inst instancepkg.Instance
	if err := json.NewDecoder(r.Body).Decode(&inst); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON"})
		return
	}
	if strings.TrimSpace(inst.ID) == "" {
		id, err := generateInstanceID()
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		inst.ID = id
	}
	if err := store.Create(r.Context(), &inst); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusCreated, inst)
}

func (h *handler) createInstanceFromPackage(w http.ResponseWriter, r *http.Request) {
	store := h.store()
	if store == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "instance store not available"})
		return
	}
	reg := h.packageRegistry()
	if reg == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "package registry not available"})
		return
	}

	var req packageInstantiateRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON"})
		return
	}
	if strings.TrimSpace(req.Kind) == "" || strings.TrimSpace(req.Name) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "kind and name are required"})
		return
	}

	pkg, ok := reg.GetPackage(req.Kind, req.Name)
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "package not found"})
		return
	}
	if err := h.requirePackageAssurance(pkg.Kind, pkg); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	inst, err := scaffoldInstanceFromPackage(pkg, req)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if strings.TrimSpace(inst.ID) == "" {
		id, err := generateInstanceID()
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		inst.ID = id
	}
	if err := store.Create(r.Context(), inst); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusCreated, inst)
}

func (h *handler) showInstance(w http.ResponseWriter, r *http.Request) {
	store := h.store()
	if store == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "instance store not available"})
		return
	}

	inst, err := store.Get(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, inst)
}

func (h *handler) updateInstance(w http.ResponseWriter, r *http.Request) {
	store := h.store()
	if store == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "instance store not available"})
		return
	}

	type updateRequest struct {
		Name          *string                         `json:"name"`
		Source        *instancepkg.InstanceSource     `json:"source"`
		Nodes         *[]instancepkg.Node             `json:"nodes"`
		Grants        *[]instancepkg.GrantBinding     `json:"grants"`
		Credentials   *map[string]instancepkg.Binding `json:"credentials"`
		Config        *map[string]any                 `json:"config"`
		Relationships *[]instancepkg.Relationship     `json:"relationships"`
	}

	var body updateRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON"})
		return
	}

	id := chi.URLParam(r, "id")
	if err := store.Update(r.Context(), id, func(inst *instancepkg.Instance) error {
		if body.Name != nil {
			inst.Name = *body.Name
		}
		if body.Source != nil {
			inst.Source = *body.Source
		}
		if body.Nodes != nil {
			inst.Nodes = *body.Nodes
		}
		if body.Grants != nil {
			inst.Grants = *body.Grants
		}
		if body.Credentials != nil {
			inst.Credentials = *body.Credentials
		}
		if body.Config != nil {
			inst.Config = *body.Config
		}
		if body.Relationships != nil {
			inst.Relationships = *body.Relationships
		}
		return nil
	}); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	inst, err := store.Get(r.Context(), id)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, inst)
}

func (h *handler) validateInstance(w http.ResponseWriter, r *http.Request) {
	store := h.store()
	if store == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "instance store not available"})
		return
	}

	inst, err := store.Get(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		return
	}
	if err := instancepkg.ValidateInstance(inst); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "valid"})
}

func (h *handler) applyInstance(w http.ResponseWriter, r *http.Request) {
	store := h.store()
	if store == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "instance store not available"})
		return
	}
	id := chi.URLParam(r, "id")
	inst, err := store.Get(r.Context(), id)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		return
	}
	if err := h.requireInstancePackageAssurance(inst); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	manifest, rtStore, err := h.compileManifestForInstance(id, inst)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if err := (runpkg.Reconciler{Home: h.homeDir()}).Reconcile(rtStore, manifest); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	h.syncRuntimeSubscriptions(manifest)
	h.reloadIngressIfNeeded(manifest)
	statuses, err := rtStore.ListNodeStatuses()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"status":   "applied",
		"instance": inst,
		"manifest": manifest,
		"nodes":    statuses,
	})
}

func (h *handler) claimInstance(w http.ResponseWriter, r *http.Request) {
	store := h.store()
	if store == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "instance store not available"})
		return
	}

	var body struct {
		Owner string `json:"owner"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON"})
		return
	}
	if err := store.Claim(r.Context(), chi.URLParam(r, "id"), body.Owner); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "claimed"})
}

func (h *handler) releaseInstance(w http.ResponseWriter, r *http.Request) {
	store := h.store()
	if store == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "instance store not available"})
		return
	}
	if err := store.Release(r.Context(), chi.URLParam(r, "id")); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "released"})
}

func (h *handler) compileRuntimeManifest(w http.ResponseWriter, r *http.Request) {
	store := h.store()
	if store == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "instance store not available"})
		return
	}
	id := chi.URLParam(r, "id")
	inst, err := store.Get(r.Context(), id)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		return
	}
	manifest, _, err := h.compileManifestForInstance(id, inst)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusCreated, manifest)
}

func (h *handler) compileManifestForInstance(id string, inst *instancepkg.Instance) (*runpkg.Manifest, *runpkg.Store, error) {
	planner := runpkg.Planner{}
	if reg := h.packageRegistry(); reg != nil {
		planner.Packages = reg
	}
	manifest, err := planner.Compile(inst)
	if err != nil {
		return nil, nil, err
	}
	store := h.store()
	if store == nil {
		return nil, nil, fmt.Errorf("instance store not available")
	}
	instanceDir, err := store.InstanceDir(id)
	if err != nil {
		return nil, nil, err
	}
	rtStore := runpkg.NewStore(instanceDir)
	if err := rtStore.SaveManifest(manifest); err != nil {
		return nil, nil, err
	}
	h.refreshAttachedAgentManifests(id)
	return manifest, rtStore, nil
}

func (h *handler) requireInstancePackageAssurance(inst *instancepkg.Instance) error {
	reg := h.packageRegistry()
	if reg == nil || inst == nil {
		return nil
	}

	seen := map[string]bool{}
	check := func(ref instancepkg.PackageRef) error {
		if strings.TrimSpace(ref.Kind) == "" || strings.TrimSpace(ref.Name) == "" {
			return nil
		}
		key := ref.Kind + "/" + ref.Name
		if seen[key] {
			return nil
		}
		seen[key] = true
		pkg, ok := reg.GetPackage(ref.Kind, ref.Name)
		if !ok {
			return nil
		}
		return h.requirePackageAssurance(ref.Kind, pkg)
	}

	if err := check(inst.Source.Package); err != nil {
		return err
	}
	for _, node := range inst.Nodes {
		if err := check(node.Package); err != nil {
			return err
		}
	}
	return nil
}

func (h *handler) requirePackageAssurance(kind string, pkg hub.InstalledPackage) error {
	if !hubpolicy.DefaultPolicy().AllowsInstall(kind, pkg.Assurance) {
		return fmt.Errorf("insufficient package assurance for %s", kind)
	}
	return nil
}

func (h *handler) showRuntimeManifest(w http.ResponseWriter, r *http.Request) {
	store := h.store()
	if store == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "instance store not available"})
		return
	}
	instanceDir, err := store.InstanceDir(chi.URLParam(r, "id"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	manifest, err := runpkg.NewStore(instanceDir).LoadManifest()
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, manifest)
}

func (h *handler) reconcileRuntime(w http.ResponseWriter, r *http.Request) {
	store := h.store()
	if store == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "instance store not available"})
		return
	}
	id := chi.URLParam(r, "id")
	instanceDir, err := store.InstanceDir(id)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	rtStore := runpkg.NewStore(instanceDir)
	manifest, err := rtStore.LoadManifest()
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		return
	}
	inst, err := store.Get(r.Context(), id)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		return
	}
	if manifest.Source.InstanceRevision.Before(inst.UpdatedAt) {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "runtime manifest is stale"})
		return
	}
	if err := (runpkg.Reconciler{Home: h.homeDir()}).Reconcile(rtStore, manifest); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	h.syncRuntimeSubscriptions(manifest)
	h.reloadIngressIfNeeded(manifest)
	writeJSON(w, http.StatusOK, manifest)
}

func (h *handler) runtimeNodeStatus(w http.ResponseWriter, r *http.Request) {
	rtStore, manifest, ok := h.runtimeContext(w, r)
	if !ok {
		return
	}
	status, err := h.runtimeManager().Status(rtStore, manifest, chi.URLParam(r, "nodeID"))
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, status)
}

func (h *handler) startRuntimeNode(w http.ResponseWriter, r *http.Request) {
	rtStore, manifest, ok := h.runtimeContext(w, r)
	if !ok {
		return
	}
	status, err := h.runtimeManager().StartAuthority(rtStore, manifest, chi.URLParam(r, "nodeID"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, status)
}

func (h *handler) stopRuntimeNode(w http.ResponseWriter, r *http.Request) {
	rtStore, manifest, ok := h.runtimeContext(w, r)
	if !ok {
		return
	}
	status, err := h.runtimeManager().StopAuthority(rtStore, manifest, chi.URLParam(r, "nodeID"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, status)
}

func (h *handler) invokeRuntimeNode(w http.ResponseWriter, r *http.Request) {
	rtStore, manifest, ok := h.runtimeContext(w, r)
	if !ok {
		return
	}
	nodeID := chi.URLParam(r, "nodeID")
	status, err := h.runtimeManager().Status(rtStore, manifest, nodeID)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		return
	}
	if status.State != runpkg.NodeStateActive || strings.TrimSpace(status.URL) == "" {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "runtime node is not active"})
		return
	}

	var payload map[string]any
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&payload); err != nil && err != io.EOF {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON"})
		return
	}
	if payload == nil {
		payload = map[string]any{}
	}
	h.forwardRuntimeInvoke(w, r, status.URL, payload)
}

func (h *handler) invokeRuntimeAction(w http.ResponseWriter, r *http.Request) {
	rtStore, manifest, ok := h.runtimeContext(w, r)
	if !ok {
		return
	}
	nodeID := chi.URLParam(r, "nodeID")
	action := chi.URLParam(r, "action")
	status, err := h.runtimeManager().Status(rtStore, manifest, nodeID)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		return
	}
	if status.State != runpkg.NodeStateActive || strings.TrimSpace(status.URL) == "" {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "runtime node is not active"})
		return
	}

	var input map[string]any
	if r.Body != nil {
		if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&input); err != nil && err != io.EOF {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON"})
			return
		}
	}
	if input == nil {
		input = map[string]any{}
	}
	subject := strings.TrimSpace(r.Header.Get("X-Agency-Subject"))
	if subject == "" {
		if agentName := strings.TrimSpace(r.Header.Get("X-Agency-Agent")); agentName != "" {
			subject = "agent:" + manifest.Metadata.InstanceName + "/" + agentName
		}
	}
	if subject == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "missing invoking subject"})
		return
	}

	payload := map[string]any{
		"subject": subject,
		"node_id": nodeID,
		"action":  action,
		"input":   input,
	}
	if consentProvided, ok := input["consent_provided"].(bool); ok {
		payload["consent_provided"] = consentProvided
	}
	h.forwardRuntimeInvoke(w, r, status.URL, payload)
}

func (h *handler) forwardRuntimeInvoke(w http.ResponseWriter, r *http.Request, runtimeURL string, payload map[string]any) {
	bodyData, err := json.Marshal(payload)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	req, err := http.NewRequestWithContext(r.Context(), http.MethodPost, strings.TrimRight(runtimeURL, "/")+"/invoke", bytes.NewReader(bodyData))
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": "failed to read runtime response"})
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(resp.StatusCode)
	_, _ = w.Write(body)
}

func (h *handler) runtimeContext(w http.ResponseWriter, r *http.Request) (*runpkg.Store, *runpkg.Manifest, bool) {
	store := h.store()
	if store == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "instance store not available"})
		return nil, nil, false
	}
	instanceDir, err := store.InstanceDir(chi.URLParam(r, "id"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return nil, nil, false
	}
	rtStore := runpkg.NewStore(instanceDir)
	manifest, err := rtStore.LoadManifest()
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		return nil, nil, false
	}
	return rtStore, manifest, true
}

func (h *handler) homeDir() string {
	if h.deps.Config == nil {
		return ""
	}
	return h.deps.Config.Home
}

func (h *handler) reloadIngressIfNeeded(manifest *runpkg.Manifest) {
	if manifest == nil || h.deps.Signal == nil {
		return
	}
	for _, node := range manifest.Runtime.Nodes {
		if node.Kind == "connector.ingress" {
			_ = h.deps.Signal.SignalContainer(context.Background(), "agency-intake", "SIGHUP")
			return
		}
	}
}

func (h *handler) syncRuntimeSubscriptions(manifest *runpkg.Manifest) {
	if manifest == nil || h.deps.EventBus == nil {
		return
	}
	h.deps.EventBus.Subscriptions().RemoveByOrigin(events.OriginInstance, manifest.Metadata.InstanceID)
	for _, sub := range manifest.Runtime.Subscriptions {
		h.deps.EventBus.Subscriptions().Add(&events.Subscription{
			ID:         sub.ID,
			SourceType: sub.SourceType,
			SourceName: sub.SourceName,
			EventType:  sub.EventType,
			Origin:     events.OriginInstance,
			OriginRef:  manifest.Metadata.InstanceID,
			Active:     true,
			Destination: events.Destination{
				Type:   events.DestRuntime,
				Target: sub.InstanceID + "/" + sub.NodeID,
			},
		})
	}
}

func generateInstanceID() (string, error) {
	b := make([]byte, 4)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate instance id: %w", err)
	}
	return "inst_" + hex.EncodeToString(b), nil
}

func (h *handler) refreshAttachedAgentManifests(instanceID string) {
	if h.deps.Config == nil {
		return
	}
	gen := manifestgen.Generator{
		Home:   h.deps.Config.Home,
		Logger: h.deps.Logger,
	}
	logger := h.deps.Logger
	if logger == nil {
		logger = slog.Default()
	}
	agents, err := gen.AttachedAgents(instanceID)
	if err != nil {
		logger.Warn("failed to discover attached agents",
			"instance_id", instanceID,
			"err", err)
		return
	}
	for _, agentName := range agents {
		if err := gen.GenerateAgentManifest(agentName); err != nil {
			logger.Warn("failed to refresh attached agent manifest",
				"instance_id", instanceID,
				"agent", agentName,
				"err", err)
		}
	}
}
