package daemon

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/zhangjianyong66/mihomo-manager/internal/config"
	"github.com/zhangjianyong66/mihomo-manager/internal/core"
	"github.com/zhangjianyong66/mihomo-manager/internal/domain"
	"github.com/zhangjianyong66/mihomo-manager/internal/ipc"
	"github.com/zhangjianyong66/mihomo-manager/internal/legacy"
	"github.com/zhangjianyong66/mihomo-manager/internal/platform"
	"github.com/zhangjianyong66/mihomo-manager/internal/store"
)

type State string

const (
	StateStarting State = "starting"
	StateRunning  State = "running"
	StateStopping State = "stopping"
	StateFailed   State = "failed"
)

var ErrRootDaemon = errors.New("root daemon is not allowed")

type Status struct {
	ProtocolVersion int        `json:"protocolVersion"`
	State           State      `json:"state"`
	PID             int        `json:"pid"`
	StartedAt       time.Time  `json:"startedAt"`
	SchemaVersion   int        `json:"schemaVersion"`
	Core            CoreStatus `json:"core"`
}

type Store interface {
	Close() error
	SchemaInfo() store.SchemaInfo
}

type Options struct {
	Paths            config.ManagerPaths
	UID              uint32
	PID              int
	Clock            func() time.Time
	OpenStore        func(context.Context, string, store.OpenOptions) (Store, error)
	Listener         net.Listener
	ShutdownTimeout  time.Duration
	CoreAdapter      core.Adapter
	CoreReadyTimeout time.Duration
	LegacyService    MigrationService
	Capabilities     *CapabilityService
}

type Server struct {
	opts         Options
	mu           sync.RWMutex
	status       Status
	http         *http.Server
	listener     net.Listener
	store        Store
	lock         *platform.FileLock
	ownedSock    bool
	core         *CoreManager
	migrations   MigrationService
	capabilities *CapabilityService
}

func New(opts Options) *Server {
	if opts.UID == 0 {
		opts.UID = uint32(os.Geteuid())
	}
	if opts.PID == 0 {
		opts.PID = os.Getpid()
	}
	if opts.Clock == nil {
		opts.Clock = time.Now
	}
	if opts.OpenStore == nil {
		opts.OpenStore = func(ctx context.Context, path string, openOptions store.OpenOptions) (Store, error) {
			return store.Open(ctx, path, openOptions)
		}
	}
	if opts.ShutdownTimeout <= 0 {
		opts.ShutdownTimeout = 5 * time.Second
	}
	return &Server{opts: opts, status: Status{ProtocolVersion: ipc.ProtocolVersion, State: StateStarting, PID: opts.PID}}
}

