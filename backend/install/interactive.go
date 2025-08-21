package install

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/phishingclub/phishingclub/config"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

var (
	// Attractive UI styles with a cohesive color scheme
	titleStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#FFFFFF")).
			Background(lipgloss.Color("#0B3D91")). // NASA blue
			Bold(true).
			Padding(2, 2)

	inputStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#0B3D91"))

	focusedStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#FFFFFF")).
			Background(lipgloss.Color("#1E88E5")). // Material blue
			Bold(true)

	blurredStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#0B3D91"))

	cursorStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#FF9E43")) // Amber accent

	helpStyle = lipgloss.NewStyle().
			Italic(true).
			Foreground(lipgloss.Color("#607D8B")) // Blue grey

	errorStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#F44336")). // Red
			Bold(true)

	buttonStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#FFFFFF")).
			Background(lipgloss.Color("#43A047")). // Green
			Bold(true).
			Padding(0, 3)
)

// InputWithHelp extends textinput.Model to include a help text
type InputWithHelp struct {
	textinput.Model
	HelpText string
}

// ConfigModel is the model for the tea app
type ConfigModel struct {
	inputs        []InputWithHelp
	focusIndex    int
	err           error
	shouldInstall bool
	config        *config.Config
}

// Init initializes the model
func (m ConfigModel) Init() tea.Cmd {
	return textinput.Blink
}

// Update handles updates
func (m ConfigModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "esc":
			return m, tea.Quit

		// Navigate between inputs with tab/shift+tab
		case "tab", "shift+tab", "enter", "up", "down":
			s := msg.String()

			// Did the user press enter while the submit button was focused?
			if s == "enter" && m.focusIndex == len(m.inputs) {
				// Validate config and set shouldInstall
				var err error
				m.shouldInstall = true

				// Apply the input values to configuration
				err = m.applyConfig()
				if err != nil {
					m.err = err
					m.shouldInstall = false
					return m, nil
				}

				return m, tea.Quit
			}

			// Cycle indexes
			if s == "up" || s == "shift+tab" {
				m.focusIndex--
			} else {
				m.focusIndex++
			}

			if m.focusIndex > len(m.inputs) {
				m.focusIndex = 0
			} else if m.focusIndex < 0 {
				m.focusIndex = len(m.inputs)
			}

			cmds := make([]tea.Cmd, len(m.inputs))
			for i := 0; i < len(m.inputs); i++ {
				if i == m.focusIndex {
					// Set focused state
					cmds[i] = m.inputs[i].Focus()
					m.inputs[i].PromptStyle = focusedStyle
					m.inputs[i].TextStyle = focusedStyle
				} else {
					// Remove focused state
					m.inputs[i].Blur()
					m.inputs[i].PromptStyle = blurredStyle
					m.inputs[i].TextStyle = blurredStyle
				}
			}

			return m, tea.Batch(cmds...)
		}
	}

	// Handle character input
	cmd := m.updateInputs(msg)

	return m, cmd
}

func (m *ConfigModel) updateInputs(msg tea.Msg) tea.Cmd {
	var cmds = make([]tea.Cmd, len(m.inputs))

	// Only text inputs with Focus() set will respond
	for i := range m.inputs {
		m.inputs[i].Model, cmds[i] = m.inputs[i].Model.Update(msg)
	}

	return tea.Batch(cmds...)
}

func (m ConfigModel) View() string {
	var b strings.Builder

	b.WriteString("\n")
	b.WriteString(titleStyle.Render(" 🐟 Phishing Club Interactive Installer "))
	b.WriteString("\n\n")

	if m.err != nil {
		b.WriteString(errorStyle.Render(fmt.Sprintf("Error: %s\n\n", m.err.Error())))
	}

	for i, input := range m.inputs {
		b.WriteString(input.View())
		// Display help text for the focused input
		if i == m.focusIndex {
			b.WriteString("\n  " + helpStyle.Render(input.HelpText))
		}
		b.WriteString("\n")
	}

	button := blurredStyle.Copy()
	if m.focusIndex == len(m.inputs) {
		button = buttonStyle
	}
	fmt.Fprintf(&b, "\n%s\n", button.Render(" Install "))

	return b.String()
}

