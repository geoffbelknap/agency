// agency-gateway/internal/models/connector.go
package models

import (
	"fmt"
	"regexp"
)

var intervalPattern = regexp.MustCompile(`^\d+[smhd]$`)

// ConnectorFollowUp defines a follow-up request after an initial poll.
type ConnectorFollowUp struct {
	URL         string  `yaml:"url" validate:"required"`
	When        *string `yaml:"when"`
	ResponseKey *string `yaml:"response_key"`
	DedupKey    *string `yaml:"dedup_key"`
	SkipFirst   bool    `yaml:"skip_first"`
}

// ConnectorWebhookAuth defines HMAC-based webhook authentication.
type ConnectorWebhookAuth struct {
	Type            string  `yaml:"type" default:"hmac_sha256"`
	SecretEnv       string  `yaml:"secret_env"`
	SecretCredref   string  `yaml:"secret_credref"`
	Header          string  `yaml:"header" default:"X-Slack-Signature"`
	TimestampHeader *string `yaml:"timestamp_header"`
	Prefix          string  `yaml:"prefix" default:"v0="`
	ChallengeField  *string `yaml:"challenge_field"`
	MaxSkewSeconds  int     `yaml:"max_skew_seconds" default:"300"`
}

// ConnectorSource defines the inbound event source for a connector.
type ConnectorSource struct {
	Type          string                 `yaml:"type" validate:"required,oneof=webhook poll schedule channel-watch none"`
	PayloadSchema map[string]interface{} `yaml:"schema"`
	WebhookAuth   *ConnectorWebhookAuth  `yaml:"webhook_auth"`
	Path          *string                `yaml:"path"`
	BodyFormat    *string                `yaml:"body_format"`
	AckStrategy   *string                `yaml:"ack_strategy"`
	URL           *string                `yaml:"url"`
	Method        string                 `yaml:"method" default:"GET"`
	Headers       map[string]string      `yaml:"headers"`
	Interval      *string                `yaml:"interval"`
	ResponseKey   *string                `yaml:"response_key"`
	DedupKey      *string                `yaml:"dedup_key"`
	FollowUp      *ConnectorFollowUp     `yaml:"follow_up"`
	Cron          *string                `yaml:"cron"`
	Channel       *string                `yaml:"channel"`
	Pattern       *string                `yaml:"pattern"`
}

// Validate implements cross-field validation for ConnectorSource.
func (cs *ConnectorSource) Validate() error {
	switch cs.Type {
	case "none":
		hasInboundFields := cs.WebhookAuth != nil ||
			cs.Path != nil ||
			cs.BodyFormat != nil ||
			cs.AckStrategy != nil ||
			cs.URL != nil ||
			cs.Interval != nil ||
			cs.ResponseKey != nil ||
			cs.Cron != nil ||
			cs.Channel != nil ||
			cs.Pattern != nil ||
			cs.Headers != nil ||
			(cs.Method != "" && cs.Method != "GET")
		if hasInboundFields {
			return fmt.Errorf("source type none does not accept inbound source fields")
		}
	case "poll":
		if cs.URL == nil || *cs.URL == "" {
			return fmt.Errorf("poll source requires 'url'")
		}
		if cs.Interval == nil || *cs.Interval == "" {
			return fmt.Errorf("poll source requires 'interval'")
		}
		if !intervalPattern.MatchString(*cs.Interval) {
			return fmt.Errorf("Invalid interval format: %s", *cs.Interval)
		}
	case "schedule":
		if cs.Cron == nil || *cs.Cron == "" {
			return fmt.Errorf("schedule source requires 'cron'")
		}
	case "channel-watch":
		if cs.Channel == nil || *cs.Channel == "" {
			return fmt.Errorf("channel-watch source requires 'channel'")
		}
		if cs.Pattern == nil || *cs.Pattern == "" {
			return fmt.Errorf("channel-watch source requires 'pattern'")
		}
	case "webhook":
		hasPollFields := cs.URL != nil ||
			cs.Interval != nil ||
			cs.ResponseKey != nil ||
			cs.Cron != nil ||
			cs.Channel != nil ||
			cs.Pattern != nil ||
			cs.Headers != nil ||
			(cs.Method != "" && cs.Method != "GET")
		if hasPollFields {
			return fmt.Errorf("webhook source does not accept poll/schedule/channel-watch fields")
		}
	}
	if cs.WebhookAuth != nil && cs.WebhookAuth.SecretEnv == "" && cs.WebhookAuth.SecretCredref == "" {
		return fmt.Errorf("webhook_auth requires either secret_env or secret_credref")
	}
	return nil
}

