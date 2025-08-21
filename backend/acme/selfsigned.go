package acme

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"time"

	"github.com/go-errors/errors"
	"github.com/phishingclub/phishingclub/build"
	"go.uber.org/zap"
)

// Information is a struct for certificate information
type Information struct {
	CommonName    string
	Organization  []string
	Country       []string
	Province      []string
	Locality      []string
	StreetAddress []string
	PostalCode    []string
}

// NewInformation creates a new Information
func NewInformation(
	commonName string,
	organization []string,
	country []string,
	province []string,
	locality []string,
	streetAddress []string,
	postalCode []string,
) Information {
	return Information{
		Organization:  organization,
		Country:       country,
		Province:      province,
		Locality:      locality,
		StreetAddress: streetAddress,
		PostalCode:    postalCode,
	}
}

// NewInformationWithDefault creates a new Information with default values
func NewInformationWithDefault() Information {
	return NewInformation(
		"",
		[]string{""},
		[]string{""},
		[]string{""},
		[]string{""},
		[]string{""},
		[]string{""},
	)
}

// CreateSelfSignedCert creates a self signed certificate with provided hostnames
func CreateSelfSignedCert(
	logger *zap.SugaredLogger,
	info Information,
	hostnames []string,
	publicPath string,
	privatePath string,
) error {
	// Process hostnames into IP addresses and DNS names
	var ipAddresses []net.IP
	var dnsNames []string

	if !build.Flags.Production {
		ipAddresses = append(ipAddresses, net.IPv4(127, 0, 0, 1), net.IPv6loopback)
		dnsNames = append(dnsNames, "localhost")
	}

	for _, h := range hostnames {
		if ip := net.ParseIP(h); ip != nil {
			ipAddresses = append(ipAddresses, ip)
		} else {
			dnsNames = append(dnsNames, h)
		}
	}

	// Use info.CommonName if provided, otherwise use first hostname or "localhost"
	commonName := info.CommonName
	if commonName == "" || commonName == "127.0.0.1" {
		if len(hostnames) > 0 {
			commonName = hostnames[0]
		} else {
			commonName = "localhost"
		}
	}

	// Create certificate with appropriate SAN extensions
	serialNumberLimit := new(big.Int).Lsh(big.NewInt(1), 128)
	serialNumber, err := rand.Int(rand.Reader, serialNumberLimit)
	if err != nil {
		return errors.Errorf("failed to generate serial number: %s", err)
	}

	cert := &x509.Certificate{
		SerialNumber: serialNumber,
		Subject: pkix.Name{
			CommonName:    commonName,
			Organization:  info.Organization,
			Country:       info.Country,
			Province:      info.Province,
			Locality:      info.Locality,
			StreetAddress: info.StreetAddress,
			PostalCode:    info.PostalCode,
		},
		IPAddresses:           ipAddresses,
		DNSNames:              dnsNames,
		NotBefore:             time.Now(),
		NotAfter:              time.Now().AddDate(10, 0, 0),
		SubjectKeyId:          []byte{0, 0, 0, 0, 0},
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth, x509.ExtKeyUsageServerAuth},
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		BasicConstraintsValid: true,
	}

	certPrivKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return errors.Errorf("failed to generate private key: %s", err)
	}

	certBytes, err := x509.CreateCertificate(rand.Reader, cert, cert, &certPrivKey.PublicKey, certPrivKey)
	if err != nil {
		return errors.Errorf("failed to create certificate: %s", err)
	}

	// Create directories if they don't exist
	certDir := filepath.Dir(publicPath)
	if err := os.MkdirAll(certDir, 0750); err != nil {
		return errors.Errorf("failed to create certificate directory: %s", err)
	}

	keyDir := filepath.Dir(privatePath)
	if err := os.MkdirAll(keyDir, 0750); err != nil {
		return errors.Errorf("failed to create key directory: %s", err)
	}

	// Write certificate
	// #nosec
	certOut, err := os.Create(publicPath)
	if err != nil {
		return errors.Errorf("failed to open certificate file for writing: %s", err)
	}
	defer certOut.Close()

	if err := pem.Encode(certOut, &pem.Block{Type: "CERTIFICATE", Bytes: certBytes}); err != nil {
		return errors.Errorf("failed to write certificate: %s", err)
	}

	// Write private key
	// #nosec
	keyOut, err := os.OpenFile(privatePath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		return errors.Errorf("failed to open key file for writing: %s", err)
	}
	defer keyOut.Close()

	privBlock := &pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(certPrivKey),
	}

	if err := pem.Encode(keyOut, privBlock); err != nil {
		return errors.Errorf("failed to write private key: %s", err)
	}
	/*
		logger.Debugf("generated self-signed certificate",
			"certificate", publicPath,
			"key", privatePath,
			"common_name", commonName,
			"ip_addresses", ipAddresses,
			"dns_names", dnsNames,
		)
	*/

	return nil
}
