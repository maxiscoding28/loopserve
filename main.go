package main

import (
	"encoding/json"
	"fmt"
	"log"
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

func init() {
	rootCmd.AddCommand(addCmd)
	rootCmd.AddCommand(startCmd)
	rootCmd.AddCommand(stopCmd)
	rootCmd.AddCommand(listCmd)
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

	// Create log file
	file, err := os.Create(logFileName)
	if err != nil {
		return fmt.Errorf("failed to create log file: %w", err)
	}
	defer file.Close()

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

	// Log the start time
	fmt.Fprintf(logFileWriter, "=== Started %s at %s (working dir: %s) ===\n", app.Name, time.Now().Format(time.RFC3339), execCmd.Dir)
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
		app.PID = 0
	}

	if err := saveAppsConfig(config); err != nil {
		log.Fatalf("Failed to save config: %v", err)
	}
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
