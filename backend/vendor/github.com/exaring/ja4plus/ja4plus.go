package ja4plus

import (
	"crypto/sha256"
	"crypto/tls"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"slices"
	"strings"
)

// greaseFilter returns true if the provided value is a GREASE entry as defined in
// https://www.rfc-editor.org/rfc/rfc8701.html
func greaseFilter(suite uint16) bool {
	return suite&0x000F == 0x000A && // low word is 0x*A
		suite>>8 == (suite&0x00FF) // high word is equal to low word
}

// JA4 generates a JA4 fingerprint from the given [tls.ClientHelloInfo].
// It extracts TLS Version, Cipher Suites, Extensions, and ALPN Protocols.
func JA4(hello *tls.ClientHelloInfo) string {
	out := make([]byte, 0, 36)

	// Determine protocol type based on the network type
	if hello.Conn != nil {
		switch hello.Conn.LocalAddr().Network() {
		case "udp", "sctp":
			out = append(out, 'd')
		case "quic":
			out = append(out, 'q')
		default:
			out = append(out, 't')
		}
	} else {
		out = append(out, 't')
	}

	// Extract TLS version
	supportedVersions := slices.DeleteFunc(slices.Sorted(slices.Values(hello.SupportedVersions)), greaseFilter)
	switch supportedVersions[len(supportedVersions)-1] {
	case tls.VersionTLS10:
		out = append(out, '1', '0')
	case tls.VersionTLS11:
		out = append(out, '1', '1')
	case tls.VersionTLS12:
		out = append(out, '1', '2')
	case tls.VersionTLS13:
		out = append(out, '1', '3')
	case tls.VersionSSL30: // deprecated, but still seen in the wild
		out = append(out, 's', '3')
	case 0x0002: // unsupported by go; still seen in the wild
		out = append(out, 's', '2')
	case 0xfeff: // DTLS 1.0
		out = append(out, 'd', '1')
	case 0xfefd: // DTLS 1.2
		out = append(out, 'd', '2')
	case 0xfefc: // DTLS 1.3
		out = append(out, 'd', '3')
	default:
		out = append(out, '0', '0')
	}

	// Check for presence of SNI
	if hello.ServerName != "" {
		out = append(out, 'd')
	} else {
		out = append(out, 'i')
	}

	// Count cipher suites; copy to avoid modifying the original
	filteredCipherSuites := slices.DeleteFunc(slices.Clone(hello.CipherSuites), greaseFilter)
	out = fmt.Appendf(out, "%02d", min(len(filteredCipherSuites), 99))

	// Count extensions; copy to avoid modifying the original
	filteredExtensions := slices.DeleteFunc(slices.Clone(hello.Extensions), greaseFilter)
	out = fmt.Appendf(out, "%02d", min(len(filteredExtensions), 99))

	// Extract first ALPN value
	var firstALPN string
	for _, proto := range hello.SupportedProtos {
		// Protocols are tecnically strings, but grease values are 2-byte non-printable, so we convert.
		// see: https://www.iana.org/assignments/tls-extensiontype-values/tls-extensiontype-values.xhtml#alpn-protocol-ids
		if len(proto) >= 2 && !greaseFilter(binary.BigEndian.Uint16([]byte(proto[:2]))) {
			firstALPN = proto
			break
		}
	}
	if firstALPN != "" {
		out = append(out, firstALPN[0], firstALPN[len(firstALPN)-1])
	} else {
		out = append(out, '0', '0')
	}

	out = append(out, '_')

	out = hex.AppendEncode(out, cipherSuiteHash(filteredCipherSuites))

	out = append(out, '_')

	out = hex.AppendEncode(out, extensionHash(filteredExtensions, hello.SignatureSchemes))

	return string(out)
}

// cipherSuiteHash computes the truncated SHA256 of sorted cipher suites.
// The input must be filtered for GREASE values.
// The return value is an unencoded byte slice of the hash.
func cipherSuiteHash(filteredCipherSuites []uint16) []byte {
	if len(filteredCipherSuites) > 0 {
		slices.Sort(filteredCipherSuites)
		cipherSuiteList := make([]string, 0, len(filteredCipherSuites))
		for _, suite := range filteredCipherSuites {
			cipherSuiteList = append(cipherSuiteList, fmt.Sprintf("%04x", suite))
		}
		cipherSuiteHash := sha256.Sum256([]byte(strings.Join(cipherSuiteList, ",")))
		return cipherSuiteHash[:6]
	} else {
		return []byte{0, 0, 0, 0, 0, 0}
	}
}

// extensionHash computes the truncated SHA256 of sorted and filtered extensions and unsorted signature algorithms.
// The provided extensions must be filtered for GREASE values.
// It sorts the provided extensions in-place.
// The return value is an unencoded byte slice of the hash.
func extensionHash(filteredExtensions []uint16, signatureSchemes []tls.SignatureScheme) []byte {
	slices.Sort(filteredExtensions)
	extensionsList := make([]string, 0, len(filteredExtensions))
	for _, ext := range filteredExtensions {
		// SNI and ALPN are counted above, but MUST be ignored for the hash.
		if ext == 0x0000 /* SNI */ || ext == 0x0010 /* ALPN */ {
			continue
		}
		extensionsList = append(extensionsList, fmt.Sprintf("%04x", ext))
	}
	if len(extensionsList) == 0 {
		return []byte{0, 0, 0, 0, 0, 0}
	}

	extensionsListRendered := strings.Join(extensionsList, ",")
	if len(signatureSchemes) > 0 {
		signatureSchemeList := make([]string, 0, len(signatureSchemes))
		for _, sig := range signatureSchemes {
			if greaseFilter(uint16(sig)) {
				continue
			}
			signatureSchemeList = append(signatureSchemeList, fmt.Sprintf("%04x", uint16(sig)))
		}
		extensionsListRendered += "_" + strings.Join(signatureSchemeList, ",")
	}
	extensionsHash := sha256.Sum256([]byte(extensionsListRendered))
	return extensionsHash[:6]
}
