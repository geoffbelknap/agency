// agency-gateway/internal/models/comms.go
package models

import (
	"fmt"
	"regexp"
	"strings"
	"time"
)

// Channel type constants.
const (
	ChannelTypeTeam   = "team"
	ChannelTypeDirect = "direct"
	ChannelTypeSystem = "system"
)

// DMChannelPrefix is the naming prefix for agent DM channels.
const DMChannelPrefix = "dm-"

// DMChannelName returns the DM channel name for an agent.
func DMChannelName(agentName string) string {
	return DMChannelPrefix + agentName
}

// IsDMChannel returns true if the channel name is a DM channel.
func IsDMChannel(name string) bool {
	return strings.HasPrefix(name, DMChannelPrefix)
}

// Channel state constants.
const (
	ChannelStateActive   = "active"
	ChannelStateArchived = "archived"
	ChannelStatePurged   = "purged"
)

// MessageFlags tracks message annotations.
type MessageFlags struct {
	Decision         bool `yaml:"decision" json:"decision"`
	Question         bool `yaml:"question" json:"question"`
	Blocker          bool `yaml:"blocker" json:"blocker"`
	Urgent           bool `yaml:"urgent" json:"urgent"`
	ApprovalRequest  bool `yaml:"approval_request" json:"approval_request"`
	ApprovalResponse bool `yaml:"approval_response" json:"approval_response"`
}

// Message represents a single channel message.
type Message struct {
	ID          string                   `yaml:"id" json:"id"`
	Channel     string                   `yaml:"channel" json:"channel" validate:"required"`
	Author      string                   `yaml:"author" json:"author" validate:"required"`
	Timestamp   time.Time                `yaml:"timestamp" json:"timestamp"`
	Content     string                   `yaml:"content" json:"content"`
	ReplyTo     *string                  `yaml:"reply_to" json:"reply_to,omitempty"`
	Flags       MessageFlags             `yaml:"flags" json:"flags"`
	Metadata    map[string]interface{}   `yaml:"metadata" json:"metadata"`
	EditedAt    *time.Time               `yaml:"edited_at" json:"edited_at,omitempty"`
	EditHistory []map[string]interface{} `yaml:"edit_history" json:"edit_history"`
	Deleted     bool                     `yaml:"deleted" json:"deleted"`
	Reactions   []map[string]interface{} `yaml:"reactions" json:"reactions"`
}

// Validate checks content bounds and non-empty when not deleted.
func (m *Message) Validate() error {
	if len(m.Content) > 10000 {
		return fmt.Errorf("message content exceeds 10000 character limit")
	}
	if !m.Deleted && strings.TrimSpace(m.Content) == "" {
		return fmt.Errorf("message content cannot be empty")
	}
	return nil
}

var channelNameRE = regexp.MustCompile(`^_?[a-z0-9][a-z0-9-]*[a-z0-9]$|^_?[a-z0-9]$`)

// Channel represents a communication channel.
type Channel struct {
	ID           string     `yaml:"id" json:"id"`
	Name         string     `yaml:"name" json:"name" validate:"required"`
	Type         string     `yaml:"type" json:"type" validate:"required,oneof=team direct system"`
	CreatedBy    string     `yaml:"created_by" json:"created_by" validate:"required"`
	CreatedAt    time.Time  `yaml:"created_at" json:"created_at"`
	Topic        string     `yaml:"topic" json:"topic"`
	Members      []string   `yaml:"members" json:"members"`
	Visibility   string     `yaml:"visibility" json:"visibility" validate:"required,oneof=open private platform-write" default:"open"`
	State        string     `yaml:"state" json:"state" validate:"required,oneof=active archived purged" default:"active"`
	DeploymentID string     `yaml:"deployment_id" json:"deployment_id"`
	BaseName     string     `yaml:"base_name" json:"base_name"`
	ArchivedAt   *time.Time `yaml:"archived_at" json:"archived_at,omitempty"`
	ArchivedBy   string     `yaml:"archived_by" json:"archived_by"`
}

// Validate checks channel name format.
func (c *Channel) Validate() error {
	if !channelNameRE.MatchString(c.Name) {
		return fmt.Errorf("channel name must be lowercase kebab-case: %q", c.Name)
	}
	return nil
}
