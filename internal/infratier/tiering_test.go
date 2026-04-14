package infratier

import "testing"

func TestStartupComponentsDefaultToCore(t *testing.T) {
	t.Setenv("AGENCY_EXPERIMENTAL_SURFACES", "")
	got := StartupComponents()
	want := []string{"egress", "comms", "knowledge", "web"}
	assertSlicesEqual(t, got, want)
}

func TestStartupComponentsIncludeExperimentalWhenEnabled(t *testing.T) {
	t.Setenv("AGENCY_EXPERIMENTAL_SURFACES", "1")
	got := StartupComponents()
	want := []string{"egress", "comms", "knowledge", "web", "intake", "web-fetch", "relay", "embeddings"}
	assertSlicesEqual(t, got, want)
}

func TestStatusComponentsDefaultToCore(t *testing.T) {
	t.Setenv("AGENCY_EXPERIMENTAL_SURFACES", "")
	got := StatusComponents()
	want := []string{"egress", "comms", "knowledge", "web"}
	assertSlicesEqual(t, got, want)
}

func assertSlicesEqual(t *testing.T, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("len mismatch: got %v want %v", got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Fatalf("mismatch at %d: got %v want %v", i, got, want)
		}
	}
}
