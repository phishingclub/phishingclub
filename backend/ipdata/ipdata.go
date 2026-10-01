// Package ipdata loads the IP to country and IP to ASN data used by the
// allow and deny filters. It reads two sources: the country data embedded in
// the binary, and packages the operator downloads into the data directory. A
// downloaded package wins over the embedded copy. ASN data only exists as a
// download.
//
// Prefixes are held in memory as fixed compact records and looked up by
// longest prefix. The store can be rebuilt at runtime after a download without
// a restart.
package ipdata

import (
	"encoding/binary"
	"fmt"
	"net/netip"
	"sort"
	"strings"
	"sync"

	"go.uber.org/zap"
)

const (
	// KindGeoIP is the country data package name.
	KindGeoIP = "geoip"
	// KindASN is the autonomous system data package name.
	KindASN = "asn"
)

// rec4 is one IPv4 prefix. val indexes into a dataset value table.
type rec4 struct {
	addr uint32
	bits uint8
	val  uint32
}

// rec6 is one IPv6 prefix. val indexes into a dataset value table.
type rec6 struct {
	addr [16]byte
	bits uint8
	val  uint32
}

// index holds the sorted prefixes of one dataset for lookup.
type index struct {
	v4 []rec4
	v6 []rec6
}

// add parses a CIDR, masks off host bits and appends it under the value.
// Bad or reserved input is skipped. It returns whether the prefix was kept.
func (x *index) add(cidr string, val uint32) bool {
	p, err := netip.ParsePrefix(strings.TrimSpace(cidr))
	if err != nil {
		return false
	}
	p = p.Masked()
	a := p.Addr()
	if a.Is4() {
		b := a.As4()
		x.v4 = append(x.v4, rec4{binary.BigEndian.Uint32(b[:]), uint8(p.Bits()), val})
		return true
	}
	if a.Is6() && !a.Is4In6() {
		x.v6 = append(x.v6, rec6{a.As16(), uint8(p.Bits()), val})
		return true
	}
	return false
}

// sortRecords orders the prefixes by address then prefix length so lookup can
// binary search each length.
func (x *index) sortRecords() {
	sort.Slice(x.v4, func(i, j int) bool {
		if x.v4[i].addr != x.v4[j].addr {
			return x.v4[i].addr < x.v4[j].addr
		}
		return x.v4[i].bits < x.v4[j].bits
	})
	sort.Slice(x.v6, func(i, j int) bool {
		if c := byteCmp(x.v6[i].addr, x.v6[j].addr); c != 0 {
			return c < 0
		}
		return x.v6[i].bits < x.v6[j].bits
	})
}

// lookup returns the value indexes of every prefix that matches the address at
// the longest matching prefix length. More than one is returned when several
// entries announce the same prefix, which happens with ASN data.
func (x *index) lookup(ip netip.Addr) []uint32 {
	if ip.Is4() {
		b := ip.As4()
		return x.lookup4(binary.BigEndian.Uint32(b[:]))
	}
	if ip.Is4In6() {
		b := ip.Unmap().As4()
		return x.lookup4(binary.BigEndian.Uint32(b[:]))
	}
	return x.lookup6(ip.As16())
}

func (x *index) lookup4(ip uint32) []uint32 {
	for bits := 32; bits >= 0; bits-- {
		var mask uint32 = 0xffffffff
		if bits < 32 {
			mask <<= uint(32 - bits)
		}
		if bits == 0 {
			mask = 0
		}
		masked := ip & mask
		lo := sort.Search(len(x.v4), func(i int) bool { return x.v4[i].addr >= masked })
		var vals []uint32
		for i := lo; i < len(x.v4) && x.v4[i].addr == masked; i++ {
			if x.v4[i].bits == uint8(bits) {
				vals = append(vals, x.v4[i].val)
			}
		}
		if len(vals) > 0 {
			return vals
		}
	}
	return nil
}

func (x *index) lookup6(ip [16]byte) []uint32 {
	for bits := 128; bits >= 0; bits-- {
		masked := maskV6(ip, bits)
		lo := sort.Search(len(x.v6), func(i int) bool { return byteCmp(x.v6[i].addr, masked) >= 0 })
		var vals []uint32
		for i := lo; i < len(x.v6) && x.v6[i].addr == masked; i++ {
			if x.v6[i].bits == uint8(bits) {
				vals = append(vals, x.v6[i].val)
			}
		}
		if len(vals) > 0 {
			return vals
		}
	}
	return nil
}

func maskV6(a [16]byte, bits int) [16]byte {
	var out [16]byte
	full := bits / 8
	rem := bits % 8
	copy(out[:full], a[:full])
	if rem > 0 && full < 16 {
		out[full] = a[full] & (byte(0xff) << uint(8-rem))
	}
	return out
}

