package ssh

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"golang.org/x/crypto/ssh"
)

type SSHClientWrapper struct {
	client     *ssh.Client
	config     *ssh.ClientConfig
	host       string
	connected  bool
	LastActive time.Time
}

// Connect establishes SSH connection using key-based authentication
// Tries KeyPath first, then falls back to default keys
func Connect(host string, port int, user string, keyPath string) (*SSHClientWrapper, error) {
	address := fmt.Sprintf("%s:%d", host, port)
	authMethods, err := createAuthMethods(keyPath)
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
		// Handle passphrase-protected keys if needed
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

// Create auth method chain (try keys, fallback to password)
func createAuthMethods(keyPath string) ([]ssh.AuthMethod, error) {
	var authMethods []ssh.AuthMethod

	// Try specified key path
	if keyPath != "" {
		signer, err := loadPrivateKey(keyPath)
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
