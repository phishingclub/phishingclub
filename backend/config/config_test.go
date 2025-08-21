package config

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"net"
	"os"
	"reflect"
	"testing"
	"testing/fstest"

	"github.com/go-errors/errors"

	"github.com/phishingclub/phishingclub/data"
	"github.com/phishingclub/phishingclub/file"
	"github.com/phishingclub/phishingclub/file/filemock"
)

const (
	DEFAULT_ADMIN_ADDR          = "127.0.0.1:8002"
	DEFAULT_PHISHING_HTTP_ADDR  = "127.0.0.1:8000"
	DEFAULT_PHISHING_HTTPS_ADDR = "127.0.0.1:8001"
	DEFAULT_ACME_EMAIL          = ""
)

var (
	adminHost           = "phish.test"
	adminTLS            = false
	adminPublicCertPath = fmt.Sprintf(
		"%s/%s",
		data.DefaultAdminCertDir,
		data.DefaultAdminPublicCertFileName,
	)
	adminPrivateCertPath = fmt.Sprintf(
		"%s/%s",
		data.DefaultAdminCertDir,
		data.DefaultAdminPrivateCertFileName,
	)

	configFileOK = []byte(`{
	"administration": {
		"address": "127.0.0.1:4000"
	}
}`)
	configFileEmpty = []byte("{")
	databaseOK      = Database{
		Engine: DefaultAdministrationUseSqlite,
		DSN:    DefaultAdministrationDSN,
	}
)

func newTestConfig() *Config {
	return &Config{
		acme: ACME{
			Email: DEFAULT_ACME_EMAIL,
		},
		tlsCertPath: adminPublicCertPath,
		tlsKeyPath:  adminPrivateCertPath,
		adminNetAddress: net.TCPAddr{
			IP:   net.IPv4(127, 0, 0, 1),
			Port: DefaultDevAdministrationPort,
		},
		phishingHTTPNetAddress: net.TCPAddr{
			IP:   net.IPv4(127, 0, 0, 1),
			Port: DefaultDevHTTPPhishingPort,
		},
		phishingHTTPSNetAddress: net.TCPAddr{
			IP:   net.IPv4(127, 0, 0, 1),
			Port: DefaultDevHTTPSPhishingPort,
		},
		database:   databaseOK,
		fileWriter: &filemock.Writer{},
	}
}

func TestNewConfig(t *testing.T) {
	t.Run("happy path", testNewConfigHappyPath)
	t.Run("invalid administration address and port split", testNewConfigInvalidAdministrationAddress)
	t.Run("invalid administration ip", testNewConfigInvalidAdministrationIP)
	t.Run("invalid administration port", testNewConfigInvalidAdministrationPort)
	t.Run("invalid administration port string", testNewConfigInvalidAdministrationPortString)
	t.Run("invalid database", testNewConfigInvalidDatabase)
	t.Run("writer with nil", testNewConfigWithNilWriter)

}

func testNewConfigWithNilWriter(t *testing.T) {
	_, err := NewConfig(
		DEFAULT_ACME_EMAIL,
		adminHost,
		adminTLS,
		adminPublicCertPath,
		adminPrivateCertPath,
		"127.0.0.1:8080",
		DEFAULT_PHISHING_HTTP_ADDR,
		DEFAULT_PHISHING_HTTPS_ADDR,
		databaseOK,
		nil,
		"",
		"",

		IPSecurityConfig{
			AdminAllowed:    defaultAdminAllowed,
			TrustedProxies:  defaultTrustedProxies,
			TrustedIPHeader: DefaultTrustedIPHeader,
		},
	)
	if err == nil {
		if !errors.Is(err, ErrWriterIsNil) {
			t.Error("expected ErrWriterIsNil error from nil writer")
		}
		t.Error("expected error from nil writer")
		return
	}
}

func testNewConfigInvalidAdministrationAddress(t *testing.T) {
	_, err := NewConfig(
		DEFAULT_ACME_EMAIL,
		adminHost,
		adminTLS,
		adminPublicCertPath,
		adminPrivateCertPath,
		"foobar",
		DEFAULT_PHISHING_HTTP_ADDR,
		DEFAULT_PHISHING_HTTPS_ADDR,
		databaseOK,
		&filemock.Writer{},
		"",
		"",
		IPSecurityConfig{
			AdminAllowed:    defaultAdminAllowed,
			TrustedProxies:  defaultTrustedProxies,
			TrustedIPHeader: DefaultTrustedIPHeader,
		},
	)
	if err == nil {
		t.Error("expected error from invalid address")
		return
	}
}

