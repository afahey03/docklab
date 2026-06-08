package services

import "testing"

func TestNormalizeEC2KeyName(t *testing.T) {
	tests := map[string]string{
		"docklab-key":     "docklab-key",
		"docklab-key.pem": "docklab-key",
		"docklab-key.PEM": "docklab-key",
		"  my-key.pem  ":  "my-key",
		"":                "",
	}

	for input, want := range tests {
		if got := normalizeEC2KeyName(input); got != want {
			t.Fatalf("normalizeEC2KeyName(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestValidateProvisionRequestNormalizesKeyName(t *testing.T) {
	req, err := validateProvisionRequest(ProvisionRequest{
		Region:       "us-east-1",
		InstanceType: "t3.micro",
		AMI:          "ami-0c2b8ca1dad447f8a",
		KeyName:      "docklab-key.pem",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if req.KeyName != "docklab-key" {
		t.Fatalf("expected normalized key name docklab-key, got %q", req.KeyName)
	}
}
