package ipdata

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/phishingclub/phishingclub/embedded"
)

// packageFormat is the format version this reader understands.
const packageFormat = 1

// FilePackage and FileEntries are the two files inside a downloaded package.
const (
	filePackage = "package.json"
	fileEntries = "entries.json"
)

// packageInfo mirrors package.json in a downloaded package.
type packageInfo struct {
	Format       int    `json:"format"`
	Name         string `json:"name"`
	Version      string `json:"version"`
	Created      string `json:"created"`
	Source       string `json:"source"`
	License      string `json:"license"`
	Entries      int    `json:"entries"`
	IPv4Prefixes int    `json:"ipv4_prefixes"`
	IPv6Prefixes int    `json:"ipv6_prefixes"`
	ContentHash  string `json:"content_hash"`
}

type geoEntry struct {
	Code string   `json:"code"`
	Name string   `json:"name"`
	IPv4 []string `json:"ipv4"`
	IPv6 []string `json:"ipv6"`
}

type asnEntry struct {
	ASN     uint32   `json:"asn"`
	Handle  string   `json:"handle"`
	Name    string   `json:"name"`
	Country string   `json:"country"`
	IPv4    []string `json:"ipv4"`
	IPv6    []string `json:"ipv6"`
}

// packageDir is where a downloaded package of the given kind is extracted.
func (s *Store) packageDir(kind string) string {
	return filepath.Join(s.dataDir, "ipdata", kind)
}

// PackageDir returns the directory a package of the given kind is installed to.
func (s *Store) PackageDir(kind string) string {
	return s.packageDir(kind)
}

// DataRoot returns the directory that holds all installed packages.
func (s *Store) DataRoot() string {
	return filepath.Join(s.dataDir, "ipdata")
}

// Validate checks that a directory holds a readable package of the kind, so a
// download can be rejected before it is moved into place.
func Validate(dir, kind string) error {
	_, entries, err := readPackage(dir, kind)
	if err != nil {
		return err
	}
	switch kind {
	case KindGeoIP:
		var l []geoEntry
		if err := json.Unmarshal(entries, &l); err != nil {
			return fmt.Errorf("parse %s: %w", fileEntries, err)
		}
		if len(l) == 0 {
			return fmt.Errorf("geoip package has no entries")
		}
	case KindASN:
		var l []asnEntry
		if err := json.Unmarshal(entries, &l); err != nil {
			return fmt.Errorf("parse %s: %w", fileEntries, err)
		}
		if len(l) == 0 {
			return fmt.Errorf("asn package has no entries")
		}
	default:
		return fmt.Errorf("unknown package kind %q", kind)
	}
	return nil
}

// readPackage reads and verifies a downloaded package directory.
func readPackage(dir, wantName string) (*packageInfo, []byte, error) {
	infoBytes, err := os.ReadFile(filepath.Join(dir, filePackage))
	if err != nil {
		return nil, nil, err
	}
	var info packageInfo
	if err := json.Unmarshal(infoBytes, &info); err != nil {
		return nil, nil, fmt.Errorf("parse %s: %w", filePackage, err)
	}
	if info.Format != packageFormat {
		return nil, nil, fmt.Errorf("package format %d, expected %d", info.Format, packageFormat)
	}
	if info.Name != wantName {
		return nil, nil, fmt.Errorf("package is %q, expected %q", info.Name, wantName)
	}
	entries, err := os.ReadFile(filepath.Join(dir, fileEntries))
	if err != nil {
		return nil, nil, err
	}
	sum := sha256.Sum256(entries)
	if hex.EncodeToString(sum[:]) != info.ContentHash {
		return nil, nil, fmt.Errorf("content hash does not match %s", filePackage)
	}
	return &info, entries, nil
}

// loadGeoPackage builds the country dataset from a downloaded package.
func (s *Store) loadGeoPackage() (*geoDataset, error) {
	info, entries, err := readPackage(s.packageDir(KindGeoIP), KindGeoIP)
	if err != nil {
		return nil, err
	}
	var list []geoEntry
	if err := json.Unmarshal(entries, &list); err != nil {
		return nil, fmt.Errorf("parse %s: %w", fileEntries, err)
	}
	ds := buildGeo(list)
	ds.info = Info{
		Name: KindGeoIP, Source: info.Source, Version: info.Version, Created: info.Created,
		License: info.License, Entries: len(list),
		IPv4Prefixes: len(ds.idx.v4), IPv6Prefixes: len(ds.idx.v6),
		ContentHash: info.ContentHash, Downloaded: true,
	}
	return ds, nil
}

