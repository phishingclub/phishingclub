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
	titleStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#FFFFFF")).
			Background(lipgloss.Color("#0B3D91")).
			Bold(true).
			Padding(2, 2)

	inputStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#0B3D91"))

	focusedStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#FFFFFF")).
			Background(lipgloss.Color("#1E88E5")).
			Bold(true)

	blurredStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#0B3D91"))

	cursorStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#FF9E43"))

	helpStyle = lipgloss.NewStyle().
			Italic(true).
			Foreground(lipgloss.Color("#607D8B"))

	errorStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#F44336")).
			Bold(true)

	buttonStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#FFFFFF")).
			Background(lipgloss.Color("#43A047")).
			Bold(true).
			Padding(0, 3)

	sectionStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#FFFFFF")).
			Background(lipgloss.Color("#6A1B9A")).
			Bold(true).
			Padding(0, 1)

	modeStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#FFFFFF")).
			Background(lipgloss.Color("#FF5722")).
			Bold(true).
			Padding(0, 1)
)

type InstallMode int

const (
	BasicMode InstallMode = iota
	AdvancedMode
)

// inputWithHelp extends textinput.Model to include help text
type InputWithHelp struct {
	textinput.Model
	HelpText string
}

// configModel is the model for the tea app
type ConfigModel struct {
	inputs        []InputWithHelp
	focusIndex    int
	err           error
	shouldInstall bool
	config        *config.Config
	currentMode   InstallMode
}

// init initializes the model
func (m ConfigModel) Init() tea.Cmd {
	return textinput.Blink
}

// update handles updates
func (m ConfigModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "esc":
			return m, tea.Quit

		// mode switching
		case "f1":
			if m.currentMode != BasicMode {
				m.currentMode = BasicMode
				m.focusIndex = 0
				m.inputs = m.createBasicInputs()
				return m, nil
			}
		case "f2":
			if m.currentMode != AdvancedMode {
				m.currentMode = AdvancedMode
				m.focusIndex = 0
				m.inputs = m.createAdvancedInputs()
				return m, nil
			}

		// navigate between inputs with tab/shift+tab
		case "tab", "shift+tab", "enter", "up", "down":
			s := msg.String()

			// check if user pressed enter while submit button was focused
			if s == "enter" && m.focusIndex == len(m.inputs) {
				// validate config and set shouldInstall
				var err error
				m.shouldInstall = true

				// apply input values to configuration
				err = m.applyConfig()
				if err != nil {
					m.err = err
					m.shouldInstall = false
					return m, nil
				}

				return m, tea.Quit
			}

			// cycle indexes
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
					// set focused state
					cmds[i] = m.inputs[i].Focus()
					m.inputs[i].PromptStyle = focusedStyle
					m.inputs[i].TextStyle = focusedStyle
				} else {
					// remove focused state
					m.inputs[i].Blur()
					m.inputs[i].PromptStyle = blurredStyle
					m.inputs[i].TextStyle = blurredStyle
				}
			}

			return m, tea.Batch(cmds...)
		}
	}

	// handle character input
	cmd := m.updateInputs(msg)
	return m, cmd
}

func (m *ConfigModel) updateInputs(msg tea.Msg) tea.Cmd {
	var cmds = make([]tea.Cmd, len(m.inputs))

	// only text inputs with Focus() set will respond
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

	// show current mode and switching instructions
	modeText := "Basic Mode"
	if m.currentMode == AdvancedMode {
		modeText = "Advanced Mode"
	}
	b.WriteString(fmt.Sprintf("Mode: %s  ", modeStyle.Render(modeText)))
	b.WriteString(helpStyle.Render("(F1: Basic | F2: Advanced)"))
	b.WriteString("\n\n")

	// group inputs by section for advanced mode
	if m.currentMode == AdvancedMode {
		m.renderAdvancedSections(&b)
	} else {
		m.renderBasicInputs(&b)
	}

	button := blurredStyle.Copy()
	if m.focusIndex == len(m.inputs) {
		button = buttonStyle
	}
	fmt.Fprintf(&b, "\n%s\n", button.Render(" Install "))

	return b.String()
}

func (m ConfigModel) renderBasicInputs(b *strings.Builder) {
	for i, input := range m.inputs {
		b.WriteString(input.View())
		// display help text for the focused input
		if i == m.focusIndex {
			b.WriteString("\n  " + helpStyle.Render(input.HelpText))
		}
		b.WriteString("\n")
	}
}

