package repositories

import (
	"context"
	"testing"
)

func TestGetByEmailNotImplemented(t *testing.T) {
	repo := NewPostgresUserRepository(nil)

	user, err := repo.GetByEmail(context.Background(), "test@example.com")
	if user != nil {
		t.Fatal("expected nil user for unimplemented repository")
	}
	if err == nil {
		t.Fatal("expected not implemented error")
	}
	if err.Error() != "not implemented" {
		t.Fatalf("expected not implemented error, got %q", err.Error())
	}
}
