package main

import (
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/SimonLariz/beacon/internal/model"
	"github.com/SimonLariz/beacon/internal/ssh"
	tea "github.com/charmbracelet/bubbletea"
)

// ViewMode represents the current view mode
type ViewMode int

const (
	ModeNormal ViewMode = iota
	ModeAddForm
	ModeCommandInput
	ModeCommandExecuting
)

// AddConnectionForm holds the input fields for adding a new connection
type AddConnectionForm struct {
	fields []string
	values map[string]string
	active int
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
		active: 0,
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
	AppState      *model.AppState
	width         int
	height        int
	mode          ViewMode
	form          *AddConnectionForm
	commandInput  string
	historyIndex  int
	statusMessage string
	statusTimeout time.Time
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
				Executions: make([]*model.CommandExecution, 0),
			})
		}
		// Load command history
		appState.CommandHistory.Commands = config.CommandHistory
	}

	return &TUIModel{
		AppState:     appState,
		mode:         ModeNormal,
		form:         NewAddConnectionForm(),
		historyIndex: -1,
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
	case commandResultMsg:
		// Handle command result
		if msg.index >= 0 && msg.index < len(m.AppState.Connections) {
			cs := m.AppState.Connections[msg.index]
			if msg.err != nil {
				m.setStatus(fmt.Sprintf("Error: %v", msg.err), 5*time.Second)
				cs.Status = model.StatusError
				cs.LastError = msg.err
			} else {
				cs.Executions = append(cs.Executions, msg.execution)
				exitMsg := "completed"
				if msg.execution.ExitCode != 0 {
					exitMsg = fmt.Sprintf("exit %d", msg.execution.ExitCode)
				}
				m.setStatus(fmt.Sprintf("Command %s", exitMsg), 3*time.Second)
			}
			cs.CurrentExec = nil
		}
		m.mode = ModeNormal
	case tea.KeyMsg:
		if m.mode == ModeCommandInput {
			return m.handleCommandInput(msg)
		}
		return m.handleKeyPress(msg)
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
	}
	return m, nil
}