// ConnectorRelayTarget defines an HTTP relay destination.
type ConnectorRelayTarget struct {
	URL         string            `yaml:"url" validate:"required"`
	Method      string            `yaml:"method" default:"POST"`
	Headers     map[string]string `yaml:"headers"`
	Body        string            `yaml:"body" validate:"required"`
	ContentType string            `yaml:"content_type" default:"application/json"`
}

// ConnectorRoute defines a routing rule for matched events.
type ConnectorRoute struct {
	Match    map[string]interface{} `yaml:"match" validate:"required"`
	Target   map[string]string      `yaml:"target"`
	Relay    *ConnectorRelayTarget  `yaml:"relay"`
	Priority string                 `yaml:"priority" validate:"omitempty,oneof=high normal low" default:"normal"`
	SLA      *string                `yaml:"sla"`
	Brief    *string                `yaml:"brief"`
}

// Validate implements target/relay mutual exclusion for ConnectorRoute.
func (cr *ConnectorRoute) Validate() error {
	hasTarget := cr.Target != nil
	hasRelay := cr.Relay != nil

	if !hasTarget && !hasRelay {
		return fmt.Errorf("Route must specify either 'target' or 'relay'")
	}
	if hasTarget && hasRelay {
		return fmt.Errorf("Route cannot specify both 'target' and 'relay'")
	}
	return nil
}

// ConnectorMCPTool defines a single MCP tool exposed by a connector.
type ConnectorMCPTool struct {
	Name                 string                 `yaml:"name" validate:"required"`
	Method               string                 `yaml:"method" default:"GET"`
	Path                 string                 `yaml:"path"`
	Parameters           map[string]interface{} `yaml:"parameters"`
	InputSchema          map[string]interface{} `yaml:"input_schema"`
	Returns              map[string]interface{} `yaml:"returns"`
	Description          string                 `yaml:"description"`
	RequiresConfig       string                 `yaml:"requires_config,omitempty"`
	QueryParams          []string               `yaml:"query_params,omitempty"`
	WhitelistCheck       string                 `yaml:"whitelist_check,omitempty"`
	RequiresConsentToken *ConsentRequirement    `yaml:"requires_consent_token,omitempty"`
}

func (ct *ConnectorMCPTool) Validate() error {
	if ct.Path == "" && len(ct.Parameters) == 0 && len(ct.InputSchema) == 0 {
		return fmt.Errorf("tool %q requires path, parameters, or input_schema", ct.Name)
	}
	schema := ct.Parameters
	if len(schema) == 0 {
		schema = ct.InputSchema
	}
	params := make(map[string]bool, len(schema))
	for name := range schema {
		params[name] = true
	}
	if ct.WhitelistCheck != "" && !params[ct.WhitelistCheck] {
		return fmt.Errorf("tool %q whitelist_check references unknown parameter %q", ct.Name, ct.WhitelistCheck)
	}
	for _, field := range ct.QueryParams {
		if !params[field] {
			return fmt.Errorf("tool %q query_params references unknown parameter %q", ct.Name, field)
		}
	}
	if ct.RequiresConsentToken == nil {
		return nil
	}
	if ct.RequiresConsentToken.OperationKind == "" {
		return fmt.Errorf("tool %q requires_consent_token.operation_kind is required", ct.Name)
	}
	if ct.RequiresConsentToken.TokenInputField == "" {
		return fmt.Errorf("tool %q requires_consent_token.token_input_field is required", ct.Name)
	}
	if ct.RequiresConsentToken.TargetInputField == "" {
		return fmt.Errorf("tool %q requires_consent_token.target_input_field is required", ct.Name)
	}
	if !params[ct.RequiresConsentToken.TokenInputField] {
		return fmt.Errorf("tool %q requires_consent_token references unknown token_input_field %q", ct.Name, ct.RequiresConsentToken.TokenInputField)
	}
	if !params[ct.RequiresConsentToken.TargetInputField] {
		return fmt.Errorf("tool %q requires_consent_token references unknown target_input_field %q", ct.Name, ct.RequiresConsentToken.TargetInputField)
	}
	return nil
}

