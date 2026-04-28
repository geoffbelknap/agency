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

func main() {
	raw := flag.String("bridges", envOr("AGENCY_VSOCK_HTTP_BRIDGES", defaultBridgeSpec), "comma-separated listen=cid:port bridges")
	flag.Parse()
	specs, err := parseBridgeSpecs(*raw)
	if err != nil {
		log.Fatal(err)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, specs); err != nil {
		log.Fatal(err)
	}
}

func run(ctx context.Context, specs []bridgeSpec) error {
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
		listeners = append(listeners, listener)
		go accept(ctx, listener, spec)
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
				continue
			}
		}
		go handle(ctx, conn, spec)
	}
}

func handle(ctx context.Context, local net.Conn, spec bridgeSpec) {
	defer local.Close()
	remote, err := dialVsock(spec.CID, spec.Port)
	if err != nil {
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