// View renders the TUI
func (m *TUIModel) View() string {
	if m.mode == ModeAddForm {
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

	result += "\n[a]dd [d]elete [c]onnect [:]command [q]uit\n"

	// Render command output if connection is selected
	if m.AppState.GetSelected() != nil {
		result += m.renderCommandOutput()
	}

	// Render command input bar if active
	if m.mode == ModeCommandInput {
		result += m.renderCommandInput()
	}

	// Status message
	if time.Now().Before(m.statusTimeout) {
		result += fmt.Sprintf("\n%s\n", m.statusMessage)
	}

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
	if m.mode == ModeAddForm {
		switch msg.String() {
		case "esc":
			m.mode = ModeNormal
			m.form = NewAddConnectionForm()
		case "tab":
			m.form.NextField()
		case "shift+tab":
			m.form.PrevField()
		case "enter":
			if m.form.IsValid() {
				// Create and add the connection
				port := 22
				if _, err := fmt.Sscanf(m.form.values["port"], "%d", &port); err != nil {
					// If invalid port, use default
					port = 22
				}

				conn := model.NewConnection(
					m.form.values["alias"],
					m.form.values["host"],
					m.form.values["user"],
					port,
				)
				conn.KeyPath = m.form.values["key_path"]
				m.AppState.AddConnection(conn)
				if err := model.SaveConfig(m.AppState.Config); err != nil {
					log.Printf("Warning: failed to save config: %v", err)
				}

				m.mode = ModeNormal
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
		m.mode = ModeAddForm
		m.form = NewAddConnectionForm()
	case "d":
		if len(m.AppState.Connections) > 0 {
			if err := m.AppState.DeleteConnection(m.AppState.SelectedIndex); err != nil {
				log.Printf("Warning: failed to delete connection: %v", err)
			}
			if err := model.SaveConfig(m.AppState.Config); err != nil {
				log.Printf("Warning: failed to save config: %v", err)
			}
		}
	case "c":
		if m.mode == ModeNormal && m.AppState.GetSelected() != nil {
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
	case ":":
		selected := m.AppState.GetSelected()
		if selected != nil && selected.Status == model.StatusConnected {
			m.mode = ModeCommandInput
			m.commandInput = ""
			m.historyIndex = -1
		} else {
			m.setStatus("No connected server selected", 2*time.Second)
		}
	case "pgup":
		m.AppState.ScrollOutputUp(10)
	case "pgdown":
		m.AppState.ScrollOutputDown(10)
	}
	return m, nil
}

// setStatus sets a temporary status message with timeout
func (m *TUIModel) setStatus(msg string, duration time.Duration) {
	m.statusMessage = msg
	m.statusTimeout = time.Now().Add(duration)
}

// handleCommandInput processes key input when in command input mode
func (m *TUIModel) handleCommandInput(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.mode = ModeNormal
		m.commandInput = ""
		m.historyIndex = -1
		return m, nil

	case "enter":
		if m.commandInput == "" {
			m.mode = ModeNormal
			return m, nil
		}

		cmd := m.commandInput
		m.AppState.AddToHistory(cmd)
		m.commandInput = ""
		m.historyIndex = -1
		m.mode = ModeCommandExecuting

		return m, m.executeCommand(cmd)

	case "up":
		if m.historyIndex < m.AppState.HistorySize()-1 {
			m.historyIndex++
			m.commandInput = m.AppState.GetHistoryItem(m.historyIndex)
		}
		return m, nil

	case "down":
		if m.historyIndex > 0 {
			m.historyIndex--
			m.commandInput = m.AppState.GetHistoryItem(m.historyIndex)
		} else if m.historyIndex == 0 {
			m.historyIndex = -1
			m.commandInput = ""
		}
		return m, nil

	case "backspace":
		if len(m.commandInput) > 0 {
			m.commandInput = m.commandInput[:len(m.commandInput)-1]
		}
		return m, nil

	default:
		if len(msg.String()) == 1 {
			m.commandInput += msg.String()
		}
	}
	return m, nil
}

// executeCommand initiates async command execution
func (m *TUIModel) executeCommand(cmd string) tea.Cmd {
	selected := m.AppState.GetSelected()
	if selected == nil || selected.Client == nil {
		return func() tea.Msg {
			return commandResultMsg{
				index: m.AppState.SelectedIndex,
				err:   fmt.Errorf("no active connection"),
			}
		}
	}

	index := m.AppState.SelectedIndex

	// Mark command as executing
	selected.CurrentExec = &model.CommandExecution{
		Command:   cmd,
		Timestamp: time.Now(),
		Completed: false,
	}

	return func() tea.Msg {
		result, err := selected.Client.ExecuteCommand(cmd)

		if err != nil {
			return commandResultMsg{
				index: index,
				err:   err,
			}
		}

		execution := &model.CommandExecution{
			Command:   cmd,
			Timestamp: time.Now(),
			ExitCode:  result.ExitCode,
			Stdout:    result.Stdout,
			Stderr:    result.Stderr,
			Duration:  result.Duration,
			Completed: true,
		}

		return commandResultMsg{
			index:     index,
			execution: execution,
		}
	}
}

// renderCommandOutput renders the command output section
func (m *TUIModel) renderCommandOutput() string {
	selected := m.AppState.GetSelected()
	if selected == nil {
		return ""
	}

	var result string
	result += fmt.Sprintf("\n━━━ Command Output (%s) ━━━\n", selected.Connection.Alias)

	// Show currently executing command
	if selected.CurrentExec != nil {
		result += fmt.Sprintf("\n$ %s\n", selected.CurrentExec.Command)
		result += "[Executing...]\n"
	}

	// Show execution history
	execCount := len(selected.Executions)
	if execCount == 0 && selected.CurrentExec == nil {
		result += "\n(No commands executed yet)\n"
		return result
	}

	// Build output lines (reverse chronological)
	var allLines []string
	for i := execCount - 1; i >= 0; i-- {
		exec := selected.Executions[i]

		timestamp := exec.Timestamp.Format("15:04:05")
		allLines = append(allLines, "")
		allLines = append(allLines, fmt.Sprintf("$ %s  [%s]", exec.Command, timestamp))

		if exec.Stdout != "" {
			lines := strings.Split(strings.TrimRight(exec.Stdout, "\n"), "\n")
			allLines = append(allLines, lines...)
		}

		if exec.Stderr != "" {
			allLines = append(allLines, "--- stderr ---")
			lines := strings.Split(strings.TrimRight(exec.Stderr, "\n"), "\n")
			allLines = append(allLines, lines...)
		}

		if exec.ExitCode != 0 {
			allLines = append(allLines, fmt.Sprintf("[Exit code: %d]", exec.ExitCode))
		}
	}

	// Apply scrolling and viewport
	outputHeight := m.height - 15
	if outputHeight < 5 {
		outputHeight = 5
	}

	totalLines := len(allLines)
	startLine := m.AppState.OutputScrollOffset
	endLine := startLine + outputHeight

	if startLine > totalLines {
		startLine = totalLines
	}
	if endLine > totalLines {
		endLine = totalLines
	}
	if startLine < 0 {
		startLine = 0
	}

	for i := startLine; i < endLine && i < len(allLines); i++ {
		result += allLines[i] + "\n"
	}

	if totalLines > outputHeight {
		result += fmt.Sprintf("\n[Lines %d-%d of %d] [PgUp/PgDown to scroll]\n",
			startLine+1, endLine, totalLines)
	}

	return result
}

// renderCommandInput renders the command input bar
func (m *TUIModel) renderCommandInput() string {
	if m.mode != ModeCommandInput {
		return ""
	}

	var result string
	result += "\n━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━\n"
	result += fmt.Sprintf(":%s█\n", m.commandInput)
	result += "[↑↓ history] [Enter] execute [Esc] cancel\n"
	return result
}

type connectResultMsg struct {
	index   int // which connection
	success bool
	err     error
}

type commandResultMsg struct {
	index     int
	execution *model.CommandExecution
	err       error
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
