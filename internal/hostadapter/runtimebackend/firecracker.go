package runtimebackend

import (
	"context"
	"fmt"

	runtimecontract "github.com/geoffbelknap/agency/internal/runtime/contract"
)

const BackendFirecracker = "firecracker"

type FirecrackerRuntimeBackend struct {
	BinaryPath string
	KernelPath string
	StateDir   string
	Images     *FirecrackerImageStore
	Tasks      *FirecrackerVMSupervisor
	Vsock      *FirecrackerVsockListenerFactory
}

func (b *FirecrackerRuntimeBackend) Name() string {
	return BackendFirecracker
}

func (b *FirecrackerRuntimeBackend) Ensure(ctx context.Context, spec runtimecontract.RuntimeSpec) error {
	_ = ctx
	_ = spec
	return fmt.Errorf("firecracker backend: Ensure not implemented")
}

func (b *FirecrackerRuntimeBackend) Stop(ctx context.Context, runtimeID string) error {
	_ = ctx
	_ = runtimeID
	return fmt.Errorf("firecracker backend: Stop not implemented")
}

func (b *FirecrackerRuntimeBackend) Inspect(ctx context.Context, runtimeID string) (runtimecontract.BackendStatus, error) {
	_ = ctx
	_ = runtimeID
	return runtimecontract.BackendStatus{}, fmt.Errorf("firecracker backend: Inspect not implemented")
}

func (b *FirecrackerRuntimeBackend) Validate(ctx context.Context, runtimeID string) error {
	_ = ctx
	_ = runtimeID
	return fmt.Errorf("firecracker backend: Validate not implemented")
}

func (b *FirecrackerRuntimeBackend) Capabilities(ctx context.Context) (runtimecontract.BackendCapabilities, error) {
	_ = ctx
	return runtimecontract.BackendCapabilities{
		SupportedTransportTypes: []string{runtimecontract.TransportTypeVsockHTTP},
		SupportsRootless:        false,
		SupportsComposeLike:     false,
		Isolation:               runtimecontract.IsolationMicroVM,
		RequiresKVM:             true,
		SupportsSnapshots:       true,
	}, nil
}
