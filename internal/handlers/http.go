package handlers

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"

	"loopserve/internal/apps"
)

// Handler contains the application configuration and provides HTTP handlers
type Handler struct {
	config *apps.Config
}

// New creates a new handler instance
func New(config *apps.Config) *Handler {
	return &Handler{config: config}
}

// ServeHome serves the main HTML page
func (h *Handler) ServeHome(w http.ResponseWriter, r *http.Request) {
	http.ServeFile(w, r, "web/index.html")
}

// ServeStatic serves static files (CSS, JS)
func (h *Handler) ServeStatic(w http.ResponseWriter, r *http.Request) {
	// Remove the /static/ prefix and serve from web directory
	path := r.URL.Path[len("/static/"):]
	fullPath := filepath.Join("web", path)

	// Set appropriate content type
	switch filepath.Ext(path) {
	case ".css":
		w.Header().Set("Content-Type", "text/css")
	case ".js":
		w.Header().Set("Content-Type", "application/javascript")
	default:
		w.Header().Set("Content-Type", "text/plain")
	}

	http.ServeFile(w, r, fullPath)
}

// GetApps returns the list of all apps
func (h *Handler) GetApps(w http.ResponseWriter, r *http.Request) {
	h.config.UpdateAppStatus()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(h.config.Apps)
}

// AddApp handles adding a new app
func (h *Handler) AddApp(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var app apps.App
	if err := json.NewDecoder(r.Body).Decode(&app); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	if app.Name == "" || app.Port == 0 || app.Command == "" {
		http.Error(w, "Name, port, and command are required", http.StatusBadRequest)
		return
	}

	if err := h.config.AddApp(app); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]string{"status": "success"})
}

// DeleteApp handles deleting an app
func (h *Handler) DeleteApp(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	if req.Name == "" {
		http.Error(w, "App name is required", http.StatusBadRequest)
		return
	}

	// Stop the app if it's running
	if app := h.config.GetApp(req.Name); app != nil {
		apps.StopApp(app)
	}

	if err := h.config.DeleteApp(req.Name); err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	json.NewEncoder(w).Encode(map[string]string{"status": "success"})
}

// StartApp handles starting a specific app or all apps
func (h *Handler) StartApp(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Name string `json:"name"`
	}

	// Try to decode JSON body
	body, _ := io.ReadAll(r.Body)
	if len(body) > 0 {
		json.Unmarshal(body, &req)
	}

	if req.Name != "" {
		// Start specific app
		app := h.config.GetApp(req.Name)
		if app == nil {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]string{"error": "App not found"})
			return
		}

		if err := apps.StartApp(app); err != nil {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
			return
		}

		// Save the updated config with new PID
		apps.SaveConfig(h.config)

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "success"})
	} else {
		// Start all apps
		var errors []string
		for i := range h.config.Apps {
			if err := apps.StartApp(&h.config.Apps[i]); err != nil {
				errors = append(errors, fmt.Sprintf("%s: %s", h.config.Apps[i].Name, err.Error()))
			}
		}

		// Save the updated config
		apps.SaveConfig(h.config)

		if len(errors) > 0 {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]interface{}{
				"status": "partial",
				"errors": errors,
			})
		} else {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]string{"status": "success"})
		}
	}
}

// StopApp handles stopping a specific app or all apps
func (h *Handler) StopApp(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Name string `json:"name"`
	}

	// Try to decode JSON body
	body, _ := io.ReadAll(r.Body)
	if len(body) > 0 {
		json.Unmarshal(body, &req)
	}

	if req.Name != "" {
		// Stop specific app
		app := h.config.GetApp(req.Name)
		if app == nil {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]string{"error": "App not found"})
			return
		}

		if err := apps.StopApp(app); err != nil {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
			return
		}

		// Save the updated config
		apps.SaveConfig(h.config)

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "success"})
	} else {
		// Stop all apps
		for i := range h.config.Apps {
			apps.StopApp(&h.config.Apps[i])
		}

		// Save the updated config
		apps.SaveConfig(h.config)

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "success"})
	}
}

// GetLogs returns the logs for a specific app
func (h *Handler) GetLogs(w http.ResponseWriter, r *http.Request) {
	appName := r.URL.Query().Get("name")
	if appName == "" {
		http.Error(w, "App name is required", http.StatusBadRequest)
		return
	}

	app := h.config.GetApp(appName)
	if app == nil {
		http.Error(w, "App not found", http.StatusNotFound)
		return
	}

	if app.LogFile == "" {
		http.Error(w, "No log file available", http.StatusNotFound)
		return
	}

	// Read the log file
	content, err := os.ReadFile(app.LogFile)
	if err != nil {
		http.Error(w, "Failed to read log file", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/plain")
	w.Write(content)
}
