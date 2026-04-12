package hubpolicy

import "testing"

func TestConnectorRequiresAskPartial(t *testing.T) {
	p := DefaultPolicy()
	ok := p.AllowsInstall("connector", []string{"publisher_verified"})
	if ok {
		t.Fatal("expected install to be denied without ask_partial assurance")
	}
}

func TestPresetAllowsPublisherVerified(t *testing.T) {
	p := DefaultPolicy()
	ok := p.AllowsInstall("preset", []string{"publisher_verified"})
	if !ok {
		t.Fatal("expected preset install to be allowed with publisher verification")
	}
}

func TestOfficialSourceAllowsLegacyConnectorInstall(t *testing.T) {
	p := DefaultPolicy()
	ok := p.AllowsInstall("connector", []string{"publisher_verified", "official_source"})
	if !ok {
		t.Fatal("expected official-source connector install to be allowed")
	}
}