func (m ConfigModel) renderAdvancedSections(b *strings.Builder) {
	sections := []struct {
		title string
		start int
		end   int
	}{
		{"Server Configuration", 0, 6},
		{"Database Configuration", 6, 8},
		{"TLS Configuration", 8, 10},
		{"Logging Configuration", 10, 12},
		{"Security Configuration", 12, len(m.inputs)},
	}

	currentSection := -1
	for i, input := range m.inputs {
		// check if we're starting a new section
		for j, section := range sections {
			if i == section.start {
				if currentSection >= 0 {
					b.WriteString("\n")
				}
				b.WriteString(sectionStyle.Render(fmt.Sprintf(" %s ", section.title)))
				b.WriteString("\n")
				currentSection = j
				break
			}
		}

		b.WriteString(input.View())
		// display help text for the focused input
		if i == m.focusIndex {
			b.WriteString("\n  " + helpStyle.Render(input.HelpText))
		}
		b.WriteString("\n")
	}
}

// createBasicInputs creates inputs for basic mode
func (m *ConfigModel) createBasicInputs() []InputWithHelp {
	var inputs []InputWithHelp
	var prompts = []struct {
		prompt       string
		defaultValue string
		placeholder  string
		description  string
	}{
		{"HTTP port", strconv.Itoa(config.DefaultProductionHTTPPhishingPort), "80", "port for HTTP phishing server"},
		{"HTTPS port", strconv.Itoa(config.DefaultProductionHTTPSPhishingPort), "443", "port for HTTPS phishing server"},
		{"Admin port", strconv.Itoa(config.DefaultProductionAdministrationPort), "0 (random port)", "admin server port - can not be the same as the ports used by the phishing server"},
		{"Admin host", config.DefaultAdminHost, "localhost", "admin server hostname - used for TLS certificate"},
		{"Use Auto TLS", config.DefaultAdminAutoTLSString, "true/false", "use automated TLS for the admin service"},
		{"ACME email", config.DefaultACMEEmail, config.DefaultACMEEmail, "email for Let's Encrypt notifications"},
	}

	for i, p := range prompts {
		inputs = append(inputs, m.createInput(i, p.prompt, p.defaultValue, p.placeholder, p.description))
	}

	return inputs
}

// createAdvancedInputs creates inputs for advanced mode
func (m *ConfigModel) createAdvancedInputs() []InputWithHelp {
	var inputs []InputWithHelp
	var prompts = []struct {
		prompt       string
		defaultValue string
		placeholder  string
		description  string
	}{
		// server configuration
		{"HTTP port", strconv.Itoa(config.DefaultProductionHTTPPhishingPort), "80", "port for HTTP phishing server"},
		{"HTTPS port", strconv.Itoa(config.DefaultProductionHTTPSPhishingPort), "443", "port for HTTPS phishing server"},
		{"Admin port", strconv.Itoa(config.DefaultProductionAdministrationPort), "0 (random port)", "admin server port - can not be the same as the ports used by the phishing server"},
		{"Admin host", config.DefaultAdminHost, "localhost", "admin server hostname - used for TLS certificate"},
		{"Use Auto TLS", config.DefaultAdminAutoTLSString, "true/false", "use automated TLS for the admin service"},
		{"ACME email", config.DefaultACMEEmail, config.DefaultACMEEmail, "email for Let's Encrypt notifications"},

		// database configuration
		{"Database engine", config.DefaultDatabase, "sqlite3/postgres", "database engine to use (sqlite3 or postgres)"},
		{"Database DSN", config.DefaultAdministrationDSN, "file:./db.sqlite3", "database connection string"},

		// tls configuration
		{"TLS cert path", "", "/path/to/cert.pem", "path to TLS certificate file (leave empty for auto TLS)"},
		{"TLS key path", "", "/path/to/key.pem", "path to TLS private key file (leave empty for auto TLS)"},

		// logging configuration
		{"Log file path", config.DefaultLogFilePath, "/var/log/phishingclub.log", "path to log file (empty for stdout)"},
		{"Error log path", config.DefaultErrLogFilePath, "/var/log/phishingclub-error.log", "path to error log file (empty for stderr)"},

		// security configuration
		{"Admin allowed IPs", "", "192.168.1.0/24,10.0.0.1", "comma-separated list of IP/CIDR ranges allowed to access admin (empty for all)"},
		{"Trusted proxies", "", "192.168.1.1,10.0.0.1", "comma-separated list of trusted proxy IPs/CIDR ranges"},
		{"Trusted IP header", config.DefaultTrustedIPHeader, "X-Real-IP", "header name to check for real client IP from trusted proxies"},
	}

	for i, p := range prompts {
		inputs = append(inputs, m.createInput(i, p.prompt, p.defaultValue, p.placeholder, p.description))
	}

	return inputs
}

