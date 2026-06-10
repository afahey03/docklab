package handlers

import (
	"errors"
	"net/http"

	"github.com/afahey03/docklab/internal/repositories"
	"github.com/afahey03/docklab/internal/services"
	"github.com/gin-gonic/gin"
)

type SnapshotHandler struct {
	snapshotService    *services.SnapshotService
	environmentHandler *EnvironmentHandler
}

type CreateSnapshotRequest struct {
	Note string `json:"note"`
}

func NewSnapshotHandler(snapshotService *services.SnapshotService, environmentHandler *EnvironmentHandler) *SnapshotHandler {
	return &SnapshotHandler{
		snapshotService:    snapshotService,
		environmentHandler: environmentHandler,
	}
}

func (h *SnapshotHandler) Create(c *gin.Context) {
	var req CreateSnapshotRequest
	// Body is optional; an empty note is fine.
	_ = c.ShouldBindJSON(&req)

	environmentID := c.Param("id")
	userEmail := c.GetString("user_email")

	snapshot, err := h.snapshotService.CreateSnapshot(c.Request.Context(), environmentID, userEmail, req.Note)
	if err != nil {
		h.handleError(c, err)
		return
	}

	c.JSON(http.StatusCreated, snapshot)
}

func (h *SnapshotHandler) List(c *gin.Context) {
	environmentID := c.Param("id")
	userEmail := c.GetString("user_email")

	snapshots, err := h.snapshotService.ListSnapshots(c.Request.Context(), environmentID, userEmail)
	if err != nil {
		h.handleError(c, err)
		return
	}

	c.JSON(http.StatusOK, gin.H{"snapshots": snapshots})
}

func (h *SnapshotHandler) Restore(c *gin.Context) {
	environmentID := c.Param("id")
	snapshotID := c.Param("snapshotId")
	userEmail := c.GetString("user_email")

	env, err := h.snapshotService.RestoreSnapshot(c.Request.Context(), environmentID, snapshotID, userEmail)
	if err != nil {
		h.handleError(c, err)
		return
	}

	c.JSON(http.StatusOK, env)
}

func (h *SnapshotHandler) Delete(c *gin.Context) {
	environmentID := c.Param("id")
	snapshotID := c.Param("snapshotId")
	userEmail := c.GetString("user_email")

	if err := h.snapshotService.DeleteSnapshot(c.Request.Context(), environmentID, snapshotID, userEmail); err != nil {
		h.handleError(c, err)
		return
	}

	c.Status(http.StatusNoContent)
}

func (h *SnapshotHandler) handleError(c *gin.Context, err error) {
	if errors.Is(err, repositories.ErrSnapshotNotFound) {
		c.JSON(http.StatusNotFound, APIErrorResponse{Code: "snapshot_not_found", Error: "snapshot not found"})
		return
	}
	if errors.Is(err, services.ErrSnapshotUnsupportedRuntime) {
		c.JSON(http.StatusConflict, APIErrorResponse{Code: "snapshot_unsupported_runtime", Error: err.Error()})
		return
	}

	h.environmentHandler.handleServiceError(c, err)
}
