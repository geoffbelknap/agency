// agency-gateway/internal/models/routing_test.go
package models

import (
	"os"
	"path/filepath"
	"testing"
)

// --- ProviderConfig.Validate ---

func TestProviderConfig_Validate(t *testing.T) {
	tests := []struct {
		name    string
		cfg     ProviderConfig
		wantErr bool
	}{
		{
			name:    "valid https",
			cfg:     ProviderConfig{APIBase: "https://api.anthropic.com", AuthEnv: "ANTHROPIC_API_KEY"},
			wantErr: false,
		},
		{
			name:    "valid http no auth",
			cfg:     ProviderConfig{APIBase: "http://localhost:8080"},
			wantErr: false,
		},
		{
			name:    "blocked host AWS metadata",
			cfg:     ProviderConfig{APIBase: "http://169.254.169.254/api"},
			wantErr: true,
		},
		{
			name:    "blocked host GCP metadata",
			cfg:     ProviderConfig{APIBase: "https://metadata.google.internal"},
			wantErr: true,
		},
		{
			name:    "raw IP on HTTPS",
			cfg:     ProviderConfig{APIBase: "https://1.2.3.4/api"},
			wantErr: true,
		},
		{
			name:    "raw IP on HTTP is OK",
			cfg:     ProviderConfig{APIBase: "http://192.168.1.1:8080"},
			wantErr: false,
		},
		{
			name:    "bad auth_env pattern",
			cfg:     ProviderConfig{APIBase: "https://api.example.com", AuthEnv: "MY_KEY_STUFF"},
			wantErr: true,
		},
		{
			name:    "valid auth_env TOKEN",
			cfg:     ProviderConfig{APIBase: "https://api.example.com", AuthEnv: "OPENAI_TOKEN"},
			wantErr: false,
		},
		{
			name:    "valid auth_env SECRET",
			cfg:     ProviderConfig{APIBase: "https://api.example.com", AuthEnv: "PROVIDER_SECRET"},
			wantErr: false,
		},
		{
			name:    "empty api_base",
			cfg:     ProviderConfig{APIBase: ""},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.cfg.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// --- RoutingSettings.Validate ---

func TestRoutingSettings_Validate(t *testing.T) {
	tests := []struct {
		name    string
		tier    string
		wantErr bool
	}{
		{"frontier", "frontier", false},
		{"standard", "standard", false},
		{"fast", "fast", false},
		{"mini", "mini", false},
		{"nano", "nano", false},
		{"invalid tier", "nonexistent", true},
		{"empty tier", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := RoutingSettings{DefaultTier: tt.tier, DefaultTimeout: 300}
			err := s.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// --- LoadAndValidate on fixtures ---

func TestRoutingConfig_Fixtures(t *testing.T) {
	tests := []struct {
		file    string
		wantErr bool
	}{
		{"valid_minimal.yaml", false},
		{"valid_full.yaml", false},
		{"invalid_blocked_host.yaml", true},
		{"invalid_bad_tier.yaml", true},
	}

	for _, tt := range tests {
		t.Run(tt.file, func(t *testing.T) {
			src := filepath.Join("testdata", "models", "routing", tt.file)
			data, err := os.ReadFile(src)
			if err != nil {
				t.Fatalf("fixture not found: %s", src)
			}

			dir := t.TempDir()
			dest := filepath.Join(dir, "routing.yaml")
			if err := os.WriteFile(dest, data, 0644); err != nil {
				t.Fatalf("write fixture: %v", err)
			}

			err = LoadAndValidate(dest)
			if tt.wantErr && err == nil {
				t.Error("expected error, got nil")
			}
			if !tt.wantErr && err != nil {
				t.Errorf("expected valid, got error: %v", err)
			}
		})
	}
}

// --- ResolveModel ---

func TestRoutingConfig_ResolveModel(t *testing.T) {
	cfg := RoutingConfig{
		Providers: map[string]ProviderConfig{
			"anthropic": {APIBase: "https://api.anthropic.com", AuthEnv: "ANTHROPIC_API_KEY"},
		},
		Models: map[string]ModelConfig{
			"claude-sonnet": {Provider: "anthropic", ProviderModel: "claude-sonnet-4-20250514"},
		},
	}

	t.Run("known model", func(t *testing.T) {
		pc, mc := cfg.ResolveModel("claude-sonnet")
		if pc == nil || mc == nil {
			t.Fatal("expected non-nil results for known model")
		}
		if pc.APIBase != "https://api.anthropic.com" {
			t.Errorf("unexpected api_base: %s", pc.APIBase)
		}
		if mc.ProviderModel != "claude-sonnet-4-20250514" {
			t.Errorf("unexpected provider_model: %s", mc.ProviderModel)
		}
	})

	t.Run("unknown model alias", func(t *testing.T) {
		pc, mc := cfg.ResolveModel("gpt-4o")
		if pc != nil || mc != nil {
			t.Error("expected nil results for unknown model")
		}
	})

	t.Run("model with missing provider", func(t *testing.T) {
		cfg2 := RoutingConfig{
			Providers: map[string]ProviderConfig{},
			Models: map[string]ModelConfig{
				"orphan": {Provider: "missing", ProviderModel: "some-model"},
			},
		}
		pc, mc := cfg2.ResolveModel("orphan")
		if pc != nil || mc != nil {
			t.Error("expected nil results when provider is missing")
		}
	})
}

// --- ResolveTier ---

func TestRoutingConfig_ResolveTier(t *testing.T) {
	cfg := RoutingConfig{
		Providers: map[string]ProviderConfig{
			"anthropic": {APIBase: "https://api.anthropic.com", AuthEnv: "ANTHROPIC_API_KEY"},
		},
		Models: map[string]ModelConfig{
			"claude-sonnet": {Provider: "anthropic", ProviderModel: "claude-sonnet-4-20250514"},
			"claude-haiku":  {Provider: "anthropic", ProviderModel: "claude-haiku-4"},
		},
		Tiers: TierConfig{
			Standard: []TierEntry{
				{Model: "claude-sonnet", Preference: 0},
			},
			Fast: []TierEntry{
				{Model: "claude-haiku", Preference: 0},
				{Model: "claude-sonnet", Preference: 10},
			},
		},
	}

	t.Run("standard tier resolves", func(t *testing.T) {
		pc, mc := cfg.ResolveTier("standard", nil)
		if pc == nil || mc == nil {
			t.Fatal("expected non-nil for standard tier")
		}
		if mc.ProviderModel != "claude-sonnet-4-20250514" {
			t.Errorf("wrong model: %s", mc.ProviderModel)
		}
	})

	t.Run("fast tier picks lowest preference", func(t *testing.T) {
		pc, mc := cfg.ResolveTier("fast", nil)
		if pc == nil || mc == nil {
			t.Fatal("expected non-nil for fast tier")
		}
		// preference 0 = claude-haiku
		if mc.ProviderModel != "claude-haiku-4" {
			t.Errorf("expected claude-haiku-4, got %s", mc.ProviderModel)
		}
	})

	t.Run("empty tier returns nil", func(t *testing.T) {
		pc, mc := cfg.ResolveTier("frontier", nil)
		if pc != nil || mc != nil {
			t.Error("expected nil for empty frontier tier")
		}
	})

	t.Run("unknown tier returns nil", func(t *testing.T) {
		pc, mc := cfg.ResolveTier("bogus", nil)
		if pc != nil || mc != nil {
			t.Error("expected nil for unknown tier")
		}
	})
}

// --- VALID_TIERS ---

func TestVALID_TIERS(t *testing.T) {
	expected := []string{"frontier", "standard", "fast", "mini", "nano", "batch"}
	if len(VALID_TIERS) != len(expected) {
		t.Fatalf("expected %d tiers, got %d", len(expected), len(VALID_TIERS))
	}
	for i, tier := range expected {
		if VALID_TIERS[i] != tier {
			t.Errorf("VALID_TIERS[%d] = %q, want %q", i, VALID_TIERS[i], tier)
		}
	}
}

// --- Batch tier and tier_strategy ---

func TestResolveTierBatch(t *testing.T) {
	cfg := RoutingConfig{
		Providers: map[string]ProviderConfig{
			"test": {APIBase: "https://api.test.com"},
		},
		Models: map[string]ModelConfig{
			"test-batch": {Provider: "test", ProviderModel: "test-batch-v1"},
		},
		Tiers: TierConfig{
			Batch: []TierEntry{{Model: "test-batch", Preference: 0}},
		},
	}
	pc, mc := cfg.ResolveTier("batch", nil)
	if pc == nil || mc == nil {
		t.Fatal("expected to resolve batch tier")
	}
	if mc.ProviderModel != "test-batch-v1" {
		t.Errorf("expected provider_model 'test-batch-v1', got %q", mc.ProviderModel)
	}
}

func TestTierStrategyValidation(t *testing.T) {
	tests := []struct {
		strategy string
		wantErr  bool
	}{
		{"strict", false},
		{"best_effort", false},
		{"catch_all", false},
		{"", false},      // defaults to best_effort
		{"invalid", true},
	}
	for _, tt := range tests {
		cfg := RoutingConfig{
			Settings: RoutingSettings{
				DefaultTier:  "standard",
				TierStrategy: tt.strategy,
			},
		}
		err := cfg.Validate()
		if (err != nil) != tt.wantErr {
			t.Errorf("strategy=%q: got err=%v, wantErr=%v", tt.strategy, err, tt.wantErr)
		}
	}
}

func TestResolveTierBestEffortFallback(t *testing.T) {
	cfg := RoutingConfig{
		Providers: map[string]ProviderConfig{
			"test": {APIBase: "https://api.test.com"},
		},
		Models: map[string]ModelConfig{
			"test-fast": {Provider: "test", ProviderModel: "fast-v1"},
		},
		Tiers: TierConfig{
			Fast: []TierEntry{{Model: "test-fast", Preference: 0}},
		},
		Settings: RoutingSettings{
			TierStrategy: "best_effort",
			DefaultTier:  "standard",
		},
	}
	pc, mc := cfg.ResolveTierWithStrategy("nano", nil)
	if pc == nil || mc == nil {
		t.Fatal("best_effort should fall back to nearest tier")
	}
	if mc.ProviderModel != "fast-v1" {
		t.Errorf("expected fallback to fast-v1, got %q", mc.ProviderModel)
	}
}

func TestResolveTierStrictNoFallback(t *testing.T) {
	cfg := RoutingConfig{
		Providers: map[string]ProviderConfig{
			"test": {APIBase: "https://api.test.com"},
		},
		Models: map[string]ModelConfig{
			"test-fast": {Provider: "test", ProviderModel: "fast-v1"},
		},
		Tiers: TierConfig{
			Fast: []TierEntry{{Model: "test-fast", Preference: 0}},
		},
		Settings: RoutingSettings{
			TierStrategy: "strict",
			DefaultTier:  "standard",
		},
	}
	pc, mc := cfg.ResolveTierWithStrategy("nano", nil)
	if pc != nil || mc != nil {
		t.Fatal("strict should not fall back")
	}
}

func TestResolveTierCatchAll(t *testing.T) {
	cfg := RoutingConfig{
		Providers: map[string]ProviderConfig{
			"test": {APIBase: "https://api.test.com"},
		},
		Models: map[string]ModelConfig{
			"test-standard": {Provider: "test", ProviderModel: "std-v1"},
		},
		Tiers: TierConfig{
			Standard: []TierEntry{{Model: "test-standard", Preference: 0}},
		},
		Settings: RoutingSettings{
			TierStrategy: "catch_all",
			DefaultTier:  "standard",
		},
	}
	pc, mc := cfg.ResolveTierWithStrategy("nano", nil)
	if pc == nil || mc == nil {
		t.Fatal("catch_all should return any available model")
	}
	if mc.ProviderModel != "std-v1" {
		t.Errorf("expected catch_all to return std-v1, got %q", mc.ProviderModel)
	}
}
