package model

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/SimonLariz/beacon/internal/ssh"
)

// Connection represents an SSH connection
type Connection struct {
	Alias   string `json:"alias"`              // User friendly name for the connection
	Host    string `json:"host"`               // Hostname or IP address
	Port    int    `json:"port"`               // SSH port
	User    string `json:"user"`               // SSH username
	KeyPath string `json:"key_path,omitempty"` // Optional path to SSH key
}

// CommandExecution represents a single command execution
type CommandExecution struct {
	Command   string        // The command that was executed
	Timestamp time.Time     // When the command was executed
	ExitCode  int           // Exit code from command
	Stdout    string        // Standard output
	Stderr    string        // Standard error
	Duration  time.Duration // How long the command took
	Completed bool          // Whether execution is complete
}

// CommandHistory stores global command history
type CommandHistory struct {
	Commands []string // List of executed commands (for up/down navigation)
	MaxSize  int      // Maximum number of commands to store
}

type ConnectionStatus int

const (
	StatusDisconnected ConnectionStatus = iota
	StatusConnecting
	StatusConnected
	StatusError
)

// ConnectionState represents the state of an SSH connection
type ConnectionState struct {
	Connection  *Connection
	Client      *ssh.SSHClientWrapper // Wrapper around ssh.Client for managing sessions
	Status      ConnectionStatus      // Current status of the connection
	LastActive  time.Time             // Timestamp of the last activity
	LastError   error                 // Error message if any
	Output      []string              // Recent output from connection (DEPRECATED)
	Executions  []*CommandExecution   // Full execution history
	CurrentExec *CommandExecution     // Currently running command (if any)
}

// Config represents the saved configuration file structure
type Config struct {
	Connections    []*Connection `json:"connections"`
	CommandHistory []string      `json:"command_history,omitempty"`
}

// AppState represents application state
type AppState struct {
	Connections        []*ConnectionState // List of all connections
	SelectedIndex      int                // Index of the currently selected connection
	Config             *Config            // Loaded configuration
	CommandHistory     *CommandHistory    // Global command history
	OutputScrollOffset int                // Current scroll position in output
}

// NewConnection creates a new Connection with defaults
func NewConnection(nickname, host, user string, port int) *Connection {
	if port == 0 {
		port = 22 // Default SSH port
	}
	return &Connection{
		Alias: nickname,
		Host:  host,
		Port:  port,
		User:  user,
	}
}

// Helper method to get status as string
func (cs *ConnectionState) StatusString() string {
	switch cs.Status {
	case StatusConnecting:
		return "connecting"
	case StatusConnected:
		return "connected"
	case StatusDisconnected:
		return "disconnected"
	case StatusError:
		return fmt.Sprintf("error: %v", cs.LastError)
	default:
		return "unknown"
	}
}

// NewAppState creates a new application state
func NewAppState() *AppState {
	return &AppState{
		Connections:   make([]*ConnectionState, 0),
		SelectedIndex: 0,
		Config:        &Config{Connections: make([]*Connection, 0), CommandHistory: make([]string, 0)},
		CommandHistory: &CommandHistory{
			Commands: make([]string, 0),
			MaxSize:  1000,
		},
		OutputScrollOffset: 0,
	}
}

// ConfigPath returns the path to the config file
func ConfigPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("failed to get home directory: %w", err)
	}

	configDir := filepath.Join(home, ".config", "beacon")
	if err := os.MkdirAll(configDir, 0700); err != nil {
		return "", fmt.Errorf("failed to create config directory: %w", err)
	}
	return filepath.Join(configDir, "connections.json"), nil
}

// LoadConfig loads the configuration from the config file
func LoadConfig() (*Config, error) {
	configPath, err := ConfigPath()
	if err != nil {
		return nil, err
	}

	// If file doesn't exist, return empty config
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		return &Config{
			Connections:    []*Connection{},
			CommandHistory: make([]string, 0),
		}, nil
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	var config Config
	if err := json.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("failed to parse config file: %w", err)
	}

	// Handle missing CommandHistory field for backwards compatibility
	if config.CommandHistory == nil {
		config.CommandHistory = make([]string, 0)
	}

	return &config, nil
}