// InitialModel creates the initial model for the tea app
func InitialModel(currentConfig *config.Config) ConfigModel {
	// Setup text inputs
	var inputs []InputWithHelp
	var prompts = []struct {
		prompt       string
		defaultValue string
		placeholder  string
		description  string
	}{
		{"HTTP port", strconv.Itoa(config.DefaultProductionHTTPPhishingPort), "80", "Port for HTTP phishing server"},
		{"HTTPS port", strconv.Itoa(config.DefaultProductionHTTPSPhishingPort), "443", "Port for HTTPS phishing server"},
		{"Admin port", strconv.Itoa(config.DefaultProductionAdministrationPort), "0 (random port)", "Admin server port - can not be the same as the ports used by the phishing server"},
		{"Admin host", config.DefaultAdminHost, "localhost", "Admin server hostname - used for TLS certificate"},
		{"Use Auto TLS", config.DefaultAdminAutoTLSString, "true/false", "Use automated TLS for the admin service"},
		{"ACME email", config.DefaultACMEEmail, config.DefaultACMEEmail, "Email for Let's Encrypt notifications"},
	}

	for i, p := range prompts {
		t := textinput.New()
		t.Cursor.Style = cursorStyle
		t.CharLimit = 64

		// Configure each input
		t.Placeholder = p.placeholder
		t.PromptStyle = blurredStyle
		t.TextStyle = blurredStyle

		// The first input is focused
		if i == 0 {
			t.PromptStyle = focusedStyle
			t.TextStyle = focusedStyle
			t.Focus()
		}

		// Set the prompt with the default value displayed
		t.Prompt = fmt.Sprintf("%s [%s]: ", p.prompt, p.defaultValue)

		// Create our custom input with help text
		input := InputWithHelp{
			Model:    t,
			HelpText: p.description,
		}

		inputs = append(inputs, input)
	}

	return ConfigModel{
		inputs: inputs,
		config: currentConfig,
	}
}

// applyConfig takes the input values and applies them to the config
func (m *ConfigModel) applyConfig() error {
	// Get the input values or use defaults if empty
	httpPort := getValueOrDefault(m.inputs[0].Value(), strconv.Itoa(config.DefaultProductionHTTPPhishingPort))
	httpsPort := getValueOrDefault(m.inputs[1].Value(), strconv.Itoa(config.DefaultProductionHTTPSPhishingPort))
	adminPort := getValueOrDefault(m.inputs[2].Value(), strconv.Itoa(config.DefaultProductionAdministrationPort))
	adminHost := getValueOrDefault(m.inputs[3].Value(), config.DefaultAdminHost)
	autoTLS := getValueOrDefault(m.inputs[4].Value(), config.DefaultAdminAutoTLSString)
	acmeEmail := getValueOrDefault(m.inputs[5].Value(), config.DefaultACMEEmail)

	// Convert ports to integers
	httpPortInt, err := strconv.Atoi(httpPort)
	if err != nil {
		return fmt.Errorf("invalid HTTP port: %w", err)
	}

	httpsPortInt, err := strconv.Atoi(httpsPort)
	if err != nil {
		return fmt.Errorf("invalid HTTPS port: %w", err)
	}

	adminPortInt, err := strconv.Atoi(adminPort)
	if err != nil {
		return fmt.Errorf("invalid admin port: %w", err)
	}

	// Validate port values
	if httpPortInt <= 0 || httpPortInt > 65535 {
		return fmt.Errorf("HTTP port must be between 1 and 65535")
	}
	if httpsPortInt <= 0 || httpsPortInt > 65535 {
		return fmt.Errorf("HTTPS port must be between 1 and 65535")
	}
	if adminPortInt < 0 || adminPortInt > 65535 {
		return fmt.Errorf("admin port must be between 0 and 65535")
	}

	// Check for port conflicts
	if httpPortInt == httpsPortInt {
		return fmt.Errorf("HTTP and HTTPS ports cannot be the same")
	}
	if adminPortInt != 0 && (adminPortInt == httpPortInt || adminPortInt == httpsPortInt) {
		return fmt.Errorf("admin port cannot be the same as HTTP or HTTPS ports")
	}

	// Convert autoTLS to boolean
	autoTLSBool := false
	if strings.ToLower(autoTLS) == config.DefaultAdminAutoTLSString {
		autoTLSBool = true
	}

	// Set values in config
	err = m.config.SetPhishingHTTPNetAddress(fmt.Sprintf("0.0.0.0:%d", httpPortInt))
	if err != nil {
		return fmt.Errorf("failed to set HTTP address: %w", err)
	}

	err = m.config.SetPhishingHTTPSNetAddress(fmt.Sprintf("0.0.0.0:%d", httpsPortInt))
	if err != nil {
		return fmt.Errorf("failed to set HTTPS address: %w", err)
	}

	err = m.config.SetAdminNetAddress(fmt.Sprintf("0.0.0.0:%d", adminPortInt))
	if err != nil {
		return fmt.Errorf("failed to set admin address: %w", err)
	}

	m.config.SetTLSHost(adminHost)
	m.config.SetTLSAuto(autoTLSBool)
	m.config.SetACMEEmail(acmeEmail)

	return nil
}

