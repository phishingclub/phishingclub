package geoip

import (
	"encoding/json"
	"fmt"
	"net"
	"strings"
	"sync"

	"github.com/phishingclub/phishingclub/embedded"
)

// countryData represents the structure from embedded geoip files
type countryData struct {
	Code string   `json:"code"`
	Name string   `json:"name"`
	IPv4 []string `json:"ipv4"`
	IPv6 []string `json:"ipv6"`
}

// metadata represents the geoip metadata file
type Metadata struct {
	Generated    string   `json:"generated"`
	Source       string   `json:"source"`
	License      string   `json:"license"`
	Countries    int      `json:"countries"`
	CountryCodes []string `json:"country_codes"`
}

// ipRange represents a single IP range with its country code
type ipRange struct {
	network     *net.IPNet
	countryCode string
}

// GeoIP provides IP to country lookup functionality
type GeoIP struct {
	ranges   []ipRange
	metadata *Metadata
	mu       sync.RWMutex
}

var (
	instance *GeoIP
	once     sync.Once
	initErr  error
)

// Instance returns the singleton GeoIP instance
func Instance() (*GeoIP, error) {
	once.Do(func() {
		instance, initErr = New()
	})
	return instance, initErr
}

// New creates a new GeoIP instance by loading embedded data
func New() (*GeoIP, error) {
	geo := &GeoIP{
		ranges: make([]ipRange, 0),
	}

	// load metadata
	if err := geo.loadMetadata(); err != nil {
		return nil, fmt.Errorf("failed to load metadata: %w", err)
	}

	// load all country data
	if err := geo.loadAllCountries(); err != nil {
		return nil, fmt.Errorf("failed to load countries: %w", err)
	}

	return geo, nil
}

// loadMetadata loads the metadata.json file
func (g *GeoIP) loadMetadata() error {
	data, err := embedded.GeoIPData.ReadFile("geoip/metadata.json")
	if err != nil {
		return fmt.Errorf("failed to read metadata: %w", err)
	}

	var meta Metadata
	if err := json.Unmarshal(data, &meta); err != nil {
		return fmt.Errorf("failed to parse metadata: %w", err)
	}

	g.metadata = &meta
	return nil
}

// loadAllCountries loads all country IP ranges
func (g *GeoIP) loadAllCountries() error {
	if g.metadata == nil {
		return fmt.Errorf("metadata not loaded")
	}

	for _, code := range g.metadata.CountryCodes {
		if err := g.loadCountry(code); err != nil {
			// log warning but continue - some countries may not have data
			continue
		}
	}

	return nil
}

// loadCountry loads IP ranges for a single country
func (g *GeoIP) loadCountry(countryCode string) error {
	filename := fmt.Sprintf("geoip/%s.json", strings.ToLower(countryCode))
	data, err := embedded.GeoIPData.ReadFile(filename)
	if err != nil {
		return fmt.Errorf("failed to read %s: %w", countryCode, err)
	}

	var country countryData
	if err := json.Unmarshal(data, &country); err != nil {
		return fmt.Errorf("failed to parse %s: %w", countryCode, err)
	}

	// add all IPv4 ranges
	for _, cidr := range country.IPv4 {
		_, network, err := net.ParseCIDR(cidr)
		if err != nil {
			continue // skip invalid entries
		}
		g.ranges = append(g.ranges, ipRange{
			network:     network,
			countryCode: country.Code,
		})
	}

	// add all IPv6 ranges
	for _, cidr := range country.IPv6 {
		_, network, err := net.ParseCIDR(cidr)
		if err != nil {
			continue // skip invalid entries
		}
		g.ranges = append(g.ranges, ipRange{
			network:     network,
			countryCode: country.Code,
		})
	}

	return nil
}

// Lookup finds the country code for an IP address
// returns (countryCode, found)
func (g *GeoIP) Lookup(ipStr string) (string, bool) {
	g.mu.RLock()
	defer g.mu.RUnlock()

	ip := net.ParseIP(ipStr)
	if ip == nil {
		return "", false
	}

	// linear search through ranges
	// for better performance, consider using a trie or radix tree
	for _, r := range g.ranges {
		if r.network.Contains(ip) {
			return r.countryCode, true
		}
	}

	return "", false
}

// GetMetadata returns the GeoIP metadata
func (g *GeoIP) GetMetadata() *Metadata {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return g.metadata
}

// GetCountryCodes returns a list of all available country codes
func (g *GeoIP) GetCountryCodes() []string {
	g.mu.RLock()
	defer g.mu.RUnlock()
	if g.metadata == nil {
		return []string{}
	}
	return g.metadata.CountryCodes
}
