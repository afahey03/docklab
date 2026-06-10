package handlers

import (
	"errors"
	"net/http"

	"github.com/afahey03/docklab/internal/repositories"
	"github.com/afahey03/docklab/internal/services"
	"github.com/gin-gonic/gin"
)

type ShareHandler struct {
	shareService       *services.ShareService
	environmentHandler *EnvironmentHandler
}

type CreateShareRequest struct {
	Email string `json:"email" binding:"required,email"`
}

func NewShareHandler(shareService *services.ShareService, environmentHandler *EnvironmentHandler) *ShareHandler {
	return &ShareHandler{
		shareService:       shareService,
		environmentHandler: environmentHandler,
	}
}

func (h *ShareHandler) Create(c *gin.Context) {
	var req CreateShareRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request payload"})
		return
	}

	environmentID := c.Param("id")
	ownerEmail := c.GetString("user_email")

	share, err := h.shareService.ShareEnvironment(c.Request.Context(), environmentID, ownerEmail, req.Email)
	if err != nil {
		h.handleError(c, err)
		return
	}

	c.JSON(http.StatusCreated, share)
}

func (h *ShareHandler) List(c *gin.Context) {
	environmentID := c.Param("id")
	ownerEmail := c.GetString("user_email")

	shares, err := h.shareService.ListShares(c.Request.Context(), environmentID, ownerEmail)
	if err != nil {
		h.handleError(c, err)
		return
	}

	c.JSON(http.StatusOK, gin.H{"shares": shares})
}

func (h *ShareHandler) Delete(c *gin.Context) {
	environmentID := c.Param("id")
	ownerEmail := c.GetString("user_email")
	sharedWithEmail := c.Param("email")

	if err := h.shareService.Unshare(c.Request.Context(), environmentID, ownerEmail, sharedWithEmail); err != nil {
		h.handleError(c, err)
		return
	}

	c.Status(http.StatusNoContent)
}

func (h *ShareHandler) handleError(c *gin.Context, err error) {
	if errors.Is(err, services.ErrCannotShareWithSelf) {
		c.JSON(http.StatusBadRequest, APIErrorResponse{Code: "cannot_share_with_self", Error: err.Error()})
		return
	}
	if errors.Is(err, services.ErrShareUserNotFound) {
		c.JSON(http.StatusNotFound, APIErrorResponse{Code: "share_user_not_found", Error: err.Error()})
		return
	}
	if errors.Is(err, repositories.ErrShareAlreadyExists) {
		c.JSON(http.StatusConflict, APIErrorResponse{Code: "share_already_exists", Error: err.Error()})
		return
	}

	h.environmentHandler.handleServiceError(c, err)
}
