package services

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/afahey03/docklab/internal/repositories"
)

type fakeRefreshRepo struct {
	tokens map[string]struct {
		email     string
		revoked   bool
		expiresAt time.Time
	}
}

func newFakeRefreshRepo() *fakeRefreshRepo {
	return &fakeRefreshRepo{tokens: map[string]struct {
		email     string
		revoked   bool
		expiresAt time.Time
	}{}}
}

func (r *fakeRefreshRepo) Create(_ context.Context, userEmail, tokenHash string, expiresAt time.Time) error {
	r.tokens[tokenHash] = struct {
		email     string
		revoked   bool
		expiresAt time.Time
	}{email: userEmail, expiresAt: expiresAt}
	return nil
}

func (r *fakeRefreshRepo) GetActive(_ context.Context, tokenHash string) (string, error) {
	entry, ok := r.tokens[tokenHash]
	if !ok || entry.revoked || entry.expiresAt.Before(time.Now()) {
		return "", repositories.ErrRefreshTokenNotFound
	}
	return entry.email, nil
}

func (r *fakeRefreshRepo) Revoke(_ context.Context, tokenHash string) error {
	entry, ok := r.tokens[tokenHash]
	if ok {
		entry.revoked = true
		r.tokens[tokenHash] = entry
	}
	return nil
}

func (r *fakeRefreshRepo) RevokeAllForUser(_ context.Context, userEmail string) error {
	for hash, entry := range r.tokens {
		if entry.email == userEmail {
			entry.revoked = true
			r.tokens[hash] = entry
		}
	}
	return nil
}

func (r *fakeRefreshRepo) DeleteExpired(_ context.Context) (int64, error) {
	return 0, nil
}

func TestIssueTokenPairAndRefreshRotation(t *testing.T) {
	svc := NewAuthService(newFakeUserRepo(), "test-secret", 60)
	svc.EnableRefreshTokens(newFakeRefreshRepo(), 30)

	pair, err := svc.IssueTokenPair(context.Background(), "user@example.com")
	if err != nil {
		t.Fatalf("expected token pair, got %v", err)
	}
	if pair.AccessToken == "" || pair.RefreshToken == "" {
		t.Fatal("expected both access and refresh tokens")
	}

	rotated, err := svc.Refresh(context.Background(), pair.RefreshToken)
	if err != nil {
		t.Fatalf("expected refresh to succeed, got %v", err)
	}
	if rotated.RefreshToken == pair.RefreshToken {
		t.Fatal("expected refresh token rotation to issue a new token")
	}

	// The presented token must be single-use.
	if _, err := svc.Refresh(context.Background(), pair.RefreshToken); !errors.Is(err, ErrInvalidRefresh) {
		t.Fatalf("expected reused refresh token to be rejected, got %v", err)
	}
}

func TestRevokeRefreshToken(t *testing.T) {
	svc := NewAuthService(newFakeUserRepo(), "test-secret", 60)
	svc.EnableRefreshTokens(newFakeRefreshRepo(), 30)

	pair, err := svc.IssueTokenPair(context.Background(), "user@example.com")
	if err != nil {
		t.Fatalf("expected token pair, got %v", err)
	}

	if err := svc.RevokeRefreshToken(context.Background(), pair.RefreshToken); err != nil {
		t.Fatalf("expected revoke to succeed, got %v", err)
	}
	if _, err := svc.Refresh(context.Background(), pair.RefreshToken); !errors.Is(err, ErrInvalidRefresh) {
		t.Fatalf("expected revoked token to be rejected, got %v", err)
	}
}

func TestRefreshWithoutRepoRejected(t *testing.T) {
	svc := NewAuthService(newFakeUserRepo(), "test-secret", 60)

	if _, err := svc.Refresh(context.Background(), "anything"); !errors.Is(err, ErrInvalidRefresh) {
		t.Fatalf("expected ErrInvalidRefresh when refresh repo is not wired, got %v", err)
	}
}
