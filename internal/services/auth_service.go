package services

import (
	"context"
	"errors"
	"time"

	"github.com/afahey03/docklab/internal/repositories"
	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"
)

type AuthService struct {
	userRepo  repositories.UserRepository
	jwtSecret []byte
	ttl       time.Duration
}

type Claims struct {
	Email string `json:"email"`
	jwt.RegisteredClaims
}

var (
	ErrInvalidCredentials = errors.New("invalid credentials")
	ErrWeakPassword      = errors.New("password must be at least 8 characters")
)

func NewAuthService(userRepo repositories.UserRepository, secret string, ttlMinutes int) *AuthService {
	return &AuthService{
		userRepo:  userRepo,
		jwtSecret: []byte(secret),
		ttl:       time.Duration(ttlMinutes) * time.Minute,
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
