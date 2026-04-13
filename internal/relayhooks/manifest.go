package relayhooks

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	instancepkg "github.com/geoffbelknap/agency/internal/instances"
	"github.com/geoffbelknap/agency/internal/models"
	runpkg "github.com/geoffbelknap/agency/internal/runtime"
	"gopkg.in/yaml.v3"
)

const manifestVersion = "v1alpha1"

type Manifest struct {
	Version string  `json:"version"`
	Routes  []Route `json:"routes"`
}

type Route struct {
	RouteID       string `json:"route_id"`
	InstanceID    string `json:"instance_id"`
	InstanceName  string `json:"instance_name"`
	NodeID        string `json:"node_id"`
	Provider      string `json:"provider"`
	PublishedName string `json:"published_name"`
	PublicPath    string `json:"public_path"`
	LocalPath     string `json:"local_path"`
	Status        string `json:"status"`
}

type Store struct {
	Home string
}

func (s Store) Save(m *Manifest) error {
	if strings.TrimSpace(s.Home) == "" {
		return fmt.Errorf("home is required")
	}
	if m == nil {
		return fmt.Errorf("manifest is required")
	}
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal relay webhook manifest: %w", err)
	}
	if err := os.MkdirAll(s.Home, 0o755); err != nil {
		return fmt.Errorf("create agency home: %w", err)
	}
	if err := os.WriteFile(filepath.Join(s.Home, "relay-webhooks.json"), data, 0o644); err != nil {
		return fmt.Errorf("write relay webhook manifest: %w", err)
	}
	return nil
}

func Build(ctx context.Context, instances *instancepkg.Store, hmacKey []byte) (*Manifest, error) {
	if instances == nil {
		return nil, fmt.Errorf("instance store is required")
	}
	items, err := instances.List(ctx)
	if err != nil {
		return nil, err
	}
	out := &Manifest{Version: manifestVersion}
	for _, inst := range items {
		instanceDir, err := instances.InstanceDir(inst.ID)
		if err != nil {
			return nil, err
		}
		manifest, err := runpkg.NewStore(instanceDir).LoadManifest()
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, err
		}
		for _, node := range manifest.Runtime.Nodes {
			if node.Kind != "connector.ingress" || node.Ingress == nil {
				continue
			}
			route, ok, err := buildRoute(inst, node, hmacKey)
			if err != nil {
				return nil, err
			}
			if ok {
				out.Routes = append(out.Routes, route)
			}
		}
	}
	sort.Slice(out.Routes, func(i, j int) bool {
		if out.Routes[i].InstanceID != out.Routes[j].InstanceID {
			return out.Routes[i].InstanceID < out.Routes[j].InstanceID
		}
		return out.Routes[i].NodeID < out.Routes[j].NodeID
	})
	return out, nil
}

func buildRoute(inst *instancepkg.Instance, node runpkg.RuntimeNode, hmacKey []byte) (Route, bool, error) {
	var cfg models.ConnectorConfig
	if err := yaml.Unmarshal([]byte(node.Ingress.ConnectorYAML), &cfg); err != nil {
		return Route{}, false, fmt.Errorf("parse ingress connector yaml for %s/%s: %w", inst.ID, node.NodeID, err)
	}
	if cfg.Source.Type != "webhook" {
		return Route{}, false, nil
	}
	localPath := cfg.Source.Path
	if localPath == nil || strings.TrimSpace(*localPath) == "" {
		path := "/webhooks/" + cfg.Name
		localPath = &path
	}
	routeID := routeIDFor(hmacKey, inst.ID, node.NodeID, strings.TrimSpace(*localPath))
	return Route{
		RouteID:       routeID,
		InstanceID:    inst.ID,
		InstanceName:  inst.Name,
		NodeID:        node.NodeID,
		Provider:      inferProvider(node.Package.Name, cfg.Name),
		PublishedName: node.Ingress.PublishedName,
		PublicPath:    "/webhooks/" + routeID,
		LocalPath:     strings.TrimSpace(*localPath),
		Status:        "active",
	}, true, nil
}

func routeIDFor(hmacKey []byte, instanceID, nodeID, localPath string) string {
	if len(hmacKey) == 0 {
		sum := sha256.Sum256([]byte(instanceID + "\x00" + nodeID + "\x00" + localPath))
		return "wh_" + hex.EncodeToString(sum[:10])
	}
	mac := hmac.New(sha256.New, hmacKey)
	_, _ = mac.Write([]byte(instanceID))
	_, _ = mac.Write([]byte{0})
	_, _ = mac.Write([]byte(nodeID))
	_, _ = mac.Write([]byte{0})
	_, _ = mac.Write([]byte(localPath))
	return "wh_" + hex.EncodeToString(mac.Sum(nil)[:10])
}

func inferProvider(packageName, connectorName string) string {
	name := strings.ToLower(strings.TrimSpace(packageName))
	if name == "" {
		name = strings.ToLower(strings.TrimSpace(connectorName))
	}
	switch {
	case strings.Contains(name, "slack"):
		return "slack"
	case strings.Contains(name, "github"):
		return "github"
	case strings.Contains(name, "stripe"):
		return "stripe"
	case strings.Contains(name, "google"):
		return "google"
	default:
		return name
	}
}
