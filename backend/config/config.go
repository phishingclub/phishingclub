package config

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"net"
	"os"
	"strconv"

	"github.com/go-errors/errors"

	"github.com/phishingclub/phishingclub/data"
	"github.com/phishingclub/phishingclub/errs"
	"github.com/phishingclub/phishingclub/file"
)

var (
	ErrMissingIP          = errors.New("missing IP")
	ErrMissingPort        = errors.New("missing port")
	ErrMissingDatabaseDSN = errors.New("missing database DSN")
	ErrInvalidIP          = errors.New("invalid IP")
	ErrInvalidPort        = errors.New("invalid port")
	ErrInvalidDatabase    = errors.New("invalid database")
	ErrWriterIsNil        = errors.New("writer is nil")
)

const (
	DefaultACMEEmail    = ""
	DefaultDevACMEEmail = ""

	DatabaseUsePostgres            = "postgres"
	DefaultAdministrationUseSqlite = "sqlite3"
	DefaultDatabase                = DefaultAdministrationUseSqlite
	DefaultAdministrationDSN       = "file:./db.sqlite3"

	DefaultDevAdministrationPort = 0 // 0 uses ephemeral port, random available port
	DefaultDevHTTPPhishingPort   = 8080
	DefaultDevHTTPSPhishingPort  = 8443

	DefaultProductionAdministrationPort = 0 // 0 uses ephemeral port, random available port
	DefaultProductionHTTPPhishingPort   = 80
	DefaultProductionHTTPSPhishingPort  = 443

	// empty is none
	DefaultLogFilePath    = ""
	DefaultErrLogFilePath = ""

	DefaultTrustedIPHeader = ""

	DefaultAdminHost          = ""
	DefaultAdminAutoTLS       = true
	DefaultAdminAutoTLSString = "true"
)

var (
	defaultTrustedProxies = []string{}
	defaultAdminAllowed   = []string{}
)

type (
	// Config config
	Config struct {
		acme ACME

		tlsHost string
		tlsAuto bool

		tlsCertPath string
		tlsKeyPath  string

		adminNetAddress         net.TCPAddr
		phishingHTTPNetAddress  net.TCPAddr
		phishingHTTPSNetAddress net.TCPAddr
		database                Database
		fileWriter              file.Writer

		LogPath    string
		ErrLogPath string

		IPSecurity IPSecurityConfig
	}

	// ConfigDTO config DTO
	ConfigDTO struct {
		ACME                 ACME                 `json:"acme"`
		AdministrationServer AdministrationServer `json:"administration"`
		PhishingServer       PhishingServer       `json:"phishing"`
		Database             Database             `json:"database"`
		Log                  Log                  `json:"log"`
		IPSecurity           IPSecurityConfig     `json:"ip_security"`
	}

	Log struct {
		Path      string `json:"path"`
		ErrorPath string `json:"errorPath"`
	}

	// AdministrationServer ConfigDTO administration
	AdministrationServer struct {
		TLSHost     string   `json:"tls_host"`
		TLSAuto     bool     `json:"tls_auto"`
		TLSCertPath string   `json:"tls_cert_path"`
		TLSKeyPath  string   `json:"tls_key_path"`
		Address     string   `json:"address"`
		AllowList   []string `json:"ip_allow_list"`
	}

	// PhishingServer ConfigDTO phishing
	PhishingServer struct {
		Http  string `json:"http"`
		Https string `json:"https"`
	}

	//  Database ConfigDTO database
	Database struct {
		Engine string `json:"engine"`
		DSN    string `json:"dsn"`
	}

	// ACME ConfigDTO acme
	ACME struct {
		Email string `json:"email"`
	}
)

type IPSecurityConfig struct {
	// ip/cidr that are allowed to access the admin interface
	AdminAllowed []string `json:"admin_allowed"`

	// ip/cidr of legitimate reverse proxies (e.g., Nginx, HAProxy, Cloudflare edges)
	TrustedProxies []string `json:"trusted_proxies"`

	// headers to check for real client IP
	// examples: CF-Connecting-IP, X-Real-IP, True-Client-IP, X-Forwarded-For
	TrustedIPHeader string `json:"trusted_ip_header"`
}

