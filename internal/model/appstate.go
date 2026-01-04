package model

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
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

type ConnectionStatus int

const (
	StatusDisconnected ConnectionStatus = iota
	StatusConnecting
	StatusConnected
	StatusError
)

// ConnectionState represents the state of an SSH connection
type ConnectionState struct {
	Connection *Connection
	Client     *ssh.SSHClientWrapper // Wrapper around ssh.Client for managing sessions
	Status     ConnectionStatus      // Current status of the connection
	LastActive time.Time             // Timestamp of the last activity
	LastError  error                 // Error message if any
	Output     []string              // Recent output from connection
}

// Config represents the saved configuration file structure
type Config struct {
	Connections []*Connection `json:"connections"`
}

// AppState represents application state
type AppState struct {
	Connections   []*ConnectionState // List of all connections
	SelectedIndex int                // Index of the currently selected connection
	Config        *Config            // Loaded configuration
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
		Config:        &Config{Connections: make([]*Connection, 0)},
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
		return &Config{Connections: []*Connection{}}, nil
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	var config Config
	if err := json.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("failed to parse config file: %w", err)
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
