package install

import (
	"bufio"
	"bytes"
	"embed"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"text/template"
	"time"
)

//go:embed systemd.service
var installFiles embed.FS

const (
	// Installation paths
	installDir = "/opt/phishingclub"
	binaryName = "phishingclub"
	dataDir    = "data"

	// User and group
	serviceUser  = "phishingclub"
	serviceGroup = "phishingclub"

	// Service
	serviceName = "phishingclub"
)

// Install handles the complete installation process interactively
func Install() error {
	return RunInteractiveInstall()
}

// InstallNonInteractive handles the non-interactive installation process
func InstallNonInteractive() error {
	if os.Geteuid() != 0 {
		return fmt.Errorf("installation must be run as root")
	}

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
	fmt.Println("If the service is restarted before the first used is setup, the password will change! - Check the logs")
	fmt.Println()
	fmt.Println("# Tips")
	fmt.Println("'journalctl -u phishingclub.service -f' to see logs")
	fmt.Println("'systemctl status phishingclub' to check status of the service")
	fmt.Println("")
	fmt.Println()

	return nil
}

// Update handles the update process of the application
func Update() error {
	if os.Geteuid() != 0 {
		return fmt.Errorf("update must be run as root")
	}

	// Check if service exists
	if err := checkServiceExists(); err != nil {
		return fmt.Errorf("service check failed: %w", err)
	}

	steps := []struct {
		name string
		fn   func() error
	}{
		{"stop service", stopService},
		{"backup current binary", backupCurrentBinary},
		{"update binary", updateBinary},
		{"start service", startService},
	}

	for _, step := range steps {
		fmt.Printf("Step: %s\n", step.name)
		if err := step.fn(); err != nil {
			return fmt.Errorf("%s: %w", step.name, err)
		}
	}

	fmt.Println()

	fmt.Println("# Post-update Status Check")
	// Give the service a moment to stabilize
	time.Sleep(2 * time.Second)

	cmd := exec.Command("systemctl", "status", serviceName)
	output, err := cmd.CombinedOutput()
	if err != nil {
		fmt.Println("⚠️  Warning: Service may not be running properly after update")
		fmt.Printf("Service status output:\n%s\n", string(output))
		fmt.Printf("Check status with: systemctl status %s\n", serviceName)
		return nil
	}

	if !strings.Contains(string(output), "active (running)") {
		fmt.Println("⚠️  Warning: Service is not in 'active' state after update")
		fmt.Printf("Service status output:\n%s\n", string(output))
		return nil
	}

	fmt.Println("✅ Service is running")
	fmt.Println()
	fmt.Println("Update completed! 🐟")
	fmt.Println()

	return nil
}

// checkServiceExists verifies that the service is installed
func checkServiceExists() error {
	servicePath := filepath.Join("/etc/systemd/system", serviceName+".service")
	if _, err := os.Stat(servicePath); os.IsNotExist(err) {
		return fmt.Errorf("service is not installed. Please run --install first")
	}
	return nil
}

// stopService stops the running service
func stopService() error {
	cmd := exec.Command("systemctl", "stop", serviceName)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to stop service: %s, error: %w", string(output), err)
	}

	// Wait a moment to ensure the service is fully stopped
	time.Sleep(2 * time.Second)
	return nil
}

// backupCurrentBinary creates a backup of the current binary
func backupCurrentBinary() error {
	currentBinary := filepath.Join(installDir, binaryName)
	backupBinary := filepath.Join(installDir, binaryName+".backup")
	// #nosec
	input, err := os.ReadFile(currentBinary)
	if err != nil {
		return fmt.Errorf("failed to read current binary: %w", err)
	}

	if err := os.WriteFile(backupBinary, input, 0600); err != nil {
		return fmt.Errorf("failed to write backup binary: %w", err)
	}

	return nil
}

// updateBinary updates the binary with the new version
func updateBinary() error {
	executable, err := os.Executable()
	if err != nil {
		return fmt.Errorf("failed to get executable path: %w", err)
	}
	// #nosec
	input, err := os.ReadFile(executable)
	if err != nil {
		return fmt.Errorf("failed to read new binary: %w", err)
	}

	binaryPath := filepath.Join(installDir, binaryName)
	if err := os.WriteFile(binaryPath, input, 0600); err != nil {
		return fmt.Errorf("failed to write new binary: %w", err)
	}

	// Set proper ownership
	if err := setPermissions(); err != nil {
		return fmt.Errorf("failed to set permissions: %w", err)
	}

	return nil
}

