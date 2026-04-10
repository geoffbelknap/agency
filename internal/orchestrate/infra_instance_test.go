package orchestrate

import (
	"runtime"
	"testing"
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

func TestInfraHostPortsRespectOverrides(t *testing.T) {
	t.Setenv("AGENCY_GATEWAY_PROXY_PORT", "19202")
	t.Setenv("AGENCY_GATEWAY_PROXY_KNOWLEDGE_PORT", "19204")
	t.Setenv("AGENCY_GATEWAY_PROXY_INTAKE_PORT", "19205")
	t.Setenv("AGENCY_KNOWLEDGE_PORT", "19214")
	t.Setenv("AGENCY_INTAKE_PORT", "19215")
	t.Setenv("AGENCY_WEB_FETCH_PORT", "19216")
	t.Setenv("AGENCY_WEB_PORT", "19280")

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
