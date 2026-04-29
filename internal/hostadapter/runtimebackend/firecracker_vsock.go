package runtimebackend

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/geoffbelknap/agency/internal/pkg/pathsafety"
)

const firecrackerGuestVsockTargetPrefix = "firecracker-vsock://"
const firecrackerVsockBridgeTargetsFile = "bridge-targets.json"

type FirecrackerVsockListenerFactory struct {
	StateDir string

	mu      sync.Mutex
	bridges map[string]*FirecrackerVsockBridge
}

type FirecrackerVsockBridge struct {
	RuntimeID string
	UDSBase   string
	Paths     map[int]string
	Targets   map[int]string

	dir       string
	cancel    context.CancelFunc
	listeners []net.Listener
}

func (f *FirecrackerVsockListenerFactory) Start(ctx context.Context, runtimeID string, targets map[int]string) (*FirecrackerVsockBridge, error) {
	runtimeID, err := pathsafety.Segment("firecracker runtime id", runtimeID)
	if err != nil {
		return nil, fmt.Errorf("firecracker vsock bridge: %w", err)
	}
	if len(targets) == 0 {
		return nil, fmt.Errorf("firecracker vsock bridge: no target ports configured")
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.bridges == nil {
		f.bridges = make(map[string]*FirecrackerVsockBridge)
	}
	if existing := f.bridges[runtimeID]; existing != nil {
		existing.close()
	}

	bridgeCtx, cancel := context.WithCancel(context.Background())
	dir, err := pathsafety.Join(f.stateDir(), runtimeID)
	if err != nil {
		cancel()
		return nil, err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		cancel()
		return nil, fmt.Errorf("create firecracker vsock dir: %w", err)
	}
	if err := writeFirecrackerVsockBridgeTargets(dir, targets); err != nil {
		cancel()
		return nil, err
	}
	bridge := &FirecrackerVsockBridge{
		RuntimeID: runtimeID,
		UDSBase:   filepath.Join(dir, "vsock.sock"),
		Paths:     make(map[int]string, len(targets)),
		Targets:   make(map[int]string, len(targets)),
		dir:       dir,
		cancel:    cancel,
	}
	for port, target := range targets {
		if port <= 0 || port > 65535 {
			bridge.close()
			return nil, fmt.Errorf("firecracker vsock bridge: invalid port %d", port)
		}
		target = strings.TrimSpace(target)
		if target == "" {
			bridge.close()
			return nil, fmt.Errorf("firecracker vsock bridge: target for port %d is empty", port)
		}
		path, err := pathsafety.Join(dir, "vsock.sock_"+strconv.Itoa(port))
		if err != nil {
			bridge.close()
			return nil, err
		}
		_ = os.Remove(path)
		listener, err := net.Listen("unix", path)
		if err != nil {
			bridge.close()
			return nil, fmt.Errorf("listen firecracker vsock uds %s: %w", path, err)
		}
		bridge.listeners = append(bridge.listeners, listener)
		bridge.Paths[port] = path
		bridge.Targets[port] = target
		go bridge.accept(bridgeCtx, listener, target)
	}
	f.bridges[runtimeID] = bridge
	return bridge, nil
}

func (f *FirecrackerVsockListenerFactory) Stop(runtimeID string) {
	runtimeID, err := pathsafety.Segment("firecracker runtime id", runtimeID)
	if err != nil {
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	bridge := f.bridges[runtimeID]
	if bridge == nil {
		return
	}
	bridge.stop()
	delete(f.bridges, runtimeID)
}

func (f *FirecrackerVsockListenerFactory) Restore(ctx context.Context, runtimeID string) (*FirecrackerVsockBridge, error) {
	runtimeID, err := pathsafety.Segment("firecracker runtime id", runtimeID)
	if err != nil {
		return nil, fmt.Errorf("firecracker vsock bridge: %w", err)
	}
	dir, err := pathsafety.Join(f.stateDir(), runtimeID)
	if err != nil {
		return nil, err
	}
	targets, err := readFirecrackerVsockBridgeTargets(dir)
	if err != nil {
		return nil, err
	}
	return f.Start(ctx, runtimeID, targets)
}

func (f *FirecrackerVsockListenerFactory) Bridge(runtimeID string) *FirecrackerVsockBridge {
	runtimeID, err := pathsafety.Segment("firecracker runtime id", runtimeID)
	if err != nil {
		return nil
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.bridges == nil {
		return nil
	}
	return f.bridges[runtimeID]
}

func (f *FirecrackerVsockListenerFactory) stateDir() string {
	if strings.TrimSpace(f.StateDir) != "" {
		return f.StateDir
	}
	return filepath.Join(os.TempDir(), "agency-firecracker")
}

func (b *FirecrackerVsockBridge) accept(ctx context.Context, listener net.Listener, target string) {
	for {
		conn, err := listener.Accept()
		if err != nil {
			select {
			case <-ctx.Done():
				return
			default:
				log.Printf("firecracker vsock bridge accept failed: %v", err)
				continue
			}
		}
		log.Printf("firecracker vsock bridge accepted connection")
		go proxyFirecrackerVsockConn(ctx, conn, target)
	}
}

func (b *FirecrackerVsockBridge) close() {
	if b.cancel != nil {
		b.cancel()
	}
	for _, listener := range b.listeners {
		_ = listener.Close()
	}
	for _, path := range b.Paths {
		_ = os.Remove(path)
	}
}

func (b *FirecrackerVsockBridge) stop() {
	b.close()
	if b.dir != "" {
		_ = os.RemoveAll(b.dir)
	}
}

func writeFirecrackerVsockBridgeTargets(dir string, targets map[int]string) error {
	persisted := make(map[string]string, len(targets))
	for port, target := range targets {
		persisted[strconv.Itoa(port)] = target
	}
	data, err := json.MarshalIndent(persisted, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal firecracker vsock bridge targets: %w", err)
	}
	path, err := pathsafety.Join(dir, firecrackerVsockBridgeTargetsFile)
	if err != nil {
		return err
	}
	if err := os.WriteFile(path, append(data, '\n'), 0o644); err != nil {
		return fmt.Errorf("write firecracker vsock bridge targets: %w", err)
	}
	return nil
}

func readFirecrackerVsockBridgeTargets(dir string) (map[int]string, error) {
	path, err := pathsafety.Join(dir, firecrackerVsockBridgeTargetsFile)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read firecracker vsock bridge targets: %w", err)
	}
	var persisted map[string]string
	if err := json.Unmarshal(data, &persisted); err != nil {
		return nil, fmt.Errorf("parse firecracker vsock bridge targets: %w", err)
	}
	targets := make(map[int]string, len(persisted))
	for rawPort, target := range persisted {
		port, err := strconv.Atoi(rawPort)
		if err != nil || port <= 0 || port > 65535 {
			return nil, fmt.Errorf("firecracker vsock bridge: invalid persisted port %q", rawPort)
		}
		targets[port] = target
	}
	if len(targets) == 0 {
		return nil, fmt.Errorf("firecracker vsock bridge: no persisted targets configured")
	}
	return targets, nil
}

func proxyFirecrackerVsockConn(ctx context.Context, guest net.Conn, target string) {
	defer guest.Close()
	host, err := dialFirecrackerVsockTarget(ctx, target)
	if err != nil {
		log.Printf("firecracker vsock bridge dial target failed")
		return
	}
	defer host.Close()
	errc := make(chan error, 2)
	go func() {
		_, err := io.Copy(host, guest)
		errc <- err
	}()
	go func() {
		_, err := io.Copy(guest, host)
		errc <- err
	}()
	select {
	case <-ctx.Done():
	case <-errc:
	}
}

func FirecrackerGuestVsockTarget(udsPath string, port int) string {
	return firecrackerGuestVsockTargetPrefix + strings.TrimSpace(udsPath) + ":" + strconv.Itoa(port)
}

func dialFirecrackerVsockTarget(ctx context.Context, target string) (net.Conn, error) {
	if strings.HasPrefix(target, firecrackerGuestVsockTargetPrefix) {
		udsPath, port, err := firecrackerGuestVsockTarget(target)
		if err != nil {
			return nil, err
		}
		conn, err := (&net.Dialer{}).DialContext(ctx, "unix", udsPath)
		if err != nil {
			return nil, err
		}
		if err := firecrackerConnectGuestVsock(conn, port); err != nil {
			_ = conn.Close()
			return nil, err
		}
		return conn, nil
	}
	network, address := firecrackerVsockTarget(target)
	return (&net.Dialer{}).DialContext(ctx, network, address)
}

func firecrackerGuestVsockTarget(target string) (string, int, error) {
	raw := strings.TrimPrefix(target, firecrackerGuestVsockTargetPrefix)
	idx := strings.LastIndex(raw, ":")
	if idx <= 0 || idx == len(raw)-1 {
		return "", 0, fmt.Errorf("firecracker vsock target: invalid guest target %q", target)
	}
	port, err := strconv.Atoi(raw[idx+1:])
	if err != nil || port <= 0 || port > 65535 {
		return "", 0, fmt.Errorf("firecracker vsock target: invalid guest port %q", raw[idx+1:])
	}
	return raw[:idx], port, nil
}

func firecrackerConnectGuestVsock(conn net.Conn, port int) error {
	deadline := time.Now().Add(5 * time.Second)
	_ = conn.SetDeadline(deadline)
	defer func() {
		_ = conn.SetDeadline(time.Time{})
	}()
	if _, err := fmt.Fprintf(conn, "CONNECT %d\n", port); err != nil {
		return err
	}
	line, err := readFirecrackerVsockLine(conn)
	if err != nil {
		return err
	}
	if !strings.HasPrefix(line, "OK ") {
		return fmt.Errorf("firecracker vsock target: connect rejected: %s", strings.TrimSpace(line))
	}
	return nil
}

func readFirecrackerVsockLine(conn net.Conn) (string, error) {
	var b strings.Builder
	buf := make([]byte, 1)
	for {
		n, err := conn.Read(buf)
		if n > 0 {
			b.WriteByte(buf[0])
			if buf[0] == '\n' {
				return b.String(), nil
			}
		}
		if err != nil {
			return b.String(), err
		}
	}
}

func firecrackerVsockTarget(target string) (string, string) {
	switch {
	case strings.HasPrefix(target, "unix://"):
		return "unix", strings.TrimPrefix(target, "unix://")
	case strings.HasPrefix(target, "tcp://"):
		return "tcp", strings.TrimPrefix(target, "tcp://")
	default:
		return "tcp", target
	}
}