// getValueOrDefault returns the value or the default if value is empty
func getValueOrDefault(value, defaultValue string) string {
	if value == "" {
		return defaultValue
	}
	return value
}

func RunInteractiveInstall() error {
	// First check if we're running as root
	if os.Geteuid() != 0 {
		return fmt.Errorf("installation must be run as root")
	}

	// Create installation directories first
	if err := createDirectories(); err != nil {
		return fmt.Errorf("failed to create install directories: %w", err)
	}

	// Get default configuration
	conf := config.NewProductionDefaultConfig()

	// Run the tea program
	p := tea.NewProgram(InitialModel(conf))
	model, err := p.Run()
	if err != nil {
		return fmt.Errorf("error running interactive installer: %w", err)
	}

	// Get the final model
	finalModel := model.(ConfigModel)
	if !finalModel.shouldInstall {
		return fmt.Errorf("installation cancelled")
	}

	// Save the config to the installation directory
	configPath := filepath.Join(installDir, "config.json")
	err = finalModel.config.WriteToFile(configPath)
	if err != nil {
		return fmt.Errorf("failed to save configuration to %s: %w", configPath, err)
	}
	if err := os.Chmod(configPath, 0600); err != nil {
		return fmt.Errorf("failed to set config file permissions: %w", err)
	}

	fmt.Printf("Configuration saved to %s\n", configPath)

	// Now run the actual installation
	err = InstallWithConfig(finalModel.config)
	if err != nil {
		return err
	}
	return nil
}

// InstallWithConfig handles the installation using the provided configuration
func InstallWithConfig(conf *config.Config) error {
	steps := []struct {
		name string
		fn   func() error
	}{
		{"check sqlite dependency", checkSQLiteDependency},
		{"create user and group", createUserAndGroup},
		{"create directories", createDirectories},
		{"install binary", installBinary},
		{"install systemd service", installSystemdService},
		{"set permissions", setPermissions},
		{"enable service", enableService},
		{"start service", startService},
		{"print info", outputCredentialsAndInfo},
	}

	for _, step := range steps {
		fmt.Printf("Step: %s\n", step.name)
		if err := step.fn(); err != nil {
			return fmt.Errorf("%s: %w", step.name, err)
		}
	}

	fmt.Println()
	fmt.Println("Installer completed successfully! 🐟")
	fmt.Println()
	fmt.Println("# Tips")
	fmt.Println("'journalctl -u phishingclub.service -f' to see logs")
	fmt.Println("'systemctl status phishingclub' to check status of the service")
	fmt.Println("")

	return nil
}