// ValidateFileWriter validates the file writer
func ValidateFileWriter(fileWriter file.Writer) error {
	if fileWriter == nil {
		return ErrWriterIsNil
	}
	return nil
}

// NewConfig factory
func NewConfig(
	acmeEmail string,
	tlsHost string,
	tlsAuto bool,
	adminPublicCertPath string,
	adminPrivateCertKey string,
	adminAddress string,
	phishingHTTPAddress string,
	phishingHTTPSAddress string,
	database Database,
	fileWriter file.Writer,
	logPath string,
	errLogPath string,
	ipSecurity IPSecurityConfig,
) (*Config, error) {
	if err := ValidateFileWriter(fileWriter); err != nil {
		return nil, errs.Wrap(err)
	}
	adminNetAddress, err := StringAddressToTCPAddr(adminAddress)
	if err != nil {
		return nil, errs.Wrap(err)
	}
	phishingHTTPNetAddress, err := StringAddressToTCPAddr(phishingHTTPAddress)
	if err != nil {
		return nil, errs.Wrap(err)
	}
	phishingHTTPSNetAddress, err := StringAddressToTCPAddr(phishingHTTPSAddress)
	if err != nil {
		return nil, errs.Wrap(err)
	}
	switch database.Engine {
	case DatabaseUsePostgres:
	case DefaultAdministrationUseSqlite:
	default:
		return nil, ErrInvalidDatabase
	}

	return &Config{
		acme: ACME{
			Email: acmeEmail,
		},
		tlsHost:                 tlsHost,
		tlsAuto:                 tlsAuto,
		tlsCertPath:             adminPublicCertPath,
		tlsKeyPath:              adminPrivateCertKey,
		adminNetAddress:         *adminNetAddress,
		phishingHTTPNetAddress:  *phishingHTTPNetAddress,
		phishingHTTPSNetAddress: *phishingHTTPSNetAddress,
		database: Database{
			Engine: database.Engine,
			DSN:    database.DSN,
		},
		fileWriter: &file.FileWriter{},
		LogPath:    logPath,
		ErrLogPath: errLogPath,
		IPSecurity: ipSecurity,
	}, nil
}

// NewDevDefaultConfig returns a default config
func NewDevDefaultConfig() *Config {
	tlsHost := "phish.test"
	tlsAuto := false
	publicCertPath := fmt.Sprintf(
		"%s/%s",
		data.DefaultAdminCertDir,
		data.DefaultAdminPublicCertFileName,
	)
	privateCertPath := fmt.Sprintf(
		"%s/%s",
		data.DefaultAdminCertDir,
		data.DefaultAdminPrivateCertFileName,
	)
	return &Config{
		acme: ACME{
			Email: DefaultACMEEmail,
		},
		tlsHost:     tlsHost,
		tlsAuto:     tlsAuto,
		tlsCertPath: publicCertPath,
		tlsKeyPath:  privateCertPath,
		adminNetAddress: net.TCPAddr{
			IP:   net.IPv4(0, 0, 0, 0),
			Port: DefaultDevAdministrationPort,
		},
		phishingHTTPNetAddress: net.TCPAddr{
			IP:   net.IPv4(0, 0, 0, 0),
			Port: DefaultDevHTTPPhishingPort,
		},
		phishingHTTPSNetAddress: net.TCPAddr{
			IP:   net.IPv4(0, 0, 0, 0),
			Port: DefaultDevHTTPSPhishingPort,
		},
		database: Database{
			Engine: DefaultAdministrationUseSqlite,
			DSN:    DefaultAdministrationDSN,
		},
		fileWriter: &file.FileWriter{},
		LogPath:    DefaultLogFilePath,
		ErrLogPath: DefaultErrLogFilePath,
		IPSecurity: IPSecurityConfig{
			AdminAllowed:    []string{},
			TrustedProxies:  []string{},
			TrustedIPHeader: "",
		},
	}
}