func testNewConfigInvalidAdministrationIP(t *testing.T) {
	_, err := NewConfig(
		DEFAULT_ACME_EMAIL,
		adminHost,
		adminTLS,
		adminPublicCertPath,
		adminPrivateCertPath,
		"999.00.999.999:1234",
		DEFAULT_PHISHING_HTTP_ADDR,
		DEFAULT_PHISHING_HTTPS_ADDR,
		databaseOK,
		&filemock.Writer{},
		"",
		"",
		IPSecurityConfig{
			AdminAllowed:    defaultAdminAllowed,
			TrustedProxies:  defaultTrustedProxies,
			TrustedIPHeader: DefaultTrustedIPHeader,
		},
	)
	if !errors.Is(err, ErrInvalidIP) {
		t.Error(err)
		return
	}
}

func testNewConfigHappyPath(t *testing.T) {
	addr := "127.0.0.1:1234"
	c, err := NewConfig(
		DEFAULT_ACME_EMAIL,
		adminHost,
		adminTLS,
		adminPublicCertPath,
		adminPrivateCertPath,
		addr,
		DEFAULT_PHISHING_HTTP_ADDR,
		DEFAULT_PHISHING_HTTPS_ADDR,
		databaseOK,
		&filemock.Writer{},
		"",
		"",
		IPSecurityConfig{
			AdminAllowed:    defaultAdminAllowed,
			TrustedProxies:  defaultTrustedProxies,
			TrustedIPHeader: DefaultTrustedIPHeader,
		},
	)
	if err != nil {
		t.Error(err)
		return
	}
	if c.AdminNetAddress() != addr {
		t.Errorf("expected %s but got %s", addr, c.AdminNetAddress())
		return
	}
	if c.database.DSN != databaseOK.DSN {
		t.Errorf("expected %s but got %s", databaseOK.DSN, c.database.DSN)
		return
	}
	if c.database.Engine != databaseOK.Engine {
		t.Errorf("expected %s but got %s", databaseOK.Engine, c.database.Engine)
		return
	}
}

func testNewConfigInvalidAdministrationPort(t *testing.T) {
	_, err := NewConfig(
		DEFAULT_ACME_EMAIL,
		adminHost,
		adminTLS,
		adminPublicCertPath,
		adminPrivateCertPath,
		"127.0.0.1:-1",
		DEFAULT_PHISHING_HTTP_ADDR,
		DEFAULT_PHISHING_HTTPS_ADDR,
		databaseOK,
		&filemock.Writer{},
		"",
		"",
		IPSecurityConfig{
			AdminAllowed:    defaultAdminAllowed,
			TrustedProxies:  defaultTrustedProxies,
			TrustedIPHeader: DefaultTrustedIPHeader,
		},
	)
	if !errors.Is(err, ErrInvalidPort) {
		t.Error(err)
		return
	}
}

func testNewConfigInvalidAdministrationPortString(t *testing.T) {
	_, err := NewConfig(
		DEFAULT_ACME_EMAIL,
		adminHost,
		adminTLS,
		adminPublicCertPath,
		adminPrivateCertPath,
		"127.0.0.1:999999999999999999999999999999999999999999",
		DEFAULT_PHISHING_HTTP_ADDR,
		DEFAULT_PHISHING_HTTPS_ADDR,
		databaseOK,
		&filemock.Writer{},
		"",
		"",
		IPSecurityConfig{
			AdminAllowed:    defaultAdminAllowed,
			TrustedProxies:  defaultTrustedProxies,
			TrustedIPHeader: DefaultTrustedIPHeader,
		},
	)
	if err == nil {
		t.Error("expected error from invalid string port")
		return
	}
}

func testNewConfigInvalidDatabase(t *testing.T) {
	_, err := NewConfig(
		DEFAULT_ACME_EMAIL,
		adminHost,
		adminTLS,
		adminPublicCertPath,
		adminPrivateCertPath,
		"127.0.0.1:1234",
		DEFAULT_PHISHING_HTTP_ADDR,
		DEFAULT_PHISHING_HTTPS_ADDR,
		Database{
			Engine: "foobar",
			DSN:    "file:./data.db?cache=shared&mode=rwc&_fk=1",
		}, &filemock.Writer{},
		"",
		"",
		IPSecurityConfig{
			AdminAllowed:    defaultAdminAllowed,
			TrustedProxies:  defaultTrustedProxies,
			TrustedIPHeader: DefaultTrustedIPHeader,
		},
	)
	if err == nil {
		t.Errorf("expected %s but got %s", ErrInvalidDatabase, err)
		return
	}
}

