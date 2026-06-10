package services

import (
	"context"
	"errors"
	"testing"

	"github.com/afahey03/docklab/internal/models"
	"github.com/afahey03/docklab/internal/repositories"
	"golang.org/x/crypto/bcrypt"
)

type fakeUserRepo struct {
	users map[string]*models.User
}

func newFakeUserRepo() *fakeUserRepo {
	return &fakeUserRepo{users: map[string]*models.User{}}
}

func (r *fakeUserRepo) GetByEmail(_ context.Context, email string) (*models.User, error) {
	user, ok := r.users[email]
	if !ok {
		return nil, repositories.ErrUserNotFound
	}
	return user, nil
}

func (r *fakeUserRepo) Create(_ context.Context, email, passwordHash string) (*models.User, error) {
	if _, exists := r.users[email]; exists {
		return nil, repositories.ErrUserAlreadyExist
	}

	user := &models.User{
		ID:       "user-1",
		Email:    email,
		Password: passwordHash,
	}
	r.users[email] = user
	return user, nil
}

func (r *fakeUserRepo) UpsertOAuth(_ context.Context, email, passwordHash, provider string) (*models.User, error) {
	if existing, ok := r.users[email]; ok {
		return existing, nil
	}
	user := &models.User{
		ID:           "user-oauth-1",
		Email:        email,
		Password:     passwordHash,
		AuthProvider: provider,
	}
	r.users[email] = user
	return user, nil
}

func TestRegisterAndLoginSuccess(t *testing.T) {
	repo := newFakeUserRepo()
	svc := NewAuthService(repo, "test-secret", 60)

	registerToken, err := svc.Register(context.Background(), "user@example.com", "password123")
	if err != nil {
		t.Fatalf("expected register to succeed, got %v", err)
	}
	if registerToken == "" {
		t.Fatal("expected register token")
	}

	loginToken, err := svc.Login(context.Background(), "user@example.com", "password123")
	if err != nil {
		t.Fatalf("expected login to succeed, got %v", err)
	}
	if loginToken == "" {
		t.Fatal("expected login token")
	}

	storedUser, ok := repo.users["user@example.com"]
	if !ok {
		t.Fatal("expected user to be stored")
	}
	if err := bcrypt.CompareHashAndPassword([]byte(storedUser.Password), []byte("password123")); err != nil {
		t.Fatalf("expected password hash to validate, got %v", err)
	}
}

func TestLoginInvalidCredentials(t *testing.T) {
	repo := newFakeUserRepo()
	svc := NewAuthService(repo, "test-secret", 60)

	_, err := svc.Login(context.Background(), "missing@example.com", "password123")
	if !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("expected invalid credentials error, got %v", err)
	}
}

func TestRegisterWeakPassword(t *testing.T) {
	repo := newFakeUserRepo()
	svc := NewAuthService(repo, "test-secret", 60)

	_, err := svc.Register(context.Background(), "user@example.com", "short")
	if !errors.Is(err, ErrWeakPassword) {
		t.Fatalf("expected weak password error, got %v", err)
	}
}