func checkSQLiteDependency() error {
	// Check if sqlite3 is installed
	if _, err := exec.LookPath("sqlite3"); err != nil {
		fmt.Println("SQLite3 is not installed. Attempting to install...")

		// Detect package manager and install sqlite
		if err := installSQLite(); err != nil {
			return fmt.Errorf("failed to install sqlite: %w", err)
		}
	}

	// Verify sqlite installation
	cmd := exec.Command("sqlite3", "--version")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("sqlite verification failed: %w", err)
	}

	fmt.Printf("SQLite version: %s", output)
	return nil
}

func installSQLite() error {
	// Detect the package manager and install sqlite
	var cmd *exec.Cmd

	// Check for apt (Debian/Ubuntu)
	if _, err := exec.LookPath("apt"); err == nil {
		cmd = exec.Command("apt", "update")
		err := cmd.Run() // Update package list
		if err != nil {
			fmt.Printf("ERR: %s\n", err)
		}
		cmd = exec.Command("apt", "install", "-y", "sqlite3")
	} else if _, err := exec.LookPath("yum"); err == nil {
		// Check for yum (RHEL/CentOS)
		cmd = exec.Command("yum", "install", "-y", "sqlite")
	} else if _, err := exec.LookPath("dnf"); err == nil {
		// Check for dnf (Fedora)
		cmd = exec.Command("dnf", "install", "-y", "sqlite")
	} else if _, err := exec.LookPath("pacman"); err == nil {
		// Check for pacman (Arch)
		cmd = exec.Command("pacman", "-S", "--noconfirm", "sqlite")
	} else {
		return fmt.Errorf("no supported package manager found (apt, yum, dnf, or pacman)")
	}

	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to install sqlite: %s, error: %w", string(output), err)
	}

	return nil
}

// enableService enables the systemd service so it gets started on boot
func enableService() error {
	if err := exec.Command("systemctl", "enable", serviceName).Run(); err != nil {
		return fmt.Errorf("failed to enable service: %w", err)
	}
	return nil
}

// startService starts the systemd service
func startService() error {
	if err := exec.Command("systemctl", "start", serviceName).Run(); err != nil {
		return fmt.Errorf("failed to start service: %w", err)
	}
	return nil
}

func Uninstall() error {
	if os.Geteuid() != 0 {
		return fmt.Errorf("uninstallation must be run as root")
	}

	// Display warning and confirmation prompt
	fmt.Println("⚠️  WARNING: Uninstallation will remove ALL components of Phishing Club, including:")
	fmt.Println("  • The application binary and its service")
	fmt.Println("  • ALL configuration files")
	fmt.Println("  • ALL data, including the database")
	fmt.Println("  • The phishingclub user and group")
	fmt.Println("\nThis operation CANNOT be undone!")

	reader := bufio.NewReader(os.Stdin)
	fmt.Print("\nType 'YES' (all caps) to confirm uninstallation: ")
	confirmation, _ := reader.ReadString('\n')
	confirmation = strings.TrimSpace(confirmation)

	if confirmation != "YES" {
		fmt.Println("Uninstallation cancelled.")
		return fmt.Errorf("uninstallation cancelled by user")
	}

	return performUninstall()
}

// UninstallNonInteractive performs uninstallation without confirmation prompts
func UninstallNonInteractive() error {
	if os.Geteuid() != 0 {
		return fmt.Errorf("uninstallation must be run as root")
	}

	return performUninstall()
}

// performUninstall handles the actual uninstallation process
func performUninstall() error {
	fmt.Println("Uninstalling Phishing Club...")

	// Stop and disable service
	err := exec.Command("systemctl", "stop", serviceName).Run()
	if err != nil {
		fmt.Printf("Warning: Failed to stop service: %v\n", err)
		// Continue with uninstallation
	}

	err = exec.Command("systemctl", "disable", serviceName).Run()
	if err != nil {
		fmt.Printf("Warning: Failed to disable service: %v\n", err)
		// Continue with uninstallation
	}

	// Remove service file
	servicePath := filepath.Join("/etc/systemd/system", serviceName+".service")
	err = os.Remove(servicePath)
	if err != nil && !os.IsNotExist(err) {
		fmt.Printf("Warning: Failed to remove service unit file: %v\n", err)
		// Continue with uninstallation
	}

	// Reload systemd
	err = exec.Command("systemctl", "daemon-reload").Run()
	if err != nil {
		fmt.Printf("Warning: Failed to reload systemctl daemon: %v\n", err)
		// Continue with uninstallation
	}

	// Remove installation directory
	err = os.RemoveAll(installDir)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to remove install directory: %w", err)
	}

	// Remove user and group
	fmt.Printf("Removing user and group: %s\n", serviceUser)
	err = exec.Command("userdel", serviceUser).Run()
	if err != nil {
		fmt.Printf("Warning: Failed to delete user %s: %v\n", serviceUser, err)
		// Continue with uninstallation
	}

	_ = exec.Command("groupdel", serviceGroup).Run()
	// Group deletion errors are not critical

	fmt.Println("\n✅ Uninstallation completed successfully!")
	fmt.Println("All Phishing Club components have been removed from your system.")

	return nil
}