func TestSetFileWriter(t *testing.T) {
	t.Run("happypath", func(t *testing.T) {
		c := newTestConfig()
		err := c.SetFileWriter(&filemock.Writer{})
		if err != nil {
			t.Error(err)
			return
		}
	})
	t.Run("nil writer", func(t *testing.T) {
		c := newTestConfig()
		err := c.SetFileWriter(nil)
		if err == nil {
			if !errors.Is(err, ErrWriterIsNil) {
				t.Error("expected ErrWriterIsNil error from nil writer")
			}
			t.Error("expected error from nil writer")
			return
		}
	})
}

func TestWriteToFile(t *testing.T) {
	filepath := "./testFile"
	c := newTestConfig()
	m := filemock.Writer{}
	dto := c.ToDTO()
	conf, err := json.MarshalIndent(dto, "", "  ")
	if err != nil {
		t.Error(err)
		return
	}
	m.
		On("Write", filepath, conf, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, os.FileMode(0644)).
		Return(0, nil)
	err = c.SetFileWriter(&m)
	if err != nil {
		t.Error(err)
		return
	}
	err = c.WriteToFile(filepath)
	if err != nil {
		t.Error(err)
		return
	}
}

func TestToDTO(t *testing.T) {
	addr := "127.0.0.1:1234"
	c, err := NewConfig(
		DEFAULT_ACME_EMAIL,
		adminHost,
		adminTLS,
		adminPublicCertPath,
		adminPrivateCertPath,
		addr,
		DEFAULT_PHISHING_HTTP_ADDR,
		DEFAULT_PHISHING_HTTPS_ADDR,
		databaseOK,
		&filemock.Writer{},
		"",
		"",
		IPSecurityConfig{
			AdminAllowed:    defaultAdminAllowed,
			TrustedProxies:  defaultTrustedProxies,
			TrustedIPHeader: DefaultTrustedIPHeader,
		},
	)
	if err != nil {
		t.Error(err)
		return
	}
	dto := c.ToDTO()
	if dto.AdministrationServer.Address != addr {
		t.Errorf("expected %s but got %s", addr, dto.AdministrationServer.Address)
		return
	}
}

func TestNewDTOFromFile(t *testing.T) {

	t.Run("happypath", testNewDTOFromFileHappyPath)
	t.Run("file error", testNewDTOFromFileFileError)
	t.Run("bad content", testNewDTOFromFileBadContent)
}

func testNewDTOFromFileHappyPath(t *testing.T) {
	filesystem := fstest.MapFS{}
	path := "config.json"
	filesystem[path] = &fstest.MapFile{
		Data: configFileOK,
	}
	dto, err := NewDTOFromFile(filesystem, path)
	if err != nil {
		t.Error(err)
		return
	}
	if dto.AdministrationServer.Address != "127.0.0.1:4000" {
		t.Errorf("Expected %s Got %s", "127.0.0.1:4000", dto.AdministrationServer.Address)
		return
	}
}

func testNewDTOFromFileFileError(t *testing.T) {
	filesystem := fstest.MapFS{}
	path := "config.json"
	_, err := NewDTOFromFile(filesystem, path)
	if !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("expected %s but got %s", fs.ErrNotExist, err)
		return
	}
}

func testNewDTOFromFileBadContent(t *testing.T) {
	filesystem := fstest.MapFS{}
	path := "config.json"
	filesystem[path] = &fstest.MapFile{
		Data: configFileEmpty,
	}
	_, err := NewDTOFromFile(filesystem, path)
	if err == nil {
		t.Error("expected error from invalid file contents")
		return
	}
}

func TestNewDefaultConfig(t *testing.T) {
	tests := []struct {
		name string
		want *Config
	}{
		{
			name: "happypath",
			want: &Config{
				tlsCertPath: adminPublicCertPath,
				tlsKeyPath:  adminPrivateCertPath,
				adminNetAddress: net.TCPAddr{
					IP:   net.IPv4(127, 0, 0, 1),
					Port: DefaultDevAdministrationPort,
				},
				database: Database{
					Engine: DefaultAdministrationUseSqlite,
					DSN:    DefaultAdministrationDSN,
				},
				fileWriter: &file.FileWriter{},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := NewDevDefaultConfig(); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("NewDefaultConfig() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestConfig_Database(t *testing.T) {
	t.Run("happypath", func(t *testing.T) {
		c := newTestConfig()
		if !reflect.DeepEqual(c.Database(), databaseOK) {
			t.Errorf("expected %v but got %v", databaseOK, c.Database())
			return
		}
	})
}
