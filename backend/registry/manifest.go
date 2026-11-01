// Package registry is the content registry (baitbox) consumer: an HTTP client
// that fetches package archives, verifies them against the archive sha256 and the
// manifest content and per file hashes, and exposes their payload for installation
// into phishingclub entities.
package registry

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/phishingclub/phishingclub/safepath"
)

// ManifestFileName is the manifest file at the root of a package archive.
const ManifestFileName = "baitbox.json"

// Entry is one payload file and the hex sha256 of its bytes.
type Entry struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
}

// Variable is a declared substitution point. A required variable with no default
// must be supplied by the installer (an @@NAME@@ secret, typically).
type Variable struct {
	Name        string  `json:"name"`
	Type        string  `json:"type"`
	Description string  `json:"description,omitempty"`
	Required    bool    `json:"required"`
	Default     *string `json:"default,omitempty"`
	// Options is the allowed value set for an enum variable, in author order.
	Options []string `json:"options,omitempty"`
	// Pick, when set, names an entity kind the installer must choose from their own
	// existing entities. The chosen entity name becomes the value, which is later
	// resolved by name.
	Pick string `json:"pick,omitempty"`
}

// Dependency is a referenced package. In the living library model the consumer
// resolves the id to the registry current version and ignores any range.
type Dependency struct {
	ID    string `json:"id"`
	Range string `json:"range"`
}

// Manifest mirrors the baitbox archive manifest fields the consumer needs. Only
// the fields used for verification and installation are modelled.
type Manifest struct {
	Format        string       `json:"format"`
	ID            string       `json:"id"`
	Type          string       `json:"type"`
	Version       string       `json:"version"`
	MinAppVersion string       `json:"minAppVersion,omitempty"`
	Title         string       `json:"title,omitempty"`
	Description   string       `json:"description,omitempty"`
	Preview       string       `json:"preview,omitempty"`
	Dependencies  []Dependency `json:"dependencies,omitempty"`
	// ShareVariables (bundle only) is true, or absent meaning true, when a variable
	// of the same name across referenced packages is filled once and shared; false
	// when each referenced package collects its own value.
	ShareVariables *bool      `json:"shareVariables,omitempty"`
	Variables      []Variable `json:"variables,omitempty"`
	ContentHash    string     `json:"contentHash"`
	Entries        []Entry    `json:"entries"`
}

// ParseManifest reads a manifest from JSON bytes
func ParseManifest(b []byte) (*Manifest, error) {
	var m Manifest
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, err
	}
	return &m, nil
}

// maxDependencies caps how many packages a bundle may reference, so one install
// cannot fan out into an unbounded number of downloads.
const maxDependencies = 100

// knownTypes is the set of package types Bait Box publishes.
var knownTypes = map[string]bool{
	"email": true, "page": true, "asset": true, "attachment": true,
	"script": true, "proxy": true, "allowdeny": true, "remotebrowser": true,
	"reporttemplate": true, "domain": true, "smtp": true, "webhook": true,
	"apisender": true, "oauthprovider": true, "template": true, "bundle": true,
}

