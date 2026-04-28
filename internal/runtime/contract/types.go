package contract

import "context"

const (
	TransportTypeLoopbackHTTP = "loopback_http"
	TransportTypeUnixHTTP     = "unix_http"
	TransportTypeVsockHTTP    = "vsock_http"

	RuntimePhaseCompiled    = "compiled"
	RuntimePhaseReconciled  = "reconciled"
	RuntimePhaseStarting    = "starting"
	RuntimePhaseRunning     = "running"
	RuntimePhaseDegraded    = "degraded"
	RuntimePhaseStopped     = "stopped"
	RuntimePhaseFailed      = "failed"
	RuntimePhaseQuarantined = "quarantined"
)

type RuntimeSpec struct {
	RuntimeID string               `yaml:"runtimeId" json:"runtimeId"`
	AgentID   string               `yaml:"agentId" json:"agentId"`
	Backend   string               `yaml:"backend,omitempty" json:"backend,omitempty"`
	Package   RuntimePackageSpec   `yaml:"package" json:"package"`
	Transport RuntimeTransportSpec `yaml:"transport" json:"transport"`
	Storage   RuntimeStorageSpec   `yaml:"storage" json:"storage"`
	Lifecycle RuntimeLifecycleSpec `yaml:"lifecycle" json:"lifecycle"`
	Health    RuntimeHealthSpec    `yaml:"health" json:"health"`
	Revision  RuntimeRevisionSpec  `yaml:"revision,omitempty" json:"revision,omitempty"`
}

type RuntimePackageSpec struct {
	Image      string            `yaml:"image,omitempty" json:"image,omitempty"`
	Entrypoint []string          `yaml:"entrypoint,omitempty" json:"entrypoint,omitempty"`
	Env        map[string]string `yaml:"env,omitempty" json:"env,omitempty"`
}

type RuntimeTransportSpec struct {
	Enforcer EnforcerTransportSpec `yaml:"enforcer" json:"enforcer"`
}

type EnforcerTransportSpec struct {
	Type     string `yaml:"type" json:"type"`
	Endpoint string `yaml:"endpoint" json:"endpoint"`
	AuthMode string `yaml:"authMode,omitempty" json:"authMode,omitempty"`
	TokenRef string `yaml:"tokenRef,omitempty" json:"tokenRef,omitempty"`
}

type RuntimeStorageSpec struct {
	ConfigPath    string `yaml:"configPath,omitempty" json:"configPath,omitempty"`
	StatePath     string `yaml:"statePath,omitempty" json:"statePath,omitempty"`
	WorkspacePath string `yaml:"workspacePath,omitempty" json:"workspacePath,omitempty"`
}

type RuntimeLifecycleSpec struct {
	RestartPolicy string `yaml:"restartPolicy,omitempty" json:"restartPolicy,omitempty"`
	RecoverState  bool   `yaml:"recoverState,omitempty" json:"recoverState,omitempty"`
}

type RuntimeHealthSpec struct {
	HealthPath    string `yaml:"healthPath,omitempty" json:"healthPath,omitempty"`
	HeartbeatFile string `yaml:"heartbeatFile,omitempty" json:"heartbeatFile,omitempty"`
}

type RuntimeRevisionSpec struct {
	InstanceRevision string `yaml:"instanceRevision,omitempty" json:"instanceRevision,omitempty"`
}

type ComponentRole string

const (
	RoleWorkspace ComponentRole = "workspace"
	RoleEnforcer  ComponentRole = "enforcer"
)

type InstanceRef struct {
	RuntimeID string
	Role      ComponentRole
}

type RuntimeStatus struct {
	RuntimeID       string                 `yaml:"runtimeId" json:"runtimeId"`
	AgentID         string                 `yaml:"agentId" json:"agentId"`
	Phase           string                 `yaml:"phase" json:"phase"`
	Healthy         bool                   `yaml:"healthy" json:"healthy"`
	Backend         string                 `yaml:"backend,omitempty" json:"backend,omitempty"`
	BackendEndpoint string                 `yaml:"backendEndpoint,omitempty" json:"backendEndpoint,omitempty"`
	BackendMode     string                 `yaml:"backendMode,omitempty" json:"backendMode,omitempty"`
	Transport       RuntimeTransportStatus `yaml:"transport" json:"transport"`
}

type RuntimeTransportStatus struct {
	Type              string `yaml:"type,omitempty" json:"type,omitempty"`
	Endpoint          string `yaml:"endpoint,omitempty" json:"endpoint,omitempty"`
	EnforcerConnected bool   `yaml:"enforcerConnected" json:"enforcerConnected"`
	LastError         string `yaml:"lastError,omitempty" json:"lastError,omitempty"`
}

type BackendStatus struct {
	RuntimeID string            `yaml:"runtimeId" json:"runtimeId"`
	Healthy   bool              `yaml:"healthy" json:"healthy"`
	Phase     string            `yaml:"phase" json:"phase"`
	Details   map[string]string `yaml:"details,omitempty" json:"details,omitempty"`
}

type Isolation string

const (
	IsolationContainer Isolation = "container"
	IsolationMicroVM   Isolation = "microvm"
	IsolationLangVM    Isolation = "langvm"
)

type BackendCapabilities struct {
	SupportedTransportTypes []string  `yaml:"supportedTransportTypes,omitempty" json:"supportedTransportTypes,omitempty"`
	SupportsRootless        bool      `yaml:"supportsRootless,omitempty" json:"supportsRootless,omitempty"`
	SupportsComposeLike     bool      `yaml:"supportsComposeLike,omitempty" json:"supportsComposeLike,omitempty"`
	Isolation               Isolation `yaml:"isolation,omitempty" json:"isolation,omitempty"`
	RequiresKVM             bool      `yaml:"requiresKVM,omitempty" json:"requiresKVM,omitempty"`
	SupportsSnapshots       bool      `yaml:"supportsSnapshots,omitempty" json:"supportsSnapshots,omitempty"`
}

type Backend interface {
	Name() string
	Ensure(ctx context.Context, spec RuntimeSpec) error
	Stop(ctx context.Context, runtimeID string) error
	Inspect(ctx context.Context, runtimeID string) (BackendStatus, error)
	Validate(ctx context.Context, runtimeID string) error
	Capabilities(ctx context.Context) (BackendCapabilities, error)
}

type RuntimeManager interface {
	Compile(ctx context.Context, runtimeID string) (RuntimeSpec, error)
	Reconcile(ctx context.Context, spec RuntimeSpec) error
	Ensure(ctx context.Context, runtimeID string) error
	Restart(ctx context.Context, runtimeID string) error
	Stop(ctx context.Context, runtimeID string) error
	Get(ctx context.Context, runtimeID string) (RuntimeStatus, error)
	Validate(ctx context.Context, runtimeID string) error
}
