package repositories

import (
	"context"
	"testing"
)

func TestGetByEmailWithNilDatabase(t *testing.T) {
	repo := NewPostgresUserRepository(nil)

	user, err := repo.GetByEmail(context.Background(), "test@example.com")
	if user != nil {
		t.Fatal("expected nil user for nil database")
	}
	if err == nil {
		t.Fatal("expected error")
	}
	if err.Error() != "database connection is nil" {
		t.Fatalf("expected nil database error, got %q", err.Error())
	}
}
