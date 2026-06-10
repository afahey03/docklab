package services

import "github.com/afahey03/docklab/internal/models"

// EnvironmentTemplates is the curated template marketplace catalog. Images are public
// and small enough for fast first launches.
var EnvironmentTemplates = []models.EnvironmentTemplate{
	{
		ID:          "blank-alpine",
		Name:        "Blank (Alpine)",
		Description: "Minimal Alpine Linux shell for general use.",
		Image:       "alpine:3.20",
		Language:    "shell",
	},
	{
		ID:          "ubuntu",
		Name:        "Ubuntu LTS",
		Description: "Full Ubuntu userland with apt for installing anything.",
		Image:       "ubuntu:24.04",
		Language:    "shell",
	},
	{
		ID:          "node",
		Name:        "Node.js 22",
		Description: "Node.js LTS with npm preinstalled for JavaScript and TypeScript.",
		Image:       "node:22-alpine",
		Language:    "javascript",
	},
	{
		ID:          "python",
		Name:        "Python 3.12",
		Description: "Python with pip for scripting, data work, and web backends.",
		Image:       "python:3.12-alpine",
		Language:    "python",
	},
	{
		ID:          "golang",
		Name:        "Go 1.25",
		Description: "Go toolchain for building backend services and CLIs.",
		Image:       "golang:1.25-alpine",
		Language:    "go",
	},
	{
		ID:          "rust",
		Name:        "Rust",
		Description: "Rust toolchain with cargo for systems programming.",
		Image:       "rust:1-alpine",
		Language:    "rust",
	},
}

// TemplateByID resolves a template, returning nil when the id is unknown.
func TemplateByID(id string) *models.EnvironmentTemplate {
	for i := range EnvironmentTemplates {
		if EnvironmentTemplates[i].ID == id {
			return &EnvironmentTemplates[i]
		}
	}
	return nil
}
