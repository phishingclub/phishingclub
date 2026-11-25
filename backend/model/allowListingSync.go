package model

import (
	"errors"
	"net"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/phishingclub/phishingclub/validate"
)

// allow listing sync entry kinds, matching the Microsoft lists
const (
	AllowListingKindDomain = "domain"
	AllowListingKindIP     = "ip"
	AllowListingKindURL    = "url"
)

// allow listing sync entry states
const (
	// AllowListingStateOwned is a value Phishing Club added and may remove
	AllowListingStateOwned = "owned"
	// AllowListingStateTenant is a value that was in the tenant already. It
	// is never removed.
	AllowListingStateTenant = "tenant"
)

// allow listing sync limits
const (
	AllowListingSyncMaxLeadMinutes  = 30 * 24 * 60
	AllowListingSyncMaxGraceMinutes = 30 * 24 * 60
	// AllowListingSyncMaxPins is the most pins one target holds
	AllowListingSyncMaxPins = 50
	// AllowListingSyncMaxObserved is the most values of one kind a report may
	// hold. Microsoft allows far fewer, the room is for values the tenant
	// added outside the limits of the portal.
	AllowListingSyncMaxObserved = 200
	// AllowListingSyncMaxValueLength is the longest value that is stored
	AllowListingSyncMaxValueLength = 300
	// AllowListingSyncMaxReportErrors is the most runner errors one report
	// may hold, each cut to AllowListingSyncMaxErrorLength
	AllowListingSyncMaxReportErrors = 20
	AllowListingSyncMaxErrorLength  = 500
	AllowListingSyncMaxNoteLength   = 255
)

// AllowListingTarget is one tenant a runner keeps allow listed
type AllowListingTarget struct {
	ID          uuid.UUID  `json:"id"`
	CreatedAt   *time.Time `json:"createdAt"`
	UpdatedAt   *time.Time `json:"updatedAt"`
	CompanyID   *uuid.UUID `json:"companyID"`
	Enabled     bool       `json:"enabled"`
	TokenPrefix string     `json:"tokenPrefix"`
	// Token is set only right after it is made, it is never stored or read back
	Token string `json:"token,omitempty"`

	LeadMinutes  int `json:"leadMinutes"`
	GraceMinutes int `json:"graceMinutes"`
	// IntervalMinutes is how often the schedule in the tenant runs the runner
	IntervalMinutes int `json:"intervalMinutes"`
	// MaxReportAgeMinutes is read only, it follows from the interval
	MaxReportAgeMinutes int `json:"maxReportAgeMinutes"`
	// ReportGapMinutes is the time between the last two reports, 0 before
	// the second report
	ReportGapMinutes int `json:"reportGapMinutes"`
	// RunnerDomain is the domain the runbook was set up to call
	RunnerDomain string `json:"runnerDomain"`
	// RunnerDomainIssue is set when the runner domain can no longer be
	// called: deleted or proxy. It is read only.
	RunnerDomainIssue string `json:"runnerDomainIssue"`

	PlanID       *uuid.UUID `json:"-"`
	PlanIssuedAt *time.Time `json:"-"`
	PlanJSON     string     `json:"-"`
	// RecentPlansJSON holds the plans handed out lately
	RecentPlansJSON string `json:"-"`

	LastPlanAt   *time.Time `json:"lastPlanAt"`
	LastReportAt *time.Time `json:"lastReportAt"`
	LastError    string     `json:"lastError"`
	LastNote     string     `json:"lastNote"`
	FailureCount int        `json:"failureCount"`
	DriftCount   int        `json:"driftCount"`
	// DeleteRequestedAt is set when the target was deleted with its values
	// removed first. It is deleted once the values are gone.
	DeleteRequestedAt *time.Time `json:"deleteRequestedAt"`
}

// IsDeleting reports if the target removes its values before it is deleted
func (t *AllowListingTarget) IsDeleting() bool {
	return t.DeleteRequestedAt != nil
}

