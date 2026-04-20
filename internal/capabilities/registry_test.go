package capabilities

import "testing"

func findEntry(entries []Entry, name string) (Entry, bool) {
	for _, entry := range entries {
		if entry.Name == name {
			return entry, true
		}
	}
	return Entry{}, false
}

func TestRegistryListIncludesProviderToolCapabilities(t *testing.T) {
	reg := NewRegistry(t.TempDir())

	entries := reg.List()
	webSearch, ok := findEntry(entries, "provider-web-search")
	if !ok {
		t.Fatalf("provider-web-search not listed")
	}
	if webSearch.Kind != "provider-tool" {
		t.Fatalf("provider-web-search kind = %q, want provider-tool", webSearch.Kind)
	}
	if webSearch.State != "disabled" {
		t.Fatalf("provider-web-search state = %q, want disabled", webSearch.State)
	}
	if webSearch.Description == "" {
		t.Fatalf("provider-web-search description is empty")
	}
	if webSearch.Spec["risk"] != "medium" {
		t.Fatalf("provider-web-search risk = %#v, want medium", webSearch.Spec["risk"])
	}
}

func TestRegistryProviderToolCapabilityStateIsGrantable(t *testing.T) {
	reg := NewRegistry(t.TempDir())

	if err := reg.Enable("provider-web-search", "", []string{"henry"}); err != nil {
		t.Fatalf("enable provider tool: %v", err)
	}

	webSearch := reg.Show("provider-web-search")
	if webSearch == nil {
		t.Fatalf("provider-web-search not found after enable")
	}
	if webSearch.State != "restricted" {
		t.Fatalf("provider-web-search state = %q, want restricted", webSearch.State)
	}
	if len(webSearch.Agents) != 1 || webSearch.Agents[0] != "henry" {
		t.Fatalf("provider-web-search agents = %#v, want [henry]", webSearch.Agents)
	}

	if err := reg.Disable("provider-web-search"); err != nil {
		t.Fatalf("disable provider tool: %v", err)
	}
	webSearch = reg.Show("provider-web-search")
	if webSearch == nil || webSearch.State != "disabled" {
		t.Fatalf("provider-web-search after disable = %#v, want disabled entry", webSearch)
	}
}
