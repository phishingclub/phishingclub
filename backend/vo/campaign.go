package vo

import (
	"encoding/json"
	"slices"
	"strconv"
	"time"

	"github.com/go-errors/errors"

	"github.com/phishingclub/phishingclub/utils"
	"github.com/phishingclub/phishingclub/validate"
)

// CampaignSendingOrder is the order which recipients are sent to
// 'asc', 'desc' or 'random' are allowed values
type CampaignSendingOrder struct {
	inner string
}

// NewCampaignSendingOrder creates a new CampaignSendingOrder
func NewCampaignSendingOrder(s string) (*CampaignSendingOrder, error) {
	switch s {
	case "random":
	case "asc":
	case "desc":
	default:
		return nil, validate.WrapErrorWithField(errors.New("not known or allowed value - use 'asc', 'desc' or 'random'"), "CampaignSendingOrder")
	}

	return &CampaignSendingOrder{
		inner: s,
	}, nil
}

// NewCampaignSendingOrderMust creates a new CampaignSendingOrder or panics
func NewCampaignSendingOrderMust(s string) *CampaignSendingOrder {
	res, err := NewCampaignSendingOrder(s)
	if err != nil {
		panic(err)
	}
	return res
}

// MarshalJSON implements the json.Marshaler interface
func (c CampaignSendingOrder) MarshalJSON() ([]byte, error) {
	return json.Marshal(c.inner)
}

// UnmarshalJSON unmarshals the json into a string
func (c *CampaignSendingOrder) UnmarshalJSON(data []byte) error {
	var str string
	if err := json.Unmarshal(data, &str); err != nil {
		return err
	}
	ss, err := NewCampaignSendingOrder(str)
	if err != nil {
		return unwrapError(err)
	}
	c.inner = ss.inner
	return nil
}

// String returns the string representation of the short string
func (c CampaignSendingOrder) String() string {
	return c.inner
}

// CampaignSortField is the field which recipients are sorted by
type CampaignSortField struct {
	inner string
}

// NewCampaignSortField creates a new CampaignSortField
func NewCampaignSortField(s string) (*CampaignSortField, error) {
	switch s {
	case "email":
	case "name":
	case "phone":
	case "position":
	case "department":
	case "city":
	case "country":
	case "misc":
	case "extraID":
	default:
		return nil, validate.WrapErrorWithField(errors.New("not known or allowed value"), "CampaignSortField")
	}

	return &CampaignSortField{
		inner: s,
	}, nil
}

// NewCampaignSortFieldMust creates a new CampaignSortField or panics
func NewCampaignSortFieldMust(s string) *CampaignSortField {
	res, err := NewCampaignSortField(s)
	if err != nil {
		panic(err)
	}
	return res
}

// MarshalJSON implements the json.Marshaler interface
func (c CampaignSortField) MarshalJSON() ([]byte, error) {
	return json.Marshal(c.inner)
}

// UnmarshalJSON unmarshals the json into a string
func (c *CampaignSortField) UnmarshalJSON(data []byte) error {
	var str string
	if err := json.Unmarshal(data, &str); err != nil {
		return err
	}
	ss, err := NewCampaignSortField(str)
	if err != nil {
		return unwrapError(err)
	}
	c.inner = ss.inner
	return nil
}

// String returns the string representation of the short string
func (c CampaignSortField) String() string {
	return c.inner
}

// CampaignWeekDays is the days of the week a campaign can be sent on
// it is represted as a bitfield where no bits is Sunday and 0b100000 is Saturday
type CampaignWeekDays struct {
	inner int
}

// NewCampaignWeekDays creates a new CampaignWeekDays
func NewCampaignWeekDays(i int) (*CampaignWeekDays, error) {
	// if above 0b01111111 (127) or below 0, it is invalid
	if i <= 0 || i > 127 {
		return nil, validate.WrapErrorWithField(errors.New("invalid week days"), "CampaignWeekDays")
	}
	return &CampaignWeekDays{inner: i}, nil
}

// NewCampaignWeekDaysMust creates a new CampaignWeekDays or panics
func NewCampaignWeekDaysMust(i int) *CampaignWeekDays {
	res, err := NewCampaignWeekDays(i)
	if err != nil {
		panic(err)
	}
	return res
}

// IsWithin checks if the week days are within start and end
func (c CampaignWeekDays) IsWithin(start *time.Time, end *time.Time) bool {
	if start == nil || end == nil {
		return false
	}
	// loop over each day between start and end
	// Iterate over days, including the start and end days
	toFind := c.AsSlice()
	current := *start
	for current.Before(*end) || current.Equal(*end) {
		// check if the current day is in the week days
		if slices.Contains(toFind, int(current.Weekday())) {
			// remove the number from the slice
			toFind = utils.SliceRemoveInt(toFind, int(current.Weekday()))
		}
		current = current.AddDate(0, 0, 1)
		if len(toFind) == 0 {
			return true
		}
	}
	return false
}

