package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/geoffbelknap/agency/internal/openapispec"
)

func main() {
	root, err := os.Getwd()
	if err != nil {
		fail(err)
	}
	specPath := filepath.Join(root, "internal", "api", "openapi.yaml")
	data, err := os.ReadFile(specPath)
	if err != nil {
		fail(err)
	}
	filtered, err := openapispec.FilterByTier(data, "core")
	if err != nil {
		fail(err)
	}
	if _, err := os.Stdout.Write(filtered); err != nil {
		fail(err)
	}
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