func byteCmp(a, b [16]byte) int {
	for i := 0; i < 16; i++ {
		if a[i] != b[i] {
			if a[i] < b[i] {
				return -1
			}
			return 1
		}
	}
	return 0
}

// Info describes a loaded dataset for the status endpoint.
type Info struct {
	Name         string `json:"name"`
	Source       string `json:"source"`
	Version      string `json:"version"`
	Created      string `json:"created"`
	License      string `json:"license"`
	Entries      int    `json:"entries"`
	IPv4Prefixes int    `json:"ipv4Prefixes"`
	IPv6Prefixes int    `json:"ipv6Prefixes"`
	ContentHash  string `json:"contentHash"`
	Downloaded   bool   `json:"downloaded"`
}

// geoDataset maps prefixes to country codes.
type geoDataset struct {
	idx   index
	codes []string
	names []string
	info  Info
}

// asnDataset maps prefixes to autonomous systems.
type asnDataset struct {
	idx       index
	nums      []uint32
	handles   []string
	names     []string
	countries []string
	byNum     map[uint32]int
	info      Info
}

// Store holds the loaded datasets and swaps them safely on reload.
type Store struct {
	mu      sync.RWMutex
	dataDir string
	logger  *zap.SugaredLogger
	geo     *geoDataset
	asn     *asnDataset
}

var std *Store

// Init loads the datasets from the embedded data and the data directory and
// installs the package level store. It is called once at startup. A failure to
// load a package is logged and leaves that dataset empty rather than stopping
// the server.
func Init(dataDir string, logger *zap.SugaredLogger) *Store {
	s := &Store{dataDir: dataDir, logger: logger}
	s.reloadGeo()
	s.reloadASN()
	std = s
	return s
}

// Get returns the package level store. Its methods are safe to call on a nil
// store and on empty datasets.
func Get() *Store { return std }

// reloadGeo builds the country dataset from a downloaded package when present,
// otherwise from the embedded data.
func (s *Store) reloadGeo() {
	if ds, err := s.loadGeoPackage(); err == nil {
		s.setGeo(ds)
		return
	} else if s.logger != nil {
		s.logger.Debugw("no downloaded geoip package, using embedded", "error", err)
	}
	ds, err := s.loadEmbeddedGeo()
	if err != nil {
		if s.logger != nil {
			s.logger.Errorw("failed to load embedded geoip data", "error", err)
		}
		return
	}
	s.setGeo(ds)
}

// reloadASN builds the ASN dataset from a downloaded package. There is no
// embedded fallback, so a missing package leaves ASN lookups empty.
func (s *Store) reloadASN() {
	ds, err := s.loadASNPackage()
	if err != nil {
		s.setASN(nil)
		if s.logger != nil {
			s.logger.Debugw("no downloaded asn package", "error", err)
		}
		return
	}
	s.setASN(ds)
}

func (s *Store) setGeo(ds *geoDataset) {
	s.mu.Lock()
	s.geo = ds
	s.mu.Unlock()
}

func (s *Store) setASN(ds *asnDataset) {
	s.mu.Lock()
	s.asn = ds
	s.mu.Unlock()
}

// LookupCountry returns the country code for an IP and whether one was found.
func (s *Store) LookupCountry(ipStr string) (string, bool) {
	if s == nil {
		return "", false
	}
	ip, err := netip.ParseAddr(ipStr)
	if err != nil {
		return "", false
	}
	s.mu.RLock()
	geo := s.geo
	s.mu.RUnlock()
	if geo == nil {
		return "", false
	}
	vals := geo.idx.lookup(ip)
	if len(vals) == 0 {
		return "", false
	}
	return geo.codes[vals[0]], true
}

// LookupASNs returns every autonomous system number that announces the longest
// prefix containing the IP. Usually one, sometimes several.
func (s *Store) LookupASNs(ipStr string) []uint32 {
	if s == nil {
		return nil
	}
	ip, err := netip.ParseAddr(ipStr)
	if err != nil {
		return nil
	}
	s.mu.RLock()
	asn := s.asn
	s.mu.RUnlock()
	if asn == nil {
		return nil
	}
	vals := asn.idx.lookup(ip)
	if len(vals) == 0 {
		return nil
	}
	out := make([]uint32, 0, len(vals))
	for _, v := range vals {
		out = append(out, asn.nums[v])
	}
	return out
}

// LookupASNDetails returns the details of every ASN that announces the longest
// prefix containing the IP. Used by the ASN lookup tool.
func (s *Store) LookupASNDetails(ipStr string) []ASN {
	nums := s.LookupASNs(ipStr)
	if len(nums) == 0 {
		return nil
	}
	return s.ResolveASNs(nums)
}

// Country is one entry of the country list.
type Country struct {
	Code string `json:"code"`
	Name string `json:"name"`
}