// Lead is how long before its start a campaign counts
func (t *AllowListingTarget) Lead() time.Duration {
	return time.Duration(t.LeadMinutes) * time.Minute
}

// Grace is how long after its close a campaign counts
func (t *AllowListingTarget) Grace() time.Duration {
	return time.Duration(t.GraceMinutes) * time.Minute
}

// MaxReportAge is how old the last report may be for a value to count as
// confirmed
func (t *AllowListingTarget) MaxReportAge() time.Duration {
	return time.Duration(AllowListingMaxReportAgeMinutes(t.IntervalMinutes)) * time.Minute
}

// AllowListingIntervals are the schedules a runner may run on, in minutes.
// An hour is the shortest schedule Azure Automation allows.
var AllowListingIntervals = []int{60, 3 * 60, 6 * 60, 12 * 60, 24 * 60}

// AllowListingDefaultInterval is the interval of a new target
const AllowListingDefaultInterval = 60

// AllowListingMaxReportAgeMinutes is how old the last report may be for a
// value to count as confirmed: two runs may be missed, plus an hour for a
// job that starts late
func AllowListingMaxReportAgeMinutes(intervalMinutes int) int {
	if intervalMinutes <= 0 {
		intervalMinutes = AllowListingDefaultInterval
	}
	return 2*intervalMinutes + 60
}

// AllowListingTargetSettings are the settings an admin sets on a target
type AllowListingTargetSettings struct {
	Enabled         bool `json:"enabled"`
	LeadMinutes     int  `json:"leadMinutes"`
	GraceMinutes    int  `json:"graceMinutes"`
	IntervalMinutes int  `json:"intervalMinutes"`
}

// Validate checks the settings are in range
func (s *AllowListingTargetSettings) Validate() error {
	if s.LeadMinutes < 0 || s.LeadMinutes > AllowListingSyncMaxLeadMinutes {
		return validate.WrapErrorWithField(errors.New("must be between 0 and 30 days"), "leadMinutes")
	}
	if s.GraceMinutes < 0 || s.GraceMinutes > AllowListingSyncMaxGraceMinutes {
		return validate.WrapErrorWithField(errors.New("must be between 0 and 30 days"), "graceMinutes")
	}
	if !slices.Contains(AllowListingIntervals, s.IntervalMinutes) {
		return validate.WrapErrorWithField(errors.New("must be 1, 3, 6, 12 or 24 hours"), "intervalMinutes")
	}
	// a value is only added or removed on a run, so a shorter time than
	// the interval could pass with no run at all
	if s.LeadMinutes < s.IntervalMinutes {
		return validate.WrapErrorWithField(errors.New("must be at least the sync interval"), "leadMinutes")
	}
	if s.GraceMinutes < s.IntervalMinutes {
		return validate.WrapErrorWithField(errors.New("must be at least the sync interval"), "graceMinutes")
	}
	return nil
}

// AllowListingEntry is a value the runner last saw in the tenant
type AllowListingEntry struct {
	ID             uuid.UUID  `json:"id"`
	Kind           string     `json:"kind"`
	Value          string     `json:"value"`
	State          string     `json:"state"`
	AddedAt        *time.Time `json:"addedAt"`
	LastSeenAt     *time.Time `json:"lastSeenAt"`
	UndesiredSince *time.Time `json:"undesiredSince"`
	// RemoveAfter is when an owned value nothing needs is removed, set on reads
	RemoveAfter *time.Time `json:"removeAfter,omitempty"`
	// Needed is set on reads when a campaign, program or pin needs the value
	Needed bool `json:"needed"`
	// Kept is set on reads for an owned value nothing needs that stays, as
	// the rule needs one domain and one sending IP and the others are the
	// tenant's
	Kept bool `json:"kept"`
}