// NewDevDefaultConfig returns a default config
func NewProductionDefaultConfig() *Config {
	tlsHost := "localhost"
	tlsAuto := DefaultAdminAutoTLS
	publicCertPath := fmt.Sprintf(
		"%s/%s",
		data.DefaultAdminCertDir,
		data.DefaultAdminPublicCertFileName,
	)
	privateCertPath := fmt.Sprintf(
		"%s/%s",
		data.DefaultAdminCertDir,
		data.DefaultAdminPrivateCertFileName,
	)
	return &Config{
		acme: ACME{
			Email: DefaultACMEEmail,
		},
		tlsHost:     tlsHost,
		tlsAuto:     tlsAuto,
		tlsCertPath: publicCertPath,
		tlsKeyPath:  privateCertPath,
		adminNetAddress: net.TCPAddr{
			IP:   net.IPv4(0, 0, 0, 0),
			Port: DefaultProductionAdministrationPort,
		},
		phishingHTTPNetAddress: net.TCPAddr{
			IP:   net.IPv4(0, 0, 0, 0),
			Port: DefaultProductionHTTPPhishingPort,
		},
		phishingHTTPSNetAddress: net.TCPAddr{
			IP:   net.IPv4(0, 0, 0, 0),
			Port: DefaultProductionHTTPSPhishingPort,
		},

		database: Database{
			Engine: DefaultAdministrationUseSqlite,
			DSN:    DefaultAdministrationDSN,
		},
		fileWriter: &file.FileWriter{},
		IPSecurity: IPSecurityConfig{
			AdminAllowed:    []string{},
			TrustedProxies:  []string{},
			TrustedIPHeader: "",
		},
	}
}

// ACMEEmail returns the acme email
func (c *Config) ACMEEmail() string {
	return c.acme.Email
}

// SetACMEEmail sets the acme email
func (c *Config) SetACMEEmail(email string) {
	c.acme.Email = email
}

// TLSHost returns the host to use for admin server
func (c *Config) TLSHost() string {
	return c.tlsHost
}

// TLSAuto returns if ACME service should handle TLS for the admin server
func (c *Config) TLSAuto() bool {
	return c.tlsAuto
}

// TLSCertPath returns the cert path
func (c *Config) TLSCertPath() string {
	return c.tlsCertPath
}

// TLSKeyPath returns the private key
func (c *Config) TLSKeyPath() string {
	return c.tlsKeyPath
}

// SetTLSCertPath returns the admin host
func (c *Config) SetTLSHost(host string) {
	c.tlsHost = host
}

// SetTLSAuto sets if a ACME service should handle TLS for the admin server
func (c *Config) SetTLSAuto(auto bool) {
	c.tlsAuto = auto
}

// SetAdminNetAddress sets the administration network address
func (c *Config) SetAdminNetAddress(adminNetAddress string) error {
	newAddr, err := StringAddressToTCPAddr(adminNetAddress)
	if err != nil {
		return err
	}
	c.adminNetAddress = *newAddr
	return nil
}

// SetPhishingHTTPNetAddress sets the phishing network address
func (c *Config) SetPhishingHTTPNetAddress(addr string) error {
	newAddr, err := StringAddressToTCPAddr(addr)
	if err != nil {
		return err
	}
	c.phishingHTTPNetAddress = *newAddr
	return nil
}

// SetPhishingHTTPNetAddress sets the phishing network address
func (c *Config) SetPhishingHTTPSNetAddress(addr string) error {
	newAddr, err := StringAddressToTCPAddr(addr)
	if err != nil {
		return err
	}
	c.phishingHTTPSNetAddress = *newAddr
	return nil
}

// SetFileWriter sets the file writer
func (c *Config) SetFileWriter(fileWriter file.Writer) error {
	if err := ValidateFileWriter(fileWriter); err != nil {
		return fmt.Errorf("failed to set file writer on config: %w", err)
	}
	c.fileWriter = fileWriter
	return nil
}