// AsSlice returns the days of the week as a slice of integers, 0 sunday .. 6 saturday
func (c CampaignWeekDays) AsSlice() []int {
	days := []int{}
	for i := 0; i < 7; i++ {
		if c.inner&(1<<i) != 0 {
			days = append(days, i)
		}
	}
	return days
}

// MarshalJSON implements the json.Marshaler interface
func (c CampaignWeekDays) MarshalJSON() ([]byte, error) {
	return json.Marshal(c.inner)
}

// UnmarshalJSON unmarshals the json into a string
func (c *CampaignWeekDays) UnmarshalJSON(data []byte) error {
	var i int
	if err := json.Unmarshal(data, &i); err != nil {
		return err
	}
	ss, err := NewCampaignWeekDays(i)
	if err != nil {
		return unwrapError(err)
	}
	c.inner = ss.inner
	return nil
}

// String returns the string representation of the short string
func (c CampaignWeekDays) String() string {
	return strconv.Itoa(c.inner)
}

// Int returns the integer representation of the short string
func (c CampaignWeekDays) Int() int {
	return c.inner
}

// Count returns the number of days in the week days
func (c CampaignWeekDays) Count() int {
	count := 0
	for i := 0; i < 6; i++ {
		if c.inner&(1<<i) != 0 {
			count++
		}
	}
	return count
}

// CampampaignTimeConstraint is a start or end time of which
// delivery a campaign can happen within.
// it is expressed as hh:mm (h hour, m minute)
type CampaignTimeConstraint struct {
	inner string
}

// NewCampaignTimeConstraint creates a new CampaignTimeConstraint
func NewCampaignTimeConstraint(s string) (*CampaignTimeConstraint, error) {
	// validate string has format hh:mm - 00-23 hours, 00-59 minutes
	if len(s) != 5 || s[2] != ':' {

		return nil, validate.WrapErrorWithField(
			errors.New("invalid time format - must be hh:mm"),
			"CampaignTimeConstraint start or end",
		)
	}
	hourStr := s[:2]
	minuteStr := s[3:]
	hour, err := strconv.Atoi(hourStr)
	if err != nil || hour < 0 || hour > 23 {
		return nil, validate.WrapErrorWithField(
			errors.New("invalid hour"),
			"CampaignTimeConstraint start or end",
		)
	}
	minute, err := strconv.Atoi(minuteStr)
	if err != nil || minute < 0 || minute > 59 {
		return nil, validate.WrapErrorWithField(
			errors.New("invalid minute"),
			"CampaignTimeConstraint start or end",
		)
	}
	return &CampaignTimeConstraint{
		inner: s,
	}, nil
}

// NewCampaignTimeConstraintMust creates a new CampaignTimeConstraint or panics
func NewCampaignTimeConstraintMust(s string) *CampaignTimeConstraint {
	res, err := NewCampaignTimeConstraint(s)
	if err != nil {
		panic(err)
	}
	return res
}

// MarshalJSON implements the json.Marshaler interface
func (c CampaignTimeConstraint) MarshalJSON() ([]byte, error) {
	return json.Marshal(c.inner)
}

// UnmarshalJSON unmarshals the json into a string
func (c *CampaignTimeConstraint) UnmarshalJSON(data []byte) error {
	var str string
	if err := json.Unmarshal(data, &str); err != nil {
		return err
	}
	ss, err := NewCampaignTimeConstraint(str)
	if err != nil {
		return unwrapError(err)
	}
	c.inner = ss.inner
	return nil
}

// String returns the string representation of the short string
func (c CampaignTimeConstraint) String() string {
	return c.inner
}

// DiffMinutes returns the difference in minutes between two times
func (c CampaignTimeConstraint) DiffMinutes(other CampaignTimeConstraint) time.Duration {
	// parse the time strings
	h1, _ := strconv.Atoi(c.inner[:2])
	m1, _ := strconv.Atoi(c.inner[3:])
	h2, _ := strconv.Atoi(other.inner[:2])
	m2, _ := strconv.Atoi(other.inner[3:])
	// calculate the difference
	return time.Duration((h2-h1)*60+m2-m1) * time.Minute
}

// Minutes returns the time as a duration
func (c CampaignTimeConstraint) Minutes() time.Duration {
	// parse the time strings
	h1, _ := strconv.Atoi(c.inner[:2])
	m1, _ := strconv.Atoi(c.inner[3:])
	// calculate the difference
	return time.Duration(h1*60+m1) * time.Minute
}

// IsBefore checks if the time is before the other time
func (c CampaignTimeConstraint) IsAfter(other CampaignTimeConstraint) bool {
	// check if hh:mm is after other hh:mm
	// this works because the chars are in order of significance
	return c.inner > other.inner
}

// IsBefore checks if the time is before the other time
func (c CampaignTimeConstraint) IsBefore(other CampaignTimeConstraint) bool {
	// check if hh:mm is before other hh:mm
	// this works because the chars are in order of significance
	return c.inner < other.inner
}

// IsEqual checks if the time is equal to the other time
func (c CampaignTimeConstraint) IsEqual(other CampaignTimeConstraint) bool {
	return c.inner == other.inner
}
