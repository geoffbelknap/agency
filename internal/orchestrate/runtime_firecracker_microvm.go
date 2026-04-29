package orchestrate

import (
	"context"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"

	agentruntime "github.com/geoffbelknap/agency/internal/hostadapter/agentruntime"
	hostruntimebackend "github.com/geoffbelknap/agency/internal/hostadapter/runtimebackend"
	runtimecontract "github.com/geoffbelknap/agency/internal/runtime/contract"
)

type firecrackerComponentRole string

const (
	firecrackerComponentWorkload firecrackerComponentRole = "workload"
	firecrackerComponentEnforcer firecrackerComponentRole = "enforcer"
)

type firecrackerEnforcerMicroVMSpec struct {
	RuntimeID        string
	ParentRuntimeID  string
	Role             firecrackerComponentRole
	Image            string
	Env              map[string]string
	Mounts           []agentruntime.EnforcerMount
	HostServicePorts map[int]string
}

func (s firecrackerEnforcerMicroVMSpec) RuntimeSpec(parent runtimecontract.RuntimeSpec) runtimecontract.RuntimeSpec {
	return runtimecontract.RuntimeSpec{
		RuntimeID: s.RuntimeID,
		AgentID:   parent.AgentID,
		Backend:   hostruntimebackend.BackendFirecracker,
		Package: runtimecontract.RuntimePackageSpec{
			Image: s.Image,
			Env:   s.Env,
		},
		Transport: runtimecontract.RuntimeTransportSpec{
			Enforcer: runtimecontract.EnforcerTransportSpec{
				Type:     runtimecontract.TransportTypeVsockHTTP,
				Endpoint: "vsock://2:8081",
				AuthMode: parent.Transport.Enforcer.AuthMode,
				TokenRef: parent.Transport.Enforcer.TokenRef,
			},
		},
		Storage:   parent.Storage,
		Lifecycle: parent.Lifecycle,
		Health:    parent.Health,
		Revision:  parent.Revision,
	}
}

func (b *firecrackerComponentRuntimeBackend) compileEnforcerMicroVMSpec(ctx context.Context, spec runtimecontract.RuntimeSpec, rotateKey bool) (firecrackerEnforcerMicroVMSpec, error) {
	enforcer := &Enforcer{
		AgentName:          spec.RuntimeID,
		ContainerName:      fmt.Sprintf("%s-%s-enforcer", prefix, spec.RuntimeID),
		Home:               b.home,
		Version:            b.version,
		SourceDir:          b.sourceDir,
		BuildID:            b.buildID,
		ProxyHostPort:      agentruntime.EnforcerProxyPort,
		ConstraintHostPort: agentruntime.EnforcerConstraintPort,
		LifecycleID:        spec.Revision.InstanceRevision,
		PrincipalUUID:      spec.AgentID,
	}
	launchSpec, err := enforcer.BuildLaunchSpec(ctx, rotateKey)
	if err != nil {
		return firecrackerEnforcerMicroVMSpec{}, err
	}
	hostServices := b.hostServiceURLs()
	targets := firecrackerHostServiceTargets(hostServices)
	env := firecrackerEnforcerMicroVMEnv(launchSpec, targets, hostServices)
	return firecrackerEnforcerMicroVMSpec{
		RuntimeID:        firecrackerComponentRuntimeID(spec.RuntimeID, firecrackerComponentEnforcer),
		ParentRuntimeID:  spec.RuntimeID,
		Role:             firecrackerComponentEnforcer,
		Image:            enforcerImage,
		Env:              env,
		Mounts:           launchSpec.Mounts,
		HostServicePorts: targets,
	}, nil
}

func firecrackerComponentRuntimeID(runtimeID string, role firecrackerComponentRole) string {
	runtimeID = strings.TrimSpace(runtimeID)
	if role == firecrackerComponentWorkload || runtimeID == "" {
		return runtimeID
	}
	return runtimeID + "-" + string(role)
}

