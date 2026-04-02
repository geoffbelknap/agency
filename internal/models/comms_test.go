// agency-gateway/internal/models/comms_test.go
package models

import (
	"strings"
	"testing"
)

func TestMessageValidate_ContentTooLong(t *testing.T) {
	m := &Message{
		Channel: "general",
		Author:  "alice",
		Content: strings.Repeat("x", 10001),
	}
	if err := m.Validate(); err == nil {
		t.Fatal("expected error for content > 10000 chars, got nil")
	}
}

func TestMessageValidate_EmptyContentNotDeleted(t *testing.T) {
	m := &Message{
		Channel: "general",
		Author:  "alice",
		Content: "   ",
		Deleted: false,
	}
	if err := m.Validate(); err == nil {
		t.Fatal("expected error for empty content when not deleted, got nil")
	}
}

func TestMessageValidate_EmptyContentDeleted(t *testing.T) {
	m := &Message{
		Channel: "general",
		Author:  "alice",
		Content: "",
		Deleted: true,
	}
	if err := m.Validate(); err != nil {
		t.Fatalf("expected no error for empty content when deleted, got: %v", err)
	}
}

func TestMessageValidate_ValidContent(t *testing.T) {
	m := &Message{
		Channel: "general",
		Author:  "alice",
		Content: "Hello, world!",
	}
	if err := m.Validate(); err != nil {
		t.Fatalf("expected no error for valid content, got: %v", err)
	}
}

func TestChannelValidate_ValidNames(t *testing.T) {
	validNames := []string{"general", "team-1", "_system", "a"}
	for _, name := range validNames {
		c := &Channel{
			Name:       name,
			Type:       ChannelTypeTeam,
			CreatedBy:  "alice",
			Visibility: "open",
			State:      ChannelStateActive,
		}
		if err := c.Validate(); err != nil {
			t.Errorf("expected valid name %q to pass, got: %v", name, err)
		}
	}
}

func TestChannelValidate_InvalidNames(t *testing.T) {
	invalidNames := []string{"UPPER", "has space", "trailing-"}
	for _, name := range invalidNames {
		c := &Channel{
			Name:       name,
			Type:       ChannelTypeTeam,
			CreatedBy:  "alice",
			Visibility: "open",
			State:      ChannelStateActive,
		}
		if err := c.Validate(); err == nil {
			t.Errorf("expected invalid name %q to fail, got nil", name)
		}
	}
}

func TestChannelStructValidation_InvalidType(t *testing.T) {
	c := &Channel{
		Name:       "general",
		Type:       "invalid-type",
		CreatedBy:  "alice",
		Visibility: "open",
		State:      ChannelStateActive,
	}
	if err := validate.Struct(c); err == nil {
		t.Fatal("expected error for invalid channel type, got nil")
	}
}

func TestChannelStructValidation_InvalidVisibility(t *testing.T) {
	c := &Channel{
		Name:       "general",
		Type:       ChannelTypeTeam,
		CreatedBy:  "alice",
		Visibility: "public",
		State:      ChannelStateActive,
	}
	if err := validate.Struct(c); err == nil {
		t.Fatal("expected error for invalid visibility, got nil")
	}
}
