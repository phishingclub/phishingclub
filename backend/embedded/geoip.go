package embedded

import (
	"embed"
)

// GeoIPData contains all embedded GeoIP country data files
//
//go:embed geoip/*.json
var GeoIPData embed.FS
