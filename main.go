package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"
)

type App struct {
	Name    string `json:"name"`
	Port    int    `json:"port"`
	Command string `json:"command"`
	PID     int    `json:"pid,omitempty"`
	LogFile string `json:"log_file,omitempty"`
}

type AppsConfig struct {
	Apps []App `json:"apps"`
}

const (
	appsFile = "apps.json"
	logsDir  = "logs"
)

var rootCmd = &cobra.Command{
	Use:   "loopserve",
	Short: "A CLI tool to manage long-running background processes",
	Long:  "Loopserve helps you manage multiple long-running processes with port allocation and logging",
}

var addCmd = &cobra.Command{
	Use:   "add [name] [port] [command]",
	Short: "Add a new app configuration",
	Long:  "Add a new app with name, port, and command to the apps.json file",
	Args:  cobra.ExactArgs(3),
	Run:   addApp,
}

var startCmd = &cobra.Command{
	Use:   "start",
	Short: "Start all configured apps",
	Long:  "Start all apps configured in apps.json as background processes",
	Run:   startApps,
}

var stopCmd = &cobra.Command{
	Use:   "stop",
	Short: "Stop all running apps",
	Long:  "Stop all apps that are currently running",
	Run:   stopApps,
}

var listCmd = &cobra.Command{
	Use:   "list",
	Short: "List all configured apps",
	Long:  "List all apps in apps.json with their status",
	Run:   listApps,
}

var uiCmd = &cobra.Command{
	Use:   "ui",
	Short: "Start the web UI for managing apps",
	Long:  "Start a web interface to manage apps in apps.json (add, edit, delete, start, stop)",
	Run:   startUI,
}

func init() {
	rootCmd.AddCommand(addCmd)
	rootCmd.AddCommand(startCmd)
	rootCmd.AddCommand(stopCmd)
	rootCmd.AddCommand(listCmd)
	rootCmd.AddCommand(uiCmd)
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}

func loadAppsConfig() (*AppsConfig, error) {
	config := &AppsConfig{Apps: []App{}}

	if _, err := os.Stat(appsFile); os.IsNotExist(err) {
		return config, nil
	}

	data, err := os.ReadFile(appsFile)
	if err != nil {
		return nil, fmt.Errorf("failed to read apps.json: %w", err)
	}

	if err := json.Unmarshal(data, config); err != nil {
		return nil, fmt.Errorf("failed to parse apps.json: %w", err)
	}

	return config, nil
}

func saveAppsConfig(config *AppsConfig) error {
	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	if err := os.WriteFile(appsFile, data, 0644); err != nil {
		return fmt.Errorf("failed to write apps.json: %w", err)
	}

	return nil
}

func addApp(cmd *cobra.Command, args []string) {
	name := args[0]
	port, err := strconv.Atoi(args[1])
	if err != nil {
		log.Fatalf("Invalid port number: %s", args[1])
	}
	command := args[2]

	config, err := loadAppsConfig()
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	// Check if app with same name or port already exists
	for _, app := range config.Apps {
		if app.Name == name {
			log.Fatalf("App with name '%s' already exists", name)
		}
		if app.Port == port {
			log.Fatalf("Port %d is already in use by app '%s'", port, app.Name)
		}
	}

	newApp := App{
		Name:    name,
		Port:    port,
		Command: command,
	}

	config.Apps = append(config.Apps, newApp)

	if err := saveAppsConfig(config); err != nil {
		log.Fatalf("Failed to save config: %v", err)
	}

	fmt.Printf("Added app '%s' on port %d with command: %s\n", name, port, command)
}

func ensureLogsDir() error {
	if _, err := os.Stat(logsDir); os.IsNotExist(err) {
		return os.MkdirAll(logsDir, 0755)
	}
	return nil
}

