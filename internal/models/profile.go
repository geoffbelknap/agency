// agency-gateway/internal/models/profile.go
package models

// Profile represents a user or agent profile with display information.
type Profile struct {
	ID          string            `yaml:"id" json:"id" validate:"required"`
	Type        string            `yaml:"type" json:"type" validate:"required,oneof=operator agent"`
	DisplayName string            `yaml:"display_name" json:"display_name" validate:"required"`
	AvatarURL   string            `yaml:"avatar_url,omitempty" json:"avatar_url,omitempty"`
	Email       string            `yaml:"email,omitempty" json:"email,omitempty"`
	Department  string            `yaml:"department,omitempty" json:"department,omitempty"`
	Title       string            `yaml:"title,omitempty" json:"title,omitempty"`
	Bio         string            `yaml:"bio,omitempty" json:"bio,omitempty"`
	Metadata    map[string]string `yaml:"metadata,omitempty" json:"metadata,omitempty"`
	Created     string            `yaml:"created" json:"created"`
	Updated     string            `yaml:"updated" json:"updated"`
}
