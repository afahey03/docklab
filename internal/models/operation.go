package models

import "time"

type Operation struct {
	ID            string    `json:"id"`
	UserEmail     string    `json:"user_email"`
	EnvironmentID string    `json:"environment_id"`
	Type          string    `json:"type"`
	Status        string    `json:"status"`
	Error         string    `json:"error"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}
