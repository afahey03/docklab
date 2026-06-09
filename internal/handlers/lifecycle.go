package handlers

import (
	"net/http"

	"github.com/afahey03/docklab/internal/services"
	"github.com/gin-gonic/gin"
)

type LifecycleHandler struct {
	cloudLifecycle *services.CloudLifecycleService
}

func NewLifecycleHandler(cloudLifecycle *services.CloudLifecycleService) *LifecycleHandler {
	return &LifecycleHandler{cloudLifecycle: cloudLifecycle}
}

func (h *LifecycleHandler) GetPolicy(c *gin.Context) {
	c.JSON(http.StatusOK, h.cloudLifecycle.Policy())
}
