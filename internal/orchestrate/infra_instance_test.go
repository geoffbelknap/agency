package orchestrate

import (
	"context"
	"runtime"
	"testing"

	"github.com/geoffbelknap/agency/internal/hostadapter/runtimehost"
)

func TestScopedInfraNameUsesInstance(t *testing.T) {
	t.Setenv("AGENCY_INFRA_INSTANCE", "danger-home-123")

	if got, want := gatewayNetName(), "agency-gateway-danger-home-123"; got != want {
		t.Fatalf("gatewayNetName() = %q, want %q", got, want)
	}
	if got, want := egressIntNetName(), "agency-egress-int-danger-home-123"; got != want {
		t.Fatalf("egressIntNetName() = %q, want %q", got, want)
	}
	if got, want := operatorNetName(), "agency-operator-danger-home-123"; got != want {
		t.Fatalf("operatorNetName() = %q, want %q", got, want)
	}
}

func TestScopedInfraNameDefaultsWithoutInstance(t *testing.T) {
	t.Setenv("AGENCY_INFRA_INSTANCE", "")

	if got, want := gatewayNetName(), baseGatewayNet; got != want {
		t.Fatalf("gatewayNetName() = %q, want %q", got, want)
	}
	if got, want := egressExtNetName(), baseEgressExtNet; got != want {
		t.Fatalf("egressExtNetName() = %q, want %q", got, want)
	}
}

func TestInfraContainerNameUsesInstance(t *testing.T) {
	t.Setenv("AGENCY_INFRA_INSTANCE", "danger-home-abc")

	inf := &Infra{Instance: infraInstanceName()}
	if got, want := inf.containerName("web"), "agency-infra-web-danger-home-abc"; got != want {
		t.Fatalf("containerName(web) = %q, want %q", got, want)
	}
}

func TestGatewayProxyServiceHostsDefaultToServiceAliases(t *testing.T) {
	inf := &Infra{}
	hosts := inf.gatewayProxyServiceHosts(context.Background())
	for role, want := range map[string]string{
		"comms":     "comms",
		"knowledge": "knowledge",
		"intake":    "intake",
	} {
		if got := hosts[role]; got != want {
			t.Fatalf("gatewayProxyServiceHosts()[%q] = %q, want %q", role, got, want)
		}
	}
}

func TestGatewayProxyServiceHostsUseAppleContainerNames(t *testing.T) {
	t.Setenv("AGENCY_INFRA_INSTANCE", "apple-home")

	appleContainer, err := runtimehost.NewRawClientForBackend(runtimehost.BackendAppleContainer, nil)
	if err != nil {
		t.Skipf("Apple container raw client unavailable: %v", err)
	}
	inf := &Infra{Instance: infraInstanceName(), cli: appleContainer}
	hosts := inf.gatewayProxyServiceHosts(context.Background())
	for role, want := range map[string]string{
		"comms":     "agency-infra-comms-apple-home",
		"knowledge": "agency-infra-knowledge-apple-home",
		"intake":    "agency-infra-intake-apple-home",
	} {
		if got := hosts[role]; got != want {
			t.Fatalf("gatewayProxyServiceHosts()[%q] = %q, want %q", role, got, want)
		}
	}
}

func TestInfraInstanceLabelDefaultsWithoutInstance(t *testing.T) {
	inf := &Infra{}
	if got, want := inf.instanceLabel(), "default"; got != want {
		t.Fatalf("instanceLabel() = %q, want %q", got, want)
	}
}

func TestInfraLabelsIncludeInstanceAndComponent(t *testing.T) {
	t.Setenv("AGENCY_INFRA_INSTANCE", "danger-home-abc")

	inf := &Infra{Instance: infraInstanceName(), BuildID: "build-123"}
	labels := inf.infraLabels(context.Background(), "agency-web:latest", "web")
	if got, want := labels["agency.instance"], "danger-home-abc"; got != want {
		t.Fatalf("agency.instance = %q, want %q", got, want)
	}
	if got, want := labels["agency.component"], "web"; got != want {
		t.Fatalf("agency.component = %q, want %q", got, want)
	}
	if got, want := labels["agency.role"], "infra"; got != want {
		t.Fatalf("agency.role = %q, want %q", got, want)
	}
}

