package handlers

import (
	"errors"
	"net/http"

	"github.com/afahey03/docklab/internal/repositories"
	"github.com/afahey03/docklab/internal/services"
	"github.com/gin-gonic/gin"
)

type EnvironmentHandler struct {
	environmentService *services.EnvironmentService
}

type CreateEnvironmentRequest struct {
	Name  string `json:"name"`
	Image string `json:"image"`
}

type ProvisionEnvironmentRequest struct {
	Region       string `json:"region"`
	InstanceType string `json:"instance_type"`
	AMI          string `json:"ami"`
	KeyName      string `json:"key_name"`
}

func NewEnvironmentHandler(environmentService *services.EnvironmentService) *EnvironmentHandler {
	return &EnvironmentHandler{environmentService: environmentService}
}

func (h *EnvironmentHandler) Create(c *gin.Context) {
	var req CreateEnvironmentRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request payload"})
		return
	}

	userEmail := c.GetString("user_email")
	env, err := h.environmentService.CreateEnvironment(c.Request.Context(), userEmail, req.Name, req.Image)
	if err != nil {
		h.handleServiceError(c, err)
		return
	}

	c.JSON(http.StatusCreated, env)
}

func (h *EnvironmentHandler) List(c *gin.Context) {
	userEmail := c.GetString("user_email")
	environments, err := h.environmentService.ListEnvironments(c.Request.Context(), userEmail)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list environments"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"environments": environments})
}

func (h *EnvironmentHandler) Get(c *gin.Context) {
	id := c.Param("id")
	userEmail := c.GetString("user_email")
	env, err := h.environmentService.GetEnvironment(c.Request.Context(), id, userEmail)
	if err != nil {
		h.handleServiceError(c, err)
		return
	}

	c.JSON(http.StatusOK, env)
}

func (h *EnvironmentHandler) Provision(c *gin.Context) {
	var req ProvisionEnvironmentRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request payload"})
		return
	}

	id := c.Param("id")
	userEmail := c.GetString("user_email")
	env, err := h.environmentService.ProvisionEnvironment(c.Request.Context(), id, userEmail, services.ProvisionRequest{
		Region:       req.Region,
		InstanceType: req.InstanceType,
		AMI:          req.AMI,
		KeyName:      req.KeyName,
	})
	if err != nil {
		h.handleServiceError(c, err)
		return
	}

	c.JSON(http.StatusOK, env)
}

func (h *EnvironmentHandler) Start(c *gin.Context) {
	id := c.Param("id")
	userEmail := c.GetString("user_email")
	env, err := h.environmentService.StartEnvironment(c.Request.Context(), id, userEmail)
	if err != nil {
		h.handleServiceError(c, err)
		return
	}

	c.JSON(http.StatusOK, env)
}

func (h *EnvironmentHandler) Stop(c *gin.Context) {
	id := c.Param("id")
	userEmail := c.GetString("user_email")
	env, err := h.environmentService.StopEnvironment(c.Request.Context(), id, userEmail)
	if err != nil {
		h.handleServiceError(c, err)
		return
	}

	c.JSON(http.StatusOK, env)
}

func (h *EnvironmentHandler) Delete(c *gin.Context) {
	id := c.Param("id")
	userEmail := c.GetString("user_email")
	if err := h.environmentService.DeleteEnvironment(c.Request.Context(), id, userEmail); err != nil {
		h.handleServiceError(c, err)
		return
	}

	c.Status(http.StatusNoContent)
}

func (h *EnvironmentHandler) handleServiceError(c *gin.Context, err error) {
	if errors.Is(err, repositories.ErrEnvironmentNotFound) {
		c.JSON(http.StatusNotFound, gin.H{"error": "environment not found"})
		return
	}
	if errors.Is(err, services.ErrDockerUnavailable) {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": err.Error()})
		return
	}
	if errors.Is(err, services.ErrTerraformUnavailable) {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": err.Error()})
		return
	}
	if errors.Is(err, services.ErrProvisionInProgress) {
		c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
}
