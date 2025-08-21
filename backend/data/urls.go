package data

import "github.com/phishingclub/phishingclub/build"

// GetCrmURL returns the URL for the CRM system depending on the environment
func GetCrmURL() string {
	if build.Flags.Production {
		return "https://user.phishing.club"
	}
	return "https://crm:8009"
}
