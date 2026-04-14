package main

import (
	"fmt"
	"os"

	"github.com/geoffbelknap/agency/internal/features"
)

func main() {
	data, err := features.WebManifestJSON()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if _, err := os.Stdout.Write(data); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
