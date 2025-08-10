package server

import (
	"fmt"
	"log"
	"net/http"

	"loopserve/internal/apps"
	"loopserve/internal/handlers"
)

// Server represents the HTTP server
type Server struct {
	config  *apps.Config
	handler *handlers.Handler
	port    int
}

// New creates a new server instance
func New(port int) (*Server, error) {
	config, err := apps.LoadConfig()
	if err != nil {
		return nil, fmt.Errorf("failed to load config: %w", err)
	}

	handler := handlers.New(config)

	return &Server{
		config:  config,
		handler: handler,
		port:    port,
	}, nil
}

// SetupRoutes configures the HTTP routes
func (s *Server) SetupRoutes() *http.ServeMux {
	mux := http.NewServeMux()

	// Serve the main page
	mux.HandleFunc("/", s.handler.ServeHome)

	// Serve static files
	mux.HandleFunc("/static/", s.handler.ServeStatic)

	// API routes
	mux.HandleFunc("/api/apps", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			s.handler.GetApps(w, r)
		case http.MethodPost:
			s.handler.AddApp(w, r)
		default:
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		}
	})

	mux.HandleFunc("/api/apps/delete", s.handler.DeleteApp)
	mux.HandleFunc("/api/apps/start", s.handler.StartApp)
	mux.HandleFunc("/api/apps/stop", s.handler.StopApp)
	mux.HandleFunc("/api/apps/logs", s.handler.GetLogs)

	return mux
}

// Start starts the HTTP server
func (s *Server) Start() error {
	mux := s.SetupRoutes()

	addr := fmt.Sprintf(":%d", s.port)
	log.Printf("Server starting on http://localhost%s", addr)

	return http.ListenAndServe(addr, mux)
}
