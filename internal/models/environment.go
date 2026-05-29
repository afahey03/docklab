package models

import "time"

type Environment struct {
	ID          string    `json:"id"`
	UserEmail   string    `json:"user_email"`
	Name        string    `json:"name"`
	Image       string    `json:"image"`
	Status      string    `json:"status"`
	ContainerID string    `json:"container_id"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}
