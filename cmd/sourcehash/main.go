package main

import (
	"fmt"
	"os"

	"github.com/geoffbelknap/agency/internal/images"
)

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: sourcehash <service>")
		os.Exit(2)
	}

	hash, err := images.SourceFingerprintForService(os.Args[1], ".")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	fmt.Println(hash)
}
