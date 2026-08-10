package data

const (
	// LureURLModeQuery carries the recipient as a query parameter, for example
	// https://example.com/login?id=<uuid>.
	LureURLModeQuery = "query"
	// LureURLModePath carries the recipient as the last path segment, for example
	// https://example.com/login/4H7K9QM2XR3T. Survives being read aloud or sent
	// over SMS, where a query string with a UUID does not.
	LureURLModePath = "path"
)

// IsValidLureURLMode reports whether mode is a known lure URL mode.
func IsValidLureURLMode(mode string) bool {
	switch mode {
	case LureURLModeQuery, LureURLModePath:
		return true
	}
	return false
}
