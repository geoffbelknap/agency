package images

import (
	"context"
	"testing"

	"github.com/docker/docker/client"
)

func TestImageExists_False(t *testing.T) {
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		t.Skip("Docker not available:", err)
	}
	exists, err := imageExists(context.Background(), cli, "agency-nonexistent-test-image-xyz:latest")
	if err != nil {
		t.Skip("Docker not responding:", err)
	}
	if exists {
		t.Error("expected nonexistent image to return false")
	}
}
