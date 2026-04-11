package api

import (
	"fmt"

	"github.com/docker/go-connections/nat"
)

func enforcerWSURLFromBindings(defaultURL string, hostBindings nat.PortMap, networkBindings nat.PortMap) string {
	if url, ok := wsURLFromPortBindings(hostBindings[nat.Port("8081/tcp")]); ok {
		return url
	}
	if url, ok := wsURLFromPortBindings(networkBindings[nat.Port("8081/tcp")]); ok {
		return url
	}
	return defaultURL
}

func wsURLFromPortBindings(bindings []nat.PortBinding) (string, bool) {
	if len(bindings) == 0 || bindings[0].HostPort == "" {
		return "", false
	}
	hostIP := bindings[0].HostIP
	if hostIP == "" || hostIP == "0.0.0.0" {
		hostIP = "127.0.0.1"
	}
	return fmt.Sprintf("ws://%s:%s/ws", hostIP, bindings[0].HostPort), true
}