func startApps(cmd *cobra.Command, args []string) {
	config, err := loadAppsConfig()
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	if len(config.Apps) == 0 {
		fmt.Println("No apps configured. Use 'loopserve add' to add an app first.")
		return
	}

	if err := ensureLogsDir(); err != nil {
		log.Fatalf("Failed to create logs directory: %v", err)
	}

	for i := range config.Apps {
		app := &config.Apps[i]

		if app.PID != 0 && isProcessRunning(app.PID) {
			fmt.Printf("App '%s' is already running (PID: %d)\n", app.Name, app.PID)
			continue
		}

		if err := startApp(app); err != nil {
			fmt.Printf("Failed to start app '%s': %v\n", app.Name, err)
			continue
		}

		fmt.Printf("Started app '%s' on port %d (PID: %d)\n", app.Name, app.Port, app.PID)
	}

	if err := saveAppsConfig(config); err != nil {
		log.Fatalf("Failed to save config: %v", err)
	}
}

func startApp(app *App) error {
	logFileName := filepath.Join(logsDir, fmt.Sprintf("%s.log", app.Name))
	app.LogFile = logFileName

	// Clear/create fresh log file
	file, err := os.Create(logFileName)
	if err != nil {
		return fmt.Errorf("failed to create log file: %w", err)
	}

	// Write startup header
	fmt.Fprintf(file, "=== Started %s at %s ===\n", app.Name, time.Now().Format(time.RFC3339))
	file.Close()

	// Parse command
	cmdParts := parseCommand(app.Command)
	if len(cmdParts) == 0 {
		return fmt.Errorf("invalid command: %s", app.Command)
	}

	// Start command
	execCmd := exec.Command(cmdParts[0], cmdParts[1:]...)

	// Set working directory to the directory containing the executable
	if filepath.IsAbs(cmdParts[0]) {
		execCmd.Dir = filepath.Dir(cmdParts[0])
	} else {
		// If it's a relative path, try to find the executable
		if execPath, err := exec.LookPath(cmdParts[0]); err == nil {
			execCmd.Dir = filepath.Dir(execPath)
		}
	}

	// Set up environment with PORT variable
	execCmd.Env = append(os.Environ(), fmt.Sprintf("PORT=%d", app.Port))

	// Redirect stdout and stderr to log file
	logFileWriter, err := os.OpenFile(logFileName, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return fmt.Errorf("failed to open log file: %w", err)
	}

	execCmd.Stdout = logFileWriter
	execCmd.Stderr = logFileWriter

	// Start the process in background
	if err := execCmd.Start(); err != nil {
		logFileWriter.Close()
		return fmt.Errorf("failed to start command: %w", err)
	}

	app.PID = execCmd.Process.Pid

	// Log additional startup info
	fmt.Fprintf(logFileWriter, "Command: %s\n", app.Command)
	fmt.Fprintf(logFileWriter, "PID: %d\n", app.PID)
	if execCmd.Dir != "" {
		fmt.Fprintf(logFileWriter, "Working directory: %s\n", execCmd.Dir)
	}
	fmt.Fprintf(logFileWriter, "--- Application Output ---\n")
	logFileWriter.Close()

	return nil
}

func parseCommand(command string) []string {
	// Simple command parsing - splits on spaces
	// For more complex parsing, consider using shell parsing libraries
	var parts []string
	var current string
	inQuotes := false

	for _, char := range command {
		if char == '"' || char == '\'' {
			inQuotes = !inQuotes
		} else if char == ' ' && !inQuotes {
			if current != "" {
				parts = append(parts, current)
				current = ""
			}
		} else {
			current += string(char)
		}
	}

	if current != "" {
		parts = append(parts, current)
	}

	return parts
}

func isProcessRunning(pid int) bool {
	process, err := os.FindProcess(pid)
	if err != nil {
		return false
	}

	// Send signal 0 to check if process exists
	err = process.Signal(syscall.Signal(0))
	return err == nil
}

