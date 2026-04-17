package agents

import (
	"fmt"

	"github.com/geoffbelknap/agency/internal/hostadapter/containerops"
)

func enforcerWSURLFromBindings(defaultURL string, hostBindings containerops.PortMap, networkBindings containerops.PortMap) string {
	if url, ok := wsURLFromPortBindings(hostBindings[containerops.Port("8081/tcp")]); ok {
		return url
	}
	if url, ok := wsURLFromPortBindings(networkBindings[containerops.Port("8081/tcp")]); ok {
		return url
	}
	return defaultURL
}

func wsURLFromPortBindings(bindings []containerops.PortBinding) (string, bool) {
	if len(bindings) == 0 || bindings[0].HostPort == "" {
		return "", false
	}
	hostIP := bindings[0].HostIP
	if hostIP == "" || hostIP == "0.0.0.0" {
		hostIP = "127.0.0.1"
	}
	return fmt.Sprintf("ws://%s:%s/ws", hostIP, bindings[0].HostPort), true
}
