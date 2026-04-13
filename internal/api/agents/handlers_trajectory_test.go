package agents

import (
	"context"
	"errors"
	"testing"
)

type trajectoryExecStub struct {
	body string
	err  error
	cmd  []string
}

func (s *trajectoryExecStub) ExecInContainer(_ context.Context, _ string, cmd []string) (string, error) {
	s.cmd = append([]string(nil), cmd...)
	return s.body, s.err
}

func (s *trajectoryExecStub) ContainerShortID(context.Context, string) string {
	return ""
}

func TestTrajectoryBodyFromExecReturnsJSONBody(t *testing.T) {
	stub := &trajectoryExecStub{body: `{"window_size":50}`}

	body, ok, err := trajectoryBodyFromExec(context.Background(), stub, "agency-agent-enforcer")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Fatal("expected exec path to succeed")
	}
	if string(body) != `{"window_size":50}` {
		t.Fatalf("unexpected body %q", string(body))
	}
	if len(stub.cmd) == 0 || stub.cmd[0] != "curl" {
		t.Fatalf("expected curl exec, got %v", stub.cmd)
	}
}

func TestTrajectoryBodyFromExecRejectsInvalidJSON(t *testing.T) {
	stub := &trajectoryExecStub{body: `not-json`}

	if _, ok, err := trajectoryBodyFromExec(context.Background(), stub, "agency-agent-enforcer"); ok || err == nil {
		t.Fatal("expected invalid JSON to fail")
	}
}

func TestTrajectoryBodyFromExecRejectsExecErrors(t *testing.T) {
	stub := &trajectoryExecStub{err: errors.New("boom")}

	if _, ok, err := trajectoryBodyFromExec(context.Background(), stub, "agency-agent-enforcer"); ok || err == nil {
		t.Fatal("expected exec error to fail")
	}
}
