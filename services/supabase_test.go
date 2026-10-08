package services

import (
	"encoding/json"
	"testing"
)

func TestInviteLink_UnmarshalRootLevel(t *testing.T) {
	raw := `{
		"id": "user-uuid-123",
		"email": "test@example.com",
		"action_link": "https://example.com/verify?token=abc",
		"verification_type": "invite"
	}`

	var link InviteLink
	if err := json.Unmarshal([]byte(raw), &link); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}

	if link.UserID() != "user-uuid-123" {
		t.Errorf("expected user-uuid-123, got %q", link.UserID())
	}
	if link.Link() != "https://example.com/verify?token=abc" {
		t.Errorf("expected action link, got %q", link.Link())
	}
}

func TestInviteLink_UnmarshalNestedUser(t *testing.T) {
	raw := `{
		"action_link": "https://example.com/verify?token=abc",
		"user": {
			"id": "nested-uuid-456",
			"email": "test2@example.com"
		}
	}`

	var link InviteLink
	if err := json.Unmarshal([]byte(raw), &link); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}

	if link.UserID() != "nested-uuid-456" {
		t.Errorf("expected nested-uuid-456, got %q", link.UserID())
	}
	if link.Link() != "https://example.com/verify?token=abc" {
		t.Errorf("expected action link, got %q", link.Link())
	}
}
