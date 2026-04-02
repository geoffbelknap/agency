package services

import "testing"

func TestNewServiceMap(t *testing.T) {
	sm := NewServiceMap([]byte("key"))
	if sm == nil {
		t.Fatal("NewServiceMap returned nil")
	}
}

func TestServiceMapRegister(t *testing.T) {
	sm := NewServiceMap([]byte("key"))
	labels := map[string]string{
		LabelServiceEnabled: "true",
		LabelServiceName:    "comms",
		LabelServicePort:    "8080",
		LabelServiceHealth:  "/health",
		LabelServiceNetwork: "agency-mediation",
		LabelServiceHMAC:    GenerateHMAC("agency-infra-comms", []byte("key")),
	}
	err := sm.Register("abc123", "agency-infra-comms", labels)
	if err != nil {
		t.Fatalf("Register failed: %v", err)
	}
}

func TestServiceMapURL(t *testing.T) {
	sm := NewServiceMap([]byte("key"))
	labels := map[string]string{
		LabelServiceEnabled: "true",
		LabelServiceName:    "comms",
		LabelServicePort:    "8080",
		LabelServiceHealth:  "/health",
		LabelServiceNetwork: "agency-mediation",
		LabelServiceHMAC:    GenerateHMAC("agency-infra-comms", []byte("key")),
	}
	sm.Register("abc123", "agency-infra-comms", labels)
	url := sm.URL("comms")
	if url != "http://agency-infra-comms:8080" {
		t.Errorf("expected http://agency-infra-comms:8080, got %s", url)
	}
}

func TestServiceMapURLUnknown(t *testing.T) {
	sm := NewServiceMap([]byte("key"))
	url := sm.URL("nonexistent")
	if url != "" {
		t.Errorf("expected empty string for unknown service, got %s", url)
	}
}

func TestServiceMapDeregister(t *testing.T) {
	sm := NewServiceMap([]byte("key"))
	labels := map[string]string{
		LabelServiceEnabled: "true",
		LabelServiceName:    "comms",
		LabelServicePort:    "8080",
		LabelServiceHealth:  "/health",
		LabelServiceNetwork: "agency-mediation",
		LabelServiceHMAC:    GenerateHMAC("agency-infra-comms", []byte("key")),
	}
	sm.Register("abc123", "agency-infra-comms", labels)
	sm.Deregister("abc123")
	url := sm.URL("comms")
	if url != "" {
		t.Errorf("expected empty after deregister, got %s", url)
	}
}

func TestServiceMapRejectsInvalidHMAC(t *testing.T) {
	sm := NewServiceMap([]byte("key"))
	labels := map[string]string{
		LabelServiceEnabled: "true",
		LabelServiceName:    "comms",
		LabelServicePort:    "8080",
		LabelServiceHealth:  "/health",
		LabelServiceNetwork: "agency-mediation",
		LabelServiceHMAC:    "invalid-hmac",
	}
	err := sm.Register("xyz789", "agency-infra-comms", labels)
	if err == nil {
		t.Error("should reject invalid HMAC")
	}
	url := sm.URL("comms")
	if url != "" {
		t.Error("rejected service should not be in map")
	}
}

func TestServiceMapAll(t *testing.T) {
	sm := NewServiceMap([]byte("key"))
	for _, name := range []string{"comms", "knowledge", "intake"} {
		cname := "agency-infra-" + name
		labels := map[string]string{
			LabelServiceEnabled: "true",
			LabelServiceName:    name,
			LabelServicePort:    "8080",
			LabelServiceHealth:  "/health",
			LabelServiceNetwork: "agency-mediation",
			LabelServiceHMAC:    GenerateHMAC(cname, []byte("key")),
		}
		sm.Register("id-"+name, cname, labels)
	}
	all := sm.All()
	if len(all) != 3 {
		t.Errorf("expected 3 services, got %d", len(all))
	}
}

func TestServiceMapIsHealthy(t *testing.T) {
	sm := NewServiceMap([]byte("key"))
	labels := map[string]string{
		LabelServiceEnabled: "true",
		LabelServiceName:    "comms",
		LabelServicePort:    "8080",
		LabelServiceHealth:  "/health",
		LabelServiceNetwork: "agency-mediation",
		LabelServiceHMAC:    GenerateHMAC("agency-infra-comms", []byte("key")),
	}
	sm.Register("abc123", "agency-infra-comms", labels)
	if sm.IsHealthy("comms") {
		t.Error("new service should not be healthy until checked")
	}
	sm.SetHealthy("comms", true)
	if !sm.IsHealthy("comms") {
		t.Error("service should be healthy after SetHealthy(true)")
	}
}

func TestServiceMapTrackCreation(t *testing.T) {
	sm := NewServiceMap([]byte("key"))
	sm.TrackCreation("abc123")
	// Now register without valid HMAC — should succeed via in-memory tracking
	labels := map[string]string{
		LabelServiceEnabled: "true",
		LabelServiceName:    "comms",
		LabelServicePort:    "8080",
		LabelServiceHealth:  "/health",
		LabelServiceNetwork: "agency-mediation",
		LabelServiceHMAC:    "does-not-matter",
	}
	err := sm.Register("abc123", "agency-infra-comms", labels)
	if err != nil {
		t.Fatalf("should accept tracked container: %v", err)
	}
}