func createUserAndGroup() error {
	// Check if group exists
	if err := exec.Command("getent", "group", serviceGroup).Run(); err != nil {
		cmd := exec.Command("groupadd", serviceGroup)
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("failed to create group: %w", err)
		}
	}

	// Check if user exists
	if err := exec.Command("getent", "passwd", serviceUser).Run(); err != nil {
		cmd := exec.Command("useradd",
			"-r",
			"-g", serviceGroup,
			"-s", "/bin/false",
			serviceUser,
		)
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("failed to create user: %w", err)
		}
	}

	return nil
}

func createDirectories() error {
	dirs := []string{
		installDir,
		filepath.Join(installDir, dataDir),
	}

	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0750); err != nil {
			return fmt.Errorf("failed to create directory %s: %w", dir, err)
		}
	}

	return nil
}

func installBinary() error {
	executable, err := os.Executable()
	if err != nil {
		return fmt.Errorf("failed to get executable path: %w", err)
	}
	// #nosec
	input, err := os.ReadFile(executable)
	if err != nil {
		return fmt.Errorf("failed to read executable: %w", err)
	}

	binaryPath := filepath.Join(installDir, binaryName)
	// #nosec
	if err := os.WriteFile(binaryPath, input, 0750); err != nil {
		return fmt.Errorf("failed to write binary: %w", err)
	}

	return nil
}

func installSystemdService() error {
	serviceTemplate, err := installFiles.ReadFile("systemd.service")
	if err != nil {
		return fmt.Errorf("failed to read service template: %w", err)
	}

	// Create template data with all required fields
	data := struct {
		User       string
		Group      string
		InstallDir string
		BinaryPath string
		ConfigPath string
		DataDir    string
	}{
		User:       serviceUser,
		Group:      serviceGroup,
		InstallDir: installDir,
		BinaryPath: filepath.Join(installDir, binaryName),
		ConfigPath: filepath.Join(installDir, "config.json"),
		DataDir:    filepath.Join(installDir, dataDir),
	}

	// Parse and execute the template
	tmpl, err := template.New("service").Parse(string(serviceTemplate))
	if err != nil {
		return fmt.Errorf("failed to parse service template: %w", err)
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return fmt.Errorf("failed to execute service template: %w", err)
	}

	servicePath := filepath.Join("/etc/systemd/system", serviceName+".service")
	// #nosec
	if err := os.WriteFile(servicePath, buf.Bytes(), 0644); err != nil {
		return fmt.Errorf("failed to write service file: %w", err)
	}

	if err := exec.Command("systemctl", "daemon-reload").Run(); err != nil {
		return fmt.Errorf("failed to reload systemd: %w", err)
	}

	return nil
}

func setPermissions() error {
	// #nosec
	cmd := exec.Command("chown", "-R",
		fmt.Sprintf("%s:%s", serviceUser, serviceGroup),
		installDir,
	)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to set ownership: %w", err)
	}

	return nil
}

func outputCredentialsAndInfo() error {
	time.Sleep(3 * time.Second)
	fmt.Println()
	fmt.Println("<<< IMPORTANT >>>")

	steps := []string{
		"journalctl -u phishingclub.service -r -n 5000 --no-pager --output=cat | grep 'Username:' -m1",
		"journalctl -u phishingclub.service -r -n 5000 --no-pager --output=cat | grep 'Password:' -m1",
		"journalctl -u phishingclub.service -r -n 5000 --no-pager --output=cat | grep 'Phishing HTTPS' -m1 -B1 | tac",
		"journalctl -u phishingclub.service -r -n 5000 --no-pager --output=cat | grep 'Phishing HTTP server' -m1 -B1 | tac",
		"journalctl -u phishingclub.service -r -n 5000 --no-pager --output=cat | grep 'Admin server' -m1 -B1 | tac",
	}
	for _, t := range steps {
		cmd := exec.Command("sh", "-c", t)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("failed to get all install information: %w", err)
		}
	}
	return nil
}
