package api

import (
	"bytes"
	"context"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"time"

	"clicrontab/internal/core"
	clicrontabmcp "clicrontab/internal/mcp"
	"clicrontab/internal/store"
	"clicrontab/web"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

// Server holds the HTTP server state.
type Server struct {
	httpServer *http.Server
	router     *chi.Mux
	store      *store.Store
	scheduler  *core.Scheduler
	mcpServer  *clicrontabmcp.MCPServer
	logger     *slog.Logger
	location   *time.Location
	authToken  string
}

// NewServer constructs the HTTP API server.
func NewServer(addr string, authToken string, store *store.Store, scheduler *core.Scheduler, mcpServer *clicrontabmcp.MCPServer, logger *slog.Logger, location *time.Location) (*Server, error) {
	router := chi.NewRouter()
	router.Use(middleware.RequestID)
	router.Use(middleware.RealIP)
	router.Use(middleware.Recoverer)

	staticFS := web.Files()

	s := &Server{
		router:    router,
		store:     store,
		scheduler: scheduler,
		mcpServer: mcpServer,
		logger:    logger,
		location:  location,
		authToken: authToken,
	}
	s.registerRoutes(staticFS)

	httpServer := &http.Server{
		Addr:         addr,
		Handler:      router,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 0,
		IdleTimeout:  60 * time.Second,
	}
	s.httpServer = httpServer
	return s, nil
}

// Start begins serving HTTP requests.
func (s *Server) Start() error {
	s.logger.Info("http server listening", "addr", s.httpServer.Addr)
	return s.httpServer.ListenAndServe()
}

// Shutdown gracefully shuts down the server.
func (s *Server) Shutdown(ctx context.Context) error {
	return s.httpServer.Shutdown(ctx)
}

func (s *Server) registerRoutes(staticFS fs.FS) {
	fileServer := http.StripPrefix("/assets/", http.FileServer(http.FS(staticFS)))

	s.router.Get("/", s.handleIndex(staticFS))
	s.router.Handle("/assets/*", fileServer)

	// Mount MCP endpoint with optional authentication
	var mcpHandler http.Handler = s.mcpServer
	if s.authToken != "" {
		mcpHandler = AuthMiddleware(s.authToken)(mcpHandler)
	}
	s.router.Handle("/mcp", mcpHandler)

	s.router.Route("/v1", func(r chi.Router) {
		// Apply authentication to all API endpoints
		if s.authToken != "" {
			r.Use(AuthMiddleware(s.authToken))
		}

		r.Post("/cron/preview", s.handleCronPreview)

		r.Route("/tasks", func(r chi.Router) {
			r.Get("/", s.handleListTasks)
			r.Post("/", s.handleCreateTask)

			r.Route("/{taskID}", func(r chi.Router) {
				r.Get("/", s.handleGetTask)
				r.Patch("/", s.handleUpdateTask)
				r.Delete("/", s.handleDeleteTask)
				r.Post("/run", s.handleRunTask)
				r.Get("/runs", s.handleListRuns)
			})
		})

		r.Route("/runs", func(r chi.Router) {
			r.Get("/{runID}", s.handleGetRun)
			r.Get("/{runID}/log", s.handleRunLog)
		})
	})
}

func (s *Server) handleIndex(staticFS fs.FS) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		file, err := staticFS.Open("index.html")
		if err != nil {
			http.Error(w, "index not found", http.StatusInternalServerError)
			return
		}
		defer file.Close()
		info, err := fs.Stat(staticFS, "index.html")
		modTime := time.Now()
		if err == nil {
			modTime = info.ModTime()
		}
		if reader, ok := file.(io.ReadSeeker); ok {
			http.ServeContent(w, r, "index.html", modTime, reader)
			return
		}
		data, err := io.ReadAll(file)
		if err != nil {
			http.Error(w, "failed to load index", http.StatusInternalServerError)
			return
		}
		http.ServeContent(w, r, "index.html", modTime, bytes.NewReader(data))
	}
}
