package consent

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const ConfigFileName = "consent-verification-keys.json"
const DeploymentRefFileName = "consent-deployment.json"

type VerificationConfig struct {
	DeploymentID     string            `json:"deployment_id"`
	MaxTTLSeconds    int               `json:"max_ttl_seconds"`
	ClockSkewMillis  int               `json:"clock_skew_millis"`
	VerificationKeys map[string]string `json:"verification_keys"`
}

type DeploymentRef struct {
	DeploymentID string `json:"deployment_id"`
}

func ConfigPath(dir string) string {
	return filepath.Join(dir, ConfigFileName)
}

func DeploymentRefPath(agentDir string) string {
	return filepath.Join(agentDir, DeploymentRefFileName)
}

func DeploymentDir(home, deploymentID string) string {
	return filepath.Join(home, "deployments", deploymentID)
}

func LoadVerificationConfig(agentDir string) (*VerificationConfig, error) {
	path := ConfigPath(agentDir)
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var cfg VerificationConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	if cfg.VerificationKeys == nil {
		cfg.VerificationKeys = map[string]string{}
	}
	return &cfg, nil
}

func (c *VerificationConfig) Normalize() {
	if c.VerificationKeys == nil {
		c.VerificationKeys = map[string]string{}
	}
	if c.MaxTTLSeconds <= 0 {
		c.MaxTTLSeconds = 900
	}
	if c.ClockSkewMillis <= 0 {
		c.ClockSkewMillis = int(DefaultClockSkew.Milliseconds())
	}
}

func (c *VerificationConfig) Write(agentDir string) error {
	c.Normalize()
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(agentDir, 0o755); err != nil {
		return err
	}
	return os.WriteFile(ConfigPath(agentDir), data, 0o644)
}

func LoadDeploymentRef(agentDir string) (*DeploymentRef, error) {
	data, err := os.ReadFile(DeploymentRefPath(agentDir))
	if err != nil {
		return nil, err
	}
	var ref DeploymentRef
	if err := json.Unmarshal(data, &ref); err != nil {
		return nil, err
	}
	return &ref, nil
}

func (r *DeploymentRef) Write(agentDir string) error {
	data, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(agentDir, 0o755); err != nil {
		return err
	}
	return os.WriteFile(DeploymentRefPath(agentDir), data, 0o644)
}

func NextKeyID(existing map[string]string, deploymentID string) string {
	maxVersion := 0
	prefix := deploymentID + ":v"
	for keyID := range existing {
		if !strings.HasPrefix(keyID, prefix) {
			continue
		}
		var version int
		if _, err := fmt.Sscanf(strings.TrimPrefix(keyID, prefix), "%d", &version); err == nil && version > maxVersion {
			maxVersion = version
		}
	}
	return fmt.Sprintf("%s:v%d", deploymentID, maxVersion+1)
}

func EncodePublicKey(pub []byte) string {
	return base64.RawURLEncoding.EncodeToString(pub)
}

func EncodePrivateKey(priv []byte) string {
	return base64.RawURLEncoding.EncodeToString(priv)
}

func SortedKeyIDs(keys map[string]string) []string {
	ids := make([]string, 0, len(keys))
	for keyID := range keys {
		ids = append(ids, keyID)
	}
	sort.Strings(ids)
	return ids
}