// createInput creates a single input field
func (m *ConfigModel) createInput(index int, prompt, defaultValue, placeholder, description string) InputWithHelp {
	t := textinput.New()
	t.Cursor.Style = cursorStyle
	t.CharLimit = 256

	// configure each input
	t.Placeholder = placeholder
	t.PromptStyle = blurredStyle
	t.TextStyle = blurredStyle

	// the first input is focused
	if index == 0 {
		t.PromptStyle = focusedStyle
		t.TextStyle = focusedStyle
		t.Focus()
	}

	// set the prompt with the default value displayed
	t.Prompt = fmt.Sprintf("%s [%s]: ", prompt, defaultValue)

	// create custom input with help text
	return InputWithHelp{
		Model:    t,
		HelpText: description,
	}
}

// initialModel creates the initial model for the tea app
func InitialModel(currentConfig *config.Config) ConfigModel {
	model := ConfigModel{
		config:      currentConfig,
		currentMode: BasicMode,
		focusIndex:  0,
	}
	model.inputs = model.createBasicInputs()
	return model
}

// applyConfig takes the input values and applies them to the config
func (m *ConfigModel) applyConfig() error {
	if m.currentMode == BasicMode {
		return m.applyBasicConfig()
	}
	return m.applyAdvancedConfig()
}

// applyBasicConfig applies basic configuration
func (m *ConfigModel) applyBasicConfig() error {
	// get the input values or use defaults if empty
	httpPort := getValueOrDefault(m.inputs[0].Value(), strconv.Itoa(config.DefaultProductionHTTPPhishingPort))
	httpsPort := getValueOrDefault(m.inputs[1].Value(), strconv.Itoa(config.DefaultProductionHTTPSPhishingPort))
	adminPort := getValueOrDefault(m.inputs[2].Value(), strconv.Itoa(config.DefaultProductionAdministrationPort))
	adminHost := getValueOrDefault(m.inputs[3].Value(), config.DefaultAdminHost)
	autoTLS := getValueOrDefault(m.inputs[4].Value(), config.DefaultAdminAutoTLSString)
	acmeEmail := getValueOrDefault(m.inputs[5].Value(), config.DefaultACMEEmail)

	return m.applyServerConfig(httpPort, httpsPort, adminPort, adminHost, autoTLS, acmeEmail)
}

// applyAdvancedConfig applies advanced configuration
func (m *ConfigModel) applyAdvancedConfig() error {
	// server configuration
	httpPort := getValueOrDefault(m.inputs[0].Value(), strconv.Itoa(config.DefaultProductionHTTPPhishingPort))
	httpsPort := getValueOrDefault(m.inputs[1].Value(), strconv.Itoa(config.DefaultProductionHTTPSPhishingPort))
	adminPort := getValueOrDefault(m.inputs[2].Value(), strconv.Itoa(config.DefaultProductionAdministrationPort))
	adminHost := getValueOrDefault(m.inputs[3].Value(), config.DefaultAdminHost)
	autoTLS := getValueOrDefault(m.inputs[4].Value(), config.DefaultAdminAutoTLSString)
	acmeEmail := getValueOrDefault(m.inputs[5].Value(), config.DefaultACMEEmail)

	// apply server config first
	if err := m.applyServerConfig(httpPort, httpsPort, adminPort, adminHost, autoTLS, acmeEmail); err != nil {
		return err
	}

	// database configuration
	dbEngine := getValueOrDefault(m.inputs[6].Value(), config.DefaultDatabase)
	dbDSN := getValueOrDefault(m.inputs[7].Value(), config.DefaultAdministrationDSN)

	// validate database engine
	if dbEngine != config.DatabaseUsePostgres && dbEngine != config.DefaultAdministrationUseSqlite {
		return fmt.Errorf("invalid database engine: %s (must be 'postgres' or 'sqlite3')", dbEngine)
	}

	// set database config
	m.config.SetDatabaseEngine(dbEngine)
	m.config.SetDatabaseDSN(dbDSN)

	// tls configuration (if not using auto TLS)
	tlsCertPath := m.inputs[8].Value()
	tlsKeyPath := m.inputs[9].Value()
	if tlsCertPath != "" {
		m.config.SetTLSCertPath(tlsCertPath)
	}
	if tlsKeyPath != "" {
		m.config.SetTLSKeyPath(tlsKeyPath)
	}

	// logging configuration
	logPath := m.inputs[10].Value()
	errLogPath := m.inputs[11].Value()
	m.config.SetLogPath(logPath)
	m.config.SetErrLogPath(errLogPath)

	// security configuration
	adminAllowed := m.inputs[12].Value()
	trustedProxies := m.inputs[13].Value()
	trustedIPHeader := m.inputs[14].Value()

	// parse comma-separated IP lists
	adminAllowedList := []string{}
	trustedProxiesList := []string{}

	if adminAllowed != "" {
		adminAllowedList = strings.Split(strings.ReplaceAll(adminAllowed, " ", ""), ",")
	}
	if trustedProxies != "" {
		trustedProxiesList = strings.Split(strings.ReplaceAll(trustedProxies, " ", ""), ",")
	}

	// set security config
	m.config.IPSecurity.AdminAllowed = adminAllowedList
	m.config.IPSecurity.TrustedProxies = trustedProxiesList
	m.config.IPSecurity.TrustedIPHeader = trustedIPHeader

	return nil
}

