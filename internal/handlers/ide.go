package handlers

import (
	"errors"
	"net/http"

	"github.com/afahey03/docklab/internal/services"
	"github.com/gin-gonic/gin"
)

type IDEHandler struct {
	ideService         *services.IDEService
	environmentService *services.EnvironmentService
	environmentHandler *EnvironmentHandler
}

func NewIDEHandler(ideService *services.IDEService, environmentService *services.EnvironmentService, environmentHandler *EnvironmentHandler) *IDEHandler {
	return &IDEHandler{
		ideService:         ideService,
		environmentService: environmentService,
		environmentHandler: environmentHandler,
	}
}

func (h *IDEHandler) Start(c *gin.Context) {
	environmentID := c.Param("id")
	userEmail := c.GetString("user_email")

	env, err := h.environmentService.GetEnvironment(c.Request.Context(), environmentID, userEmail)
	if err != nil {
		h.handleError(c, err)
		return
	}

	status, err := h.ideService.Start(c.Request.Context(), env)
	if err != nil {
		h.handleError(c, err)
		return
	}

	c.JSON(http.StatusOK, status)
}

func (h *IDEHandler) Stop(c *gin.Context) {
	environmentID := c.Param("id")
	userEmail := c.GetString("user_email")

	env, err := h.environmentService.GetEnvironment(c.Request.Context(), environmentID, userEmail)
	if err != nil {
		h.handleError(c, err)
		return
	}

	if err := h.ideService.Stop(c.Request.Context(), env); err != nil {
		h.handleError(c, err)
		return
	}

	c.Status(http.StatusNoContent)
}

func (h *IDEHandler) Status(c *gin.Context) {
	environmentID := c.Param("id")
	userEmail := c.GetString("user_email")

	env, err := h.environmentService.GetEnvironment(c.Request.Context(), environmentID, userEmail)
	if err != nil {
		h.handleError(c, err)
		return
	}

	status, err := h.ideService.Status(c.Request.Context(), env)
	if err != nil {
		h.handleError(c, err)
		return
	}

	c.JSON(http.StatusOK, status)
}

func (h *IDEHandler) handleError(c *gin.Context, err error) {
	if errors.Is(err, services.ErrIDEDisabled) {
		c.JSON(http.StatusServiceUnavailable, APIErrorResponse{Code: "ide_disabled", Error: err.Error()})
		return
	}
	if errors.Is(err, services.ErrIDEUnsupportedRuntime) {
		c.JSON(http.StatusConflict, APIErrorResponse{Code: "ide_unsupported_runtime", Error: err.Error()})
		return
	}

	h.environmentHandler.handleServiceError(c, err)
}
