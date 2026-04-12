package cli

import (
	"slices"
	"testing"

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
