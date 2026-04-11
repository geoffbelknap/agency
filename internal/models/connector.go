// agency-gateway/internal/models/connector.go
package models

import (
	"fmt"
	"regexp"
	"strings"
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
	SecretEnv       string  `yaml:"secret_env" validate:"required"`
	Header          string  `yaml:"header" default:"X-Slack-Signature"`
	TimestampHeader *string `yaml:"timestamp_header"`
	Prefix          string  `yaml:"prefix" default:"v0="`
	ChallengeField  *string `yaml:"challenge_field"`
}

// ConnectorSource defines the inbound event source for a connector.
type ConnectorSource struct {
	Type                string                 `yaml:"type" validate:"required,oneof=webhook poll schedule channel-watch none"`
	PayloadSchema       map[string]interface{} `yaml:"schema"`
	WebhookAuth         *ConnectorWebhookAuth  `yaml:"webhook_auth"`
	Path                *string                `yaml:"path"`
	BodyFormat          *string                `yaml:"body_format"`
	PayloadField        *string                `yaml:"payload_field"`
	ResponseStatus      *int                   `yaml:"response_status"`
	ResponseBody        *string                `yaml:"response_body"`
	ResponseContentType *string                `yaml:"response_content_type"`
	URL                 *string                `yaml:"url"`
	Method              string                 `yaml:"method" default:"GET"`
	Headers             map[string]string      `yaml:"headers"`
	Interval            *string                `yaml:"interval"`
	ResponseKey         *string                `yaml:"response_key"`
	DedupKey            *string                `yaml:"dedup_key"`
	FollowUp            *ConnectorFollowUp     `yaml:"follow_up"`
	Cron                *string                `yaml:"cron"`
	Channel             *string                `yaml:"channel"`
	Pattern             *string                `yaml:"pattern"`
}

// Validate implements cross-field validation for ConnectorSource.
func (cs *ConnectorSource) Validate() error {
	if cs.BodyFormat != nil {
		switch *cs.BodyFormat {
		case "json", "form_urlencoded", "form_urlencoded_payload_json_field":
		default:
			return fmt.Errorf("unsupported body_format: %s", *cs.BodyFormat)
		}
	}

	switch cs.Type {
	case "none":
		hasFields := cs.URL != nil ||
			cs.Interval != nil ||
			cs.ResponseKey != nil ||
			cs.Cron != nil ||
			cs.Channel != nil ||
			cs.Pattern != nil ||
			cs.Headers != nil ||
			cs.WebhookAuth != nil ||
			cs.FollowUp != nil ||
			cs.Path != nil ||
			cs.BodyFormat != nil ||
			cs.PayloadField != nil ||
			cs.ResponseStatus != nil ||
			cs.ResponseBody != nil ||
			cs.ResponseContentType != nil ||
			cs.Method != "" && cs.Method != "GET"
		if hasFields {
			return fmt.Errorf("none source does not accept webhook/poll/schedule/channel-watch fields")
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
		if cs.ResponseStatus != nil && (*cs.ResponseStatus < 200 || *cs.ResponseStatus > 299) {
			return fmt.Errorf("webhook response_status must be a 2xx status code")
		}
		if cs.Path != nil {
			if *cs.Path == "" || !strings.HasPrefix(*cs.Path, "/") {
				return fmt.Errorf("webhook source path must start with '/'")
			}
		}
		if cs.PayloadField != nil && (cs.BodyFormat == nil || *cs.BodyFormat != "form_urlencoded_payload_json_field") {
			return fmt.Errorf("payload_field is only valid with body_format 'form_urlencoded_payload_json_field'")
		}
		hasPollFields := cs.URL != nil ||
			cs.Interval != nil ||
			cs.ResponseKey != nil ||
			cs.Cron != nil ||
			cs.Channel != nil ||
			cs.Pattern != nil ||
			cs.Headers != nil ||
			cs.Method != "GET"
		if hasPollFields {
			return fmt.Errorf("webhook source does not accept poll/schedule/channel-watch fields")
		}
	default:
		if cs.Path != nil || cs.BodyFormat != nil || cs.PayloadField != nil || cs.ResponseStatus != nil || cs.ResponseBody != nil || cs.ResponseContentType != nil {
			return fmt.Errorf("%s source does not accept webhook body/path fields", cs.Type)
		}
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
	Name        string                 `yaml:"name" validate:"required"`
	Method      string                 `yaml:"method" default:"GET"`
	Path        string                 `yaml:"path" validate:"required"`
	Parameters  map[string]interface{} `yaml:"parameters"`
	Description string                 `yaml:"description"`
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
	Type               string            `yaml:"type" json:"type" default:"none"` // none | bearer | jwt-exchange | oauth2
	TokenURL           string            `yaml:"token_url" json:"token_url,omitempty"`
	TokenParams        map[string]string `yaml:"token_params" json:"token_params,omitempty"`
	TokenResponseField string            `yaml:"token_response_field" json:"token_response_field" default:"access_token"`
	TokenTTLSeconds    int               `yaml:"token_ttl_seconds" json:"token_ttl_seconds" default:"3600"`
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
	Kind        string              `yaml:"kind" validate:"required,oneof=connector" default:"connector"`
	Name        string              `yaml:"name" validate:"required"`
	Version     string              `yaml:"version" default:"1.0.0"`
	Description string              `yaml:"description"`
	Author      string              `yaml:"author"`
	License     string              `yaml:"license,omitempty"`
	Requires    *ConnectorRequires  `yaml:"requires"`
	Source      ConnectorSource     `yaml:"source" validate:"required"`
	Routes      []ConnectorRoute    `yaml:"routes" validate:"required"`
	MCP         *ConnectorMCP       `yaml:"mcp"`
	RateLimits  ConnectorRateLimits `yaml:"rate_limits"`
}

// Validate implements cross-field validation for ConnectorConfig.
func (cc *ConnectorConfig) Validate() error {
	if len(cc.Routes) == 0 && cc.MCP == nil {
		return fmt.Errorf("Connector must define at least one route or MCP tool")
	}

	if err := cc.Source.Validate(); err != nil {
		return err
	}

	if cc.Source.Type == "none" && len(cc.Routes) > 0 {
		return fmt.Errorf("none source connectors cannot define routes")
	}

	for i := range cc.Routes {
		if err := cc.Routes[i].Validate(); err != nil {
			return err
		}
	}

	return nil
}