// loadASNPackage builds the ASN dataset from a downloaded package.
func (s *Store) loadASNPackage() (*asnDataset, error) {
	info, entries, err := readPackage(s.packageDir(KindASN), KindASN)
	if err != nil {
		return nil, err
	}
	var list []asnEntry
	if err := json.Unmarshal(entries, &list); err != nil {
		return nil, fmt.Errorf("parse %s: %w", fileEntries, err)
	}
	ds := buildASN(list)
	ds.info = Info{
		Name: KindASN, Source: info.Source, Version: info.Version, Created: info.Created,
		License: info.License, Entries: len(list),
		IPv4Prefixes: len(ds.idx.v4), IPv6Prefixes: len(ds.idx.v6),
		ContentHash: info.ContentHash, Downloaded: true,
	}
	return ds, nil
}

// embeddedMetadata mirrors the embedded geoip metadata.json.
type embeddedMetadata struct {
	Generated    string   `json:"generated"`
	Source       string   `json:"source"`
	License      string   `json:"license"`
	CountryCodes []string `json:"country_codes"`
}

// embeddedCountry mirrors an embedded per country file.
type embeddedCountry struct {
	Code string   `json:"code"`
	Name string   `json:"name"`
	IPv4 []string `json:"ipv4"`
	IPv6 []string `json:"ipv6"`
}

// loadEmbeddedGeo builds the country dataset from the data embedded in the
// binary. This keeps the current embedded format untouched.
func (s *Store) loadEmbeddedGeo() (*geoDataset, error) {
	metaBytes, err := embedded.GeoIPData.ReadFile("geoip/metadata.json")
	if err != nil {
		return nil, err
	}
	var meta embeddedMetadata
	if err := json.Unmarshal(metaBytes, &meta); err != nil {
		return nil, fmt.Errorf("parse embedded metadata: %w", err)
	}
	list := make([]geoEntry, 0, len(meta.CountryCodes))
	for _, code := range meta.CountryCodes {
		name := fmt.Sprintf("geoip/%s.json", strings.ToLower(code))
		data, err := embedded.GeoIPData.ReadFile(name)
		if err != nil {
			continue
		}
		var c embeddedCountry
		if err := json.Unmarshal(data, &c); err != nil {
			continue
		}
		list = append(list, geoEntry{Code: c.Code, Name: c.Name, IPv4: c.IPv4, IPv6: c.IPv6})
	}
	ds := buildGeo(list)
	ds.info = Info{
		Name: KindGeoIP, Source: meta.Source, Version: "embedded", Created: meta.Generated,
		License: meta.License, Entries: len(list),
		IPv4Prefixes: len(ds.idx.v4), IPv6Prefixes: len(ds.idx.v6),
		Downloaded: false,
	}
	return ds, nil
}

// buildGeo turns country entries into a sorted dataset.
func buildGeo(list []geoEntry) *geoDataset {
	sort.Slice(list, func(i, j int) bool { return list[i].Code < list[j].Code })
	ds := &geoDataset{
		codes: make([]string, len(list)),
		names: make([]string, len(list)),
	}
	for i, e := range list {
		ds.codes[i] = e.Code
		ds.names[i] = e.Name
		for _, c := range e.IPv4 {
			ds.idx.add(c, uint32(i))
		}
		for _, c := range e.IPv6 {
			ds.idx.add(c, uint32(i))
		}
	}
	ds.idx.sortRecords()
	return ds
}

// buildASN turns ASN entries into a sorted dataset.
func buildASN(list []asnEntry) *asnDataset {
	sort.Slice(list, func(i, j int) bool { return list[i].ASN < list[j].ASN })
	ds := &asnDataset{
		nums:      make([]uint32, len(list)),
		handles:   make([]string, len(list)),
		names:     make([]string, len(list)),
		countries: make([]string, len(list)),
		byNum:     make(map[uint32]int, len(list)),
	}
	for i, e := range list {
		ds.nums[i] = e.ASN
		ds.handles[i] = e.Handle
		ds.names[i] = e.Name
		ds.countries[i] = e.Country
		ds.byNum[e.ASN] = i
		for _, c := range e.IPv4 {
			ds.idx.add(c, uint32(i))
		}
		for _, c := range e.IPv6 {
			ds.idx.add(c, uint32(i))
		}
	}
	ds.idx.sortRecords()
	return ds
}
