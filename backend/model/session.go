package model

import (
	"time"

	"github.com/google/uuid"
	"github.com/phishingclub/phishingclub/data"
	"github.com/phishingclub/phishingclub/errs"
)

// todo move this to a global place like data or a config of sorts
const (
	SessionIdleTimeout = 24 * time.Hour
	SessionMaxAgeAt    = 24 * 3 * time.Hour
)

// used in runtime for API requests
// api session is only for the single request, more like a api session contex
type APISession struct {
	IP     string
	UserID *uuid.UUID
}

// Session reprensents a user session
// no Validate or ToDBMap as it is never created from user input
type Session struct {
	ID                *uuid.UUID `json:"id,omitempty"`
	CreatedAt         *time.Time `json:"createdAt"`
	UpdatedAt         *time.Time `json:"updatedAt"`
	ExpiresAt         *time.Time `json:"expiresAt"`
	MaxAgeAt          *time.Time `json:"maxAgeAt"`
	IP                string     `json:"ip"`
	User              *User      `json:"user"`
	IsUserLoaded      bool       `json:"-"`
	IsAPITokenRequest bool
}

// NewSystemSession creates a new system session
func NewSystemSession() (*Session, error) {
	id, err := uuid.Parse(data.SystemSessionID)
	if err != nil {
		return nil, errs.Wrap(err)
	}
	now := time.Now()
	longTimeFromNow := now.Add(time.Duration(420 * time.Now().Year())).UTC()
	expiresAt := &longTimeFromNow
	maxAgeAt := &longTimeFromNow
	user, err := NewSystemUser()
	if err != nil {
		return nil, errs.Wrap(err)
	}
	return &Session{
		ID:           &id,
		ExpiresAt:    expiresAt,
		MaxAgeAt:     maxAgeAt,
		IP:           "127.0.0.1",
		User:         user,
		IsUserLoaded: true,
	}, nil

}

// Renew updates the session
func (s *Session) Renew(lease time.Duration) {
	now := time.Now().UTC()
	expiresAt := now.Add(lease)
	s.ExpiresAt = &expiresAt
}

// IsExpired returns true if the session is expired
func (s *Session) IsExpired() bool {
	// is total lifetime over max lifetime?
	if time.Now().After(*s.MaxAgeAt) {
		return true
	}
	// is idle timeout over?
	if time.Now().After(*s.ExpiresAt) {
		return true
	}
	return false
}