func firecrackerEnforcerMicroVMEnv(spec agentruntime.EnforcerLaunchSpec, hostServicePorts map[int]string, hostServices map[string]string) map[string]string {
	env := make(map[string]string, len(spec.Env)+8)
	for key, value := range spec.Env {
		env[key] = value
	}
	env["ENFORCER_PORT"] = spec.ProxyPort
	env["CONSTRAINT_WS_PORT"] = spec.ConstraintPort
	env["ENFORCER_BIND_ADDR"] = "0.0.0.0"
	env["CONSTRAINT_WS_BIND_ADDR"] = "0.0.0.0"
	env["AGENCY_VSOCK_HTTP_BRIDGES"] = firecrackerBridgeEnv(hostServicePorts)
	env["AGENCY_VSOCK_HTTP_GUEST_LISTENERS"] = "3128=127.0.0.1:3128,8081=127.0.0.1:8081"
	for key, value := range firecrackerGuestServiceURLs(hostServices) {
		env[key] = value
	}
	if gatewayURL := env["GATEWAY_URL"]; gatewayURL != "" && spec.AgentName != "" {
		env["ENFORCER_AUDIT_URL"] = strings.TrimRight(gatewayURL, "/") + "/api/v1/agents/" + url.PathEscape(spec.AgentName) + "/logs/enforcer"
	}
	for port, target := range hostServicePorts {
		env[hostruntimebackend.FirecrackerHostServiceTargetEnv(port)] = "http://" + target
	}
	if overlays, err := firecrackerRootFSOverlayEnv(spec.Mounts); err == nil && overlays != "" {
		env[hostruntimebackend.FirecrackerRootFSOverlaysEnv] = overlays
	}
	delete(env, hostruntimebackend.FirecrackerEnforcerProxyTargetEnv)
	delete(env, hostruntimebackend.FirecrackerEnforcerControlTargetEnv)
	return env
}

func firecrackerRootFSOverlayEnv(mounts []agentruntime.EnforcerMount) (string, error) {
	overlays := make([]hostruntimebackend.FirecrackerRootFSOverlay, 0, len(mounts))
	for _, mount := range mounts {
		overlays = append(overlays, hostruntimebackend.FirecrackerRootFSOverlay{
			HostPath:  mount.HostPath,
			GuestPath: mount.GuestPath,
			Mode:      mount.Mode,
		})
	}
	return hostruntimebackend.FirecrackerRootFSOverlaysEnvValue(overlays)
}

func firecrackerHostServiceTargets(urls map[string]string) map[int]string {
	targets := map[int]string{}
	for _, service := range []string{"gateway", "comms", "knowledge", "web-fetch", "egress"} {
		raw := strings.TrimSpace(urls[service])
		if raw == "" {
			continue
		}
		port, target, ok := firecrackerServicePortTarget(raw)
		if ok {
			targets[port] = target
		}
	}
	return targets
}

func firecrackerGuestServiceURLs(hostServices map[string]string) map[string]string {
	urls := map[string]string{}
	for service, envKey := range map[string]string{
		"gateway":   "GATEWAY_URL",
		"comms":     "COMMS_URL",
		"knowledge": "KNOWLEDGE_URL",
		"web-fetch": "WEB_FETCH_URL",
		"egress":    "EGRESS_PROXY",
	} {
		port, _, ok := firecrackerServicePortTarget(hostServices[service])
		if ok {
			urls[envKey] = fmt.Sprintf("http://127.0.0.1:%d", port)
		}
	}
	return urls
}

func firecrackerServicePortTarget(raw string) (int, string, bool) {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Host == "" {
		return 0, "", false
	}
	port, err := strconv.Atoi(parsed.Port())
	if err != nil || port <= 0 {
		return 0, "", false
	}
	return port, parsed.Host, true
}

func firecrackerBridgeEnv(targets map[int]string) string {
	ports := make([]int, 0, len(targets))
	for port := range targets {
		ports = append(ports, port)
	}
	sort.Ints(ports)
	entries := make([]string, 0, len(ports))
	for _, port := range ports {
		entries = append(entries, fmt.Sprintf("127.0.0.1:%d=2:%d", port, port))
	}
	return strings.Join(entries, ",")
}
