package service

import (
	"context"
	"sync"
	"time"

	"github.com/go-errors/errors"
	"github.com/google/uuid"
	"github.com/phishingclub/phishingclub/data"
	"github.com/phishingclub/phishingclub/errs"
	"github.com/phishingclub/phishingclub/model"
	"github.com/phishingclub/phishingclub/repository"
	"go.uber.org/zap"
)

// IPAllowListEntry represents a single IP allow list entry
type IPAllowListEntry struct {
	IP            string    `json:"ip"`
	ProxyConfigID string    `json:"proxyConfigID"`
	ExpiresAt     time.Time `json:"expiresAt"`
	CreatedAt     time.Time `json:"createdAt"`
}

// IPAllowListService manages IP allow listing for proxy configurations
type IPAllowListService struct {
	Common
	logger          *zap.SugaredLogger
	allowList       sync.Map // map[string]int64 (ip+proxyConfigID -> expiry timestamp)
	mu              sync.RWMutex
	cleanupDone     chan bool
	ProxyRepository *repository.Proxy
}

// NewIPAllowListService creates a new IP allow list service
func NewIPAllowListService(logger *zap.SugaredLogger, proxyRepo *repository.Proxy) *IPAllowListService {
	common := Common{
		Logger: logger,
	}
	service := &IPAllowListService{
		Common:          common,
		logger:          logger,
		cleanupDone:     make(chan bool),
		ProxyRepository: proxyRepo,
	}

	// Start cleanup goroutine
	go service.periodicCleanup()

	return service
}

// AddIP adds an IP to the allow list for a specific proxy configuration
// must only be called internally and not exposed via. API
func (s *IPAllowListService) AddIP(ip string, proxyConfigID string, duration time.Duration) {
	if ip == "" || proxyConfigID == "" {
		return
	}

	key := ip + "-" + proxyConfigID
	expiry := time.Now().Add(duration).Unix()

	s.allowList.Store(key, expiry)

	s.logger.Debugw("IP allow listed",
		"ip", ip,
		"proxy_config_id", proxyConfigID,
		"expires_at", time.Unix(expiry, 0).Format(time.RFC3339),
	)
}

// IsIPAllowed checks if an IP is allowed for a specific proxy configuration
// must only be called internally and not exposed via. API
func (s *IPAllowListService) IsIPAllowed(ip string, proxyConfigID string) bool {
	if ip == "" || proxyConfigID == "" {
		return false
	}

	key := ip + "-" + proxyConfigID

	if expiryVal, exists := s.allowList.Load(key); exists {
		expiry := expiryVal.(int64)
		if time.Now().Unix() < expiry {
			s.logger.Debugw("IP found in allow list",
				"ip", ip,
				"proxy_config_id", proxyConfigID,
				"expires_at", time.Unix(expiry, 0).Format(time.RFC3339),
			)
			return true
		}
		// Expired, remove it
		s.allowList.Delete(key)
		s.logger.Debugw("IP allow list entry expired and removed",
			"ip", ip,
			"proxy_config_id", proxyConfigID,
		)
	}

	return false
}