// AllowListingPin keeps a template or a single value allow listed without a
// campaign
type AllowListingPin struct {
	ID                   uuid.UUID  `json:"id"`
	CreatedAt            *time.Time `json:"createdAt"`
	CampaignTemplateID   *uuid.UUID `json:"campaignTemplateID"`
	CampaignTemplateName string     `json:"campaignTemplateName"`
	Kind                 string     `json:"kind"`
	Value                string     `json:"value"`
	Note                 string     `json:"note"`
	ExpiresAt            *time.Time `json:"expiresAt"`
}

// AllowListingPinInput is a new pin
type AllowListingPinInput struct {
	CampaignTemplateID *uuid.UUID `json:"campaignTemplateID"`
	Kind               string     `json:"kind"`
	Value              string     `json:"value"`
	Note               string     `json:"note"`
	ExpiresAt          *time.Time `json:"expiresAt"`
}

// Validate checks a pin names either a template or one valid value, and
// writes the value in its normal form
func (p *AllowListingPinInput) Validate(now time.Time) error {
	p.Note = strings.TrimSpace(p.Note)
	if len(p.Note) > AllowListingSyncMaxNoteLength {
		return validate.WrapErrorWithField(errors.New("is too long"), "note")
	}
	if p.ExpiresAt != nil && !p.ExpiresAt.After(now) {
		return validate.WrapErrorWithField(errors.New("must be in the future"), "expiresAt")
	}
	hasValue := strings.TrimSpace(p.Value) != "" || strings.TrimSpace(p.Kind) != ""
	if p.CampaignTemplateID != nil {
		if hasValue {
			return validate.WrapErrorWithField(errors.New("pin a template or a value, not both"), "value")
		}
		return nil
	}
	if !hasValue {
		return validate.WrapErrorWithField(errors.New("pin a template or a value"), "value")
	}
	value, err := NormalizeAllowListingValue(p.Kind, p.Value, true)
	if err != nil {
		return validate.WrapErrorWithField(err, "value")
	}
	p.Value = value
	return nil
}

// AllowListingOp is one value to add or remove
type AllowListingOp struct {
	Kind  string `json:"kind"`
	Value string `json:"value"`
}

// Key is the kind and value as one map key
func (o AllowListingOp) Key() string {
	return o.Kind + "\x00" + o.Value
}

// AllowListingPlan is what a runner applies in the tenant
type AllowListingPlan struct {
	PlanID   uuid.UUID        `json:"planId"`
	IssuedAt time.Time        `json:"issuedAt"`
	Add      []AllowListingOp `json:"add"`
	Remove   []AllowListingOp `json:"remove"`
	// Deleting is set while the target removes its values before it is
	// deleted
	Deleting bool `json:"deleting"`
	// RemoveRule is set when every domain and sending IP of the rule is
	// owned and due to be removed and none is added. The runner then removes
	// the whole rule, as the rule can not be left without a domain or a
	// sending IP.
	RemoveRule bool `json:"removeRule"`
}

// AllowListingObserved are the values a runner read back from the tenant
type AllowListingObserved struct {
	Domains []string `json:"domains"`
	IPs     []string `json:"ips"`
	URLs    []string `json:"urls"`
}

// AllowListingReport is what a runner sends after it applied a plan.
// Before is what the runner read before it changed anything, Observed what
// it read after. A value counts as added by Phishing Club only when the plan
// added it and it was not there before.
type AllowListingReport struct {
	PlanID   string                `json:"planId"`
	Before   *AllowListingObserved `json:"before"`
	Observed AllowListingObserved  `json:"observed"`
	Errors   []string              `json:"errors"`
	// AddedNow are the values this run added, as kind:value
	AddedNow []string `json:"addedNow"`
	// Failed is set when the run stopped before it could read and report the
	// lists. Only Errors is used, the ledger is not changed.
	Failed bool `json:"failed"`
}

// ParseAllowListingKey reads a kind:value item a runner reports
func ParseAllowListingKey(item string) (AllowListingOp, bool) {
	kind, value, ok := strings.Cut(item, ":")
	if !ok {
		return AllowListingOp{}, false
	}
	value, err := NormalizeAllowListingValue(kind, value, true)
	if err != nil {
		return AllowListingOp{}, false
	}
	return AllowListingOp{Kind: kind, Value: value}, true
}

