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