var (
	idRe      = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,63}/[a-z0-9][a-z0-9._-]{0,127}$`)
	versionRe = regexp.MustCompile(`^\d+\.\d+\.\d+(-[0-9A-Za-z.-]+)?$`)
	hexRe     = regexp.MustCompile(`^[0-9a-fA-F]{64}$`)
	// the same variable name rule Bait Box applies when a package is published.
	varNameRe = regexp.MustCompile(`^[A-Z_][A-Z0-9_]{0,63}$`)
)

// ValidVariableName reports whether name is a variable name as Bait Box
// publishes it, so it can be used in an @@NAME@@ token.
func ValidVariableName(name string) bool { return varNameRe.MatchString(name) }

// ValidVersion reports whether v is a semver string as Bait Box publishes it.
func ValidVersion(v string) bool { return versionRe.MatchString(v) }

// canonicalEntryPath reports whether p is a strict relative path inside the
// archive (see safepath.Check) and not the manifest itself.
func canonicalEntryPath(p string) bool {
	return p != ManifestFileName && safepath.Check(p) == nil
}

// Validate checks only the fields the consumer relies on.
func (m *Manifest) Validate() error {
	if !idRe.MatchString(m.ID) {
		return fmt.Errorf("the package id %q is not valid", m.ID)
	}
	if !knownTypes[m.Type] {
		return fmt.Errorf("the package type %q is not known", m.Type)
	}
	if !versionRe.MatchString(m.Version) {
		return fmt.Errorf("the version %q is not a semantic version", m.Version)
	}
	if !hexRe.MatchString(m.ContentHash) {
		return errors.New("the content hash is not valid")
	}
	if len(m.Dependencies) > maxDependencies {
		return fmt.Errorf("the package references %d packages and the limit is %d", len(m.Dependencies), maxDependencies)
	}
	seenDep := map[string]bool{}
	for _, d := range m.Dependencies {
		if !idRe.MatchString(d.ID) || d.ID == m.ID {
			return fmt.Errorf("the referenced package %q is not valid", d.ID)
		}
		if seenDep[d.ID] {
			return fmt.Errorf("the package %q is referenced more than once", d.ID)
		}
		seenDep[d.ID] = true
	}
	// a pick value must come from the operator, so a pick has no default and no
	// second declaration of the same name can supply one.
	seenVar := map[string]bool{}
	for _, v := range m.Variables {
		if !varNameRe.MatchString(v.Name) {
			return fmt.Errorf("it declares the variable name %q, which is not valid", v.Name)
		}
		if seenVar[v.Name] {
			return fmt.Errorf("it declares the variable %q more than once", v.Name)
		}
		seenVar[v.Name] = true
		if v.Pick != "" && v.Default != nil {
			return fmt.Errorf("it gives the pick variable %q a default value", v.Name)
		}
	}
	if len(m.Entries) == 0 {
		return errors.New("it lists no files")
	}
	seen := map[string]bool{}
	for _, e := range m.Entries {
		if !canonicalEntryPath(e.Path) {
			return fmt.Errorf("the file path %q is not valid", e.Path)
		}
		// entries that differ only by letter case would land on the same file on a
		// case insensitive file system.
		if seen[safepath.FoldKey(e.Path)] {
			return fmt.Errorf("the file %q is listed more than once", e.Path)
		}
		seen[safepath.FoldKey(e.Path)] = true
		if !hexRe.MatchString(e.SHA256) {
			return fmt.Errorf("the checksum of the file %q is not valid", e.Path)
		}
	}
	return nil
}

// Namespace returns the namespace part of the id ("namespace/name").
func (m *Manifest) Namespace() string {
	return IDNamespace(m.ID)
}

// IDNamespace returns the namespace part of a package id ("namespace/name").
func IDNamespace(id string) string {
	if i := strings.IndexByte(id, '/'); i >= 0 {
		return id[:i]
	}
	return ""
}

// PackageName returns the name part of the id.
func (m *Manifest) PackageName() string {
	if i := strings.IndexByte(m.ID, '/'); i >= 0 {
		return m.ID[i+1:]
	}
	return ""
}

// ComputeContentHash reproduces the baitbox content hash: entries sorted by path,
// each written as "path:sha256\n", hashed with sha256. The algorithm is identical
// to the producer so the two sides agree on a package's fingerprint.
func ComputeContentHash(entries []Entry) string {
	sorted := make([]Entry, len(entries))
	copy(sorted, entries)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Path < sorted[j].Path })
	h := sha256.New()
	for _, e := range sorted {
		h.Write([]byte(e.Path))
		h.Write([]byte{':'})
		h.Write([]byte(e.SHA256))
		h.Write([]byte{'\n'})
	}
	return hex.EncodeToString(h.Sum(nil))
}
