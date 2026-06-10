package services

import (
	"strings"
	"testing"
)

func TestNormalizeRepoURL(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{name: "empty is allowed", input: "", wantErr: false},
		{name: "https url ok", input: "https://github.com/user/repo", wantErr: false},
		{name: "https with .git ok", input: "https://github.com/user/repo.git", wantErr: false},
		{name: "ssh url rejected", input: "git@github.com:user/repo.git", wantErr: true},
		{name: "http rejected", input: "http://github.com/user/repo", wantErr: true},
		{name: "shell metacharacters rejected", input: "https://github.com/user/repo; rm -rf /", wantErr: true},
		{name: "quotes rejected", input: `https://github.com/user/"repo"`, wantErr: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := normalizeRepoURL(tc.input)
			if tc.wantErr && err == nil {
				t.Fatalf("expected error for %q", tc.input)
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("expected no error for %q, got %v", tc.input, err)
			}
		})
	}
}

func TestBuildCloneScriptIncludesRepo(t *testing.T) {
	script := buildCloneScript("https://github.com/user/repo.git")
	if !strings.Contains(script, "https://github.com/user/repo.git") {
		t.Fatal("expected clone script to reference the repo URL")
	}
	if !strings.Contains(script, "git clone") {
		t.Fatal("expected clone script to invoke git clone")
	}
	if !strings.Contains(script, "/workspace") {
		t.Fatal("expected clone script to target /workspace")
	}
}

func TestTemplateCatalogIsValid(t *testing.T) {
	if len(EnvironmentTemplates) == 0 {
		t.Fatal("expected at least one environment template")
	}

	seen := map[string]bool{}
	for _, template := range EnvironmentTemplates {
		if template.ID == "" || template.Name == "" || template.Image == "" {
			t.Fatalf("template missing required fields: %+v", template)
		}
		if seen[template.ID] {
			t.Fatalf("duplicate template id %s", template.ID)
		}
		seen[template.ID] = true
	}
}
