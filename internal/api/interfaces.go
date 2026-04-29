package api

import (
	"context"
	"fmt"

	runtimecontract "github.com/geoffbelknap/agency/internal/runtime/contract"
)

// SignalSender sends OS signals to named runtime instances.
// Used by modules that need to SIGHUP enforcers for config reload.
type SignalSender interface {
	Signal(ctx context.Context, ref runtimecontract.InstanceRef, signal string) error
}

type noopSignalSender struct{}

func (noopSignalSender) Signal(context.Context, runtimecontract.InstanceRef, string) error {
	return fmt.Errorf("signal sender unavailable")
}

type noopCommsClient struct{}

func (noopCommsClient) CommsRequest(context.Context, string, string, interface{}) ([]byte, error) {
	return nil, fmt.Errorf("comms client unavailable")
}

type noopRuntimeExecClient struct{}

func (noopRuntimeExecClient) Exec(context.Context, runtimecontract.InstanceRef, []string) (string, error) {
	return "", fmt.Errorf("runtime exec unavailable")
}

func (noopRuntimeExecClient) ShortID(context.Context, runtimecontract.InstanceRef) string {
	return ""
}
