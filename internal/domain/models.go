package domain

import "time"

type Service struct {
	ID          string            `json:"id" yaml:"id"`
	Name        string            `json:"name" yaml:"name"`
	DisplayName string            `json:"displayName" yaml:"displayName"`
	Description string            `json:"description" yaml:"description"`
	OwnerTeam   string            `json:"ownerTeam" yaml:"ownerTeam"`
	Tier        string            `json:"tier" yaml:"tier"`
	Labels      map[string]string `json:"labels" yaml:"labels"`
	CreatedAt   time.Time         `json:"createdAt" yaml:"createdAt"`
	UpdatedAt   time.Time         `json:"updatedAt" yaml:"updatedAt"`
}

// Asset is an inventoried physical, virtual, network, cloud or application
// resource. AssetID is stable across name and address changes and is scoped to
// an organization.
type Asset struct {
	ID          string            `json:"id" yaml:"id"`
	AssetID     string            `json:"assetId" yaml:"assetId"`
	Name        string            `json:"name" yaml:"name"`
	Kind        string            `json:"kind" yaml:"kind"`
	Site        string            `json:"site" yaml:"site"`
	OwnerTeam   string            `json:"ownerTeam" yaml:"ownerTeam"`
	Environment string            `json:"environment" yaml:"environment"`
	Lifecycle   string            `json:"lifecycle" yaml:"lifecycle"`
	Source      string            `json:"source" yaml:"source"`
	LastSeen    *time.Time        `json:"lastSeen,omitempty" yaml:"lastSeen,omitempty"`
	Labels      map[string]string `json:"labels" yaml:"labels"`
	CreatedAt   time.Time         `json:"createdAt" yaml:"createdAt"`
	UpdatedAt   time.Time         `json:"updatedAt" yaml:"updatedAt"`
}

type Agent struct {
	ID            string            `json:"id"`
	Name          string            `json:"name"`
	Region        string            `json:"region"`
	CloudProvider string            `json:"cloudProvider"`
	Account       string            `json:"account"`
	Cluster       string            `json:"cluster"`
	Network       string            `json:"network"`
	Location      string            `json:"location"`
	Team          string            `json:"team"`
	Environment   string            `json:"environment"`
	Labels        map[string]string `json:"labels"`
	Capabilities  []string          `json:"capabilities"`
	LastHeartbeat *time.Time        `json:"lastHeartbeat,omitempty"`
	Status        string            `json:"status"`
}

type Release struct {
	ID                 string            `json:"id"`
	Service            string            `json:"service"`
	Environment        string            `json:"environment"`
	DeploymentID       string            `json:"deploymentId"`
	CommitSHA          string            `json:"commitSha"`
	Image              string            `json:"image"`
	ImageDigest        string            `json:"imageDigest"`
	Version            string            `json:"version"`
	Pipeline           string            `json:"pipeline"`
	PipelineURL        string            `json:"pipelineUrl"`
	Actor              string            `json:"actor"`
	DeploymentStrategy string            `json:"deploymentStrategy"`
	Cluster            string            `json:"cluster"`
	Namespace          string            `json:"namespace"`
	Labels             map[string]string `json:"labels"`
	DeployedAt         time.Time         `json:"deployedAt"`
	CreatedAt          time.Time         `json:"createdAt"`
}

type Validation struct {
	ID         string            `json:"id"`
	ReleaseID  string            `json:"releaseId"`
	Mode       string            `json:"mode"`
	Status     string            `json:"status"`
	Result     string            `json:"result"`
	Summary    string            `json:"summary"`
	Checks     []ValidationCheck `json:"checks"`
	StartedAt  *time.Time        `json:"startedAt,omitempty"`
	FinishedAt *time.Time        `json:"finishedAt,omitempty"`
	CreatedAt  time.Time         `json:"createdAt"`
}

type ValidationCheck struct {
	Name      string         `json:"name"`
	Source    string         `json:"source"`
	Status    string         `json:"status"`
	Required  bool           `json:"required"`
	Observed  any            `json:"observed,omitempty"`
	Threshold any            `json:"threshold,omitempty"`
	Message   string         `json:"message"`
	Evidence  map[string]any `json:"evidence,omitempty"`
}

