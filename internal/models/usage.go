package models

import "time"

// EnvironmentUsage is a single EC2 runtime session for an environment. A session opens
// when the instance becomes billable (provisioned or restarted) and closes when it is
// stopped, terminated, or the environment is deleted.
type EnvironmentUsage struct {
	ID               string     `json:"id"`
	EnvironmentID    string     `json:"environment_id"`
	UserEmail        string     `json:"user_email"`
	EnvironmentName  string     `json:"environment_name"`
	InstanceType     string     `json:"instance_type"`
	Region           string     `json:"region"`
	HourlyRateUSD    float64    `json:"hourly_rate_usd"`
	StartedAt        time.Time  `json:"started_at"`
	EndedAt          *time.Time `json:"ended_at"`
	RuntimeMinutes   float64    `json:"runtime_minutes"`
	EstimatedCostUSD float64    `json:"estimated_cost_usd"`
}
