package models

// EnvironmentTemplate is a curated, prebuilt workspace definition users can launch
// from the dashboard template marketplace.
type EnvironmentTemplate struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Image       string `json:"image"`
	Language    string `json:"language"`
}
