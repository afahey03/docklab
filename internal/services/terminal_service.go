package services

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"time"

	"github.com/afahey03/docklab/internal/models"
	"github.com/afahey03/docklab/internal/repositories"
	"github.com/creack/pty"
	"github.com/gorilla/websocket"
	"golang.org/x/crypto/ssh"
)

const activityRefreshInterval = 60 * time.Second

var ErrEnvironmentNotRunning = errors.New("environment must be running to open terminal")

type TerminalClientMessage struct {
	Type string `json:"type"`
	Data string `json:"data,omitempty"`
	Cols uint16 `json:"cols,omitempty"`
	Rows uint16 `json:"rows,omitempty"`
}

type TerminalService struct {
	environmentRepo repositories.EnvironmentRepository
	resolver        *RuntimeResolver
}

func NewTerminalService(environmentRepo repositories.EnvironmentRepository, resolver *RuntimeResolver) *TerminalService {
	return &TerminalService{
		environmentRepo: environmentRepo,
		resolver:        resolver,
	}
}

func (s *TerminalService) ProxySession(ctx context.Context, userEmail, environmentID string, ws *websocket.Conn) error {
	env, err := s.environmentRepo.GetByIDForUser(ctx, environmentID, userEmail)
	if err != nil {
		return err
	}
	if env.Status != "running" {
		return ErrEnvironmentNotRunning
	}

	s.trackActivity(ctx, environmentID)

	if UsesRemoteRuntime(env) {
		return s.proxyRemoteSession(ctx, env, ws)
	}

	return s.proxyLocalSession(ctx, env, ws)
}

func (s *TerminalService) trackActivity(ctx context.Context, environmentID string) {
	activityCtx, activityCancel := context.WithCancel(ctx)
	go func() {
		defer activityCancel()
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
}

func (s *TerminalService) proxyLocalSession(ctx context.Context, env *models.Environment, ws *websocket.Conn) error {
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

	return s.bridgePTY(ctx, ws, ptmx, cmd.Process)
}

func (s *TerminalService) proxyRemoteSession(ctx context.Context, env *models.Environment, ws *websocket.Conn) error {
	factory := s.resolver.SSHFactory()
	client, err := factory.Connect(ctx, env.PublicIP)
	if err != nil {
		return err
	}
	defer client.Close()

	session, err := client.NewSession()
	if err != nil {
		return fmt.Errorf("create ssh session: %w", err)
	}
	defer session.Close()

	modes := ssh.TerminalModes{
		ssh.ECHO:          1,
		ssh.TTY_OP_ISPEED: 14400,
		ssh.TTY_OP_OSPEED: 14400,
	}
	if err := session.RequestPty("xterm-256color", 24, 80, modes); err != nil {
		return fmt.Errorf("request pty: %w", err)
	}

	stdin, err := session.StdinPipe()
	if err != nil {
		return err
	}
	stdout, err := session.StdoutPipe()
	if err != nil {
		return err
	}

	command := fmt.Sprintf("docker exec -i %s sh", shellQuote(RemoteContainerName(env.ID)))
	if err := session.Start(command); err != nil {
		return fmt.Errorf("start remote shell: %w", err)
	}

	defer func() {
		_ = session.Close()
		_ = ws.Close()
		_ = session.Wait()
	}()

	errCh := make(chan error, 2)

	go func() {
		buf := make([]byte, 4096)
		for {
			n, readErr := stdout.Read(buf)
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
				if _, err := io.WriteString(stdin, message.Data); err != nil {
					errCh <- err
					return
				}
			case "resize":
				if message.Cols == 0 || message.Rows == 0 {
					continue
				}
				if err := session.WindowChange(int(message.Rows), int(message.Cols)); err != nil {
					errCh <- err
					return
				}
			}
		}
	}()

	select {
	case <-ctx.Done():
		return nil
	case runErr := <-errCh:
		if errors.Is(runErr, io.EOF) || websocket.IsCloseError(runErr, websocket.CloseNormalClosure, websocket.CloseGoingAway) {
			return nil
		}
		return runErr
	}
}

func (s *TerminalService) bridgePTY(ctx context.Context, ws *websocket.Conn, ptmx *os.File, process interface{ Kill() error }) error {
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
		if process != nil {
			_ = process.Kill()
		}
		return nil
	case runErr := <-errCh:
		if process != nil {
			_ = process.Kill()
		}
		if errors.Is(runErr, io.EOF) || websocket.IsCloseError(runErr, websocket.CloseNormalClosure, websocket.CloseGoingAway) {
			return nil
		}
		return runErr
	}
}
