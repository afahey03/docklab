package services

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/afahey03/docklab/internal/repositories"
	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"
)

var (
	ErrGitHubOAuthNotConfigured = errors.New("GitHub OAuth is not configured on this server")
	ErrGitHubOAuthFailed        = errors.New("GitHub OAuth sign-in failed")
)

// GitHubOAuthService implements the GitHub authorization-code flow. The state parameter
// is a short-lived signed JWT so no server-side session storage is needed.
type GitHubOAuthService struct {
	clientID     string
	clientSecret string
	redirectURL  string
	userRepo     repositories.UserRepository
	authService  *AuthService
	httpClient   *http.Client
	stateSecret  []byte

	authorizeEndpoint string
	tokenEndpoint     string
	apiBaseURL        string
}

func NewGitHubOAuthService(clientID, clientSecret, redirectURL, stateSecret string, userRepo repositories.UserRepository, authService *AuthService) *GitHubOAuthService {
	return &GitHubOAuthService{
		clientID:          clientID,
		clientSecret:      clientSecret,
		redirectURL:       redirectURL,
		userRepo:          userRepo,
		authService:       authService,
		httpClient:        &http.Client{Timeout: 15 * time.Second},
		stateSecret:       []byte(stateSecret),
		authorizeEndpoint: "https://github.com/login/oauth/authorize",
		tokenEndpoint:     "https://github.com/login/oauth/access_token",
		apiBaseURL:        "https://api.github.com",
	}
}

func (s *GitHubOAuthService) Configured() bool {
	return s != nil && s.clientID != "" && s.clientSecret != ""
}

// AuthorizeURL builds the GitHub authorize redirect with a signed state token.
func (s *GitHubOAuthService) AuthorizeURL() (string, error) {
	if !s.Configured() {
		return "", ErrGitHubOAuthNotConfigured
	}

	state, err := s.newStateToken()
	if err != nil {
		return "", err
	}

	query := url.Values{}
	query.Set("client_id", s.clientID)
	query.Set("scope", "read:user user:email")
	query.Set("state", state)
	if s.redirectURL != "" {
		query.Set("redirect_uri", s.redirectURL)
	}

	return s.authorizeEndpoint + "?" + query.Encode(), nil
}

// HandleCallback exchanges the code, resolves the GitHub user's email, upserts the
// DockLab user, and returns a DockLab token pair.
func (s *GitHubOAuthService) HandleCallback(ctx context.Context, code, state string) (*TokenPair, string, error) {
	if !s.Configured() {
		return nil, "", ErrGitHubOAuthNotConfigured
	}
	if err := s.validateStateToken(state); err != nil {
		return nil, "", fmt.Errorf("%w: invalid state", ErrGitHubOAuthFailed)
	}

	accessToken, err := s.exchangeCode(ctx, code)
	if err != nil {
		return nil, "", err
	}

	email, err := s.fetchPrimaryEmail(ctx, accessToken)
	if err != nil {
		return nil, "", err
	}

	// OAuth users get an unusable random password hash; they sign in via GitHub only.
	randomSecret := make([]byte, 32)
	if _, err := rand.Read(randomSecret); err != nil {
		return nil, "", err
	}
	passwordHash, err := bcrypt.GenerateFromPassword([]byte(hex.EncodeToString(randomSecret)), bcrypt.DefaultCost)
	if err != nil {
		return nil, "", err
	}

	user, err := s.userRepo.UpsertOAuth(ctx, email, string(passwordHash), "github")
	if err != nil {
		return nil, "", err
	}

	pair, err := s.authService.IssueTokenPair(ctx, user.Email)
	if err != nil {
		return nil, "", err
	}
	return pair, user.Email, nil
}

func (s *GitHubOAuthService) exchangeCode(ctx context.Context, code string) (string, error) {
	form := url.Values{}
	form.Set("client_id", s.clientID)
	form.Set("client_secret", s.clientSecret)
	form.Set("code", code)
	if s.redirectURL != "" {
		form.Set("redirect_uri", s.redirectURL)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.tokenEndpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("%w: token exchange: %v", ErrGitHubOAuthFailed, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", err
	}

	var payload struct {
		AccessToken      string `json:"access_token"`
		Error            string `json:"error"`
		ErrorDescription string `json:"error_description"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return "", fmt.Errorf("%w: parse token response", ErrGitHubOAuthFailed)
	}
	if payload.AccessToken == "" {
		return "", fmt.Errorf("%w: %s", ErrGitHubOAuthFailed, payload.ErrorDescription)
	}

	return payload.AccessToken, nil
}

func (s *GitHubOAuthService) fetchPrimaryEmail(ctx context.Context, accessToken string) (string, error) {
	emails, err := s.githubGET(ctx, accessToken, "/user/emails")
	if err == nil {
		var parsed []struct {
			Email    string `json:"email"`
			Primary  bool   `json:"primary"`
			Verified bool   `json:"verified"`
		}
		if json.Unmarshal(emails, &parsed) == nil {
			for _, item := range parsed {
				if item.Primary && item.Verified {
					return strings.ToLower(item.Email), nil
				}
			}
			for _, item := range parsed {
				if item.Verified {
					return strings.ToLower(item.Email), nil
				}
			}
		}
	}

	// Fall back to the public profile email.
	profile, err := s.githubGET(ctx, accessToken, "/user")
	if err != nil {
		return "", err
	}
	var user struct {
		Email string `json:"email"`
		Login string `json:"login"`
	}
	if err := json.Unmarshal(profile, &user); err != nil {
		return "", fmt.Errorf("%w: parse user profile", ErrGitHubOAuthFailed)
	}
	if user.Email != "" {
		return strings.ToLower(user.Email), nil
	}
	if user.Login != "" {
		// GitHub users may hide all emails; use the noreply convention.
		return strings.ToLower(user.Login) + "@users.noreply.github.com", nil
	}

	return "", fmt.Errorf("%w: no email available on GitHub account", ErrGitHubOAuthFailed)
}

func (s *GitHubOAuthService) githubGET(ctx context.Context, accessToken, path string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.apiBaseURL+path, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Accept", "application/vnd.github+json")

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: github api: %v", ErrGitHubOAuthFailed, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("%w: github api returned %d for %s", ErrGitHubOAuthFailed, resp.StatusCode, path)
	}

	return io.ReadAll(io.LimitReader(resp.Body, 1<<20))
}

func (s *GitHubOAuthService) newStateToken() (string, error) {
	claims := jwt.RegisteredClaims{
		Subject:   "github-oauth-state",
		ExpiresAt: jwt.NewNumericDate(time.Now().Add(10 * time.Minute)),
		IssuedAt:  jwt.NewNumericDate(time.Now()),
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString(s.stateSecret)
}

func (s *GitHubOAuthService) validateStateToken(state string) error {
	token, err := jwt.Parse(state, func(token *jwt.Token) (interface{}, error) {
		if token.Method != jwt.SigningMethodHS256 {
			return nil, errors.New("invalid signing method")
		}
		return s.stateSecret, nil
	})
	if err != nil || !token.Valid {
		return errors.New("invalid state token")
	}
	return nil
}
