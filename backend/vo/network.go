package vo

import (
	"encoding/json"
	"net"
	"strings"

	"github.com/go-errors/errors"
	"github.com/phishingclub/phishingclub/errs"
)

type IPNetSlice []IPNet

// UnmarshalJSON implements custom unmarshaling for IPNetSlice
func (s *IPNetSlice) UnmarshalJSON(data []byte) error {
	// if empty string
	if strings.TrimSpace(string(data)) == "\"\"" {
		return errors.New("CIDRs is empty.")
	}
	var str string
	if err := json.Unmarshal(data, &str); err != nil {
		return err
	}
	strs := strings.Split(str, "\n")
	// Convert each string to IPNet
	result := make(IPNetSlice, len(strs))
	for i, cidr := range strs {
		ipnet, err := NewIPNet(cidr)
		if err != nil {
			return unwrapError(err)
		}
		result[i] = *ipnet
	}

	*s = result
	return nil
}

// MarshalJSON implements custom marshaling for IPNetSlice
func (s IPNetSlice) MarshalJSON() ([]byte, error) {
	if len(s) == 0 {
		return json.Marshal("")
	}
	strs := make([]string, len(s))
	for i, ipnet := range s {
		strs[i] = ipnet.String()
	}

	return json.Marshal(
		strings.Join(strs, "\n"),
	)
}

type IPNet struct {
	net.IPNet
}

// NewIPNet creates a new IPNet
func NewIPNet(ipNet string) (*IPNet, error) {
	_, in, err := net.ParseCIDR(ipNet)
	if err != nil {
		return nil, errs.Wrap(err)
	}
	return &IPNet{
		*in,
	}, nil
}

// NewIPNetMust creates a new IPNet and panics if it is invalid
func NewIPNetMust(ipNet string) *IPNet {
	i, err := NewIPNet(ipNet)
	if err != nil {
		panic(err)
	}
	return i
}

// MarshalJSON implements the json.Marshaler interface
func (i IPNet) MarshalJSON() ([]byte, error) {
	return json.Marshal(i.String())
}

// UnmarshalJSON unmarshals the json into a string
func (i *IPNet) UnmarshalJSON(data []byte) error {
	var str string
	if err := json.Unmarshal(data, &str); err != nil {
		return err
	}
	ss, err := NewIPNet(str)
	if err != nil {
		return unwrapError(err)
	}
	i.IPNet = ss.IPNet
	return nil
}

// String returns the string representation of the IPNet
func (i IPNet) String() string {
	return i.IPNet.String()
}
