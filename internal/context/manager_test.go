package context

import (
	"testing"
)

func TestManagerPushCreatesChange(t *testing.T) {
	m := NewManager(nil)
	constraints := map[string]interface{}{"budget": map[string]interface{}{"max_daily_usd": 5.0}}
	change, err := m.Push("test-agent", constraints, "", "test reason", "operator")
	if err != nil {
		t.Fatal(err)
	}
	if change.Agent != "test-agent" {
		t.Errorf("agent = %q, want test-agent", change.Agent)
	}
	if change.Version != 1 {
		t.Errorf("version = %d, want 1", change.Version)
	}
	if change.Status != StatusPending {
		t.Errorf("status = %s, want pending", change.Status)
	}
	if change.Hash == "" {
		t.Error("hash should not be empty")
	}
}

func TestManagerPushIncrementsVersion(t *testing.T) {
	m := NewManager(nil)
	c := map[string]interface{}{"foo": "bar"}
	ch1, _ := m.Push("agent-a", c, "", "r1", "op")
	ch2, _ := m.Push("agent-a", c, "", "r2", "op")
	if ch2.Version != ch1.Version+1 {
		t.Errorf("version %d should be %d+1", ch2.Version, ch1.Version)
	}
}

func TestManagerGetStatus(t *testing.T) {
	m := NewManager(nil)
	c := map[string]interface{}{"foo": "bar"}
	change, _ := m.Push("agent-a", c, "", "r", "op")

	status := m.GetStatus("agent-a")
	if status == nil {
		t.Fatal("status should not be nil")
	}
	if status.ChangeID != change.ChangeID {
		t.Errorf("change_id mismatch")
	}
}

func TestManagerAck(t *testing.T) {
	m := NewManager(nil)
	c := map[string]interface{}{"foo": "bar"}
	change, _ := m.Push("agent-a", c, "", "r", "op")

	err := m.Ack("agent-a", change.ChangeID, change.Version, change.Hash)
	if err != nil {
		t.Fatal(err)
	}
	status := m.GetStatus("agent-a")
	if status.Status != StatusAcked {
		t.Errorf("status = %s, want acked", status.Status)
	}
}

func TestManagerAckHashMismatch(t *testing.T) {
	m := NewManager(nil)
	c := map[string]interface{}{"foo": "bar"}
	change, _ := m.Push("agent-a", c, "", "r", "op")

	err := m.Ack("agent-a", change.ChangeID, change.Version, "wrong-hash")
	if err == nil {
		t.Error("expected error for hash mismatch")
	}
	status := m.GetStatus("agent-a")
	if status.Status != StatusHashMismatch {
		t.Errorf("status = %s, want hash_mismatch", status.Status)
	}
}

func TestManagerChangesHistory(t *testing.T) {
	m := NewManager(nil)
	c := map[string]interface{}{"foo": "bar"}
	m.Push("agent-a", c, "", "r1", "op")
	m.Push("agent-a", c, "", "r2", "op")

	history := m.Changes("agent-a")
	if len(history) != 2 {
		t.Errorf("history len = %d, want 2", len(history))
	}
}

func TestManagerDisconnectAlert(t *testing.T) {
	var alertAgent string
	var alertMsg string

	m := NewManager(nil)
	m.SetAlertFunc(func(agent, message string) {
		alertAgent = agent
		alertMsg = message
	})

	m.HandleEnforcerDisconnect("test-agent")

	if alertAgent != "test-agent" {
		t.Errorf("alert agent = %q, want test-agent", alertAgent)
	}
	if alertMsg == "" {
		t.Error("alert message should not be empty")
	}
}
