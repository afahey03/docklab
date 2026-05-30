package models

import "time"

type Environment struct {
	ID           string    `json:"id"`
	UserEmail    string    `json:"user_email"`
	Name         string    `json:"name"`
	Image        string    `json:"image"`
	Status       string    `json:"status"`
	ContainerID  string    `json:"container_id"`
	CloudStatus  string    `json:"cloud_status"`
	CloudRegion  string    `json:"cloud_region"`
	InstanceID   string    `json:"instance_id"`
	PublicIP     string    `json:"public_ip"`
	TerraformDir string    `json:"terraform_dir"`
	CloudError   string    `json:"cloud_error"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}
