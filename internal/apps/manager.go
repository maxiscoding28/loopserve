package apps

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"
)

// App represents an application configuration
type App struct {
	Name    string `json:"name"`
	Port    int    `json:"port"`
	Command string `json:"command"`
	PID     int    `json:"pid,omitempty"`
	LogFile string `json:"log_file,omitempty"`
}

// Config holds the application configuration
type Config struct {
	Apps []App `json:"apps"`
}

const (
	AppsFile = "apps.json"
	LogsDir  = "logs"
)

// LoadConfig loads the apps configuration from file
func LoadConfig() (*Config, error) {
	config := &Config{Apps: []App{}}

	if _, err := os.Stat(AppsFile); os.IsNotExist(err) {
		return config, nil
	}

	data, err := os.ReadFile(AppsFile)
	if err != nil {
		return nil, fmt.Errorf("failed to read apps.json: %w", err)
	}

	if err := json.Unmarshal(data, config); err != nil {
		return nil, fmt.Errorf("failed to parse apps.json: %w", err)
	}

	return config, nil
}

// SaveConfig saves the apps configuration to file
func SaveConfig(config *Config) error {
	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	if err := os.WriteFile(AppsFile, data, 0644); err != nil {
		return fmt.Errorf("failed to write apps.json: %w", err)
	}

	return nil
}

// AddApp adds a new app to the configuration
func (c *Config) AddApp(app App) error {
	// Check for duplicate names
	for _, existingApp := range c.Apps {
		if existingApp.Name == app.Name {
			return fmt.Errorf("app with name '%s' already exists", app.Name)
		}
		if existingApp.Port == app.Port {
			return fmt.Errorf("port %d is already in use", app.Port)
		}
	}

	c.Apps = append(c.Apps, app)
	return SaveConfig(c)
}

// DeleteApp removes an app from the configuration
func (c *Config) DeleteApp(name string) error {
	for i, app := range c.Apps {
		if app.Name == name {
			c.Apps = append(c.Apps[:i], c.Apps[i+1:]...)
			return SaveConfig(c)
		}
	}
	return fmt.Errorf("app '%s' not found", name)
}

// GetApp returns an app by name
func (c *Config) GetApp(name string) *App {
	for i, app := range c.Apps {
		if app.Name == name {
			return &c.Apps[i]
		}
	}
	return nil
}

// IsProcessRunning checks if a process with the given PID is running
func IsProcessRunning(pid int) bool {
	if pid <= 0 {
		return false
	}

	process, err := os.FindProcess(pid)
	if err != nil {
		return false
	}

	err = process.Signal(syscall.Signal(0))
	return err == nil
}

// StartApp starts an application
func StartApp(app *App) error {
	if app.PID > 0 && IsProcessRunning(app.PID) {
		return fmt.Errorf("app '%s' is already running with PID %d", app.Name, app.PID)
	}

	// Create logs directory if it doesn't exist
	if err := os.MkdirAll(LogsDir, 0755); err != nil {
		return fmt.Errorf("failed to create logs directory: %w", err)
	}

	// Set up log file
	logFile := filepath.Join(LogsDir, app.Name+".log")
	app.LogFile = logFile

	file, err := os.OpenFile(logFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return fmt.Errorf("failed to open log file: %w", err)
	}
	defer file.Close()

	// Write startup message to log
	timestamp := time.Now().Format("2006-01-02 15:04:05")
	file.WriteString(fmt.Sprintf("[%s] Starting %s...\n", timestamp, app.Name))

	// Start the process
	cmd := exec.Command("sh", "-c", app.Command)
	cmd.Stdout = file
	cmd.Stderr = file
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	// Set working directory to the directory containing the command
	if commandDir := filepath.Dir(app.Command); commandDir != "." {
		cmd.Dir = commandDir
	}

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start app: %w", err)
	}

	app.PID = cmd.Process.Pid
	file.WriteString(fmt.Sprintf("[%s] Started with PID %d\n", timestamp, app.PID))

	return nil
}

// StopApp stops an application
func StopApp(app *App) error {
	if app.PID <= 0 || !IsProcessRunning(app.PID) {
		app.PID = 0
		return nil
	}

	// Find the process
	process, err := os.FindProcess(app.PID)
	if err != nil {
		app.PID = 0
		return nil
	}

	// Try graceful shutdown first (SIGTERM)
	if err := process.Signal(syscall.SIGTERM); err != nil {
		app.PID = 0
		return nil
	}

	// Wait a bit for graceful shutdown
	time.Sleep(2 * time.Second)

	// Check if process is still running
	if IsProcessRunning(app.PID) {
		// Force kill (SIGKILL)
		process.Signal(syscall.SIGKILL)
		time.Sleep(1 * time.Second)
	}

	app.PID = 0
	return nil
}

// UpdateAppStatus updates the PID status for all apps
func (c *Config) UpdateAppStatus() {
	for i := range c.Apps {
		if c.Apps[i].PID > 0 && !IsProcessRunning(c.Apps[i].PID) {
			c.Apps[i].PID = 0
		}
	}
}
