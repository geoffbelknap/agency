package policy

import (
	"path/filepath"
	"testing"
)

func TestEngine_ComputeForScope_AppendsInstanceScopeStep(t *testing.T) {
	home := filepath.Join(testdataDir(t), "simple_two_level")
	e := NewEngine(home)

	ep := e.ComputeForScope("test", "instance:community-admin")
	if len(ep.Chain) == 0 {
		t.Fatal("expected policy chain entries")
	}
	last := ep.Chain[len(ep.Chain)-1]
	if last.Level != "instance_scope" {
		t.Fatalf("last chain level = %q, want instance_scope", last.Level)
	}
	if last.Detail != "instance:community-admin" {
		t.Fatalf("last chain detail = %q, want instance:community-admin", last.Detail)
	}
}
