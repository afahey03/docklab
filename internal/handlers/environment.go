package handlers

import (
	"errors"
	"net/http"

	"github.com/afahey03/docklab/internal/repositories"
	"github.com/afahey03/docklab/internal/services"
	"github.com/gin-gonic/gin"
)

type APIErrorResponse struct {
	Code  string `json:"code"`
	Error string `json:"error"`
}

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
	op, err := h.environmentService.QueueProvisionEnvironment(c.Request.Context(), id, userEmail, services.ProvisionRequest{
		Region:       req.Region,
		InstanceType: req.InstanceType,
		AMI:          req.AMI,
		KeyName:      req.KeyName,
	})
	if err != nil {
		h.handleServiceError(c, err)
		return
	}

	c.JSON(http.StatusAccepted, op)
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
	op, err := h.environmentService.QueueDeleteEnvironment(c.Request.Context(), id, userEmail)
	if err != nil {
		h.handleServiceError(c, err)
		return
	}

	c.JSON(http.StatusAccepted, op)
}

func (h *EnvironmentHandler) DestroyCloud(c *gin.Context) {
	id := c.Param("id")
	userEmail := c.GetString("user_email")
	op, err := h.environmentService.QueueDestroyCloudEnvironment(c.Request.Context(), id, userEmail)
	if err != nil {
		h.handleServiceError(c, err)
		return
	}

	c.JSON(http.StatusAccepted, op)
}

func (h *EnvironmentHandler) GetOperation(c *gin.Context) {
	operationID := c.Param("id")
	userEmail := c.GetString("user_email")
	op, err := h.environmentService.GetOperation(c.Request.Context(), operationID, userEmail)
	if err != nil {
		h.handleServiceError(c, err)
		return
	}

	c.JSON(http.StatusOK, op)
}

func (h *EnvironmentHandler) handleServiceError(c *gin.Context, err error) {
	if errors.Is(err, repositories.ErrEnvironmentNotFound) {
		c.JSON(http.StatusNotFound, APIErrorResponse{Code: "environment_not_found", Error: "environment not found"})
		return
	}
	if errors.Is(err, services.ErrDockerUnavailable) {
		c.JSON(http.StatusServiceUnavailable, APIErrorResponse{Code: "docker_unavailable", Error: err.Error()})
		return
	}
	if errors.Is(err, services.ErrTerraformUnavailable) {
		c.JSON(http.StatusServiceUnavailable, APIErrorResponse{Code: "terraform_unavailable", Error: err.Error()})
		return
	}
	if errors.Is(err, services.ErrTerraformStateBackendConfigMissing) {
		c.JSON(http.StatusServiceUnavailable, APIErrorResponse{Code: "terraform_state_config_missing", Error: err.Error()})
		return
	}
	if errors.Is(err, services.ErrProvisionInProgress) {
		c.JSON(http.StatusConflict, APIErrorResponse{Code: "provision_in_progress", Error: err.Error()})
		return
	}
	if errors.Is(err, services.ErrOperationInProgress) {
		c.JSON(http.StatusConflict, APIErrorResponse{Code: "operation_in_progress", Error: err.Error()})
		return
	}
	if errors.Is(err, services.ErrOperationNotFound) {
		c.JSON(http.StatusNotFound, APIErrorResponse{Code: "operation_not_found", Error: "operation not found"})
		return
	}

	var validationErr *services.ProvisionValidationError
	if errors.As(err, &validationErr) {
		c.JSON(http.StatusBadRequest, APIErrorResponse{Code: validationErr.Code, Error: validationErr.Error()})
		return
	}

	c.JSON(http.StatusInternalServerError, APIErrorResponse{Code: "internal_error", Error: err.Error()})
}
