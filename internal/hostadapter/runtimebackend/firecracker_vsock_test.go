package runtimebackend

import (
	"bufio"
	"context"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestFirecrackerVsockBridgeForwardsToTarget(t *testing.T) {
	targetPath := shortSocketPath(t, "target.sock")
	target, err := net.Listen("unix", targetPath)
	if err != nil {
		t.Fatal(err)
	}
	defer target.Close()
	go func() {
		conn, err := target.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		line, _ := bufio.NewReader(conn).ReadString('\n')
		_, _ = conn.Write([]byte("echo:" + line))
	}()

	factory := &FirecrackerVsockListenerFactory{StateDir: shortSocketTempDir(t)}
	bridge, err := factory.Start(context.Background(), "alice", map[int]string{
		9999: "unix://" + targetPath,
	})
	if err != nil {
		t.Fatalf("Start returned error: %v", err)
	}
	defer factory.Stop("alice")

	conn, err := net.Dial("unix", bridge.Paths[9999])
	if err != nil {
		t.Fatalf("dial bridge: %v", err)
	}
	defer conn.Close()
	if _, err := conn.Write([]byte("ping\n")); err != nil {
		t.Fatalf("write bridge: %v", err)
	}
	got, err := bufio.NewReader(conn).ReadString('\n')
	if err != nil {
		t.Fatalf("read bridge: %v", err)
	}
	if got != "echo:ping\n" {
		t.Fatalf("bridge response = %q", got)
	}
}

func TestFirecrackerVsockBridgeStopUnlinksSockets(t *testing.T) {
	targetPath := shortSocketPath(t, "target.sock")
	target, err := net.Listen("unix", targetPath)
	if err != nil {
		t.Fatal(err)
	}
	defer target.Close()

	stateDir := shortSocketTempDir(t)
	factory := &FirecrackerVsockListenerFactory{StateDir: stateDir}
	bridge, err := factory.Start(context.Background(), "alice", map[int]string{
		9999: "unix://" + targetPath,
	})
	if err != nil {
		t.Fatalf("Start returned error: %v", err)
	}
	socketPath := bridge.Paths[9999]
	if _, err := os.Stat(socketPath); err != nil {
		t.Fatalf("socket missing before stop: %v", err)
	}
	factory.Stop("alice")
	if _, err := os.Stat(socketPath); !os.IsNotExist(err) {
		t.Fatalf("socket still present after stop: %v", err)
	}
	if _, err := os.Stat(filepath.Join(stateDir, "alice")); !os.IsNotExist(err) {
		t.Fatalf("runtime dir still present after stop: %v", err)
	}
}

func TestFirecrackerVsockBridgeRestoresPersistedTargets(t *testing.T) {
	targetPath := shortSocketPath(t, "target.sock")
	target, err := net.Listen("unix", targetPath)
	if err != nil {
		t.Fatal(err)
	}
	defer target.Close()

	stateDir := shortSocketTempDir(t)
	firstFactory := &FirecrackerVsockListenerFactory{StateDir: stateDir}
	first, err := firstFactory.Start(context.Background(), "alice", map[int]string{
		9999: "unix://" + targetPath,
	})
	if err != nil {
		t.Fatalf("Start returned error: %v", err)
	}
	for _, listener := range first.listeners {
		_ = listener.Close()
	}

	restoredFactory := &FirecrackerVsockListenerFactory{StateDir: stateDir}
	restored, err := restoredFactory.Restore(context.Background(), "alice")
	if err != nil {
		t.Fatalf("Restore returned error: %v", err)
	}
	defer restoredFactory.Stop("alice")
	if restored.Targets[9999] != "unix://"+targetPath {
		t.Fatalf("restored targets = %#v", restored.Targets)
	}
	if _, err := os.Stat(restored.Paths[9999]); err != nil {
		t.Fatalf("restored socket missing: %v", err)
	}
}

func TestFirecrackerVsockTargetParsesNetworkPrefixes(t *testing.T) {
	for _, tt := range []struct {
		target  string
		network string
		address string
	}{
		{"127.0.0.1:9999", "tcp", "127.0.0.1:9999"},
		{"tcp://127.0.0.1:9999", "tcp", "127.0.0.1:9999"},
		{"unix:///tmp/service.sock", "unix", "/tmp/service.sock"},
	} {
		network, address := firecrackerVsockTarget(tt.target)
		if network != tt.network || address != tt.address {
			t.Fatalf("firecrackerVsockTarget(%q) = %q %q", tt.target, network, address)
		}
	}
}

func TestFirecrackerGuestVsockTargetHandshake(t *testing.T) {
	socketPath := shortSocketPath(t, "vsock.sock")
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		line, _ := bufio.NewReader(conn).ReadString('\n')
		if line != "CONNECT 8081\n" {
			return
		}
		_, _ = conn.Write([]byte("OK 1073741824\n"))
		line, _ = bufio.NewReader(conn).ReadString('\n')
		_, _ = conn.Write([]byte("echo:" + line))
	}()

	conn, err := dialFirecrackerVsockTarget(context.Background(), FirecrackerGuestVsockTarget(socketPath, 8081))
	if err != nil {
		t.Fatalf("dialFirecrackerVsockTarget returned error: %v", err)
	}
	defer conn.Close()
	if _, err := conn.Write([]byte("ping\n")); err != nil {
		t.Fatalf("write target: %v", err)
	}
	got, err := bufio.NewReader(conn).ReadString('\n')
	if err != nil {
		t.Fatalf("read target: %v", err)
	}
	if got != "echo:ping\n" {
		t.Fatalf("guest vsock response = %q", got)
	}
}