// applyServerConfig applies server configuration (common to both modes)
func (m *ConfigModel) applyServerConfig(httpPort, httpsPort, adminPort, adminHost, autoTLS, acmeEmail string) error {
	// convert ports to integers
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

	// validate port values
	if httpPortInt <= 0 || httpPortInt > 65535 {
		return fmt.Errorf("HTTP port must be between 1 and 65535")
	}
	if httpsPortInt <= 0 || httpsPortInt > 65535 {
		return fmt.Errorf("HTTPS port must be between 1 and 65535")
	}
	if adminPortInt < 0 || adminPortInt > 65535 {
		return fmt.Errorf("admin port must be between 0 and 65535")
	}

	// check for port conflicts
	if httpPortInt == httpsPortInt {
		return fmt.Errorf("HTTP and HTTPS ports cannot be the same")
	}
	if adminPortInt != 0 && (adminPortInt == httpPortInt || adminPortInt == httpsPortInt) {
		return fmt.Errorf("admin port cannot be the same as HTTP or HTTPS ports")
	}

	// convert autoTLS to boolean
	autoTLSBool := false
	if strings.ToLower(autoTLS) == config.DefaultAdminAutoTLSString {
		autoTLSBool = true
	}

	// set values in config
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
	// first check if we're running as root
	if os.Geteuid() != 0 {
		return fmt.Errorf("installation must be run as root")
	}

	// create installation directories first
	if err := createDirectories(); err != nil {
		return fmt.Errorf("failed to create install directories: %w", err)
	}

	// get default configuration
	conf := config.NewProductionDefaultConfig()

	// run the tea program
	p := tea.NewProgram(InitialModel(conf))
	model, err := p.Run()
	if err != nil {
		return fmt.Errorf("error running interactive installer: %w", err)
	}

	// get the final model
	finalModel := model.(ConfigModel)
	if !finalModel.shouldInstall {
		return fmt.Errorf("installation cancelled")
	}

	// save the config to the installation directory
	configPath := filepath.Join(installDir, "config.json")
	err = finalModel.config.WriteToFile(configPath)
	if err != nil {
		return fmt.Errorf("failed to save configuration to %s: %w", configPath, err)
	}
	if err := os.Chmod(configPath, 0600); err != nil {
		return fmt.Errorf("failed to set config file permissions: %w", err)
	}

	fmt.Printf("Configuration saved to %s\n", configPath)

	// now run the actual installation
	err = InstallWithConfig(finalModel.config)
	if err != nil {
		return err
	}
	return nil
}

// runInteractiveConfigOnly runs the interactive installer and saves config without installing
func RunInteractiveConfigOnly(configPath string) error {
	fmt.Println("📝 Running in CONFIG-ONLY mode - no actual installation will be performed")
	fmt.Println()

	// get default configuration (no root check needed for config-only mode)
	conf := config.NewProductionDefaultConfig()

	// run the tea program
	p := tea.NewProgram(InitialModel(conf))
	model, err := p.Run()
	if err != nil {
		return fmt.Errorf("error running interactive installer: %w", err)
	}

	// get the final model
	finalModel := model.(ConfigModel)
	if !finalModel.shouldInstall {
		fmt.Println("Installation cancelled by user")
		return nil
	}

	// save the config to the specified path
	err = finalModel.config.WriteToFile(configPath)
	if err != nil {
		return fmt.Errorf("failed to save configuration to %s: %w", configPath, err)
	}

	fmt.Printf("✅ Configuration saved to %s\n", configPath)
	fmt.Println("💡 Review the config file and run without --config-only flag as root to install")

	return nil
}

// installWithConfig handles the installation using the provided configuration
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