func stopApps(cmd *cobra.Command, args []string) {
	config, err := loadAppsConfig()
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	for i := range config.Apps {
		app := &config.Apps[i]

		if app.PID == 0 {
			fmt.Printf("App '%s' is not running\n", app.Name)
			continue
		}

		if !isProcessRunning(app.PID) {
			fmt.Printf("App '%s' (PID: %d) is not running\n", app.Name, app.PID)

			// Clear the log file when we detect the process has stopped
			if app.LogFile != "" {
				if file, err := os.Create(app.LogFile); err == nil {
					fmt.Fprintf(file, "=== App '%s' stopped (process not found) at %s ===\n", app.Name, time.Now().Format(time.RFC3339))
					file.Close()
				}
			}

			app.PID = 0
			continue
		}

		process, err := os.FindProcess(app.PID)
		if err != nil {
			fmt.Printf("Failed to find process for app '%s' (PID: %d): %v\n", app.Name, app.PID, err)
			continue
		}

		if err := process.Signal(os.Interrupt); err != nil {
			fmt.Printf("Failed to stop app '%s' (PID: %d): %v\n", app.Name, app.PID, err)
			continue
		}

		fmt.Printf("Stopped app '%s' (PID: %d)\n", app.Name, app.PID)

		// Clear the log file when stopping
		if app.LogFile != "" {
			if file, err := os.Create(app.LogFile); err == nil {
				fmt.Fprintf(file, "=== App '%s' stopped at %s ===\n", app.Name, time.Now().Format(time.RFC3339))
				file.Close()
			}
		}

		app.PID = 0
	}

	if err := saveAppsConfig(config); err != nil {
		log.Fatalf("Failed to save config: %v", err)
	}
}

func startIndividualApp(appName string) error {
	config, err := loadAppsConfig()
	if err != nil {
		return fmt.Errorf("failed to load config: %v", err)
	}

	if err := ensureLogsDir(); err != nil {
		return fmt.Errorf("failed to create logs directory: %v", err)
	}

	// Find the app
	var targetApp *App
	for i := range config.Apps {
		if config.Apps[i].Name == appName {
			targetApp = &config.Apps[i]
			break
		}
	}

	if targetApp == nil {
		return fmt.Errorf("app '%s' not found", appName)
	}

	if targetApp.PID != 0 && isProcessRunning(targetApp.PID) {
		return fmt.Errorf("app '%s' is already running (PID: %d)", targetApp.Name, targetApp.PID)
	}

	if err := startApp(targetApp); err != nil {
		return fmt.Errorf("failed to start app '%s': %v", targetApp.Name, err)
	}

	if err := saveAppsConfig(config); err != nil {
		return fmt.Errorf("failed to save config: %v", err)
	}

	return nil
}

func stopIndividualApp(appName string) error {
	config, err := loadAppsConfig()
	if err != nil {
		return fmt.Errorf("failed to load config: %v", err)
	}

	// Find the app
	var targetApp *App
	for i := range config.Apps {
		if config.Apps[i].Name == appName {
			targetApp = &config.Apps[i]
			break
		}
	}

	if targetApp == nil {
		return fmt.Errorf("app '%s' not found", appName)
	}

	if targetApp.PID == 0 {
		return fmt.Errorf("app '%s' is not running", targetApp.Name)
	}

	if !isProcessRunning(targetApp.PID) {
		// Clear the log file when we detect the process has stopped
		if targetApp.LogFile != "" {
			if file, err := os.Create(targetApp.LogFile); err == nil {
				fmt.Fprintf(file, "=== App '%s' stopped (process not found) at %s ===\n", targetApp.Name, time.Now().Format(time.RFC3339))
				file.Close()
			}
		}
		targetApp.PID = 0

		if err := saveAppsConfig(config); err != nil {
			return fmt.Errorf("failed to save config: %v", err)
		}

		return fmt.Errorf("app '%s' (PID: %d) is not running", targetApp.Name, targetApp.PID)
	}

	process, err := os.FindProcess(targetApp.PID)
	if err != nil {
		return fmt.Errorf("failed to find process for app '%s' (PID: %d): %v", targetApp.Name, targetApp.PID, err)
	}

	if err := process.Signal(os.Interrupt); err != nil {
		return fmt.Errorf("failed to stop app '%s' (PID: %d): %v", targetApp.Name, targetApp.PID, err)
	}

	// Clear the log file when stopping
	if targetApp.LogFile != "" {
		if file, err := os.Create(targetApp.LogFile); err == nil {
			fmt.Fprintf(file, "=== App '%s' stopped at %s ===\n", targetApp.Name, time.Now().Format(time.RFC3339))
			file.Close()
		}
	}

	targetApp.PID = 0

	if err := saveAppsConfig(config); err != nil {
		return fmt.Errorf("failed to save config: %v", err)
	}

	return nil
}

