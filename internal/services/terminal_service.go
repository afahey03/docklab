package services

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"sync"
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

// terminalBackend abstracts the underlying shell transport (local PTY, kubectl PTY, or
// SSH session) so shared sessions can multiplex clients over any of them.
type terminalBackend struct {
	output     io.Reader
	writeInput func(data string) error
	resize     func(rows, cols uint16) error
	terminate  func()
}

// terminalSession is one live shell shared by every connected websocket client of an
// environment. The first client creates it; later clients attach to the same PTY,
// enabling collaborative shared terminals.
type terminalSession struct {
	backend *terminalBackend

	mu      sync.Mutex
	clients map[*websocket.Conn]struct{}
	closed  bool
}

func (s *terminalSession) addClient(ws *websocket.Conn) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.clients[ws] = struct{}{}
}

// removeClient detaches a websocket and reports how many clients remain.
func (s *terminalSession) removeClient(ws *websocket.Conn) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.clients, ws)
	return len(s.clients)
}

// broadcast writes terminal output to every attached client.
func (s *terminalSession) broadcast(data []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for ws := range s.clients {
		if err := ws.WriteMessage(websocket.TextMessage, data); err != nil {
			delete(s.clients, ws)
			_ = ws.Close()
		}
	}
}

// closeAll terminates the backend and disconnects every client.
func (s *terminalSession) closeAll() {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	s.closed = true
	clients := make([]*websocket.Conn, 0, len(s.clients))
	for ws := range s.clients {
		clients = append(clients, ws)
	}
	s.clients = make(map[*websocket.Conn]struct{})
	s.mu.Unlock()

	s.backend.terminate()
	for _, ws := range clients {
		_ = ws.Close()
	}
}

type TerminalService struct {
	environmentRepo repositories.EnvironmentRepository
	resolver        *RuntimeResolver
	shareService    *ShareService
	metrics         *Metrics

	sessionsMu sync.Mutex
	sessions   map[string]*terminalSession
}

func NewTerminalService(environmentRepo repositories.EnvironmentRepository, resolver *RuntimeResolver) *TerminalService {
	return &TerminalService{
		environmentRepo: environmentRepo,
		resolver:        resolver,
		sessions:        make(map[string]*terminalSession),
	}
}

// EnableSharing lets users with an environment share join its terminal session.
func (s *TerminalService) EnableSharing(shareService *ShareService) {
	s.shareService = shareService
}

func (s *TerminalService) SetMetrics(metrics *Metrics) {
	s.metrics = metrics
}

func (s *TerminalService) ProxySession(ctx context.Context, userEmail, environmentID string, ws *websocket.Conn) error {
	env, err := s.resolveAccessibleEnvironment(ctx, userEmail, environmentID)
	if err != nil {
		return err
	}
	if env.Status != "running" {
		return ErrEnvironmentNotRunning
	}

	s.trackActivity(ctx, environmentID)
	s.metrics.TerminalClientConnected()
	defer s.metrics.TerminalClientDisconnected()

	session, err := s.attachOrCreateSession(ctx, env, ws)
	if err != nil {
		return err
	}

	// Read this client's input until it disconnects.
	clientErr := s.runClientLoop(ctx, session, ws)

	if remaining := session.removeClient(ws); remaining == 0 {
		s.dropSession(environmentID, session)
		session.closeAll()
	}

	if clientErr != nil && !errors.Is(clientErr, io.EOF) &&
		!websocket.IsCloseError(clientErr, websocket.CloseNormalClosure, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
		return clientErr
	}
	return nil
}

func (s *TerminalService) resolveAccessibleEnvironment(ctx context.Context, userEmail, environmentID string) (*models.Environment, error) {
	if s.shareService != nil {
		env, _, err := s.shareService.GetAccessibleEnvironment(ctx, environmentID, userEmail)
		return env, err
	}
	return s.environmentRepo.GetByIDForUser(ctx, environmentID, userEmail)
}

func (s *TerminalService) attachOrCreateSession(ctx context.Context, env *models.Environment, ws *websocket.Conn) (*terminalSession, error) {
	s.sessionsMu.Lock()
	defer s.sessionsMu.Unlock()

	if existing, ok := s.sessions[env.ID]; ok {
		existing.addClient(ws)
		_ = ws.WriteMessage(websocket.TextMessage, []byte("\r\n[docklab] joined shared terminal session\r\n"))
		return existing, nil
	}

	backend, err := s.newBackend(env)
	if err != nil {
		return nil, err
	}

	session := &terminalSession{
		backend: backend,
		clients: map[*websocket.Conn]struct{}{ws: {}},
	}
	s.sessions[env.ID] = session

	go func() {
		buf := make([]byte, 4096)
		for {
			n, readErr := backend.output.Read(buf)
			if n > 0 {
				session.broadcast(buf[:n])
			}
			if readErr != nil {
				s.dropSession(env.ID, session)
				session.closeAll()
				return
			}
		}
	}()

	return session, nil
}

