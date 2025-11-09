package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	ipverseBaseURL = "https://raw.githubusercontent.com/ipverse/rir-ip/master/country"
	outputDir      = "../embedded/geoip"
	maxConcurrent  = 10
)

// countryData represents the structure from ipverse aggregated.json files
type countryData struct {
	Country          string   `json:"country"`
	CountryCode      string   `json:"country-code"`
	DelegationStatus []string `json:"delegation-status"`
	Mode             string   `json:"mode"`
	Subnets          struct {
		IPv4 []string `json:"ipv4"`
		IPv6 []string `json:"ipv6"`
	} `json:"subnets"`
}

// optimizedData is our simplified format for embedding
type optimizedData struct {
	Code string   `json:"code"`
	Name string   `json:"name"`
	IPv4 []string `json:"ipv4"`
	IPv6 []string `json:"ipv6"`
}

// metadata tracks what countries we have
type metadata struct {
	Generated    string   `json:"generated"`
	Source       string   `json:"source"`
	License      string   `json:"license"`
	Countries    int      `json:"countries"`
	CountryCodes []string `json:"country_codes"`
}

func main() {
	fmt.Println("Fetching GeoIP data from ipverse/rir-ip...")
	fmt.Println("   Source: https://github.com/ipverse/rir-ip")
	fmt.Println("   License: CC0-1.0 (Public Domain)")
	fmt.Println()

	// create output directory
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		fatal("failed to create output directory: %v", err)
	}

	// get list of all country codes
	countries, err := getCountryCodes()
	if err != nil {
		fatal("failed to get country codes: %v", err)
	}

	fmt.Printf("Found %d countries to fetch.\n\n", len(countries))

	// fetch country data concurrently
	results := fetchAllCountries(countries)

	// save results
	saved := saveResults(results)

	// create metadata file
	createMetadata(saved)

	fmt.Println()
	fmt.Printf("Successfully fetched and saved data for %d countries\n", len(saved))
	fmt.Printf("Data saved to: %s\n", outputDir)
	fmt.Println()
}

// getCountryCodes returns list of ISO country codes to fetch
// using a curated list of all valid ISO 3166-1 alpha-2 codes
func getCountryCodes() ([]string, error) {
	// all ISO 3166-1 alpha-2 country codes (lowercase for ipverse URLs)
	codes := []string{
		"ad", "ae", "af", "ag", "ai", "al", "am", "ao", "aq", "ar", "as", "at",
		"au", "aw", "ax", "az", "ba", "bb", "bd", "be", "bf", "bg", "bh", "bi",
		"bj", "bl", "bm", "bn", "bo", "bq", "br", "bs", "bt", "bv", "bw", "by",
		"bz", "ca", "cc", "cd", "cf", "cg", "ch", "ci", "ck", "cl", "cm", "cn",
		"co", "cr", "cu", "cv", "cw", "cx", "cy", "cz", "de", "dj", "dk", "dm",
		"do", "dz", "ec", "ee", "eg", "eh", "er", "es", "et", "fi", "fj", "fk",
		"fm", "fo", "fr", "ga", "gb", "gd", "ge", "gf", "gg", "gh", "gi", "gl",
		"gm", "gn", "gp", "gq", "gr", "gs", "gt", "gu", "gw", "gy", "hk", "hm",
		"hn", "hr", "ht", "hu", "id", "ie", "il", "im", "in", "io", "iq", "ir",
		"is", "it", "je", "jm", "jo", "jp", "ke", "kg", "kh", "ki", "km", "kn",
		"kp", "kr", "kw", "ky", "kz", "la", "lb", "lc", "li", "lk", "lr", "ls",
		"lt", "lu", "lv", "ly", "ma", "mc", "md", "me", "mf", "mg", "mh", "mk",
		"ml", "mm", "mn", "mo", "mp", "mq", "mr", "ms", "mt", "mu", "mv", "mw",
		"mx", "my", "mz", "na", "nc", "ne", "nf", "ng", "ni", "nl", "no", "np",
		"nr", "nu", "nz", "om", "pa", "pe", "pf", "pg", "ph", "pk", "pl", "pm",
		"pn", "pr", "ps", "pt", "pw", "py", "qa", "re", "ro", "rs", "ru", "rw",
		"sa", "sb", "sc", "sd", "se", "sg", "sh", "si", "sj", "sk", "sl", "sm",
		"sn", "so", "sr", "ss", "st", "sv", "sx", "sy", "sz", "tc", "td", "tf",
		"tg", "th", "tj", "tk", "tl", "tm", "tn", "to", "tr", "tt", "tv", "tw",
		"tz", "ua", "ug", "um", "us", "uy", "uz", "va", "vc", "ve", "vg", "vi",
		"vn", "vu", "wf", "ws", "ye", "yt", "za", "zm", "zw",
	}

	return codes, nil
}