func listApps(cmd *cobra.Command, args []string) {
	config, err := loadAppsConfig()
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	if len(config.Apps) == 0 {
		fmt.Println("No apps configured.")
		return
	}

	fmt.Printf("%-15s %-6s %-10s %-30s %s\n", "NAME", "PORT", "STATUS", "COMMAND", "LOG FILE")
	fmt.Println(strings.Repeat("=", 80))

	for _, app := range config.Apps {
		status := "stopped"
		if app.PID != 0 && isProcessRunning(app.PID) {
			status = fmt.Sprintf("running:%d", app.PID)
		}

		logFile := app.LogFile
		if logFile == "" {
			logFile = "none"
		}

		fmt.Printf("%-15s %-6d %-10s %-30s %s\n",
			app.Name,
			app.Port,
			status,
			truncateString(app.Command, 30),
			logFile)
	}
}

func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}

// Web UI implementation
func startUI(cmd *cobra.Command, args []string) {
	port := 9090 // Default port for the UI

	// Set up HTTP handlers
	http.HandleFunc("/", serveHome)
	http.HandleFunc("/api/apps", handleApps)
	http.HandleFunc("/api/apps/start", handleStartApps)
	http.HandleFunc("/api/apps/stop", handleStopApps)
	http.HandleFunc("/api/apps/delete", handleDeleteApp)
	http.HandleFunc("/api/apps/logs", handleAppLogs)

	fmt.Printf("Starting Loopserve Web UI on http://localhost:%d\n", port)
	log.Fatal(http.ListenAndServe(fmt.Sprintf(":%d", port), nil))
}