func (s *TerminalService) dropSession(environmentID string, session *terminalSession) {
	s.sessionsMu.Lock()
	if current, ok := s.sessions[environmentID]; ok && current == session {
		delete(s.sessions, environmentID)
	}
	s.sessionsMu.Unlock()
}

func (s *TerminalService) runClientLoop(ctx context.Context, session *terminalSession, ws *websocket.Conn) error {
	for {
		select {
		case <-ctx.Done():
			return nil
		default:
		}

		_, payload, readErr := ws.ReadMessage()
		if readErr != nil {
			return readErr
		}

		var message TerminalClientMessage
		if err := json.Unmarshal(payload, &message); err != nil {
			return err
		}

		switch message.Type {
		case "input":
			if err := session.backend.writeInput(message.Data); err != nil {
				return err
			}
		case "resize":
			if message.Cols == 0 || message.Rows == 0 {
				continue
			}
			if err := session.backend.resize(message.Rows, message.Cols); err != nil {
				return err
			}
		}
	}
}

func (s *TerminalService) newBackend(env *models.Environment) (*terminalBackend, error) {
	if UsesRemoteRuntime(env) {
		return s.newRemoteBackend(env)
	}
	return s.newLocalBackend(env)
}

// newLocalBackend starts a PTY running docker exec (or kubectl exec for the
// kubernetes backend) attached to the workspace.
func (s *TerminalService) newLocalBackend(env *models.Environment) (*terminalBackend, error) {
	var cmd *exec.Cmd
	if s.resolver.LocalBackend() == RuntimeBackendKubernetes {
		k8s := s.resolver.KubernetesRuntime()
		args := append(k8s.BaseArgs(), "exec", "-it", "deploy/"+env.ContainerID, "--", "sh")
		cmd = exec.Command("kubectl", args...)
	} else {
		cmd = exec.Command("docker", "exec", "-it", env.ContainerID, "sh")
	}

	ptmx, err := pty.StartWithSize(cmd, &pty.Winsize{Rows: 24, Cols: 80})
	if err != nil {
		if errors.Is(err, exec.ErrNotFound) {
			return nil, ErrDockerUnavailable
		}
		return nil, fmt.Errorf("failed to start PTY session: %w", err)
	}

	return &terminalBackend{
		output: ptmx,
		writeInput: func(data string) error {
			_, err := io.WriteString(ptmx, data)
			return err
		},
		resize: func(rows, cols uint16) error {
			return pty.Setsize(ptmx, &pty.Winsize{Rows: rows, Cols: cols})
		},
		terminate: func() {
			_ = ptmx.Close()
			if cmd.Process != nil {
				_ = cmd.Process.Kill()
			}
			_ = cmd.Wait()
		},
	}, nil
}

func (s *TerminalService) newRemoteBackend(env *models.Environment) (*terminalBackend, error) {
	factory := s.resolver.SSHFactory()
	client, err := factory.Connect(context.Background(), env.PublicIP)
	if err != nil {
		return nil, err
	}

	session, err := client.NewSession()
	if err != nil {
		_ = client.Close()
		return nil, fmt.Errorf("create ssh session: %w", err)
	}

	modes := ssh.TerminalModes{
		ssh.ECHO:          1,
		ssh.TTY_OP_ISPEED: 14400,
		ssh.TTY_OP_OSPEED: 14400,
	}
	if err := session.RequestPty("xterm-256color", 24, 80, modes); err != nil {
		_ = session.Close()
		_ = client.Close()
		return nil, fmt.Errorf("request pty: %w", err)
	}

	stdin, err := session.StdinPipe()
	if err != nil {
		_ = session.Close()
		_ = client.Close()
		return nil, err
	}
	stdout, err := session.StdoutPipe()
	if err != nil {
		_ = session.Close()
		_ = client.Close()
		return nil, err
	}

	command := fmt.Sprintf("docker exec -i %s sh", shellQuote(RemoteContainerName(env.ID)))
	if err := session.Start(command); err != nil {
		_ = session.Close()
		_ = client.Close()
		return nil, fmt.Errorf("start remote shell: %w", err)
	}

	return &terminalBackend{
		output: stdout,
		writeInput: func(data string) error {
			_, err := io.WriteString(stdin, data)
			return err
		},
		resize: func(rows, cols uint16) error {
			return session.WindowChange(int(rows), int(cols))
		},
		terminate: func() {
			_ = session.Close()
			_ = client.Close()
		},
	}, nil
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
