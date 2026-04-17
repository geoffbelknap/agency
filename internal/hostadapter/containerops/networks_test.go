package containerops

import (
	"errors"
	"testing"
)

func TestIsNetworkNotFoundRecognizesNerdctlErrors(t *testing.T) {
	cases := []string{
		`time="2026-04-17T22:36:01Z" level=error msg="no network found matching: agency-agent-internal"`,
		`time="2026-04-17T22:36:01Z" level=fatal msg="unable to find any network matching the provided request"`,
	}
	for _, msg := range cases {
		if !IsNetworkNotFound(errors.New(msg)) {
			t.Fatalf("IsNetworkNotFound(%q) = false", msg)
		}
	}
}