func serveHome(w http.ResponseWriter, r *http.Request) {
	html := `<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>Loopserve</title>
    <style>
        * {
            margin: 0;
            padding: 0;
            box-sizing: border-box;
        }
        
        body {
            font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif;
            background: #f9f9f9;
            color: #333;
            line-height: 1.5;
            margin: 0;
            padding: 1rem;
        }
        
        .container {
            max-width: 1200px;
            margin: 0 auto;
            padding: 0;
        }
        
        h1 {
            font-size: 2rem;
            font-weight: 600;
            color: #111;
            margin-bottom: 1.5rem;
            letter-spacing: -0.025em;
        }
        
        .actions {
            display: flex;
            gap: 0.5rem;
            margin-bottom: 1rem;
            flex-wrap: wrap;
            justify-content: flex-end;
        }
        
        button, a.btn-open, a.btn-start {
            padding: 0.5rem 1rem;
            border: 1px solid #ccc;
            border-radius: 4px;
            background: #f0f0f0;
            color: #333;
            cursor: pointer;
            font-size: 0.9rem;
            font-weight: 500;
            transition: all 0.15s ease;
            text-decoration: none;
            display: inline-block;
            white-space: nowrap;
            min-width: 60px;
        }
        
        button:hover, a.btn-open:hover, a.btn-start:hover {
            background: #e9ecef;
            border-color: #9ca3af;
        }
        
        .btn-primary {
            background: #007bff;
            color: white;
            border-color: #007bff;
        }
        
        .btn-primary:hover {
            background: #0056b3;
            border-color: #0056b3;
        }
        
        .btn-success, .btn-start {
            background: #28a745;
            color: white;
            border-color: #28a745;
        }
        
        .btn-success:hover, .btn-start:hover {
            background: #218838;
            border-color: #218838;
        }
        
        .btn-open {
            background: #28a745;
            color: white;
            border-color: #28a745;
        }
        
        .btn-open:hover {
            background: #218838;
            border-color: #218838;
        }
        
        .btn-stop {
            background: #ffc107;
            color: #212529;
            border-color: #ffc107;
        }
        
        .btn-stop:hover {
            background: #e0a800;
            border-color: #e0a800;
        }
        
        .btn-relay {
            background: #007bff;
            color: white;
            border-color: #007bff;
        }
        
        .btn-relay:hover {
            background: #0056b3;
            border-color: #0056b3;
        }
        
        .btn-danger {
            background: #dc3545;
            color: white;
            border-color: #dc3545;
        }
        
        .btn-danger:hover {
            background: #c82333;
            border-color: #c82333;
        }
        
        .btn-secondary {
            background: #6c757d;
            color: white;
            border-color: #6c757d;
        }
        
        .btn-secondary:hover {
            background: #5a6268;
            border-color: #5a6268;
        }
        
        button:disabled {
            opacity: 0.5;
            cursor: not-allowed;
        }
        
        .form-section {
            background: white;
            border: 1px solid #ddd;
            border-radius: 8px;
            padding: 2rem;
            margin-bottom: 1rem;
            box-shadow: 0 1px 3px rgba(0,0,0,0.05);
        }
        
        .form-section h3 {
            font-size: 1.5rem;
            font-weight: 600;
            color: #333;
            margin: 0 0 1rem 0;
        }
        
        .form-row {
            display: flex;
            gap: 0.5rem;
            flex-wrap: wrap;
            align-items: end;
        }
        
        .form-group {
            display: flex;
            flex-direction: column;
            flex: 1;
            min-width: 120px;
        }
        
        .form-group.command {
            flex: 2;
        }
        
        label {
            font-size: 0.9rem;
            font-weight: 500;
            color: #333;
            margin-bottom: 0.5rem;
            display: block;
        }
        
        input[type="text"], 
        input[type="number"] {
            padding: 0.5rem;
            border: 1px solid #ccc;
            border-radius: 4px;
            font-size: 0.9rem;
            background: white;
            transition: border-color 0.15s ease;
            width: 100%;
            box-sizing: border-box;
        }
        
        input:focus {
            outline: none;
            border-color: #007bff;
        }
        
        .table-container {
            background: white;
            border: 1px solid #ddd;
            border-radius: 8px;
            overflow: hidden;
            box-shadow: 0 1px 3px rgba(0,0,0,0.05);
        }
        
        table {
            width: 100%;
            border-collapse: collapse;
            background: white;
        }
        
        th, td {
            padding: 0.75rem 1rem;
            text-align: left;
            border-bottom: 1px solid #eee;
            vertical-align: middle;
        }
        
        th {
            background-color: #f0f0f0;
            font-weight: 600;
            font-size: 0.9rem;
            white-space: nowrap;
        }
        
        td {
            font-size: 0.9rem;
            word-wrap: break-word;
        }
        
        tbody tr:hover {
            background-color: #f5f5f5;
        }
        
        tbody tr:last-child td {
            border-bottom: none;
        }
        
        .status-running {
            color: #28a745;
            font-weight: 600;
        }
        
        .status-stopped {
            color: #6c757d;
        }
        
        .actions-cell {
            display: flex;
            gap: 0.25rem;
            align-items: center;
            flex-wrap: wrap;
        }
        
        .actions-cell button,
        .actions-cell a {
            padding: 0.25rem 0.5rem;
            font-size: 0.8rem;
            border-radius: 4px;
            text-decoration: none;
            display: inline-block;
            min-width: 50px;
            text-align: center;
        }
        
        .logs {
            background: #f8f9fa;
            color: #333;
            padding: 1rem;
            border-radius: 8px;
            font-family: 'SF Mono', Monaco, 'Cascadia Code', 'Roboto Mono', Consolas, 'Courier New', monospace;
            font-size: 0.8rem;
            max-height: 400px;
            overflow-y: auto;
            margin-top: 1rem;
            border: 1px solid #ddd;
            box-shadow: 0 1px 3px rgba(0,0,0,0.05);
        }
        
        .empty-state {
            text-align: center;
            padding: 2rem 1rem;
            color: #6c757d;
        }
        
        .empty-state h3 {
            font-size: 1.2rem;
            font-weight: 600;
            margin-bottom: 0.5rem;
            color: #495057;
        }
        
        .empty-state p {
            font-size: 0.9rem;
        }
        
        code {
            background: #f8f9fa;
            padding: 0.2rem 0.4rem;
            border-radius: 3px;
            font-size: 0.85rem;
            color: #e83e8c;
        }
    </style>
</head>
<body>
    <div class="container">
        <h1>Loopserve</h1>
        
        <div class="actions">
            <button class="btn-success" onclick="startAllApps()">Start All</button>
            <button class="btn-secondary" onclick="stopAllApps()">Stop All</button>
            <button onclick="loadApps()">Refresh</button>
        </div>

        <div class="form-section">
            <h3>Add App</h3>
            <div class="form-row">
                <div class="form-group">
                    <label for="appName">Name</label>
                    <input type="text" id="appName" placeholder="my-app" />
                </div>
                <div class="form-group">
                    <label for="appPort">Port</label>
                    <input type="number" id="appPort" placeholder="3000" min="1" max="65535" />
                </div>
                <div class="form-group command">
                    <label for="appCommand">Command</label>
                    <input type="text" id="appCommand" placeholder="./my-app" />
                </div>
                <button class="btn-primary" onclick="addApp()">Add</button>
            </div>
        </div>

        <div class="table-container">
            <table id="appsTable">
                <thead>
                    <tr>
                        <th>App</th>
                        <th>Command</th>
                        <th>Port</th>
                        <th>Status</th>
                        <th>PID</th>
                        <th>Relay</th>
                        <th>Actions</th>
                    </tr>
                </thead>
                <tbody>
                </tbody>
            </table>
        </div>

        <div id="logs" class="logs" style="display:none;">
            <div id="logsContent"></div>
        </div>
    </div>

    <script>
        let apps = [];
        let currentlyViewingLogs = null; // Track which app's logs are currently shown
        
        async function loadApps() {
            try {
                const response = await fetch('/api/apps');
                apps = await response.json();
                renderApps();
            } catch (error) {
                console.error('Error loading apps:', error);
                alert('Error loading apps: ' + error.message);
            }
        }
        
        function renderApps() {
            const tbody = document.querySelector('#appsTable tbody');
            
            if (apps.length === 0) {
                tbody.innerHTML = ` + "`" + `
                    <tr>
                        <td colspan="7">
                            <div class="empty-state">
                                <h3>No apps configured</h3>
                                <p>Add your first app using the form above</p>
                            </div>
                        </td>
                    </tr>
                ` + "`" + `;
                return;
            }
            
            tbody.innerHTML = '';
            
            apps.forEach(app => {
                const row = document.createElement('tr');
                const isRunning = app.pid && app.pid > 0;
                const status = isRunning ? 'running' : 'stopped';
                const statusClass = isRunning ? 'status-running' : 'status-stopped';
                const pidDisplay = isRunning ? app.pid : '-';
                
                // Check if this app's logs are currently being viewed
                const isViewingLogs = currentlyViewingLogs === app.name;
                const logsButtonClass = isViewingLogs ? 'btn-primary' : 'btn-secondary';
                const logsButtonText = isViewingLogs ? 'Hide Logs' : 'Logs';
                
                row.innerHTML = ` + "`" + `
                    <td>${app.name}</td>
                    <td title="${app.command}"><code>${truncate(app.command, 50)}</code></td>
                    <td>${app.port}</td>
                    <td class="${statusClass}">${status}</td>
                    <td>${pidDisplay}</td>
                    <td>-</td>
                    <td>
                        <div class="actions-cell">
                            ${isRunning ? ` + "`<a href=\"http://localhost:${app.port}\" target=\"_blank\" class=\"btn-open\">Open</a>`" + ` : ` + "`<button class=\"btn-start\" onclick=\"startApp('${app.name}')\">Start</button>`" + `}
                            ${isRunning ? ` + "`<button class=\"btn-stop\" onclick=\"stopApp('${app.name}')\">Stop</button>`" + ` : ''}
                            ${app.log_file ? ` + "`<button class=\"${logsButtonClass}\" onclick=\"viewLogs('${app.name}')\">${logsButtonText}</button>`" + ` : ''}
                            <button class="btn-relay" onclick="manageRelay('${app.name}')">Relay</button>
                            <button class="btn-danger" onclick="deleteApp('${app.name}')">Delete</button>
                        </div>
                    </td>
                ` + "`" + `;
                tbody.appendChild(row);
            });
        }
        
        function truncate(str, maxLen) {
            return str.length > maxLen ? str.substring(0, maxLen) + '...' : str;
        }
        
        async function addApp() {
            const name = document.getElementById('appName').value.trim();
            const port = parseInt(document.getElementById('appPort').value);
            const command = document.getElementById('appCommand').value.trim();
            
            if (!name || !port || !command) {
                alert('Please fill in all fields');
                return;
            }
            
            if (apps.find(app => app.name === name)) {
                alert('App with this name already exists');
                return;
            }
            
            if (apps.find(app => app.port === port)) {
                alert('Port is already in use');
                return;
            }
            
            try {
                const response = await fetch('/api/apps', {
                    method: 'POST',
                    headers: { 'Content-Type': 'application/json' },
                    body: JSON.stringify({ name, port, command })
                });
                
                if (response.ok) {
                    document.getElementById('appName').value = '';
                    document.getElementById('appPort').value = '';
                    document.getElementById('appCommand').value = '';
                    loadApps();
                } else {
                    const error = await response.text();
                    alert('Error adding app: ' + error);
                }
            } catch (error) {
                alert('Error adding app: ' + error.message);
            }
        }
        
        async function deleteApp(name) {
            if (!confirm(` + "`Are you sure you want to delete '${name}'?`" + `)) return;
            
            try {
                const response = await fetch('/api/apps/delete', {
                    method: 'POST',
                    headers: { 'Content-Type': 'application/json' },
                    body: JSON.stringify({ name })
                });
                
                if (response.ok) {
                    loadApps();
                } else {
                    const error = await response.text();
                    alert('Error deleting app: ' + error);
                }
            } catch (error) {
                alert('Error deleting app: ' + error.message);
            }
        }
        
        async function startAllApps() {
            try {
                const response = await fetch('/api/apps/start', { method: 'POST' });
                if (response.ok) {
                    setTimeout(loadApps, 1000);
                } else {
                    const error = await response.text();
                    alert('Error starting apps: ' + error);
                }
            } catch (error) {
                alert('Error starting apps: ' + error.message);
            }
        }
        
        async function stopAllApps() {
            try {
                const response = await fetch('/api/apps/stop', { method: 'POST' });
                if (response.ok) {
                    setTimeout(loadApps, 1000);
                } else {
                    const error = await response.text();
                    alert('Error stopping apps: ' + error);
                }
            } catch (error) {
                alert('Error stopping apps: ' + error.message);
            }
        }
        
        async function viewLogs(appName) {
            const logsElement = document.getElementById('logs');
            
            // If clicking the same app's logs button and logs are visible, hide them
            if (currentlyViewingLogs === appName && logsElement.style.display === 'block') {
                logsElement.style.display = 'none';
                currentlyViewingLogs = null;
                // Immediately update button states
                loadApps();
                return;
            }
            
            const app = apps.find(a => a.name === appName);
            if (!app || !app.log_file) {
                alert('No log file available for this app');
                return;
            }
            
            // Immediately update state and button appearance
            currentlyViewingLogs = appName;
            logsElement.style.display = 'block';
            
            // Update button states immediately
            loadApps();
            
            // Show loading indicator in logs
            document.getElementById('logsContent').textContent = 'Loading logs...';
            
            try {
                const response = await fetch(` + "`/api/apps/logs?name=${encodeURIComponent(appName)}`" + `);
                const logs = await response.text();
                
                document.getElementById('logsContent').textContent = logs;
                logsElement.scrollTop = logsElement.scrollHeight;
            } catch (error) {
                document.getElementById('logsContent').textContent = 'Error loading logs: ' + error.message;
            }
        }
        
        function manageRelay(appName) {
            // Placeholder function for relay management
            alert('Relay management for ' + appName + ' - Coming soon!');
        }
        
        function startApp(appName) {
            fetch('/api/apps/start', {
                method: 'POST',
                headers: {
                    'Content-Type': 'application/json',
                },
                body: JSON.stringify({ name: appName })
            })
            .then(response => response.json())
            .then(data => {
                if (data.error) {
                    alert('Error starting app: ' + data.error);
                } else {
                    alert('App ' + appName + ' started successfully');
                    loadApps(); // Refresh the table
                }
            })
            .catch(error => {
                alert('Error starting app: ' + error);
            });
        }
        
        function stopApp(appName) {
            fetch('/api/apps/stop', {
                method: 'POST',
                headers: {
                    'Content-Type': 'application/json',
                },
                body: JSON.stringify({ name: appName })
            })
            .then(response => response.json())
            .then(data => {
                if (data.error) {
                    alert('Error stopping app: ' + data.error);
                } else {
                    alert('App ' + appName + ' stopped successfully');
                    loadApps(); // Refresh the table
                }
            })
            .catch(error => {
                alert('Error stopping app: ' + error);
            });
        }
        
        // Load apps on page load
        loadApps();
        
        // Auto-refresh every 5 seconds
        setInterval(loadApps, 5000);
    </script>
</body>
</html>`

	w.Header().Set("Content-Type", "text/html")
	w.Write([]byte(html))
}