// Write writes the config to a writer
func (c *Config) WriteToFile(filepath string) error {
	dto := c.ToDTO()
	conf, err := json.MarshalIndent(dto, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	// Write the content to the writer
	if _, err := c.fileWriter.Write(filepath, conf, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0644); err != nil {
		return fmt.Errorf("failed to write config: %w", err)
	}

	return nil
}

// StringAddressToTCPAddr converts a string address to a TCPAddr
func StringAddressToTCPAddr(address string) (*net.TCPAddr, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, errs.Wrap(err)
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return nil, ErrInvalidIP
	}
	// convert port to int
	p, err := strconv.Atoi(port)
	if err != nil {
		return nil, errs.Wrap(err)
	}
	if p < 0 || p > 65535 {
		return nil, ErrInvalidPort
	}
	return &net.TCPAddr{
		IP:   ip,
		Port: p,
	}, nil
}

// FromMap creates a *Config from a DTO
func FromDTO(dto *ConfigDTO) (*Config, error) {
	return NewConfig(
		dto.ACME.Email,
		dto.AdministrationServer.TLSHost,
		dto.AdministrationServer.TLSAuto,
		dto.AdministrationServer.TLSCertPath,
		dto.AdministrationServer.TLSKeyPath,
		dto.AdministrationServer.Address,
		dto.PhishingServer.Http,
		dto.PhishingServer.Https,
		dto.Database,
		file.FileWriter{},
		dto.Log.Path,
		dto.Log.ErrorPath,
		dto.IPSecurity,
	)
}

// ToDTO converts a *Config to a *ConfigDTO
func (c *Config) ToDTO() *ConfigDTO {
	allowList := make([]string, 0)

	return &ConfigDTO{
		ACME: ACME{
			Email: c.acme.Email,
		},
		AdministrationServer: AdministrationServer{
			TLSHost:     c.TLSHost(),
			TLSAuto:     c.TLSAuto(),
			TLSCertPath: c.TLSCertPath(),
			TLSKeyPath:  c.TLSKeyPath(),
			Address:     c.AdminNetAddress(),
			AllowList:   allowList,
		},
		PhishingServer: PhishingServer{
			Http:  c.phishingHTTPNetAddress.String(),
			Https: c.phishingHTTPSNetAddress.String(),
		},
		Database: Database{
			Engine: c.database.Engine,
			DSN:    c.database.DSN,
		},
		Log: Log{
			Path:      c.LogPath,
			ErrorPath: c.ErrLogPath,
		},
		IPSecurity: c.IPSecurity,
	}
}

// AdminNetAddress returns the administration network address
func (c *Config) AdminNetAddress() string {
	return c.adminNetAddress.String()
}

// AdminNetAddressPort returns the administration network address port
func (c *Config) AdminNetAddressPort() int {
	return c.adminNetAddress.Port
}

// PhishingHTTPNetAddress returns the phishing network address
func (c *Config) PhishingHTTPNetAddress() string {
	return c.phishingHTTPNetAddress.String()
}

// PhishingHTTPNetAddressPort returns the phishing network address port
func (c *Config) PhishingHTTPNetAddressPort() int {
	return c.phishingHTTPNetAddress.Port
}

// PhishingHTTPSNetAddress returns the phishing network address
func (c *Config) PhishingHTTPSNetAddress() string {
	return c.phishingHTTPSNetAddress.String()
}

// PhishingHTTPSNetAddressPort returns the phishing network address port
func (c *Config) PhishingHTTPSNetAddressPort() int {
	return c.phishingHTTPSNetAddress.Port
}

// Database returns the database
func (c *Config) Database() Database {
	return c.database
}

// NewDTOFromFile creates a *ConfigDTO from a file
func NewDTOFromFile(filesystem fs.FS, path string) (*ConfigDTO, error) {
	var conf ConfigDTO
	f, err := filesystem.Open(path)
	if err != nil {
		return nil, errs.Wrap(err)
	}
	dec := json.NewDecoder(f)
	err = dec.Decode(&conf)
	if err != nil {
		return nil, errs.Wrap(err)
	}
	return &conf, nil
}
