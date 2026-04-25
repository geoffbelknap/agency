package models

import "testing"

func TestMeeseeksSpawnRequestValidation(t *testing.T) {
	tests := []struct {
		name    string
		req     MeeseeksSpawnRequest
		wantErr bool
	}{
		{"valid minimal", MeeseeksSpawnRequest{Task: "do thing"}, false},
		{"valid with fast", MeeseeksSpawnRequest{Task: "do thing", Model: "fast"}, false},
		{"valid with standard", MeeseeksSpawnRequest{Task: "do thing", Model: "standard"}, false},
		{"valid with frontier", MeeseeksSpawnRequest{Task: "do thing", Model: "frontier"}, false},
		{"valid with budget", MeeseeksSpawnRequest{Task: "do thing", Budget: 0.05}, false},
		{"valid with tools", MeeseeksSpawnRequest{Task: "do thing", Tools: []string{"read_file"}}, false},
		{"empty task", MeeseeksSpawnRequest{}, true},
		{"bad model", MeeseeksSpawnRequest{Task: "x", Model: "opus"}, true},
		{"negative budget", MeeseeksSpawnRequest{Task: "x", Budget: -1}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.req.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestMeeseeksSpawnRequestTaskTooLong(t *testing.T) {
	longTask := make([]byte, 2001)
	for i := range longTask {
		longTask[i] = 'a'
	}
	req := MeeseeksSpawnRequest{Task: string(longTask)}
	if err := req.Validate(); err == nil {
		t.Error("expected error for overly long task")
	}
}

func TestNewMeeseeksID(t *testing.T) {
	id := NewMeeseeksID()
	if len(id) < 12 {
		t.Errorf("id too short: %s", id)
	}
	if id[:4] != "mks-" {
		t.Errorf("id missing prefix: %s", id)
	}

	// IDs should be unique
	id2 := NewMeeseeksID()
	if id == id2 {
		t.Errorf("two IDs should be unique, both are %s", id)
	}
}