func handleApps(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case "GET":
		config, err := loadAppsConfig()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		// Update status for each app
		for i := range config.Apps {
			app := &config.Apps[i]
			if app.PID != 0 && !isProcessRunning(app.PID) {
				// Clear the log file when we detect the process has stopped
				if app.LogFile != "" {
					if file, err := os.Create(app.LogFile); err == nil {
						fmt.Fprintf(file, "=== App '%s' stopped (detected via status check) at %s ===\n", app.Name, time.Now().Format(time.RFC3339))
						file.Close()
					}
				}
				app.PID = 0 // Reset PID if process is not running
			}
		}

		// Save the config to persist any PID changes
		if err := saveAppsConfig(config); err != nil {
			// Log error but don't fail the request
			fmt.Printf("Warning: Failed to save config after status update: %v\n", err)
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(config.Apps)

	case "POST":
		var newApp App
		if err := json.NewDecoder(r.Body).Decode(&newApp); err != nil {
			http.Error(w, "Invalid JSON", http.StatusBadRequest)
			return
		}

		config, err := loadAppsConfig()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		// Validate
		for _, app := range config.Apps {
			if app.Name == newApp.Name {
				http.Error(w, "App with this name already exists", http.StatusBadRequest)
				return
			}
			if app.Port == newApp.Port {
				http.Error(w, "Port already in use", http.StatusBadRequest)
				return
			}
		}

		config.Apps = append(config.Apps, newApp)

		if err := saveAppsConfig(config); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		w.WriteHeader(http.StatusCreated)
	}
}

