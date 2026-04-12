package runtime

import (
	"crypto/ed25519"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/geoffbelknap/agency/internal/config"
	agencyconsent "github.com/geoffbelknap/agency/internal/consent"
)

func LoadConsentValidator(_ string, manifest *Manifest) (*agencyconsent.Validator, error) {
	if manifest == nil || manifest.Source.ConsentDeploymentID == "" {
		return nil, nil
	}
	cfg := config.Load()
	deploymentDir := agencyconsent.DeploymentDir(cfg.Home, manifest.Source.ConsentDeploymentID)
	data, err := os.ReadFile(filepath.Join(deploymentDir, agencyconsent.ConfigFileName))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read consent verification config: %w", err)
	}
	var verificationCfg agencyconsent.VerificationConfig
	if err := json.Unmarshal(data, &verificationCfg); err != nil {
		return nil, fmt.Errorf("parse consent verification config: %w", err)
	}
	verificationCfg.Normalize()
	keys := make(map[string]ed25519.PublicKey, len(verificationCfg.VerificationKeys))
	for keyID, encoded := range verificationCfg.VerificationKeys {
		pub, err := agencyconsent.ParsePublicKey(encoded)
		if err != nil {
			return nil, fmt.Errorf("parse consent public key %q: %w", keyID, err)
		}
		keys[keyID] = pub
	}
	return agencyconsent.NewValidator(
		verificationCfg.DeploymentID,
		keys,
		time.Duration(verificationCfg.MaxTTLSeconds)*time.Second,
		time.Duration(verificationCfg.ClockSkewMillis)*time.Millisecond,
	), nil
}