type Scenario struct {
	ID          string         `json:"id" yaml:"id"`
	Name        string         `json:"name" yaml:"name"`
	ServiceRef  string         `json:"serviceRef" yaml:"serviceRef"`
	Environment string         `json:"environment" yaml:"environment"`
	Type        string         `json:"type" yaml:"type"`
	Schedule    map[string]any `json:"schedule" yaml:"schedule"`
	Spec        map[string]any `json:"spec" yaml:"spec"`
	Enabled     bool           `json:"enabled" yaml:"enabled"`
	Version     int            `json:"version" yaml:"version"`
}

type SyntheticResult struct {
	ScenarioID string            `json:"scenarioId"`
	Status     string            `json:"status"`
	DurationMS int64             `json:"durationMs"`
	HTTPStatus int               `json:"httpStatus"`
	Assertions []ValidationCheck `json:"assertions"`
	StartedAt  time.Time         `json:"startedAt"`
	FinishedAt time.Time         `json:"finishedAt"`
}

type SyntheticRun struct {
	ID         string         `json:"id"`
	ScenarioID string         `json:"scenarioId"`
	Scenario   string         `json:"scenario"`
	Status     string         `json:"status"`
	StartedAt  *time.Time     `json:"startedAt,omitempty"`
	FinishedAt *time.Time     `json:"finishedAt,omitempty"`
	Result     map[string]any `json:"result"`
}

type Incident struct {
	ID               string     `json:"id"`
	Title            string     `json:"title"`
	Severity         string     `json:"severity"`
	Status           string     `json:"status"`
	Commander        string     `json:"commander,omitempty"`
	Summary          string     `json:"summary,omitempty"`
	DeduplicationKey string     `json:"deduplicationKey,omitempty"`
	AcknowledgedAt   *time.Time `json:"acknowledgedAt,omitempty"`
	AcknowledgedBy   string     `json:"acknowledgedBy,omitempty"`
	CreatedAt        time.Time  `json:"createdAt"`
	ResolvedAt       *time.Time `json:"resolvedAt,omitempty"`
}

type AlertRoute struct {
	ID      string         `json:"id"`
	Name    string         `json:"name"`
	Spec    map[string]any `json:"spec"`
	Enabled bool           `json:"enabled"`
}

type NotificationDelivery struct {
	ID          string     `json:"id"`
	RouteID     string     `json:"routeId,omitempty"`
	Status      string     `json:"status"`
	Attempts    int        `json:"attempts"`
	LastError   string     `json:"lastError,omitempty"`
	CreatedAt   time.Time  `json:"createdAt"`
	DeliveredAt *time.Time `json:"deliveredAt,omitempty"`
}

type IncidentEscalation struct {
	ID          string     `json:"id"`
	RouteID     string     `json:"routeId"`
	TargetRef   string     `json:"targetRef"`
	Status      string     `json:"status"`
	ScheduledAt time.Time  `json:"scheduledAt"`
	CancelledAt *time.Time `json:"cancelledAt,omitempty"`
	EscalatedAt *time.Time `json:"escalatedAt,omitempty"`
}

// DataLifecycleRequest records an export or erasure request. Approval is a
// control-plane decision only: no data is exported or deleted by this model.
// A separately deployed fulfiller must record its evidence before it can move
// a request beyond approved.
type DataLifecycleRequest struct {
	ID          string         `json:"id"`
	RequestType string         `json:"requestType"`
	Status      string         `json:"status"`
	RequestedBy string         `json:"requestedBy"`
	ApprovedBy  string         `json:"approvedBy,omitempty"`
	Reason      string         `json:"reason"`
	Scope       map[string]any `json:"scope"`
	Evidence    map[string]any `json:"evidence"`
	CreatedAt   time.Time      `json:"createdAt"`
	UpdatedAt   time.Time      `json:"updatedAt"`
	CompletedAt *time.Time     `json:"completedAt,omitempty"`
}
