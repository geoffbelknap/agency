package main

import "testing"

func TestGatewayConstraintWSURL(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		agent  string
		expect string
	}{
		{"http", "http://gateway:8200", "alpha", "ws://gateway:8200/api/v1/agents/alpha/context/ws"},
		{"https", "https://gateway.example.com", "alpha", "wss://gateway.example.com/api/v1/agents/alpha/context/ws"},
		{"bare", "gateway:8200", "alpha", "ws://gateway:8200/api/v1/agents/alpha/context/ws"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := gatewayConstraintWSURL(tc.input, tc.agent); got != tc.expect {
				t.Fatalf("gatewayConstraintWSURL(%q, %q) = %q, want %q", tc.input, tc.agent, got, tc.expect)
			}
		})
	}
}
