package instances

import "time"

type PackageRef struct {
	Kind    string `yaml:"kind" json:"kind"`
	Name    string `yaml:"name" json:"name"`
	Version string `yaml:"version,omitempty" json:"version,omitempty"`
}

type InstanceSource struct {
	Template PackageRef `yaml:"template,omitempty" json:"template,omitempty"`
	Package  PackageRef `yaml:"package,omitempty" json:"package,omitempty"`
}

type Node struct {
	ID      string         `yaml:"id" json:"id"`
	Kind    string         `yaml:"kind" json:"kind"`
	Package PackageRef     `yaml:"package,omitempty" json:"package,omitempty"`
	Config  map[string]any `yaml:"config,omitempty" json:"config,omitempty"`
}

type Binding struct {
	Type   string         `yaml:"type" json:"type"`
	Target string         `yaml:"target,omitempty" json:"target,omitempty"`
	Config map[string]any `yaml:"config,omitempty" json:"config,omitempty"`
}

type GrantBinding struct {
	Principal string         `yaml:"principal" json:"principal"`
	Action    string         `yaml:"action" json:"action"`
	Resource  string         `yaml:"resource,omitempty" json:"resource,omitempty"`
	Config    map[string]any `yaml:"config,omitempty" json:"config,omitempty"`
}

type Relationship struct {
	From string `yaml:"from" json:"from"`
	To   string `yaml:"to" json:"to"`
	Type string `yaml:"type" json:"type"`
}

type Claim struct {
	Owner     string    `yaml:"owner" json:"owner"`
	ClaimedAt time.Time `yaml:"claimed_at" json:"claimed_at"`
}

type Instance struct {
	ID            string             `yaml:"id" json:"id"`
	Name          string             `yaml:"name" json:"name"`
	Source        InstanceSource     `yaml:"source" json:"source"`
	Nodes         []Node             `yaml:"nodes,omitempty" json:"nodes,omitempty"`
	Grants        []GrantBinding     `yaml:"grants,omitempty" json:"grants,omitempty"`
	Credentials   map[string]Binding `yaml:"credentials,omitempty" json:"credentials,omitempty"`
	Config        map[string]any     `yaml:"config,omitempty" json:"config,omitempty"`
	Relationships []Relationship     `yaml:"relationships,omitempty" json:"relationships,omitempty"`
	Claim         *Claim             `yaml:"claim,omitempty" json:"claim,omitempty"`
	CreatedAt     time.Time          `yaml:"created_at,omitempty" json:"created_at,omitempty"`
	UpdatedAt     time.Time          `yaml:"updated_at,omitempty" json:"updated_at,omitempty"`
}
