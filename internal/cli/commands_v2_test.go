package cli

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"

	"github.com/geoffbelknap/agency/internal/apiclient"
	"github.com/spf13/cobra"
)

func TestCommands_V2RootsExist(t *testing.T) {
	root := &cobra.Command{Use: "agency"}
	RegisterCommands(root)

	names := []string{}
	for _, cmd := range root.Commands() {
		names = append(names, cmd.Name())
	}
	for _, want := range []string{"package", "instance", "authz"} {
		if !slices.Contains(names, want) {
			t.Fatalf("missing root command %q in %v", want, names)
		}
	}
}

func TestInstanceRuntimeSubcommandExists(t *testing.T) {
	root := &cobra.Command{Use: "agency"}
	RegisterCommands(root)

	instanceIndex := slices.IndexFunc(root.Commands(), func(c *cobra.Command) bool { return c.Name() == "instance" })
	if instanceIndex < 0 {
		t.Fatal("missing instance root command")
	}
	instance := root.Commands()[instanceIndex]
	instanceNames := []string{}
	for _, cmd := range instance.Commands() {
		instanceNames = append(instanceNames, cmd.Name())
	}
	for _, want := range []string{"list", "create-from-package", "show", "validate", "update", "apply", "runtime"} {
		if !slices.Contains(instanceNames, want) {
			t.Fatalf("missing instance command %q in %v", want, instanceNames)
		}
	}
	runtimeIndex := slices.IndexFunc(instance.Commands(), func(c *cobra.Command) bool { return c.Name() == "runtime" })
	if runtimeIndex < 0 {
		t.Fatal("missing instance runtime subcommand")
	}
	runtime := instance.Commands()[runtimeIndex]
	names := []string{}
	for _, cmd := range runtime.Commands() {
		names = append(names, cmd.Name())
	}
	for _, want := range []string{"manifest", "compile", "reconcile", "start", "stop", "invoke"} {
		if !slices.Contains(names, want) {
			t.Fatalf("missing runtime command %q in %v", want, names)
		}
	}
}

func TestResolveInstanceRefByName(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/instances" {
			t.Fatalf("unexpected path %q", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"instances": []map[string]any{
				{"id": "inst_123", "name": "drive-alpha"},
			},
		})
	}))
	defer srv.Close()

	c := apiclient.NewClient(srv.URL)
	id, err := resolveInstanceRef(context.Background(), c, "drive-alpha")
	if err != nil {
		t.Fatalf("resolveInstanceRef(): %v", err)
	}
	if id != "inst_123" {
		t.Fatalf("id = %q, want inst_123", id)
	}
}

func TestResolveInstanceRefRejectsAmbiguousNames(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"instances": []map[string]any{
				{"id": "inst_123", "name": "shared-name"},
				{"id": "inst_456", "name": "shared-name"},
			},
		})
	}))
	defer srv.Close()

	c := apiclient.NewClient(srv.URL)
	_, err := resolveInstanceRef(context.Background(), c, "shared-name")
	if err == nil {
		t.Fatal("expected ambiguity error")
	}
}
