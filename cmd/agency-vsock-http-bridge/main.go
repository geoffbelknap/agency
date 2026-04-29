//go:build linux

package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"unsafe"

	"golang.org/x/sys/unix"
)

const defaultBridgeSpec = "127.0.0.1:3128=2:3128,127.0.0.1:8081=2:8081"

type bridgeSpec struct {
	Listen string
	CID    uint32
	Port   uint32
}

type guestListenerSpec struct {
	Port   uint32
	Target string
}

func main() {
	raw := flag.String("bridges", envOr("AGENCY_VSOCK_HTTP_BRIDGES", defaultBridgeSpec), "comma-separated listen=cid:port bridges")
	rawGuestListeners := flag.String("guest-listeners", envOr("AGENCY_VSOCK_HTTP_GUEST_LISTENERS", ""), "comma-separated port=target listeners exposed over guest vsock")
	flag.Parse()
	specs, err := parseBridgeSpecs(*raw)
	if err != nil {
		log.Fatal(err)
	}
	guestListeners, err := parseGuestListenerSpecs(*rawGuestListeners)
	if err != nil {
		log.Fatal(err)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, specs, guestListeners); err != nil {
		log.Fatal(err)
	}
}

func run(ctx context.Context, specs []bridgeSpec, guestListeners []guestListenerSpec) error {
	if err := bringUpLoopback(); err != nil {
		return err
	}
	var listeners []net.Listener
	for _, spec := range specs {
		listener, err := net.Listen("tcp", spec.Listen)
		if err != nil {
			closeListeners(listeners)
			return fmt.Errorf("listen %s: %w", spec.Listen, err)
		}
		log.Printf("tcp bridge listening on %s -> vsock %d:%d", spec.Listen, spec.CID, spec.Port)
		listeners = append(listeners, listener)
		go accept(ctx, listener, spec)
	}
	for _, spec := range guestListeners {
		fd, err := listenVsock(spec.Port)
		if err != nil {
			closeListeners(listeners)
			return fmt.Errorf("listen vsock %d: %w", spec.Port, err)
		}
		log.Printf("vsock listener on port %d -> tcp %s", spec.Port, spec.Target)
		go acceptVsock(ctx, fd, spec)
	}
	<-ctx.Done()
	closeListeners(listeners)
	return nil
}

type ifreqFlags struct {
	Name  [unix.IFNAMSIZ]byte
	Flags int16
	_     [22]byte
}

func bringUpLoopback() error {
	fd, err := unix.Socket(unix.AF_INET, unix.SOCK_DGRAM, 0)
	if err != nil {
		return fmt.Errorf("open loopback control socket: %w", err)
	}
	defer unix.Close(fd)

	var ifr ifreqFlags
	copy(ifr.Name[:], "lo")
	if _, _, errno := unix.Syscall(unix.SYS_IOCTL, uintptr(fd), uintptr(unix.SIOCGIFFLAGS), uintptr(unsafe.Pointer(&ifr))); errno != 0 {
		return fmt.Errorf("read loopback flags: %w", errno)
	}
	ifr.Flags |= unix.IFF_UP
	if _, _, errno := unix.Syscall(unix.SYS_IOCTL, uintptr(fd), uintptr(unix.SIOCSIFFLAGS), uintptr(unsafe.Pointer(&ifr))); errno != 0 {
		return fmt.Errorf("bring loopback up: %w", errno)
	}
	return nil
}

func accept(ctx context.Context, listener net.Listener, spec bridgeSpec) {
	for {
		conn, err := listener.Accept()
		if err != nil {
			select {
			case <-ctx.Done():
				return
			default:
				log.Printf("tcp bridge accept failed: %v", err)
				continue
			}
		}
		log.Printf("tcp bridge accepted local connection on %s", spec.Listen)
		go handle(ctx, conn, spec)
	}
}

func handle(ctx context.Context, local net.Conn, spec bridgeSpec) {
	defer local.Close()
	remote, err := dialVsock(spec.CID, spec.Port)
	if err != nil {
		log.Printf("dial vsock %d:%d failed: %v", spec.CID, spec.Port, err)
		return
	}
	defer remote.Close()
	errc := make(chan error, 2)
	go func() {
		_, err := io.Copy(remote, local)
		errc <- err
	}()
	go func() {
		_, err := io.Copy(local, remote)
		errc <- err
	}()
	select {
	case <-ctx.Done():
	case <-errc:
	}
}