func handleStartApps(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Name string `json:"name"`
	}

	// Try to decode JSON body for individual app operation
	if err := json.NewDecoder(r.Body).Decode(&req); err == nil && req.Name != "" {
		// Start individual app
		if err := startIndividualApp(req.Name); err != nil {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"message": "App started successfully"})
	} else {
		// Start all apps (legacy behavior)
		startApps(nil, []string{})
		w.WriteHeader(http.StatusOK)
	}
}

func handleStopApps(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Name string `json:"name"`
	}

	// Try to decode JSON body for individual app operation
	if err := json.NewDecoder(r.Body).Decode(&req); err == nil && req.Name != "" {
		// Stop individual app
		if err := stopIndividualApp(req.Name); err != nil {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"message": "App stopped successfully"})
	} else {
		// Stop all apps (legacy behavior)
		stopApps(nil, []string{})
		w.WriteHeader(http.StatusOK)
	}
}

func handleDeleteApp(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
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

	config, err := loadAppsConfig()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Find and remove the app
	newApps := []App{}
	found := false
	for _, app := range config.Apps {
		if app.Name != req.Name {
			newApps = append(newApps, app)
		} else {
			found = true
			// Stop the app if it's running
			if app.PID != 0 && isProcessRunning(app.PID) {
				if process, err := os.FindProcess(app.PID); err == nil {
					process.Signal(os.Interrupt)
				}
			}
		}
	}

	if !found {
		http.Error(w, "App not found", http.StatusNotFound)
		return
	}

	config.Apps = newApps

	if err := saveAppsConfig(config); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
}

func handleAppLogs(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	appName := r.URL.Query().Get("name")
	if appName == "" {
		http.Error(w, "App name is required", http.StatusBadRequest)
		return
	}

	config, err := loadAppsConfig()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Find the app
	var app *App
	for i := range config.Apps {
		if config.Apps[i].Name == appName {
			app = &config.Apps[i]
			break
		}
	}

	if app == nil {
		http.Error(w, "App not found", http.StatusNotFound)
		return
	}

	if app.LogFile == "" {
		http.Error(w, "No log file available for this app", http.StatusNotFound)
		return
	}

	// Read the log file
	logContent, err := os.ReadFile(app.LogFile)
	if err != nil {
		http.Error(w, fmt.Sprintf("Error reading log file: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/plain")
	w.Write(logContent)
}
