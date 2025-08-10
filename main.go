package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"
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

func main() {
	startUI()
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

func ensureLogsDir() error {
	if _, err := os.Stat(logsDir); os.IsNotExist(err) {
		return os.MkdirAll(logsDir, 0755)
	}
	return nil
}

// Web UI implementation
func startUI() {
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
            background: rgba(96, 165, 250, 0.12);
            color: #1f2937;
            border-color: rgba(96, 165, 250, 0.2);
        }
        
        .btn-primary:hover {
            background: rgba(96, 165, 250, 0.2);
            border-color: rgba(96, 165, 250, 0.3);
        }
        
        .btn-success, .btn-start {
            background: rgba(34, 197, 94, 0.12);
            color: #1f2937;
            border-color: rgba(34, 197, 94, 0.2);
        }
        
        .btn-success:hover, .btn-start:hover {
            background: rgba(34, 197, 94, 0.2);
            border-color: rgba(34, 197, 94, 0.3);
        }
        
        .btn-open {
            background: rgba(34, 197, 94, 0.12);
            color: #1f2937;
            border-color: rgba(34, 197, 94, 0.2);
        }
        
        .btn-open:hover {
            background: rgba(34, 197, 94, 0.2);
            border-color: rgba(34, 197, 94, 0.3);
        }
        
        .btn-stop {
            background: rgba(251, 146, 60, 0.12);
            color: #1f2937;
            border-color: rgba(251, 146, 60, 0.2);
        }
        
        .btn-stop:hover {
            background: rgba(251, 146, 60, 0.2);
            border-color: rgba(251, 146, 60, 0.3);
        }
        
        .btn-restart {
            background: rgba(168, 85, 247, 0.12);
            color: #1f2937;
            border-color: rgba(168, 85, 247, 0.2);
        }
        
        .btn-restart:hover {
            background: rgba(168, 85, 247, 0.2);
            border-color: rgba(168, 85, 247, 0.3);
        }
        
        .btn-relay {
            background: rgba(14, 165, 233, 0.12);
            color: #1f2937;
            border-color: rgba(14, 165, 233, 0.2);
        }
        
        .btn-relay:hover {
            background: rgba(14, 165, 233, 0.2);
            border-color: rgba(14, 165, 233, 0.3);
        }
        
        .btn-danger {
            background: rgba(239, 68, 68, 0.12);
            color: #1f2937;
            border-color: rgba(239, 68, 68, 0.2);
        }
        
        .btn-danger:hover {
            background: rgba(239, 68, 68, 0.2);
            border-color: rgba(239, 68, 68, 0.3);
        }
        
        .btn-secondary {
            background: rgba(107, 114, 128, 0.12);
            color: #1f2937;
            border-color: rgba(107, 114, 128, 0.2);
        }
        
        .btn-secondary:hover {
            background: rgba(107, 114, 128, 0.2);
            border-color: rgba(107, 114, 128, 0.3);
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
        
        .status-badge {
            display: inline-block;
            padding: 0.25rem 0.5rem;
            border-radius: 12px;
            font-size: 0.75rem;
            font-weight: 600;
            text-transform: uppercase;
            letter-spacing: 0.025em;
        }
        
        .status-badge.running {
            background: rgba(34, 197, 94, 0.1);
            color: #065f46;
            border: 1px solid rgba(34, 197, 94, 0.2);
        }
        
        .status-badge.stopped {
            background: rgba(107, 114, 128, 0.1);
            color: #374151;
            border: 1px solid rgba(107, 114, 128, 0.2);
        }
        
        .hidden {
            display: none;
        }
        
        /* Flash message styles */
        .flash-message {
            position: fixed;
            top: 1rem;
            left: 50%;
            transform: translateX(-50%);
            padding: 1rem 2rem;
            border-radius: 8px;
            font-weight: 500;
            font-size: 0.9rem;
            box-shadow: 0 4px 12px rgba(0, 0, 0, 0.15);
            z-index: 1000;
            min-width: 300px;
            text-align: center;
            transition: all 0.3s ease;
            opacity: 0;
            transform: translateX(-50%) translateY(-20px);
        }
        
        .flash-message.show {
            opacity: 1;
            transform: translateX(-50%) translateY(0);
        }
        
        .flash-message.success {
            background: rgba(34, 197, 94, 0.9);
            color: white;
            border: 1px solid rgba(34, 197, 94, 1);
        }
        
        .flash-message.error {
            background: rgba(239, 68, 68, 0.9);
            color: white;
            border: 1px solid rgba(239, 68, 68, 1);
        }
        
        .flash-message.info {
            background: rgba(59, 130, 246, 0.9);
            color: white;
            border: 1px solid rgba(59, 130, 246, 1);
        }
        
        .actions-cell {
            display: grid;
            grid-template-columns: repeat(3, 1fr);
            gap: 0.25rem;
            align-items: center;
            justify-items: center;
            width: 100%;
            max-width: 240px;
        }
        
        .actions-cell button,
        .actions-cell a {
            padding: 0.25rem 0.4rem;
            font-size: 0.75rem;
            border-radius: 4px;
            text-decoration: none;
            display: inline-block;
            text-align: center;
            white-space: nowrap;
            min-height: 28px;
            line-height: 1.2;
            width: 100%;
            min-width: 50px;
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
                        <th>Status</th>
                        <th>Port</th>
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
        
        <!-- Flash message for notifications -->
        <div id="flashMessage" class="flash-message">
            <span id="flashText">Message here</span>
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
                showFlash('error', 'Error loading apps: ' + error.message);
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
                const statusClass = isRunning ? 'running' : 'stopped';
                const pidDisplay = isRunning ? app.pid : '-';
                
                // Check if this app's logs are currently being viewed
                const isViewingLogs = currentlyViewingLogs === app.name;
                const logsButtonClass = isViewingLogs ? 'btn-primary' : 'btn-secondary';
                const logsButtonText = isViewingLogs ? 'Hide' : 'Logs';
                
                row.innerHTML = ` + "`" + `
                    <td>${app.name}</td>
                    <td title="${app.command}"><code>${truncate(app.command, 50)}</code></td>
                    <td><span class="status-badge ${statusClass}">${status}</span></td>
                    <td>${app.port}</td>
                    <td>${pidDisplay}</td>
                    <td>-</td>
                    <td>
                        <div class="actions-cell">
                            ${isRunning ? 
                                ` + "`" + `<a href="http://localhost:${app.port}" target="_blank" class="btn-open">Open</a>` + "`" + ` +
                                ` + "`" + `<button class="btn-restart" onclick="restartApp('${app.name}')">Restart</button>` + "`" + ` +
                                ` + "`" + `<button class="btn-stop" onclick="stopApp('${app.name}')">Stop</button>` + "`" + ` +
                                (app.log_file ? ` + "`" + `<button class="${logsButtonClass}" onclick="viewLogs('${app.name}')">${logsButtonText}</button>` + "`" + ` : '<span></span>') +
                                ` + "`" + `<button class="btn-relay" onclick="manageRelay('${app.name}')">Relay</button>` + "`" + ` +
                                ` + "`" + `<button class="btn-danger" onclick="deleteApp('${app.name}')">Delete</button>` + "`" + ` : 
                                ` + "`" + `<button class="btn-start" onclick="startApp('${app.name}')">Start</button>` + "`" + ` +
                                '<span></span><span></span>' +
                                (app.log_file ? ` + "`" + `<button class="${logsButtonClass}" onclick="viewLogs('${app.name}')">${logsButtonText}</button>` + "`" + ` : '<span></span>') +
                                ` + "`" + `<button class="btn-relay" onclick="manageRelay('${app.name}')">Relay</button>` + "`" + ` +
                                ` + "`" + `<button class="btn-danger" onclick="deleteApp('${app.name}')">Delete</button>` + "`" + `}
                        </div>
                    </td>
                ` + "`" + `;
                tbody.appendChild(row);
            });
        }
        
        function truncate(str, maxLen) {
            return str.length > maxLen ? str.substring(0, maxLen) + '...' : str;
        }
        
        // Flash message functions
        function showFlash(type, message) {
            const flash = document.getElementById('flashMessage');
            const text = document.getElementById('flashText');
            
            // Remove any existing type classes
            flash.classList.remove('success', 'error', 'info', 'show');
            
            // Add the appropriate type class
            flash.classList.add(type);
            text.textContent = message;
            
            // Show the flash message
            flash.classList.add('show');
            
            // Auto-hide after 3 seconds
            setTimeout(() => {
                flash.classList.remove('show');
            }, 3000);
        }
        
        async function addApp() {
            const name = document.getElementById('appName').value.trim();
            const port = parseInt(document.getElementById('appPort').value);
            const command = document.getElementById('appCommand').value.trim();
            
            if (!name || !port || !command) {
                showFlash('error', 'Please fill in all fields');
                return;
            }
            
            if (apps.find(app => app.name === name)) {
                showFlash('error', 'App with this name already exists');
                return;
            }
            
            if (apps.find(app => app.port === port)) {
                showFlash('error', 'Port is already in use');
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
                    showFlash('success', ` + "`App '${name}' added successfully`" + `);
                    loadApps();
                } else {
                    const error = await response.text();
                    showFlash('error', 'Error adding app: ' + error);
                }
            } catch (error) {
                showFlash('error', 'Error adding app: ' + error.message);
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
                    showFlash('success', ` + "`App '${name}' deleted successfully`" + `);
                    loadApps();
                } else {
                    const error = await response.text();
                    showFlash('error', 'Error deleting app: ' + error);
                }
            } catch (error) {
                showFlash('error', 'Error deleting app: ' + error.message);
            }
        }
        
        async function startAllApps() {
            try {
                const response = await fetch('/api/apps/start', { method: 'POST' });
                if (response.ok) {
                    showFlash('success', 'All apps started successfully');
                    setTimeout(loadApps, 1000);
                } else {
                    const error = await response.text();
                    showFlash('error', 'Error starting apps: ' + error);
                }
            } catch (error) {
                showFlash('error', 'Error starting apps: ' + error.message);
            }
        }
        
        async function stopAllApps() {
            try {
                const response = await fetch('/api/apps/stop', { method: 'POST' });
                if (response.ok) {
                    showFlash('success', 'All apps stopped successfully');
                    setTimeout(loadApps, 1000);
                } else {
                    const error = await response.text();
                    showFlash('error', 'Error stopping apps: ' + error);
                }
            } catch (error) {
                showFlash('error', 'Error stopping apps: ' + error.message);
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
                showFlash('error', 'No log file available for this app');
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
            showFlash('info', 'Relay management for ' + appName + ' - Coming soon!');
        }
        
        async function restartApp(appName) {
            try {
                // First stop the app
                const stopResponse = await fetch('/api/apps/stop', {
                    method: 'POST',
                    headers: {
                        'Content-Type': 'application/json',
                    },
                    body: JSON.stringify({ name: appName })
                });
                
                const stopData = await stopResponse.json();
                if (stopData.error) {
                    showFlash('error', 'Error stopping app: ' + stopData.error);
                    return;
                }
                
                // Wait a moment for the process to fully stop
                await new Promise(resolve => setTimeout(resolve, 1000));
                
                // Then start the app
                const startResponse = await fetch('/api/apps/start', {
                    method: 'POST',
                    headers: {
                        'Content-Type': 'application/json',
                    },
                    body: JSON.stringify({ name: appName })
                });
                
                const startData = await startResponse.json();
                if (startData.error) {
                    showFlash('error', 'Error starting app: ' + startData.error);
                } else {
                    showFlash('success', ` + "`App '${appName}' restarted successfully`" + `);
                    loadApps(); // Refresh the table
                }
            } catch (error) {
                showFlash('error', 'Error restarting app: ' + error.message);
            }
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
                    showFlash('error', 'Error starting app: ' + data.error);
                } else {
                    showFlash('success', ` + "`App '${appName}' started successfully`" + `);
                    loadApps(); // Refresh the table
                }
            })
            .catch(error => {
                showFlash('error', 'Error starting app: ' + error.message);
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
                    showFlash('error', 'Error stopping app: ' + data.error);
                } else {
                    showFlash('success', ` + "`App '${appName}' stopped successfully`" + `);
                    loadApps(); // Refresh the table
                }
            })
            .catch(error => {
                showFlash('error', 'Error stopping app: ' + error.message);
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
		// Start all apps
		config, err := loadAppsConfig()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		if err := ensureLogsDir(); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		for i := range config.Apps {
			app := &config.Apps[i]
			if app.PID != 0 && isProcessRunning(app.PID) {
				continue // Skip already running apps
			}
			startApp(app)
		}

		if err := saveAppsConfig(config); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

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
		// Stop all apps
		config, err := loadAppsConfig()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		for i := range config.Apps {
			app := &config.Apps[i]
			if app.PID == 0 {
				continue // Skip not running apps
			}

			if !isProcessRunning(app.PID) {
				app.PID = 0
				continue
			}

			if process, err := os.FindProcess(app.PID); err == nil {
				process.Signal(os.Interrupt)
			}
			app.PID = 0
		}

		if err := saveAppsConfig(config); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

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