// Countries returns the available country codes and names, sorted by code.
func (s *Store) Countries() []Country {
	if s == nil {
		return nil
	}
	s.mu.RLock()
	geo := s.geo
	s.mu.RUnlock()
	if geo == nil {
		return nil
	}
	out := make([]Country, len(geo.codes))
	for i := range geo.codes {
		out[i] = Country{Code: geo.codes[i], Name: geo.names[i]}
	}
	return out
}

// CountryCodes returns just the available country codes, sorted.
func (s *Store) CountryCodes() []string {
	if s == nil {
		return nil
	}
	s.mu.RLock()
	geo := s.geo
	s.mu.RUnlock()
	if geo == nil {
		return nil
	}
	out := make([]string, len(geo.codes))
	copy(out, geo.codes)
	return out
}

// ASN is one entry of an ASN search or resolve result.
type ASN struct {
	ASN     uint32 `json:"asn"`
	Handle  string `json:"handle"`
	Name    string `json:"name"`
	Country string `json:"country"`
}

// ASNLoaded reports whether ASN data is currently loaded and usable.
func (s *Store) ASNLoaded() bool {
	if s == nil {
		return false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.asn != nil
}

// HasASN reports whether the ASN exists in the loaded dataset.
func (s *Store) HasASN(num uint32) bool {
	if s == nil {
		return false
	}
	s.mu.RLock()
	asn := s.asn
	s.mu.RUnlock()
	if asn == nil {
		return false
	}
	_, ok := asn.byNum[num]
	return ok
}

// ResolveASNs returns the details of the given ASN numbers that exist. Numbers
// that are absent from the dataset are left out, which lets the caller show a
// warning for orphaned filter entries.
func (s *Store) ResolveASNs(nums []uint32) []ASN {
	if s == nil {
		return nil
	}
	s.mu.RLock()
	asn := s.asn
	s.mu.RUnlock()
	if asn == nil {
		return nil
	}
	out := []ASN{}
	for _, n := range nums {
		if i, ok := asn.byNum[n]; ok {
			out = append(out, ASN{ASN: asn.nums[i], Handle: asn.handles[i], Name: asn.names[i], Country: asn.countries[i]})
		}
	}
	return out
}

// SearchASN returns ASNs matching the query by number, name or handle, up to
// limit results. Matches are ranked so exact and prefix matches come before a
// match in the middle of a name, so a query like "M247" lists the M247 systems
// first.
func (s *Store) SearchASN(query string, limit int) []ASN {
	if s == nil {
		return nil
	}
	s.mu.RLock()
	asn := s.asn
	s.mu.RUnlock()
	if asn == nil || query == "" {
		return nil
	}
	if limit <= 0 || limit > 100 {
		limit = 25
	}
	q := strings.ToLower(strings.TrimSpace(query))

	// nums are stored in ascending ASN order (see newASNDataset), so entries
	// are seen smallest first. Collect at most limit per score bucket: the
	// first limit in a bucket are already its smallest ASNs, so no later entry
	// can displace them. This keeps the work bounded so a broad query over a
	// large dataset never allocates or sorts the whole match set.
	var buckets [3][]ASN
	for i := range asn.nums {
		numStr := fmt.Sprintf("%d", asn.nums[i])
		name := strings.ToLower(asn.names[i])
		handle := strings.ToLower(asn.handles[i])

		score := -1
		switch {
		case numStr == q || handle == q || name == q:
			score = 0
		case strings.HasPrefix(numStr, q) || strings.HasPrefix(handle, q) || strings.HasPrefix(name, q):
			score = 1
		case strings.Contains(name, q) || strings.Contains(handle, q):
			score = 2
		}
		if score < 0 || len(buckets[score]) >= limit {
			continue
		}
		buckets[score] = append(buckets[score], ASN{
			ASN:     asn.nums[i],
			Handle:  asn.handles[i],
			Name:    asn.names[i],
			Country: asn.countries[i],
		})
		// once the best bucket is full, every remaining entry has a larger ASN
		// and cannot outrank what is already collected, so the scan can stop
		if len(buckets[0]) >= limit {
			break
		}
	}

	// best score first, each bucket already in ascending ASN order
	out := make([]ASN, 0, limit)
	for score := range buckets {
		for _, a := range buckets[score] {
			if len(out) >= limit {
				return out
			}
			out = append(out, a)
		}
	}
	return out
}

// Status returns the state of both packages for the settings screen.
func (s *Store) Status() map[string]Info {
	out := map[string]Info{}
	if s == nil {
		return out
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.geo != nil {
		out[KindGeoIP] = s.geo.info
	}
	if s.asn != nil {
		out[KindASN] = s.asn.info
	}
	return out
}

// Reload rebuilds one dataset from disk after a download or a removal.
func (s *Store) Reload(kind string) {
	if s == nil {
		return
	}
	switch kind {
	case KindGeoIP:
		s.reloadGeo()
	case KindASN:
		s.reloadASN()
	}
}
