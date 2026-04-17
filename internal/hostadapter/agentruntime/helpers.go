package agentruntime

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"net"
	"os"
	"strings"
	"time"

	"github.com/docker/docker/client"
)

const (
	prefix           = "agency"
	baseGatewayNet   = "agency-gateway"
	baseEgressIntNet = "agency-egress-int"
)

func mapToEnv(values map[string]string) []string {
	env := make([]string, 0, len(values))
	for k, v := range values {
		env = append(env, k+"="+v)
	}
	return env
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func infraInstanceName() string {
	instance := strings.TrimSpace(strings.ToLower(os.Getenv("AGENCY_INFRA_INSTANCE")))
	if instance == "" {
		return ""
	}
	instance = strings.NewReplacer("_", "-", ".", "-", "/", "-").Replace(instance)
	instance = strings.Trim(instance, "-")
	return instance
}

func scopedInfraName(base string) string {
	instance := infraInstanceName()
	if instance == "" {
		return base
	}
	return fmt.Sprintf("%s-%s", base, instance)
}

func gatewayNetName() string {
	return scopedInfraName(baseGatewayNet)
}

func egressIntNetName() string {
	return scopedInfraName(baseEgressIntNet)
}

func pickLoopbackPort() (string, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", err
	}
	defer listener.Close()
	return fmt.Sprintf("%d", listener.Addr().(*net.TCPAddr).Port), nil
}

func waitContainerRunning(ctx context.Context, cli *client.Client, name string, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		if info, err := cli.ContainerInspect(ctx, name); err == nil && info.State != nil {
			if info.State.Running {
				return nil
			}
			if info.State.Status == "exited" || info.State.Status == "dead" {
				return fmt.Errorf("container %s exited before becoming running", name)
			}
		}

		select {
		case <-ticker.C:
		case <-ctx.Done():
			return fmt.Errorf("container %s did not start within %v", name, timeout)
		}
	}
}

func WaitContainerRunning(ctx context.Context, cli *client.Client, name string, timeout time.Duration) error {
	return waitContainerRunning(ctx, cli, name, timeout)
}

func waitContainerHealthy(ctx context.Context, cli *client.Client, name string, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		if done, err := inspectContainerHealth(ctx, cli, name); done || err != nil {
			return err
		}

		select {
		case <-ticker.C:
		case <-ctx.Done():
			return fmt.Errorf("container %s did not become healthy within %v", name, timeout)
		}
	}
}

func inspectContainerHealth(ctx context.Context, cli *client.Client, name string) (bool, error) {
	info, err := cli.ContainerInspect(ctx, name)
	if err != nil {
		return false, nil
	}
	if info.State == nil {
		return false, nil
	}
	if info.State.Health == nil {
		if info.State.Running {
			return true, nil
		}
		if info.State.Status == "exited" || info.State.Status == "dead" {
			return true, fmt.Errorf("container %s exited before becoming healthy", name)
		}
		return false, nil
	}
	if info.State.Health.Status == "healthy" {
		return true, nil
	}
	if info.State.Status == "exited" || info.State.Status == "dead" {
		return true, fmt.Errorf("container %s exited before becoming healthy", name)
	}
	return false, nil
}

func generateToken(n int) string {
	buf := make([]byte, n)
	_, _ = rand.Read(buf)
	return base64.URLEncoding.EncodeToString(buf)[:n+10]
}

func splitLines(s string) []string {
	lines := strings.Split(s, "\n")
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line != "" {
			out = append(out, line)
		}
	}
	return out
}

func trimQuotes(s string) string {
	if len(s) >= 2 && s[0] == '"' && s[len(s)-1] == '"' {
		return s[1 : len(s)-1]
	}
	return s
}
