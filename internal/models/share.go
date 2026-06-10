package models

import "time"

// EnvironmentShare grants another user read-and-terminal access to an environment.
type EnvironmentShare struct {
	ID              string    `json:"id"`
	EnvironmentID   string    `json:"environment_id"`
	OwnerEmail      string    `json:"owner_email"`
	SharedWithEmail string    `json:"shared_with_email"`
	CreatedAt       time.Time `json:"created_at"`
}
