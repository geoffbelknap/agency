package runtimehost

import (
	"fmt"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/docker/docker/api/types/container"
)

const (
	BaseGatewayNet   = "agency-gateway"
	BaseEgressIntNet = "agency-egress-int"
	BaseEgressExtNet = "agency-egress-ext"
	BaseOperatorNet  = "agency-operator"
)

var infraNameSanitizer = regexp.MustCompile(`[^a-z0-9-]+`)

var DefaultImages = map[string]string{
	"egress":        "agency-egress:latest",
	"comms":         "agency-comms:latest",
	"knowledge":     "agency-knowledge:latest",
	"intake":        "agency-intake:latest",
	"web-fetch":     "agency-web-fetch:latest",
	"web":           "agency-web:latest",
	"relay":         "agency-relay:latest",
	"embeddings":    "agency-embeddings:latest",
	"gateway-proxy": "agency-gateway-proxy:latest",
}

var DefaultHealthChecks = map[string]*container.HealthConfig{
	"egress": {
		Test:        []string{"CMD-SHELL", `python -c "import socket; s=socket.socket(); s.settimeout(2); s.connect(('127.0.0.1',3128)); s.close()"`},
		Interval:    10 * time.Second,
		Timeout:     3 * time.Second,
		StartPeriod: 5 * time.Second,
		Retries:     3,
	},
	"comms": {
		Test:        []string{"CMD-SHELL", `python -c "import urllib.request; urllib.request.urlopen('http://127.0.0.1:8080/health')"`},
		Interval:    10 * time.Second,
		Timeout:     3 * time.Second,
		StartPeriod: 5 * time.Second,
		Retries:     3,
	},
	"knowledge": {
		Test:        []string{"CMD-SHELL", `python -c "import urllib.request; urllib.request.urlopen('http://127.0.0.1:8080/health')"`},
		Interval:    10 * time.Second,
		Timeout:     3 * time.Second,
		StartPeriod: 5 * time.Second,
		Retries:     3,
	},
	"intake": {
		Test:        []string{"CMD-SHELL", `python -c "import urllib.request; urllib.request.urlopen('http://127.0.0.1:8080/health')"`},
		Interval:    10 * time.Second,
		Timeout:     3 * time.Second,
		StartPeriod: 5 * time.Second,
		Retries:     3,
	},
	"web-fetch": {
		Test:        []string{"CMD", "wget", "-q", "-O-", "http://127.0.0.1:8080/health"},
		Interval:    10 * time.Second,
		Timeout:     3 * time.Second,
		StartPeriod: 5 * time.Second,
		Retries:     3,
	},
	"web": {
		Test:        []string{"CMD", "wget", "-q", "-O-", "http://127.0.0.1:8280/health"},
		Interval:    10 * time.Second,
		Timeout:     3 * time.Second,
		StartPeriod: 5 * time.Second,
		Retries:     3,
	},
	"embeddings": {
		Test:        []string{"CMD-SHELL", `bash -c "echo > /dev/tcp/127.0.0.1/11434"`},
		Interval:    10 * time.Second,
		Timeout:     3 * time.Second,
		StartPeriod: 10 * time.Second,
		Retries:     3,
	},
}

func InfraInstanceName() string {
	instance := strings.TrimSpace(strings.ToLower(os.Getenv("AGENCY_INFRA_INSTANCE")))
	if instance == "" {
		return ""
	}
	instance = infraNameSanitizer.ReplaceAllString(instance, "-")
	instance = strings.Trim(instance, "-")
	return instance
}

func ScopedInfraName(base string) string {
	instance := InfraInstanceName()
	if instance == "" {
		return base
	}
	return fmt.Sprintf("%s-%s", base, instance)
}

func GatewayNetName() string {
	return ScopedInfraName(BaseGatewayNet)
}

func EgressIntNetName() string {
	return ScopedInfraName(BaseEgressIntNet)
}

func EgressExtNetName() string {
	return ScopedInfraName(BaseEgressExtNet)
}

func OperatorNetName() string {
	return ScopedInfraName(BaseOperatorNet)
}