// CutUTF8 cuts s to at most n bytes without splitting a character
func CutUTF8(s string, n int) string {
	if len(s) <= n {
		return s
	}
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return s[:n]
}

// Ops returns the observed values in their normal form, each once, and how
// many values were left out because they are empty, too long or hold
// control characters
func (o *AllowListingObserved) Ops() ([]AllowListingOp, int, error) {
	out := []AllowListingOp{}
	seen := map[string]bool{}
	dropped := 0
	for _, list := range []struct {
		kind   string
		values []string
	}{
		{AllowListingKindDomain, o.Domains},
		{AllowListingKindIP, o.IPs},
		{AllowListingKindURL, o.URLs},
	} {
		if len(list.values) > AllowListingSyncMaxObserved {
			return nil, 0, validate.WrapErrorWithField(errors.New("too many values"), "observed")
		}
		for _, v := range list.values {
			// an empty item is what PowerShell sends for an empty list
			if strings.TrimSpace(v) == "" {
				continue
			}
			value, err := NormalizeAllowListingValue(list.kind, v, false)
			if err != nil {
				dropped++
				continue
			}
			op := AllowListingOp{Kind: list.kind, Value: value}
			if !seen[op.Key()] {
				seen[op.Key()] = true
				out = append(out, op)
			}
		}
	}
	return out, dropped, nil
}

// CleanErrors returns the runner errors cut to the limits
func (r *AllowListingReport) CleanErrors() []string {
	out := []string{}
	for _, e := range r.Errors {
		e = strings.Map(func(r rune) rune {
			if r < 0x20 || r == 0x7f {
				return ' '
			}
			return r
		}, strings.TrimSpace(e))
		if e == "" {
			continue
		}
		e = CutUTF8(e, AllowListingSyncMaxErrorLength)
		out = append(out, e)
		if len(out) == AllowListingSyncMaxReportErrors {
			break
		}
	}
	return out
}

var (
	// a domain name in ASCII form, such as one normalizeHost gives
	allowListingDomainRe = regexp.MustCompile(`^([a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?\.)+[a-z0-9-]{2,63}$`)
	// a simulation URL as Phishing Club writes it, one exact host and /*.
	// A wildcard host is never written.
	allowListingStrictURLRe = regexp.MustCompile(`^([a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?\.)+[a-z0-9-]{2,63}/\*$`)
)

// NormalizeAllowListingValue checks a value of a kind and returns it in its
// normal form. Strict allows only the forms Phishing Club writes itself, and
// every value in a plan is strict. A value read from the tenant is not
// strict: the tenant may hold forms Phishing Club never writes, such as an
// IP range written as a to b, a URL with a path or a name that is not ASCII.
// It is kept as it is, only trimmed and in lower case, and must be short and
// hold no control characters. Such a value can never be in a plan, so it is
// only ever a value of the tenant.
func NormalizeAllowListingValue(kind string, value string, strict bool) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", errors.New("empty value")
	}
	if len(value) > AllowListingSyncMaxValueLength {
		return "", errors.New("value is too long")
	}
	for _, r := range value {
		if r < 0x20 || r == 0x7f || (r >= 0x80 && r < 0xa0) {
			return "", errors.New("value has characters that are not allowed")
		}
		if strict && (r == ' ' || r > 0x7e) {
			return "", errors.New("value has characters that are not allowed")
		}
	}
	switch kind {
	case AllowListingKindDomain:
		value = strings.TrimSuffix(strings.ToLower(value), ".")
		if strict && !allowListingDomainRe.MatchString(value) {
			return "", errors.New("not a domain")
		}
		return value, nil
	case AllowListingKindIP:
		lower := strings.ToLower(value)
		if !strict {
			// a value read from the tenant is kept as it is written, so it
			// only matches a value Phishing Club writes when it is written
			// the same way. The runner compares the same way.
			return lower, nil
		}
		// an IPv4 address written as IPv6 is refused, its range would be
		// checked against the wrong width
		if strings.Contains(value, ":") && strings.Contains(value, ".") {
			return "", errors.New("not an IP address or range")
		}
		if ip := net.ParseIP(value); ip != nil {
			if ip.String() != lower {
				return "", errors.New("not in the normal form, write it as " + ip.String())
			}
			return lower, nil
		}
		if ip, network, err := net.ParseCIDR(value); err == nil {
			ones, _ := network.Mask.Size()
			if !ip.Equal(network.IP) {
				return "", errors.New("not a network address")
			}
			if ones < AllowListingMinPrefix(ip) {
				return "", errors.New("the range is wider than allowed")
			}
			normal := network.IP.String() + "/" + strconv.Itoa(ones)
			if normal != lower {
				return "", errors.New("not in the normal form, write it as " + normal)
			}
			return lower, nil
		}
		return "", errors.New("not an IP address or range")
	case AllowListingKindURL:
		value = strings.ToLower(value)
		if strict && !allowListingStrictURLRe.MatchString(value) {
			return "", errors.New("must be a host followed by /*")
		}
		return value, nil
	}
	return "", errors.New("unknown kind")
}