// SaveConfig saves the configuration to the config file
func SaveConfig(config *Config) error {
	configPath, err := ConfigPath()
	if err != nil {
		return err
	}

	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to serialize config: %w", err)
	}

	if err := os.WriteFile(configPath, data, 0600); err != nil {
		return fmt.Errorf("failed to write config file: %w", err)
	}
	return nil
}

// AddConnection adds a new connection to the app state
func (app *AppState) AddConnection(conn *Connection) {
	app.Config.Connections = append(app.Config.Connections, conn)
	app.Connections = append(app.Connections, &ConnectionState{
		Connection: conn,
		Status:     StatusDisconnected,
		Output:     make([]string, 0),
		Executions: make([]*CommandExecution, 0),
	})
}

// DeleteConnection deletes a connection from the app state
func (app *AppState) DeleteConnection(index int) error {
	if index < 0 || index >= len(app.Config.Connections) {
		return fmt.Errorf("invalid connection index")
	}

	// Remove from both slices
	app.Config.Connections = append(app.Config.Connections[:index], app.Config.Connections[index+1:]...)
	app.Connections = append(app.Connections[:index], app.Connections[index+1:]...)

	// Adjust selected index if necessary
	if app.SelectedIndex >= len(app.Connections) && app.SelectedIndex > 0 {
		app.SelectedIndex--
	}
	return nil
}

// GetSelected returns the currently selected connection
func (app *AppState) GetSelected() *ConnectionState {
	if app.SelectedIndex < 0 || app.SelectedIndex >= len(app.Connections) {
		return nil
	}
	return app.Connections[app.SelectedIndex]
}

// SelectNext selects the next connection in the list
func (app *AppState) SelectNext() {
	if len(app.Connections) == 0 {
		return
	}
	app.SelectedIndex = (app.SelectedIndex + 1) % len(app.Connections)
}

// SelectPrevious selects the previous connection in the list
func (app *AppState) SelectPrevious() {
	if len(app.Connections) == 0 {
		return
	}
	app.SelectedIndex = (app.SelectedIndex - 1 + len(app.Connections)) % len(app.Connections)
}

// AddToHistory adds a command to global history
func (app *AppState) AddToHistory(cmd string) {
	if cmd == "" {
		return
	}

	// Avoid duplicates (if last command is same, don't add)
	if len(app.CommandHistory.Commands) > 0 {
		last := app.CommandHistory.Commands[len(app.CommandHistory.Commands)-1]
		if last == cmd {
			return
		}
	}

	app.CommandHistory.Commands = append(app.CommandHistory.Commands, cmd)
	app.Config.CommandHistory = app.CommandHistory.Commands

	// Enforce max size
	if len(app.CommandHistory.Commands) > app.CommandHistory.MaxSize {
		app.CommandHistory.Commands = app.CommandHistory.Commands[1:]
		app.Config.CommandHistory = app.CommandHistory.Commands
	}
}

// GetHistoryItem retrieves command at index (in reverse order, most recent first)
func (app *AppState) GetHistoryItem(index int) string {
	if index < 0 || index >= len(app.CommandHistory.Commands) {
		return ""
	}
	// Return in reverse order (most recent first)
	reverseIndex := len(app.CommandHistory.Commands) - 1 - index
	return app.CommandHistory.Commands[reverseIndex]
}

// HistorySize returns the number of commands in history
func (app *AppState) HistorySize() int {
	return len(app.CommandHistory.Commands)
}

// ScrollOutputUp scrolls the output view up by lines
func (app *AppState) ScrollOutputUp(lines int) {
	app.OutputScrollOffset += lines
	// Clamp to max
	selected := app.GetSelected()
	if selected != nil && len(selected.Executions) > 0 {
		// Rough estimate: each execution takes ~3 lines + output lines
		totalLines := 0
		for _, exec := range selected.Executions {
			totalLines += 3 // Command line + separator + exit code
			totalLines += len(strings.Split(exec.Stdout, "\n"))
			if exec.Stderr != "" {
				totalLines += len(strings.Split(exec.Stderr, "\n")) + 1
			}
		}
		if app.OutputScrollOffset > totalLines {
			app.OutputScrollOffset = totalLines
		}
	}
}

// ScrollOutputDown scrolls the output view down by lines
func (app *AppState) ScrollOutputDown(lines int) {
	app.OutputScrollOffset -= lines
	if app.OutputScrollOffset < 0 {
		app.OutputScrollOffset = 0
	}
}
