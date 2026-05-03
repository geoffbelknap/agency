package orchestrate

import "testing"

func TestHostServiceRuntimeBackendIncludesMicroagent(t *testing.T) {
	for _, backend := range []string{"firecracker", "apple-vf-microvm", "microagent"} {
		t.Run(backend, func(t *testing.T) {
			if !hostServiceRuntimeBackend(backend) {
				t.Fatalf("hostServiceRuntimeBackend(%q) = false, want true", backend)
			}
		})
	}
}

func TestMicroagentUsesHostServicesWithoutGatewayProxy(t *testing.T) {
	inf := &Infra{Home: t.TempDir(), RuntimeBackendName: "microagent"}

	if !inf.hostCommsEnabled() {
		t.Fatal("microagent should use host comms")
	}
	if !inf.hostEgressEnabled() {
		t.Fatal("microagent should use host egress")
	}
	if !inf.hostKnowledgeEnabled() {
		t.Fatal("microagent should use host knowledge")
	}
	if !inf.hostWebEnabled() {
		t.Fatal("microagent should use host web")
	}
	if inf.hostGatewayProxyEnabled() {
		t.Fatal("microagent should not start the container gateway proxy")
	}
}
