package models

import "time"

// EnvironmentSnapshot is a point-in-time image of a workspace container. Workspace
// files under /workspace are archived into the image before docker commit because
// mounted volumes are excluded from commits. Restoring recreates the container and
// unpacks that archive back onto the volume.
type EnvironmentSnapshot struct {
	ID            string    `json:"id"`
	EnvironmentID string    `json:"environment_id"`
	UserEmail     string    `json:"user_email"`
	ImageTag      string    `json:"image_tag"`
	Note          string    `json:"note"`
	RuntimeTarget string    `json:"runtime_target"`
	CreatedAt     time.Time `json:"created_at"`
}
