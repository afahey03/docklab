package handlers

import (
	"errors"
	"net/http"
	"net/url"

	"github.com/afahey03/docklab/internal/repositories"
	"github.com/afahey03/docklab/internal/services"
	"github.com/gin-gonic/gin"
)

type AuthHandler struct {
	authService     *services.AuthService
	githubOAuth     *services.GitHubOAuthService
	frontendBaseURL string
}

type LoginRequest struct {
	Email    string `json:"email" binding:"required,email"`
	Password string `json:"password" binding:"required,min=8"`
}

type RegisterRequest struct {
	Email    string `json:"email" binding:"required,email"`
	Password string `json:"password" binding:"required,min=8"`
}

type RefreshRequest struct {
	RefreshToken string `json:"refresh_token" binding:"required"`
}

func NewAuthHandler(authService *services.AuthService, githubOAuth *services.GitHubOAuthService, frontendBaseURL string) *AuthHandler {
	return &AuthHandler{
		authService:     authService,
		githubOAuth:     githubOAuth,
		frontendBaseURL: frontendBaseURL,
	}
}

func (h *AuthHandler) Login(c *gin.Context) {
	var req LoginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request payload"})
		return
	}

	if _, err := h.authService.Login(c.Request.Context(), req.Email, req.Password); err != nil {
		if errors.Is(err, services.ErrInvalidCredentials) {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid email or password"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to login"})
		return
	}

	pair, err := h.authService.IssueTokenPair(c.Request.Context(), req.Email)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to login"})
		return
	}

	c.JSON(http.StatusOK, pair)
}

func (h *AuthHandler) Register(c *gin.Context) {
	var req RegisterRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request payload"})
		return
	}

	if _, err := h.authService.Register(c.Request.Context(), req.Email, req.Password); err != nil {
		if errors.Is(err, repositories.ErrUserAlreadyExist) {
			c.JSON(http.StatusConflict, gin.H{"error": "user already exists"})
			return
		}
		if errors.Is(err, services.ErrWeakPassword) {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to register user"})
		return
	}

	pair, err := h.authService.IssueTokenPair(c.Request.Context(), req.Email)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to register user"})
		return
	}

	c.JSON(http.StatusCreated, pair)
}

func (h *AuthHandler) Refresh(c *gin.Context) {
	var req RefreshRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request payload"})
		return
	}

	pair, err := h.authService.Refresh(c.Request.Context(), req.RefreshToken)
	if err != nil {
		if errors.Is(err, services.ErrInvalidRefresh) {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid or expired refresh token"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to refresh session"})
		return
	}

	c.JSON(http.StatusOK, pair)
}

func (h *AuthHandler) Logout(c *gin.Context) {
	var req RefreshRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request payload"})
		return
	}

	if err := h.authService.RevokeRefreshToken(c.Request.Context(), req.RefreshToken); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to logout"})
		return
	}

	c.Status(http.StatusNoContent)
}

func (h *AuthHandler) Me(c *gin.Context) {
	email := c.GetString("user_email")
	c.JSON(http.StatusOK, gin.H{
		"email": email,
	})
}

// GitHubLogin redirects the browser to GitHub's OAuth authorize page.
func (h *AuthHandler) GitHubLogin(c *gin.Context) {
	authorizeURL, err := h.githubOAuth.AuthorizeURL()
	if err != nil {
		if errors.Is(err, services.ErrGitHubOAuthNotConfigured) {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "GitHub login is not configured on this server"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to start GitHub login"})
		return
	}

	c.Redirect(http.StatusFound, authorizeURL)
}

// GitHubCallback finishes the OAuth flow and redirects to the frontend with tokens in
// the URL fragment (fragments never reach servers or logs).
func (h *AuthHandler) GitHubCallback(c *gin.Context) {
	code := c.Query("code")
	state := c.Query("state")
	if code == "" {
		h.redirectWithOAuthError(c, "missing authorization code")
		return
	}

	pair, _, err := h.githubOAuth.HandleCallback(c.Request.Context(), code, state)
	if err != nil {
		h.redirectWithOAuthError(c, "GitHub sign-in failed")
		return
	}

	fragment := url.Values{}
	fragment.Set("token", pair.AccessToken)
	if pair.RefreshToken != "" {
		fragment.Set("refresh_token", pair.RefreshToken)
	}

	c.Redirect(http.StatusFound, h.frontendBaseURL+"/login#"+fragment.Encode())
}

func (h *AuthHandler) redirectWithOAuthError(c *gin.Context, message string) {
	fragment := url.Values{}
	fragment.Set("oauth_error", message)
	c.Redirect(http.StatusFound, h.frontendBaseURL+"/login#"+fragment.Encode())
}