func TestServiceLabelsIncludeInfraInstance(t *testing.T) {
	t.Setenv("AGENCY_INFRA_INSTANCE", "danger-home-abc")

	inf := &Infra{Instance: infraInstanceName(), BuildID: "build-123", hmacKey: []byte("test")}
	labels := inf.serviceLabels(context.Background(), "agency-comms:latest", "comms", "8080")
	if got, want := labels["agency.instance"], "danger-home-abc"; got != want {
		t.Fatalf("agency.instance = %q, want %q", got, want)
	}
	if got, want := labels["agency.managed"], "true"; got != want {
		t.Fatalf("agency.managed = %q, want %q", got, want)
	}
}

func TestInfraHostPortsRespectOverrides(t *testing.T) {
	t.Setenv("AGENCY_GATEWAY_PROXY_PORT", "19202")
	t.Setenv("AGENCY_GATEWAY_PROXY_KNOWLEDGE_PORT", "19204")
	t.Setenv("AGENCY_GATEWAY_PROXY_INTAKE_PORT", "19205")
	t.Setenv("AGENCY_KNOWLEDGE_PORT", "19214")
	t.Setenv("AGENCY_INTAKE_PORT", "19215")
	t.Setenv("AGENCY_WEB_FETCH_PORT", "19216")
	t.Setenv("AGENCY_WEB_PORT", "19280")
	t.Setenv("AGENCY_EGRESS_PROXY_PORT", "19312")

	inf := &Infra{}
	if got, want := inf.gatewayProxyPort("8202"), "19202"; got != want {
		t.Fatalf("gatewayProxyPort(8202) = %q, want %q", got, want)
	}
	if got, want := inf.gatewayProxyPort("8204"), "19204"; got != want {
		t.Fatalf("gatewayProxyPort(8204) = %q, want %q", got, want)
	}
	if got, want := inf.gatewayProxyPort("8205"), "19205"; got != want {
		t.Fatalf("gatewayProxyPort(8205) = %q, want %q", got, want)
	}
	if got, want := inf.knowledgePort(), "19214"; got != want {
		t.Fatalf("knowledgePort() = %q, want %q", got, want)
	}
	if got, want := inf.intakePort(), "19215"; got != want {
		t.Fatalf("intakePort() = %q, want %q", got, want)
	}
	if got, want := inf.webFetchPort(), "19216"; got != want {
		t.Fatalf("webFetchPort() = %q, want %q", got, want)
	}
	if got, want := inf.webPort(), "19280"; got != want {
		t.Fatalf("webPort() = %q, want %q", got, want)
	}
	if got, want := inf.egressProxyPort(), "19312"; got != want {
		t.Fatalf("egressProxyPort() = %q, want %q", got, want)
	}
}

