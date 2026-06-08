package handlers

import (
	"errors"
	"net/http"

	"github.com/afahey03/docklab/internal/repositories"
	"github.com/afahey03/docklab/internal/services"
	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
)

type TerminalHandler struct {
	authService     *services.AuthService
	terminalService *services.TerminalService
	upgrader        websocket.Upgrader
}

func NewTerminalHandler(authService *services.AuthService, terminalService *services.TerminalService) *TerminalHandler {
	return &TerminalHandler{
		authService:     authService,
		terminalService: terminalService,
		upgrader: websocket.Upgrader{
			CheckOrigin: func(_ *http.Request) bool { return true },
		},
	}
}

func (h *TerminalHandler) WebSocket(c *gin.Context) {
	token := c.Query("token")
	if token == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "missing token"})
		return
	}

	claims, err := h.authService.ParseToken(token)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid token"})
		return
	}

	ws, err := h.upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		return
	}
	defer ws.Close()

	environmentID := c.Param("id")
	if err := h.terminalService.ProxySession(c.Request.Context(), claims.Email, environmentID, ws); err != nil {
		h.sendTerminalError(ws, err)
	}
}

func (h *TerminalHandler) sendTerminalError(ws *websocket.Conn, err error) {
	switch {
	case errors.Is(err, repositories.ErrEnvironmentNotFound):
		_ = ws.WriteMessage(websocket.TextMessage, []byte("\r\n[docklab] environment not found\r\n"))
	case errors.Is(err, services.ErrEnvironmentNotRunning):
		_ = ws.WriteMessage(websocket.TextMessage, []byte("\r\n[docklab] environment is not running\r\n"))
	case errors.Is(err, services.ErrDockerUnavailable):
		_ = ws.WriteMessage(websocket.TextMessage, []byte("\r\n[docklab] docker CLI is not installed or unavailable\r\n"))
	case errors.Is(err, services.ErrSSHPrivateKeyMissing):
		_ = ws.WriteMessage(websocket.TextMessage, []byte("\r\n[docklab] SSH private key is not configured on the server\r\n"))
	case errors.Is(err, services.ErrSSHConnectionFailed):
		_ = ws.WriteMessage(websocket.TextMessage, []byte("\r\n[docklab] failed to connect to remote host over SSH\r\n"))
	default:
		_ = ws.WriteMessage(websocket.TextMessage, []byte("\r\n[docklab] terminal session ended with an error\r\n"))
	}
}
