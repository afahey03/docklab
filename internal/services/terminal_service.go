package services

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"time"

	"github.com/afahey03/docklab/internal/repositories"
	"github.com/creack/pty"
	"github.com/gorilla/websocket"
)

const activityRefreshInterval = 60 * time.Second

var ErrEnvironmentNotRunning = errors.New("environment must be running to open terminal")

type TerminalService struct {
	environmentRepo repositories.EnvironmentRepository
}

type TerminalClientMessage struct {
	Type string `json:"type"`
	Data string `json:"data,omitempty"`
	Cols uint16 `json:"cols,omitempty"`
	Rows uint16 `json:"rows,omitempty"`
}

func NewTerminalService(environmentRepo repositories.EnvironmentRepository) *TerminalService {
	return &TerminalService{environmentRepo: environmentRepo}
}

func (s *TerminalService) ProxySession(ctx context.Context, userEmail, environmentID string, ws *websocket.Conn) error {
	env, err := s.environmentRepo.GetByIDForUser(ctx, environmentID, userEmail)
	if err != nil {
		return err
	}
	if env.Status != "running" {
		return ErrEnvironmentNotRunning
	}

	// Record that the environment is active. We fire-and-forget so a slow DB write
	// doesn't delay the terminal opening. We also refresh on a ticker while the
	// session is alive so long-running sessions don't get auto-stopped.
	activityCtx, activityCancel := context.WithCancel(ctx)
	defer activityCancel()

	go func() {
		_ = s.environmentRepo.UpdateLastActivity(activityCtx, environmentID)

		ticker := time.NewTicker(activityRefreshInterval)
		defer ticker.Stop()
		for {
			select {
			case <-activityCtx.Done():
				return
			case <-ticker.C:
				_ = s.environmentRepo.UpdateLastActivity(activityCtx, environmentID)
			}
		}
	}()

	cmd := exec.CommandContext(ctx, "docker", "exec", "-it", env.ContainerID, "sh")
	ptmx, err := pty.StartWithSize(cmd, &pty.Winsize{Rows: 24, Cols: 80})
	if err != nil {
		if errors.Is(err, exec.ErrNotFound) {
			return ErrDockerUnavailable
		}
		return fmt.Errorf("failed to start PTY session: %w", err)
	}
	defer func() {
		_ = ptmx.Close()
		_ = ws.Close()
		_ = cmd.Wait()
	}()

	errCh := make(chan error, 2)

	go func() {
		buf := make([]byte, 4096)
		for {
			n, readErr := ptmx.Read(buf)
			if n > 0 {
				if writeErr := ws.WriteMessage(websocket.TextMessage, buf[:n]); writeErr != nil {
					errCh <- writeErr
					return
				}
			}
			if readErr != nil {
				errCh <- readErr
				return
			}
		}
	}()

	go func() {
		for {
			_, payload, readErr := ws.ReadMessage()
			if readErr != nil {
				errCh <- readErr
				return
			}

			var message TerminalClientMessage
			if err := json.Unmarshal(payload, &message); err != nil {
				errCh <- err
				return
			}

			switch message.Type {
			case "input":
				if _, err := io.WriteString(ptmx, message.Data); err != nil {
					errCh <- err
					return
				}
			case "resize":
				if message.Cols == 0 || message.Rows == 0 {
					continue
				}
				if err := pty.Setsize(ptmx, &pty.Winsize{Rows: message.Rows, Cols: message.Cols}); err != nil {
					errCh <- err
					return
				}
			}
		}
	}()

	select {
	case <-ctx.Done():
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
		return nil
	case runErr := <-errCh:
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
		if errors.Is(runErr, io.EOF) || websocket.IsCloseError(runErr, websocket.CloseNormalClosure, websocket.CloseGoingAway) {
			return nil
		}
		return runErr
	}
}