func TestSuppressDirectServiceHostPortsForAnyRootlessPodman(t *testing.T) {
	// gateway-proxy is the authoritative front door for the services that
	// otherwise collide on their host port (knowledge on 8204, intake on
	// 8205). Rootless podman refuses overlapping host-port bindings via
	// rootlessport, so we suppress the direct bindings for every rootless
	// endpoint — the WSL-only carveout that used to live here was
	// incomplete and broke native Linux rootless installs.
	origDetect := detectWSLHost
	defer func() { detectWSLHost = origDetect }()

	rootlessPodman, err := runtimehost.NewRawClientForBackend(runtimehost.BackendPodman, map[string]string{
		"host": "/run/user/1000/podman/podman.sock",
	})
	if err != nil {
		t.Fatal(err)
	}

	for _, wsl := range []bool{true, false} {
		detectWSLHost = func() bool { return wsl }
		if got := (&Infra{cli: rootlessPodman}).suppressDirectServiceHostPorts(); !got {
			t.Fatalf("rootless Podman (wsl=%v) should suppress direct service host ports", wsl)
		}
	}

	rootfulPodman, err := runtimehost.NewRawClientForBackend(runtimehost.BackendPodman, map[string]string{
		"host": "/run/podman/podman.sock",
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := (&Infra{cli: rootfulPodman}).suppressDirectServiceHostPorts(); got {
		t.Fatal("rootful Podman should keep direct service host ports")
	}

	appleContainer, err := runtimehost.NewRawClientForBackend(runtimehost.BackendAppleContainer, nil)
	if err == nil {
		if got := (&Infra{cli: appleContainer}).suppressDirectServiceHostPorts(); !got {
			t.Fatal("Apple container should suppress direct service host ports")
		}
	}
}

func TestInfraWebGatewayRoutingDefaults(t *testing.T) {
	inf := &Infra{GatewayAddr: "127.0.0.1:18300"}
	t.Setenv("AGENCY_WEB_PORT", "18380")

	if runtime.GOOS == "linux" {
		if !inf.webUsesHostNetwork() {
			t.Fatal("Linux loopback gateway should use host networking for agency-web")
		}
		if got, want := inf.webGatewayHost(), "127.0.0.1"; got != want {
			t.Fatalf("webGatewayHost() = %q, want %q", got, want)
		}
		if got, want := inf.webListenAddr(), "127.0.0.1:18380"; got != want {
			t.Fatalf("webListenAddr() = %q, want %q", got, want)
		}
		if got, want := inf.webHealthCheck().Test[4], "http://127.0.0.1:18380/health"; got != want {
			t.Fatalf("web health URL = %q, want %q", got, want)
		}
		return
	}

	if inf.webUsesHostNetwork() {
		t.Fatal("non-Linux web should keep bridge networking")
	}
	if got, want := inf.webGatewayHost(), "gateway"; got != want {
		t.Fatalf("webGatewayHost() = %q, want %q", got, want)
	}
	if got, want := inf.webListenAddr(), "8280"; got != want {
		t.Fatalf("webListenAddr() = %q, want %q", got, want)
	}
}

func TestInfraWebGatewayRoutingForAppleContainer(t *testing.T) {
	appleContainer, err := runtimehost.NewRawClientForBackend(runtimehost.BackendAppleContainer, nil)
	if err != nil {
		t.Skipf("Apple container raw client unavailable: %v", err)
	}
	inf := &Infra{GatewayAddr: "192.168.128.1:8200", cli: appleContainer}

	if inf.webUsesHostNetwork() {
		t.Fatal("Apple container web should keep bridge networking")
	}
	if got, want := inf.webGatewayHost(), "192.168.128.1"; got != want {
		t.Fatalf("webGatewayHost() = %q, want %q", got, want)
	}
	if got, want := inf.webListenAddr(), "8280"; got != want {
		t.Fatalf("webListenAddr() = %q, want %q", got, want)
	}
}

func TestInfraWebNetworkModeOverride(t *testing.T) {
	inf := &Infra{GatewayAddr: "127.0.0.1:18300"}

	t.Setenv("AGENCY_WEB_NETWORK_MODE", "bridge")
	if inf.webUsesHostNetwork() {
		t.Fatal("AGENCY_WEB_NETWORK_MODE=bridge should disable host networking")
	}

	t.Setenv("AGENCY_WEB_NETWORK_MODE", "host")
	if runtime.GOOS == "linux" && !inf.webUsesHostNetwork() {
		t.Fatal("AGENCY_WEB_NETWORK_MODE=host should enable host networking on Linux")
	}
	if runtime.GOOS != "linux" && inf.webUsesHostNetwork() {
		t.Fatal("AGENCY_WEB_NETWORK_MODE=host should not force unsupported non-Linux host networking")
	}
}
