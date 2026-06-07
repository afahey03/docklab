package models

import "time"

type Environment struct {
	ID                 string     `json:"id"`
	UserEmail          string     `json:"user_email"`
	Name               string     `json:"name"`
	Image              string     `json:"image"`
	Status             string     `json:"status"`
	ContainerID        string     `json:"container_id"`
	CloudStatus        string     `json:"cloud_status"`
	CloudRegion        string     `json:"cloud_region"`
	CloudInstanceType  string     `json:"cloud_instance_type"`
	InstanceID         string     `json:"instance_id"`
	PublicIP           string     `json:"public_ip"`
	TerraformDir       string     `json:"terraform_dir"`
	CloudError         string     `json:"cloud_error"`
	CloudProvisionedAt *time.Time `json:"cloud_provisioned_at"`
	LastActivityAt     time.Time  `json:"last_activity_at"`
	CreatedAt          time.Time  `json:"created_at"`
	UpdatedAt          time.Time  `json:"updated_at"`
}