func (s *Server) Run(ctx context.Context) (finalErr error) {
	if os.Geteuid() == 0 {
		return ErrRootDaemon
	}
	if ctx == nil {
		ctx = context.Background()
	}
	lock, err := platform.AcquireFileLock(s.opts.Paths.Lock)
	if err != nil {
		return fmt.Errorf("acquire daemon lock: %w", err)
	}
	s.lock = lock
	defer func() {
		if closeErr := s.cleanup(); finalErr == nil && closeErr != nil {
			finalErr = closeErr
		}
	}()

	stateStore, err := s.opts.OpenStore(ctx, s.opts.Paths.Database, store.OpenOptions{})
	if err != nil {
		s.setState(StateFailed)
		return fmt.Errorf("open daemon state: %w", err)
	}
	s.store = stateStore
	if s.opts.CoreAdapter != nil {
		repository, ok := stateStore.(CoreRepository)
		if !ok {
			s.setState(StateFailed)
			return errors.New("daemon store does not implement core repository")
		}
		generationStore := core.NewGenerationStore(core.GenerationStoreOptions{
			GenerationsDir: s.opts.Paths.GenerationsDir,
			RuntimeState:   s.opts.Paths.RuntimeState,
			LogPath:        s.opts.Paths.CoreLog,
		})
		s.core = NewCoreManager(CoreManagerOptions{
			Adapter: s.opts.CoreAdapter, Configs: generationStore, Repository: repository,
			Coordinator: NewCoordinator(), Supervisor: NewSupervisor(s.opts.CoreAdapter, s.opts.CoreReadyTimeout),
		})
	}
	var compatibility *legacy.Compatibility
	if s.opts.LegacyService != nil {
		s.migrations = s.opts.LegacyService
	} else if repository, ok := stateStore.(legacy.Repository); ok {
		legacyPaths := config.Load()
		legacyPaths.LogFile = s.opts.Paths.CoreLog
		legacyService := legacy.NewService(legacyPaths, s.opts.Paths, repository)
		s.migrations = legacyService
		compatibility = legacyService.Compatibility()
	}
	s.capabilities = s.opts.Capabilities
	if s.capabilities == nil && compatibility != nil {
		if repository, ok := stateStore.(CapabilityStore); ok {
			s.capabilities = NewCapabilityService(repository, s.core, compatibility, config.Load())
		}
	}

	listener := s.opts.Listener
	if listener == nil {
		activated, activatedOK, activationErr := platform.ActivatedListener()
		if activationErr != nil {
			return activationErr
		}
		if activatedOK {
			listener = platform.EnforcePeerCredentials(activated, s.opts.UID)
		} else {
			listener, err = platform.ListenUnix(s.opts.Paths.Socket, s.opts.UID)
			if err != nil {
				s.setState(StateFailed)
				return err
			}
			s.ownedSock = true
		}
	}
	s.listener = listener
	s.mu.Lock()
	s.status = Status{ProtocolVersion: ipc.ProtocolVersion, State: StateRunning, PID: s.opts.PID, StartedAt: s.opts.Clock().UTC(), SchemaVersion: stateStore.SchemaInfo().Version}
	s.mu.Unlock()

	mux := http.NewServeMux()
	mux.HandleFunc("/v1/health", s.handleStatus)
	mux.HandleFunc("/v1/status", s.handleStatus)
	mux.HandleFunc("/v1/migrations/plan", s.handleMigrationPlan)
	mux.HandleFunc("/v1/migrations/apply", s.handleMigrationApply)
	mux.HandleFunc("/v1/migrations/status", s.handleMigrationStatus)
	mux.HandleFunc("/v1/migrations/rollback", s.handleMigrationRollback)
	if s.capabilities != nil {
		registerCapabilityRoutes(mux, s.capabilities)
	}
	handler := ipc.NewServer(NewRequestCache(5*time.Minute, 1024).Middleware(mux))
	s.http = &http.Server{Handler: handler, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 30 * time.Second, WriteTimeout: 30 * time.Second, IdleTimeout: 30 * time.Second, MaxHeaderBytes: 64 << 10}
	serveErr := make(chan error, 1)
	go func() { serveErr <- s.http.Serve(listener) }()

	select {
	case <-ctx.Done():
		s.setState(StateStopping)
		shutdownCtx, cancel := context.WithTimeout(context.Background(), s.opts.ShutdownTimeout)
		defer cancel()
		if err := s.http.Shutdown(shutdownCtx); err != nil {
			return fmt.Errorf("shutdown daemon: %w", err)
		}
		if err := <-serveErr; err != nil && !errors.Is(err, http.ErrServerClosed) {
			return fmt.Errorf("serve daemon: %w", err)
		}
		return nil
	case err := <-serveErr:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			s.setState(StateFailed)
			return fmt.Errorf("serve daemon: %w", err)
		}
		return nil
	}
}

func (s *Server) Status() Status {
	s.mu.RLock()
	status := s.status
	s.mu.RUnlock()
	if s.core != nil {
		status.Core = s.core.Status()
	} else {
		status.Core = CoreStatus{State: domain.CoreStateStopped}
	}
	return status
}

func (s *Server) setState(state State) {
	s.mu.Lock()
	s.status.State = state
	s.mu.Unlock()
}

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		_ = ipc.WriteError(w, http.StatusBadRequest, "INVALID_REQUEST", "请求方法无效", false, nil)
		return
	}
	status := s.Status()
	if status.State != StateRunning {
		_ = ipc.WriteError(w, http.StatusServiceUnavailable, "DAEMON_UNAVAILABLE", "daemon 尚未就绪", true, nil)
		return
	}
	_ = ipc.WriteJSON(w, http.StatusOK, "DaemonStatus", status)
}

func (s *Server) cleanup() error {
	var result error
	if s.core != nil {
		ctx, cancel := context.WithTimeout(context.Background(), s.opts.ShutdownTimeout)
		result = errors.Join(result, s.core.Close(ctx))
		cancel()
	}
	if s.listener != nil {
		if err := s.listener.Close(); err != nil && !errors.Is(err, net.ErrClosed) {
			result = errors.Join(result, err)
		}
	}
	if s.store != nil {
		result = errors.Join(result, s.store.Close())
	}
	if s.ownedSock {
		if err := os.Remove(s.opts.Paths.Socket); err != nil && !errors.Is(err, os.ErrNotExist) {
			result = errors.Join(result, err)
		}
	}
	if s.lock != nil {
		result = errors.Join(result, s.lock.Close())
	}
	return result
}