// GetEntriesForProxyConfig returns allow list entries for a specific proxy configuration
func (s *IPAllowListService) GetEntriesForProxyConfig(
	ctx context.Context,
	session *model.Session,
	proxyConfigID *uuid.UUID,
) ([]IPAllowListEntry, error) {
	ae := NewAuditEvent("IPAllowList.GetEntriesForProxyConfig", session)

	// check permissions
	isAuthorized, err := IsAuthorized(session, data.PERMISSION_ALLOW_GLOBAL)
	if err != nil && !errors.Is(err, errs.ErrAuthorizationFailed) {
		s.LogAuthError(err)
		return nil, errs.Wrap(err)
	}
	if !isAuthorized {
		s.AuditLogNotAuthorized(ae)
		return nil, errs.ErrAuthorizationFailed
	}

	var entries []IPAllowListEntry
	now := time.Now()
	proxyConfigIDStr := proxyConfigID.String()

	s.allowList.Range(func(key, value interface{}) bool {
		keyStr := key.(string)
		expiry := value.(int64)
		expiryTime := time.Unix(expiry, 0)

		// Skip expired entries
		if now.Unix() >= expiry {
			s.allowList.Delete(key)
			return true
		}

		// Parse key to extract IP and proxy config ID
		parts := parseAllowListKey(keyStr)
		if len(parts) == 2 && parts[1] == proxyConfigIDStr {
			entries = append(entries, IPAllowListEntry{
				IP:            parts[0],
				ProxyConfigID: parts[1],
				ExpiresAt:     expiryTime,
				CreatedAt:     expiryTime.Add(-10 * time.Minute), // Assume 10 minute duration
			})
		}

		return true
	})

	ae.Details["proxy_config_id"] = proxyConfigID.String()
	if userId, err := session.User.ID.Get(); err == nil {
		ae.Details["user_id"] = userId.String()
	}

	return entries, nil
}

// ClearExpired removes all expired entries from the allow list
func (s *IPAllowListService) ClearExpired() int {
	count := 0
	now := time.Now().Unix()

	s.allowList.Range(func(key, value interface{}) bool {
		expiry := value.(int64)
		if now >= expiry {
			s.allowList.Delete(key)
			count++
		}
		return true
	})

	if count > 0 {
		s.logger.Debugw("Cleaned up expired IP allow list entries", "count", count)
	}

	return count
}

// ClearForProxyConfig removes all entries for a specific proxy configuration
func (s *IPAllowListService) ClearForProxyConfig(
	ctx context.Context,
	session *model.Session,
	proxyConfigID *uuid.UUID,
) (int, error) {
	ae := NewAuditEvent("IPAllowList.ClearForProxyConfig", session)

	// check permissions
	isAuthorized, err := IsAuthorized(session, data.PERMISSION_ALLOW_GLOBAL)
	if err != nil && !errors.Is(err, errs.ErrAuthorizationFailed) {
		s.LogAuthError(err)
		return 0, errs.Wrap(err)
	}
	if !isAuthorized {
		s.AuditLogNotAuthorized(ae)
		return 0, errs.ErrAuthorizationFailed
	}
	count := 0
	proxyConfigIDStr := proxyConfigID.String()

	s.allowList.Range(func(key, value interface{}) bool {
		keyStr := key.(string)
		parts := parseAllowListKey(keyStr)
		if len(parts) == 2 && parts[1] == proxyConfigIDStr {
			s.allowList.Delete(key)
			count++
		}
		return true
	})

	ae.Details["proxy_config_id"] = proxyConfigID.String()
	if userId, err := session.User.ID.Get(); err == nil {
		ae.Details["user_id"] = userId.String()
	}

	return count, nil
}

// periodicCleanup runs periodic cleanup of expired entries
func (s *IPAllowListService) periodicCleanup() {
	ticker := time.NewTicker(5 * time.Minute) // Clean up every 5 minutes
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			s.ClearExpired()
		case <-s.cleanupDone:
			return
		}
	}
}

// Stop stops the background cleanup goroutine
func (s *IPAllowListService) Stop() {
	close(s.cleanupDone)
}

// parseAllowListKey parses the allow list key format "ip-proxyConfigID"
func parseAllowListKey(key string) []string {
	// Find the last occurrence of "-" to handle IPv6 addresses
	lastIndex := -1
	for i := len(key) - 1; i >= 0; i-- {
		if key[i] == '-' {
			// Check if this looks like a UUID separator (36 chars after this point)
			remaining := key[i+1:]
			if len(remaining) == 36 {
				// Validate it looks like a UUID
				if _, err := uuid.Parse(remaining); err == nil {
					lastIndex = i
					break
				}
			}
		}
	}

	if lastIndex == -1 {
		return []string{key} // Fallback if parsing fails
	}

	ip := key[:lastIndex]
	proxyConfigID := key[lastIndex+1:]

	return []string{ip, proxyConfigID}
}
