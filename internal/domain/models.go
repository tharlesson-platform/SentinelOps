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