// AllowListingMinPrefix is the shortest prefix of a range Phishing Club
// lists, /24 for IPv4 and /48 for IPv6. A wider range would let mail from
// far too many senders skip filtering.
func AllowListingMinPrefix(ip net.IP) int {
	if ip.To4() != nil {
		return 24
	}
	return 48
}

// allow listing sync status of a campaign or program
const (
	// AllowListingStatusNoTarget is when the scope has no sync target
	AllowListingStatusNoTarget = "noTarget"
	// AllowListingStatusDisabled is when the target is turned off
	AllowListingStatusDisabled = "disabled"
	// AllowListingStatusNotCounting is when the campaign does not need allow
	// listing yet or any more
	AllowListingStatusNotCounting = "notCounting"
	// AllowListingStatusConfirmed is when the runner confirmed every value
	AllowListingStatusConfirmed = "confirmed"
	// AllowListingStatusWaiting is when values wait for the next sync
	AllowListingStatusWaiting = "waiting"
	// AllowListingStatusStale is when the last report is too old
	AllowListingStatusStale = "stale"
	// AllowListingStatusOverLimit is when the values do not fit the limits
	AllowListingStatusOverLimit = "overLimit"
	// AllowListingStatusIncomplete is when a template lacks values, such as a
	// sender without a sending IP
	AllowListingStatusIncomplete = "incomplete"
)

// AllowListingSyncStatus is the allow listing sync state of one campaign or
// program
type AllowListingSyncStatus struct {
	Status       string           `json:"status"`
	TargetID     *uuid.UUID       `json:"targetID"`
	LastReportAt *time.Time       `json:"lastReportAt"`
	Missing      []AllowListingOp `json:"missing"`
	// Incomplete lists the templates that lack a value, by name
	Incomplete []string `json:"incomplete"`
}

// AllowListingSyncSource is one campaign, program or pin in a plan preview
type AllowListingSyncSource struct {
	Type       string           `json:"type"`
	ID         uuid.UUID        `json:"id"`
	Name       string           `json:"name"`
	Status     string           `json:"status"`
	Values     []AllowListingOp `json:"values"`
	Incomplete []string         `json:"incomplete"`
}

// AllowListingSyncPreview is what the next sync would do and why
type AllowListingSyncPreview struct {
	Add     []AllowListingOp         `json:"add"`
	Remove  []AllowListingOp         `json:"remove"`
	Sources []AllowListingSyncSource `json:"sources"`
	// SourcesTotal is how many sources count, Sources holds the first of them
	SourcesTotal int `json:"sourcesTotal"`
}
