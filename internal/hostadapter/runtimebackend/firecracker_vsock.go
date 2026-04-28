package runtimebackend

import (
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
)

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
	runtimeID = strings.TrimSpace(runtimeID)
	if runtimeID == "" {
		return nil, fmt.Errorf("firecracker vsock bridge: runtime id is required")
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
		existing.stop()
	}

	bridgeCtx, cancel := context.WithCancel(ctx)
	dir := filepath.Join(f.stateDir(), runtimeID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		cancel()
		return nil, fmt.Errorf("create firecracker vsock dir: %w", err)
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
			bridge.stop()
			return nil, fmt.Errorf("firecracker vsock bridge: invalid port %d", port)
		}
		target = strings.TrimSpace(target)
		if target == "" {
			bridge.stop()
			return nil, fmt.Errorf("firecracker vsock bridge: target for port %d is empty", port)
		}
		path := bridge.UDSBase + "_" + strconv.Itoa(port)
		_ = os.Remove(path)
		listener, err := net.Listen("unix", path)
		if err != nil {
			bridge.stop()
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
	f.mu.Lock()
	defer f.mu.Unlock()
	bridge := f.bridges[runtimeID]
	if bridge == nil {
		return
	}
	bridge.stop()
	delete(f.bridges, runtimeID)
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
				continue
			}
		}
		go proxyFirecrackerVsockConn(ctx, conn, target)
	}
}

func (b *FirecrackerVsockBridge) stop() {
	if b.cancel != nil {
		b.cancel()
	}
	for _, listener := range b.listeners {
		_ = listener.Close()
	}
	for _, path := range b.Paths {
		_ = os.Remove(path)
	}
	if b.dir != "" {
		_ = os.RemoveAll(b.dir)
	}
}

func proxyFirecrackerVsockConn(ctx context.Context, guest net.Conn, target string) {
	defer guest.Close()
	network, address := firecrackerVsockTarget(target)
	host, err := (&net.Dialer{}).DialContext(ctx, network, address)
	if err != nil {
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