func TestFirecrackerGuestVsockTargetRejectsBadAck(t *testing.T) {
	socketPath := shortSocketPath(t, "vsock.sock")
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		_, _ = bufio.NewReader(conn).ReadString('\n')
		_, _ = conn.Write([]byte("ERR\n"))
	}()

	_, err = dialFirecrackerVsockTarget(context.Background(), FirecrackerGuestVsockTarget(socketPath, 8081))
	if err == nil {
		t.Fatal("expected bad ack to fail")
	}
}

func TestFirecrackerVsockBridgeRejectsInvalidConfig(t *testing.T) {
	factory := &FirecrackerVsockListenerFactory{StateDir: t.TempDir()}
	if _, err := factory.Start(context.Background(), "", map[int]string{9999: "unix:///tmp/x"}); err == nil {
		t.Fatal("expected empty runtime id to fail")
	}
	if _, err := factory.Start(context.Background(), "alice", nil); err == nil {
		t.Fatal("expected empty targets to fail")
	}
	if _, err := factory.Start(context.Background(), "alice", map[int]string{0: "unix:///tmp/x"}); err == nil {
		t.Fatal("expected invalid port to fail")
	}
	if _, err := factory.Start(context.Background(), "alice", map[int]string{9999: ""}); err == nil {
		t.Fatal("expected empty target to fail")
	}
}

func TestFirecrackerVsockBridgeReplacesExistingBridge(t *testing.T) {
	targetPath := shortSocketPath(t, "target.sock")
	target, err := net.Listen("unix", targetPath)
	if err != nil {
		t.Fatal(err)
	}
	defer target.Close()

	factory := &FirecrackerVsockListenerFactory{StateDir: shortSocketTempDir(t)}
	first, err := factory.Start(context.Background(), "alice", map[int]string{9999: "unix://" + targetPath})
	if err != nil {
		t.Fatal(err)
	}
	firstPath := first.Paths[9999]
	runtimeDir := filepath.Join(factory.StateDir, "alice")
	keepFile := filepath.Join(runtimeDir, "firecracker.json")
	if err := os.WriteFile(keepFile, []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	second, err := factory.Start(context.Background(), "alice", map[int]string{10000: "unix://" + targetPath})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(firstPath); !os.IsNotExist(err) {
		t.Fatalf("old socket still present: %v", err)
	}
	if _, err := os.Stat(second.Paths[10000]); err != nil {
		t.Fatalf("new socket missing: %v", err)
	}
	if _, err := os.Stat(keepFile); err != nil {
		t.Fatalf("runtime file should survive bridge replacement: %v", err)
	}
	factory.Stop("alice")
	time.Sleep(10 * time.Millisecond)
}
