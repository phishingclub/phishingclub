package vo

import (
	"encoding/json"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/go-errors/errors"

	"github.com/phishingclub/phishingclub/validate"
)

// IANA zone names are letters, digits, '_', '+', '-' and '/' separated parts,
// e.g. "Europe/Copenhagen", "America/Argentina/Buenos_Aires" or "Etc/GMT+5".
// No dots, so a name can never point outside the zone database.
var campaignTimeZonePattern = regexp.MustCompile(`^[A-Za-z0-9_+\-]+(/[A-Za-z0-9_+\-]+){0,3}$`)

// the IANA regions a zone name starts with. This keeps out names that load from
// the host's zone files but are not zones a browser knows, such as "localtime"
// (the server's own zone), "posixrules", "Factory", "right/..." and "posix/...".
var campaignTimeZoneAreas = []string{
	"Africa", "America", "Antarctica", "Arctic", "Asia", "Atlantic",
	"Australia", "Europe", "Indian", "Pacific", "Etc",
}

// CampaignTimeZone is the IANA time zone the Daily Slots week days and hours
// of a campaign are given in
type CampaignTimeZone struct {
	inner string
	loc   *time.Location
}

// NewCampaignTimeZone creates a CampaignTimeZone from an IANA zone name
func NewCampaignTimeZone(s string) (*CampaignTimeZone, error) {
	// "Local" fails the region check below, it is the server's own zone and not
	// one the user picked
	if len(s) == 0 || len(s) > 64 || !campaignTimeZonePattern.MatchString(s) {
		return nil, validate.WrapErrorWithField(errors.New("invalid time zone"), "constraintTimeZone")
	}
	if s != "UTC" {
		area, _, found := strings.Cut(s, "/")
		if !found || !slices.Contains(campaignTimeZoneAreas, area) {
			return nil, validate.WrapErrorWithField(errors.New("invalid time zone"), "constraintTimeZone")
		}
	}
	loc, err := time.LoadLocation(s)
	if err != nil {
		return nil, validate.WrapErrorWithField(errors.New("unknown time zone"), "constraintTimeZone")
	}
	return &CampaignTimeZone{inner: s, loc: loc}, nil
}

// NewCampaignTimeZoneMust creates a CampaignTimeZone or panics
func NewCampaignTimeZoneMust(s string) *CampaignTimeZone {
	res, err := NewCampaignTimeZone(s)
	if err != nil {
		panic(err)
	}
	return res
}

// Location returns the loaded zone
func (c CampaignTimeZone) Location() *time.Location {
	return c.loc
}

// String returns the zone name
func (c CampaignTimeZone) String() string {
	return c.inner
}

// MarshalJSON implements the json.Marshaler interface
func (c CampaignTimeZone) MarshalJSON() ([]byte, error) {
	return json.Marshal(c.inner)
}

// UnmarshalJSON implements the json.Unmarshaler interface
func (c *CampaignTimeZone) UnmarshalJSON(data []byte) error {
	var str string
	if err := json.Unmarshal(data, &str); err != nil {
		return err
	}
	tz, err := NewCampaignTimeZone(str)
	if err != nil {
		return unwrapError(err)
	}
	*c = *tz
	return nil
}

// HourMinute returns the hour and minute of the time
func (c CampaignTimeConstraint) HourMinute() (int, int) {
	h, _ := strconv.Atoi(c.inner[:2])
	m, _ := strconv.Atoi(c.inner[3:])
	return h, m
}
