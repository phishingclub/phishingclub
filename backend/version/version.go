package version

import (
	_ "embed"
	"fmt"
	"strings"

	"github.com/phishingclub/phishingclub/build"
)

var (
	// overwritten in build
	version string
	// overwritten in build
	hash string
)

// Get returns the version
// version format is ENV-VERSION-GIT_COMIT
// ex. prod-1.2.3-fb198cd-pro
func Get() string {
	h := strings.TrimSpace(hash)
	v := strings.TrimSpace(version)
	if build.Flags.Production {
		return fmt.Sprintf("prod-%s-%s", v, h)
	}
	return "dev-version-commit"
}

// GetSemver returns the version such as 1.2.3
func GetSemver() string {
	return version
}

func Hash() string {
	return strings.TrimSpace(hash)
}