// ConnectorMCP defines the MCP server configuration for a connector.
type ConnectorMCP struct {
	Name       string             `yaml:"name" validate:"required"`
	Credential string             `yaml:"credential" validate:"required"`
	APIBase    *string            `yaml:"api_base"`
	Server     *string            `yaml:"server"`
	Tools      []ConnectorMCPTool `yaml:"tools"`
}

// ConnectorCredential defines a credential required by a connector.
type ConnectorCredential struct {
	Name        string `yaml:"name" json:"name" validate:"required"`
	Description string `yaml:"description" json:"description"`
	Type        string `yaml:"type" json:"type" default:"secret"`          // secret | config
	Scope       string `yaml:"scope" json:"scope" default:"service-grant"` // service-grant | env-var | file
	GrantName   string `yaml:"grant_name" json:"grant_name,omitempty"`
	SetupURL    string `yaml:"setup_url" json:"setup_url,omitempty"`
	Example     string `yaml:"example" json:"example,omitempty"`
}

// ConnectorAuth defines authentication configuration for a connector.
type ConnectorAuth struct {
	Type               string            `yaml:"type" json:"type" default:"none"` // none | bearer | jwt-exchange | oauth2 | google_service_account
	TokenURL           string            `yaml:"token_url" json:"token_url,omitempty"`
	TokenParams        map[string]string `yaml:"token_params" json:"token_params,omitempty"`
	TokenResponseField string            `yaml:"token_response_field" json:"token_response_field" default:"access_token"`
	TokenTTLSeconds    int               `yaml:"token_ttl_seconds" json:"token_ttl_seconds" default:"3600"`
	Scopes             []string          `yaml:"scopes" json:"scopes,omitempty"`
}

// ConnectorRequires lists service dependencies for a connector.
type ConnectorRequires struct {
	Services      []string              `yaml:"services" json:"services"`
	Credentials   []ConnectorCredential `yaml:"credentials" json:"credentials"`
	Auth          *ConnectorAuth        `yaml:"auth" json:"auth,omitempty"`
	EgressDomains []string              `yaml:"egress_domains" json:"egress_domains"`
}

// ConnectorRateLimits defines rate limiting parameters for a connector.
type ConnectorRateLimits struct {
	MaxPerHour    int `yaml:"max_per_hour" default:"100"`
	MaxConcurrent int `yaml:"max_concurrent" default:"10"`
}

// ConnectorConfig is the schema for connector YAML files.
type ConnectorConfig struct {
	Kind        string                 `yaml:"kind" validate:"required,oneof=connector" default:"connector"`
	Name        string                 `yaml:"name" validate:"required"`
	Version     string                 `yaml:"version" default:"1.0.0"`
	Description string                 `yaml:"description"`
	Author      string                 `yaml:"author"`
	License     string                 `yaml:"license,omitempty"`
	Requires    *ConnectorRequires     `yaml:"requires"`
	Source      ConnectorSource        `yaml:"source" validate:"required"`
	Config      map[string]interface{} `yaml:"config,omitempty"`
	Routes      []ConnectorRoute       `yaml:"routes"`
	MCP         *ConnectorMCP          `yaml:"mcp"`
	Tools       []ConnectorMCPTool     `yaml:"tools,omitempty"`
	RateLimits  ConnectorRateLimits    `yaml:"rate_limits"`
}

// Validate implements cross-field validation for ConnectorConfig.
func (cc *ConnectorConfig) Validate() error {
	if err := cc.Source.Validate(); err != nil {
		return err
	}
	if len(cc.Routes) == 0 && cc.Source.Type != "none" {
		return fmt.Errorf("Connector must define at least one route")
	}
	if cc.Source.Type == "none" && len(cc.Routes) != 0 {
		return fmt.Errorf("tool-only connectors must not define routes")
	}
	if len(cc.Routes) == 0 && cc.Source.Type == "none" && cc.MCP == nil && len(cc.Tools) == 0 {
		return fmt.Errorf("tool-only connectors must define at least one tool")
	}

	for i := range cc.Routes {
		if err := cc.Routes[i].Validate(); err != nil {
			return err
		}
	}
	if cc.MCP != nil {
		for i := range cc.MCP.Tools {
			if err := cc.MCP.Tools[i].Validate(); err != nil {
				return err
			}
		}
	}
	for i := range cc.Tools {
		if err := cc.Tools[i].Validate(); err != nil {
			return err
		}
	}

	return nil
}