// fetchAllCountries fetches data for all countries concurrently
func fetchAllCountries(countries []string) map[string]*optimizedData {
	results := make(map[string]*optimizedData)
	var mu sync.Mutex

	// semaphore to limit concurrent requests
	sem := make(chan struct{}, maxConcurrent)
	var wg sync.WaitGroup

	for _, code := range countries {
		wg.Add(1)
		go func(cc string) {
			defer wg.Done()

			sem <- struct{}{}        // acquire
			defer func() { <-sem }() // release

			data, err := fetchCountry(cc)
			if err != nil {
				// silently skip countries that don't exist in ipverse
				// (not all ISO codes have allocated IPs)
				return
			}

			mu.Lock()
			results[cc] = data
			mu.Unlock()

			fmt.Printf("  ✓ %s (%s) - %d IPv4, %d IPv6 ranges\n",
				strings.ToUpper(cc), data.Name, len(data.IPv4), len(data.IPv6))
		}(code)
	}

	wg.Wait()
	return results
}

// fetchCountry fetches and optimizes data for a single country
func fetchCountry(code string) (*optimizedData, error) {
	url := fmt.Sprintf("%s/%s/aggregated.json", ipverseBaseURL, code)

	resp, err := http.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == 404 {
		return nil, fmt.Errorf("country not found")
	}

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("http %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var raw countryData
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, err
	}

	// convert to optimized format
	optimized := &optimizedData{
		Code: strings.ToUpper(raw.CountryCode),
		Name: raw.Country,
		IPv4: raw.Subnets.IPv4,
		IPv6: raw.Subnets.IPv6,
	}

	// handle nil slices
	if optimized.IPv4 == nil {
		optimized.IPv4 = []string{}
	}
	if optimized.IPv6 == nil {
		optimized.IPv6 = []string{}
	}

	return optimized, nil
}

// saveResults saves all country data to individual files
func saveResults(results map[string]*optimizedData) []string {
	var saved []string

	for code, data := range results {
		filename := filepath.Join(outputDir, fmt.Sprintf("%s.json", strings.ToLower(code)))

		jsonData, err := json.MarshalIndent(data, "", "  ")
		if err != nil {
			fmt.Printf("  ✗ failed to marshal %s: %v\n", code, err)
			continue
		}

		if err := os.WriteFile(filename, jsonData, 0644); err != nil {
			fmt.Printf("  ✗ failed to write %s: %v\n", code, err)
			continue
		}

		saved = append(saved, strings.ToUpper(code))
	}

	return saved
}

// createMetadata creates a metadata file with info about the dataset
func createMetadata(countries []string) {
	sort.Strings(countries)

	meta := metadata{
		Generated:    time.Now().UTC().Format(time.RFC3339),
		Source:       "https://github.com/ipverse/rir-ip",
		License:      "CC0-1.0 (Public Domain)",
		Countries:    len(countries),
		CountryCodes: countries,
	}

	jsonData, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		fmt.Printf("failed to create metadata: %v\n", err)
		return
	}

	filename := filepath.Join(outputDir, "metadata.json")
	if err := os.WriteFile(filename, jsonData, 0644); err != nil {
		fmt.Printf("failed to write metadata: %v\n", err)
		return
	}

	fmt.Printf("\nMetadata saved to: %s\n", filename)
}

func fatal(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "❌ Error: "+format+"\n", args...)
	os.Exit(1)
}