func listenVsock(port uint32) (int, error) {
	fd, err := unix.Socket(unix.AF_VSOCK, unix.SOCK_STREAM, 0)
	if err != nil {
		return -1, err
	}
	if err := unix.Bind(fd, &unix.SockaddrVM{CID: unix.VMADDR_CID_ANY, Port: port}); err != nil {
		_ = unix.Close(fd)
		return -1, err
	}
	if err := unix.Listen(fd, 128); err != nil {
		_ = unix.Close(fd)
		return -1, err
	}
	return fd, nil
}

func acceptVsock(ctx context.Context, fd int, spec guestListenerSpec) {
	go func() {
		<-ctx.Done()
		_ = unix.Close(fd)
	}()
	for {
		connFD, _, err := unix.Accept(fd)
		if err != nil {
			select {
			case <-ctx.Done():
				return
			default:
				log.Printf("vsock accept failed on port %d: %v", spec.Port, err)
				continue
			}
		}
		go handleVsockGuestConnection(ctx, connFD, spec.Target)
	}
}

func handleVsockGuestConnection(ctx context.Context, fd int, target string) {
	local := os.NewFile(uintptr(fd), "vsock")
	defer local.Close()
	remote, err := (&net.Dialer{}).DialContext(ctx, "tcp", target)
	if err != nil {
		log.Printf("dial guest listener target %s failed: %v", target, err)
		return
	}
	defer remote.Close()
	errc := make(chan error, 2)
	go func() {
		_, err := io.Copy(remote, local)
		errc <- err
	}()
	go func() {
		_, err := io.Copy(local, remote)
		errc <- err
	}()
	select {
	case <-ctx.Done():
	case <-errc:
	}
}

func dialVsock(cid, port uint32) (io.ReadWriteCloser, error) {
	fd, err := unix.Socket(unix.AF_VSOCK, unix.SOCK_STREAM, 0)
	if err != nil {
		return nil, err
	}
	if err := unix.Connect(fd, &unix.SockaddrVM{CID: cid, Port: port}); err != nil {
		_ = unix.Close(fd)
		return nil, err
	}
	file := os.NewFile(uintptr(fd), "vsock")
	return file, nil
}

func parseGuestListenerSpecs(raw string) ([]guestListenerSpec, error) {
	var specs []guestListenerSpec
	for _, item := range strings.Split(raw, ",") {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		parts := strings.SplitN(item, "=", 2)
		if len(parts) != 2 {
			return nil, fmt.Errorf("invalid guest listener %q", item)
		}
		port, err := parseUint32(parts[0])
		if err != nil {
			return nil, fmt.Errorf("invalid guest listener port %q: %w", parts[0], err)
		}
		target := strings.TrimSpace(parts[1])
		if target == "" {
			return nil, fmt.Errorf("guest listener %d target is empty", port)
		}
		specs = append(specs, guestListenerSpec{Port: port, Target: target})
	}
	return specs, nil
}

func parseBridgeSpecs(raw string) ([]bridgeSpec, error) {
	var specs []bridgeSpec
	for _, item := range strings.Split(raw, ",") {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		parts := strings.SplitN(item, "=", 2)
		if len(parts) != 2 {
			return nil, fmt.Errorf("invalid bridge %q", item)
		}
		target := strings.SplitN(parts[1], ":", 2)
		if len(target) != 2 {
			return nil, fmt.Errorf("invalid bridge target %q", parts[1])
		}
		cid, err := parseUint32(target[0])
		if err != nil {
			return nil, fmt.Errorf("invalid bridge CID %q: %w", target[0], err)
		}
		port, err := parseUint32(target[1])
		if err != nil {
			return nil, fmt.Errorf("invalid bridge port %q: %w", target[1], err)
		}
		specs = append(specs, bridgeSpec{Listen: strings.TrimSpace(parts[0]), CID: cid, Port: port})
	}
	if len(specs) == 0 {
		return nil, fmt.Errorf("no bridges configured")
	}
	return specs, nil
}

func parseUint32(raw string) (uint32, error) {
	value, err := strconv.ParseUint(strings.TrimSpace(raw), 10, 32)
	return uint32(value), err
}

func closeListeners(listeners []net.Listener) {
	var wg sync.WaitGroup
	for _, listener := range listeners {
		wg.Add(1)
		go func(listener net.Listener) {
			defer wg.Done()
			_ = listener.Close()
		}(listener)
	}
	wg.Wait()
}

func envOr(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}
