package ssh

import (
	"bufio"
	"bytes"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
)

// CommandResult contains the result of a command execution
type CommandResult struct {
	Stdout   string
	Stderr   string
	ExitCode int
	Duration time.Duration
	Error    error
}

type SSHClientWrapper struct {
	client     *ssh.Client
	config     *ssh.ClientConfig
	host       string
	connected  bool
	LastActive time.Time
}

// Connect establishes SSH connection using key-based authentication
// Tries KeyPath first, then SSH config, then falls back to default keys
func Connect(host string, port int, user string, keyPath string) (*SSHClientWrapper, error) {
	address := fmt.Sprintf("%s:%d", host, port)
	authMethods, err := createAuthMethods(keyPath, host)
	if err != nil {
		return nil, fmt.Errorf("failed to create auth methods: %v", err)
	}

	sshConfig := &ssh.ClientConfig{
		User:            user,
		Auth:            authMethods,
		HostKeyCallback: ssh.InsecureIgnoreHostKey(), // For simplicity; consider verifying host keys in production
		Timeout:         10 * time.Second,
	}

	client, err := ssh.Dial("tcp", address, sshConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to dial SSH: %v", err)
	}

	return &SSHClientWrapper{
		client:    client,
		config:    sshConfig,
		host:      address,
		connected: true,
	}, nil
}

// Disconnect closes the SSH connection
func (s *SSHClientWrapper) Disconnect() error {
	if s.client != nil {
		err := s.client.Close()
		if err != nil {
			return fmt.Errorf("failed to close SSH connection: %v", err)
		}
		s.connected = false
	}
	return nil
}

// IsConnected checks if the SSH client is connected
func (s *SSHClientWrapper) IsConnected() bool {
	return s.connected
}

// Ping tests if connection is still alive
func (s *SSHClientWrapper) Ping() error {
	session, err := s.client.NewSession()
	if err != nil {
		return fmt.Errorf("failed to create session: %v", err)
	}
	defer session.Close()

	// Run a simple command to test connection
	if err := session.Run("echo ping"); err != nil {
		return fmt.Errorf("ping command failed: %v", err)
	}
	return nil
}

// ExecuteCommand runs a command on the remote server and returns the result
// This is a blocking call - should be wrapped in a goroutine by the caller
func (s *SSHClientWrapper) ExecuteCommand(cmd string) (*CommandResult, error) {
	start := time.Now()

	// Check if connected
	if !s.connected || s.client == nil {
		return nil, fmt.Errorf("not connected to server")
	}

	// Create new session
	session, err := s.client.NewSession()
	if err != nil {
		return nil, fmt.Errorf("failed to create session: %w", err)
	}
	defer session.Close()

	// Set environment variables for UTF-8 locale support
	// This helps with proper character encoding for TUI apps
	// Ignore errors - not all SSH servers support Setenv
	_ = session.Setenv("LANG", "en_US.UTF-8")
	_ = session.Setenv("LC_ALL", "en_US.UTF-8")

	// Set up pipes for stdout and stderr
	var stdoutBuf, stderrBuf bytes.Buffer
	session.Stdout = &stdoutBuf
	session.Stderr = &stderrBuf

	// Execute command
	err = session.Run(cmd)

	// Determine exit code
	exitCode := 0
	if err != nil {
		if exitErr, ok := err.(*ssh.ExitError); ok {
			exitCode = exitErr.ExitStatus()
			err = nil // Command executed but exited with non-zero code
		} else {
			// Connection error or other issue
			return nil, fmt.Errorf("command execution failed: %w", err)
		}
	}

	return &CommandResult{
		Stdout:   stdoutBuf.String(),
		Stderr:   stderrBuf.String(),
		ExitCode: exitCode,
		Duration: time.Since(start),
		Error:    err,
	}, nil
}

// expandPath expands ~ to the user's home directory
func expandPath(path string) (string, error) {
	if path == "" {
		return path, nil
	}
	if len(path) >= 2 && path[:2] == "~/" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("failed to get home directory: %v", err)
		}
		return filepath.Join(home, path[2:]), nil
	}
	return path, nil
}

// Loads private key from file path
// Handles passphrase-protected keys
func loadPrivateKey(keyPath string) (ssh.Signer, error) {
	// Expand ~ to home directory
	expandedPath, err := expandPath(keyPath)
	if err != nil {
		return nil, err
	}

	keyData, err := os.ReadFile(expandedPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read private key: %v", err)
	}

	signer, err := ssh.ParsePrivateKey(keyData)
	if err != nil {
		return nil, fmt.Errorf("failed to parse private key: %v", err)
	}
	return signer, nil
}

