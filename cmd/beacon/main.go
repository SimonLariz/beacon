package main

import (
	"fmt"
	"log"
	"time"

	"github.com/SimonLariz/beacon/internal/model"
	"github.com/SimonLariz/beacon/internal/ssh"
	tea "github.com/charmbracelet/bubbletea"
)

// AddConnectionForm holds the input fields for adding a new connection
type AddConnectionForm struct {
	fields   []string
	values   map[string]string
	active   int
	complete bool
}

// NewAddConnectionForm creates a new AddConnectionForm
func NewAddConnectionForm() *AddConnectionForm {
	return &AddConnectionForm{
		fields: []string{"alias", "host", "user", "port", "key_path"},
		values: map[string]string{
			"alias":    "",
			"host":     "",
			"user":     "",
			"port":     "22",
			"key_path": "",
		},
		active:   0,
		complete: false,
	}
}

// GetActiveField returns the name of the currently active field
func (f *AddConnectionForm) GetActiveField() string {
	if f.active < len(f.fields) {
		return f.fields[f.active]
	}
	return ""
}

// NextField moves to the next field
func (f *AddConnectionForm) NextField() {
	f.active = (f.active + 1) % len(f.fields)
}

// PrevField moves to the previous field
func (f *AddConnectionForm) PrevField() {
	f.active--
	if f.active < 0 {
		f.active = len(f.fields) - 1
	}
}

// AddChar adds a character to the active field
func (f *AddConnectionForm) AddChar(ch string) {
	f.values[f.GetActiveField()] += ch
}

// RemoveChar removes the last character from the active field
func (f *AddConnectionForm) RemoveChar() {
	field := f.GetActiveField()
	if len(f.values[field]) > 0 {
		f.values[field] = f.values[field][:len(f.values[field])-1]
	}
}

// IsValid checks if all required fields are filled
func (f *AddConnectionForm) IsValid() bool {
	return f.values["alias"] != "" && f.values["host"] != "" && f.values["user"] != ""
}

// TUIModel represents the state of the TUI application
type TUIModel struct {
	AppState *model.AppState
	width    int
	height   int
	addMode  bool
	form     *AddConnectionForm
}

func NewTUIModel() *TUIModel {
	appState := model.NewAppState()

	// Load existing configuration
	config, err := model.LoadConfig()
	if err != nil {
		log.Printf("Warning: Failed to load config: %v", err)
	} else if config != nil && len(config.Connections) > 0 {
		appState.Config = config
		for _, conn := range config.Connections {
			appState.Connections = append(appState.Connections, &model.ConnectionState{
				Connection: conn,
				Status:     model.StatusDisconnected,
				Output:     make([]string, 0),
			})
		}
	}

	return &TUIModel{
		AppState: appState,
		addMode:  false,
		form:     NewAddConnectionForm(),
	}
}

// Init initializes the model
func (m *TUIModel) Init() tea.Cmd {
	return nil
}

// Update handles user input
func (m *TUIModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case connectResultMsg:
		// Handle connection result
		if msg.index >= 0 && msg.index < len(m.AppState.Connections) {
			cs := m.AppState.Connections[msg.index]
			if msg.success {
				cs.Status = model.StatusConnected
				cs.LastError = nil
				cs.LastActive = time.Now()
			} else {
				cs.Status = model.StatusError
				cs.LastError = msg.err
			}
		}
	case tea.KeyMsg:
		return m.handleKeyPress(msg)
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
	}
	return m, nil
}

// View renders the TUI
func (m *TUIModel) View() string {
	if m.addMode {
		return m.renderAddForm()
	}

	if len(m.AppState.Connections) == 0 {
		return "No connections. Press 'a' to add one, or 'q' to quit.\n"
	}

	var result string
	result += "=== BEACON - SSH Session Manager ===\n\n"
	result += "Connections:\n"

	for i, cs := range m.AppState.Connections {
		marker := "  "
		if i == m.AppState.SelectedIndex {
			marker = "> "
		}

		// Color-code status (use lipgloss later)
		status := cs.StatusString()

		result += fmt.Sprintf("%s[%d] %s @ %s:%d (user: %s) - %s\n",
			marker,
			i,
			cs.Connection.Alias,
			cs.Connection.Host,
			cs.Connection.Port,
			cs.Connection.User,
			status,
		)

		// Show error if present
		if cs.LastError != nil {
			result += fmt.Sprintf("     Error: %v\n", cs.LastError)
		}
		result += "\n"
	}

	result += "\n[a]dd [d]elete [c]onnect [q]uit\n"
	return result
}

