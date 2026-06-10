package services

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"time"

	"github.com/afahey03/docklab/internal/repositories"
	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"
)

type AuthService struct {
	userRepo    repositories.UserRepository
	refreshRepo repositories.RefreshTokenRepository
	jwtSecret   []byte
	ttl         time.Duration
	refreshTTL  time.Duration
}

// TokenPair bundles a short-lived access token with a long-lived refresh token.
type TokenPair struct {
	AccessToken  string `json:"token"`
	RefreshToken string `json:"refresh_token,omitempty"`
}

type Claims struct {
	Email string `json:"email"`
	jwt.RegisteredClaims
}

var (
	ErrInvalidCredentials = errors.New("invalid credentials")
	ErrWeakPassword       = errors.New("password must be at least 8 characters")
	ErrInvalidRefresh     = errors.New("invalid or expired refresh token")
)

func NewAuthService(userRepo repositories.UserRepository, secret string, ttlMinutes int) *AuthService {
	return &AuthService{
		userRepo:   userRepo,
		jwtSecret:  []byte(secret),
		ttl:        time.Duration(ttlMinutes) * time.Minute,
		refreshTTL: 30 * 24 * time.Hour,
	}
}

// EnableRefreshTokens wires the persistence layer for refresh tokens. Without it the
// service still issues access tokens but refresh-related methods return ErrInvalidRefresh.
func (a *AuthService) EnableRefreshTokens(refreshRepo repositories.RefreshTokenRepository, ttlDays int) {
	a.refreshRepo = refreshRepo
	if ttlDays > 0 {
		a.refreshTTL = time.Duration(ttlDays) * 24 * time.Hour
	}
}

func (a *AuthService) Register(ctx context.Context, email, password string) (string, error) {
	if len(password) < 8 {
		return "", ErrWeakPassword
	}

	passwordHash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return "", err
	}

	user, err := a.userRepo.Create(ctx, email, string(passwordHash))
	if err != nil {
		return "", err
	}

	return a.GenerateToken(user.Email)
}

func (a *AuthService) Login(ctx context.Context, email, password string) (string, error) {
	user, err := a.userRepo.GetByEmail(ctx, email)
	if err != nil {
		if errors.Is(err, repositories.ErrUserNotFound) {
			return "", ErrInvalidCredentials
		}
		return "", err
	}

	if err := bcrypt.CompareHashAndPassword([]byte(user.Password), []byte(password)); err != nil {
		return "", ErrInvalidCredentials
	}

	return a.GenerateToken(user.Email)
}

// IssueTokenPair returns a fresh access token plus a rotating refresh token.
func (a *AuthService) IssueTokenPair(ctx context.Context, email string) (*TokenPair, error) {
	accessToken, err := a.GenerateToken(email)
	if err != nil {
		return nil, err
	}

	pair := &TokenPair{AccessToken: accessToken}
	if a.refreshRepo == nil {
		return pair, nil
	}

	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return nil, err
	}
	refreshToken := hex.EncodeToString(raw)

	if err := a.refreshRepo.Create(ctx, email, hashRefreshToken(refreshToken), time.Now().Add(a.refreshTTL)); err != nil {
		return nil, err
	}

	pair.RefreshToken = refreshToken
	return pair, nil
}

// Refresh validates and rotates a refresh token, returning a new token pair.
func (a *AuthService) Refresh(ctx context.Context, refreshToken string) (*TokenPair, error) {
	if a.refreshRepo == nil || refreshToken == "" {
		return nil, ErrInvalidRefresh
	}

	tokenHash := hashRefreshToken(refreshToken)
	email, err := a.refreshRepo.GetActive(ctx, tokenHash)
	if err != nil {
		if errors.Is(err, repositories.ErrRefreshTokenNotFound) {
			return nil, ErrInvalidRefresh
		}
		return nil, err
	}

	// Rotation: revoke the presented token before issuing a replacement.
	if err := a.refreshRepo.Revoke(ctx, tokenHash); err != nil {
		return nil, err
	}

	return a.IssueTokenPair(ctx, email)
}

// RevokeRefreshToken invalidates a refresh token (logout).
func (a *AuthService) RevokeRefreshToken(ctx context.Context, refreshToken string) error {
	if a.refreshRepo == nil || refreshToken == "" {
		return nil
	}
	return a.refreshRepo.Revoke(ctx, hashRefreshToken(refreshToken))
}

func hashRefreshToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func (a *AuthService) GenerateToken(email string) (string, error) {
	now := time.Now()
	claims := Claims{
		Email: email,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   email,
			ExpiresAt: jwt.NewNumericDate(now.Add(a.ttl)),
			IssuedAt:  jwt.NewNumericDate(now),
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString(a.jwtSecret)
}

func (a *AuthService) ParseToken(tokenString string) (*Claims, error) {
	token, err := jwt.ParseWithClaims(tokenString, &Claims{}, func(token *jwt.Token) (interface{}, error) {
		if token.Method != jwt.SigningMethodHS256 {
			return nil, errors.New("invalid signing method")
		}
		return a.jwtSecret, nil
	})
	if err != nil {
		return nil, err
	}

	claims, ok := token.Claims.(*Claims)
	if !ok || !token.Valid {
		return nil, errors.New("invalid token claims")
	}

	return claims, nil
}