// Returns default SSH key paths
func getDefaultKeyPaths() []string {
	home, err := os.UserHomeDir()
	if err != nil {
		return []string{}
	}
	return []string{
		filepath.Join(home, ".ssh", "id_rsa"),
		filepath.Join(home, ".ssh", "id_ed25519"),
		filepath.Join(home, ".ssh", "id_ecdsa"),
	}
}

// getSSHConfigKeyPaths reads ~/.ssh/config and returns identity files for a given host
func getSSHConfigKeyPaths(host string) []string {
	home, err := os.UserHomeDir()
	if err != nil {
		return []string{}
	}

	configPath := filepath.Join(home, ".ssh", "config")
	file, err := os.Open(configPath)
	if err != nil {
		return []string{}
	}
	defer file.Close()

	var keyPaths []string
	var currentHost string
	var hostMatches bool

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())

		// Skip empty lines and comments
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		// Parse Host directive
		if strings.HasPrefix(line, "Host ") {
			currentHost = strings.TrimPrefix(line, "Host ")
			hostMatches = matchSSHConfigPattern(currentHost, host)
			continue
		}

		// If we're in a matching host block, look for IdentityFile
		if hostMatches && strings.HasPrefix(line, "IdentityFile ") {
			keyPath := strings.TrimPrefix(line, "IdentityFile ")
			// Expand ~ to home directory
			if strings.HasPrefix(keyPath, "~") {
				keyPath = filepath.Join(home, keyPath[1:])
			}
			keyPaths = append(keyPaths, keyPath)
		}
	}

	return keyPaths
}

// matchSSHConfigPattern checks if a host matches an SSH config pattern
// Supports wildcards like "192.168.1.*" or "*.example.com"
func matchSSHConfigPattern(pattern, host string) bool {
	// Exact match
	if pattern == host {
		return true
	}

	// Wildcard matching
	if strings.Contains(pattern, "*") {
		// Simple wildcard matching (not full glob support)
		pattern = strings.ReplaceAll(pattern, "*", ".*")
		// Use simple string prefix/suffix matching for common patterns
		if strings.HasPrefix(pattern, ".*") {
			// Pattern like "*.example.com"
			suffix := pattern[2:] // Remove ".*"
			if strings.HasSuffix(host, suffix) {
				return true
			}
		} else if strings.HasSuffix(pattern, ".*") {
			// Pattern like "192.168.1.*"
			prefix := pattern[:len(pattern)-2] // Remove ".*"
			if strings.HasPrefix(host, prefix) {
				return true
			}
		}
	}

	return false
}

// getAgentMethods tries to connect to SSH agent and get auth methods
// This mimics what the standard ssh command does
func getAgentMethods() []ssh.AuthMethod {
	sshAgentAddr := os.Getenv("SSH_AUTH_SOCK")
	if sshAgentAddr == "" {
		return []ssh.AuthMethod{}
	}

	conn, err := net.Dial("unix", sshAgentAddr)
	if err != nil {
		return []ssh.AuthMethod{}
	}
	defer conn.Close()

	agentClient := agent.NewClient(conn)
	signers, err := agentClient.Signers()
	if err != nil || len(signers) == 0 {
		return []ssh.AuthMethod{}
	}

	return []ssh.AuthMethod{ssh.PublicKeys(signers...)}
}

// Create auth method chain (try agent, then keys from SSH config, then default keys)
func createAuthMethods(keyPath string, host string) ([]ssh.AuthMethod, error) {
	var authMethods []ssh.AuthMethod

	// Try SSH agent first (this is what standard ssh command does)
	agentMethods := getAgentMethods()
	authMethods = append(authMethods, agentMethods...)

	// Try specified key path
	if keyPath != "" {
		signer, err := loadPrivateKey(keyPath)
		if err == nil {
			authMethods = append(authMethods, ssh.PublicKeys(signer))
		}
	}

	// Try keys from SSH config (matches against host)
	for _, path := range getSSHConfigKeyPaths(host) {
		signer, err := loadPrivateKey(path)
		if err == nil {
			authMethods = append(authMethods, ssh.PublicKeys(signer))
		}
	}

	// Try default key paths
	for _, path := range getDefaultKeyPaths() {
		signer, err := loadPrivateKey(path)
		if err == nil {
			authMethods = append(authMethods, ssh.PublicKeys(signer))
		}
	}

	// TODO: Add password auth method if needed

	if len(authMethods) == 0 {
		return nil, fmt.Errorf("no valid authentication methods found")
	}
	return authMethods, nil
}