// renderAddForm renders the add connection form
func (m *TUIModel) renderAddForm() string {
	var result string
	result += "=== ADD NEW CONNECTION ===\n\n"

	// Render each field
	fields := []string{"alias", "host", "user", "port", "key_path"}
	labels := []string{"Alias (nickname)", "Host (IP or hostname)", "User (SSH username)", "Port (default 22)", "Key Path (optional)"}

	for i, field := range fields {
		prefix := "  "
		if i == m.form.active {
			prefix = "> " // Highlight active field
		}
		result += fmt.Sprintf("%s%s: %s\n", prefix, labels[i], m.form.values[field])
	}

	result += "\n[Tab]next [Shift+Tab]prev [Enter]save [Esc]cancel\n"
	return result
}

func (m *TUIModel) handleKeyPress(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// If in add mode, handle form input
	if m.addMode {
		switch msg.String() {
		case "esc":
			m.addMode = false
			m.form = NewAddConnectionForm()
		case "tab":
			m.form.NextField()
		case "shift+tab":
			m.form.PrevField()
		case "enter":
			if m.form.IsValid() {
				// Create and add the connection
				port := 22
				fmt.Sscanf(m.form.values["port"], "%d", &port)

				conn := model.NewConnection(
					m.form.values["alias"],
					m.form.values["host"],
					m.form.values["user"],
					port,
				)
				conn.KeyPath = m.form.values["key_path"]
				m.AppState.AddConnection(conn)
				model.SaveConfig(m.AppState.Config)

				m.addMode = false
				m.form = NewAddConnectionForm()
			}
		case "backspace":
			m.form.RemoveChar()
		default:
			// Add character to active field
			if len(msg.String()) == 1 {
				m.form.AddChar(msg.String())
			}
		}
		return m, nil
	}

	// Normal mode key handling
	switch msg.String() {
	case "q", "ctrl+c":
		return m, tea.Quit
	case "up":
		m.AppState.SelectPrevious()
	case "down":
		m.AppState.SelectNext()
	case "a":
		m.addMode = true
		m.form = NewAddConnectionForm()
	case "d":
		if len(m.AppState.Connections) > 0 {
			m.AppState.DeleteConnection(m.AppState.SelectedIndex)
			model.SaveConfig(m.AppState.Config)
		}
	case "c":
		if !m.addMode && m.AppState.GetSelected() != nil {
			cs := m.AppState.GetSelected()
			// Don't connect if already connecting/connected
			if cs.Status == model.StatusConnecting || cs.Status == model.StatusConnected {
				return m, nil
			}
			// Mark as connecting
			cs.Status = model.StatusConnecting
			// Start async connection
			return m, m.connectToSelectedServer()
		}
	}
	return m, nil
}

type connectResultMsg struct {
	index   int // which connection
	success bool
	err     error
}

// connectToSelectedServer initiates SSH connection asynchronously
// Returns a bubbletea.Cmd that will send a message when done
func (m *TUIModel) connectToSelectedServer() tea.Cmd {
	// Get selected connection
	selected := m.AppState.GetSelected()
	if selected == nil {
		return nil
	}
	// Call ssh.Connect in a goroutine
	return func() tea.Msg {
		conn := selected.Connection
		sshClient, err := ssh.Connect(conn.Host, conn.Port, conn.User, conn.KeyPath)
		if err != nil {
			return connectResultMsg{index: m.AppState.SelectedIndex, success: false, err: err}
		}
		// Store the SSH client in the connection state
		selected.Client = sshClient
		return connectResultMsg{index: m.AppState.SelectedIndex, success: true, err: nil}
	}
}

func main() {
	model := NewTUIModel()
	p := tea.NewProgram(model)

	if _, err := p.Run(); err != nil {
		log.Fatalf("Error running program: %v", err)
	}
}
