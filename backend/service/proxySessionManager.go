package service

import (
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/phishingclub/phishingclub/database"
	"github.com/phishingclub/phishingclub/model"
	"go.uber.org/zap"
)

// ProxySession represents an active MITM proxy session
type ProxySession struct {
	ID                    string
	CampaignRecipientID   *uuid.UUID
	CampaignID            *uuid.UUID
	RecipientID           *uuid.UUID
	Campaign              *model.Campaign
	Domain                *database.Domain
	TargetDomain          string
	Config                sync.Map // map[string]ProxyServiceDomainConfig
	CreatedAt             time.Time
	RequiredCaptures      sync.Map     // map[string]bool
	CapturedData          sync.Map     // map[string]map[string]string
	NextPageType          atomic.Value // string - accessed concurrently by multiple requests
	IsComplete            atomic.Bool  // accessed concurrently when checking capture completion
	CookieBundleSubmitted atomic.Bool  // accessed concurrently to prevent duplicate submissions
}

// ProxySessionManager manages proxy session lifecycle and storage
type ProxySessionManager struct {
	Common
	sessions                  sync.Map // map[sessionID]*ProxySession
	campaignRecipientSessions sync.Map // map[campaignRecipientID]sessionID
	urlMappings               sync.Map // map[rewritten URL]original URL
}

// NewProxySessionManager creates a new proxy session manager
func NewProxySessionManager(logger *zap.SugaredLogger) *ProxySessionManager {
	return &ProxySessionManager{
		Common: Common{
			Logger: logger,
		},
	}
}

// GetSession retrieves a session by ID
func (m *ProxySessionManager) GetSession(sessionID string) (*ProxySession, bool) {
	val, ok := m.sessions.Load(sessionID)
	if !ok {
		return nil, false
	}
	session, ok := val.(*ProxySession)
	return session, ok
}

// StoreSession stores a session
func (m *ProxySessionManager) StoreSession(sessionID string, session *ProxySession) {
	m.sessions.Store(sessionID, session)
}

// DeleteSession deletes a session and its associated campaign recipient mapping
func (m *ProxySessionManager) DeleteSession(sessionID string) {
	if val, ok := m.sessions.Load(sessionID); ok {
		if session, ok := val.(*ProxySession); ok {
			if session.CampaignRecipientID != nil {
				m.campaignRecipientSessions.Delete(session.CampaignRecipientID.String())
			}
		}
		m.sessions.Delete(sessionID)
	}
}

// GetSessionByCampaignRecipient retrieves a session ID by campaign recipient ID
func (m *ProxySessionManager) GetSessionByCampaignRecipient(campaignRecipientID string) (string, bool) {
	val, ok := m.campaignRecipientSessions.Load(campaignRecipientID)
	if !ok {
		return "", false
	}
	sessionID, ok := val.(string)
	return sessionID, ok
}

// StoreCampaignRecipientSession stores the mapping between campaign recipient and session
func (m *ProxySessionManager) StoreCampaignRecipientSession(campaignRecipientID string, sessionID string) {
	m.campaignRecipientSessions.Store(campaignRecipientID, sessionID)
}

// StoreURLMapping stores a URL mapping for rewrite tracking
func (m *ProxySessionManager) StoreURLMapping(rewrittenURL string, originalURL string) {
	m.urlMappings.Store(rewrittenURL, originalURL)
}

// GetURLMapping retrieves the original URL for a rewritten URL
func (m *ProxySessionManager) GetURLMapping(rewrittenURL string) (string, bool) {
	val, ok := m.urlMappings.Load(rewrittenURL)
	if !ok {
		return "", false
	}
	originalURL, ok := val.(string)
	return originalURL, ok
}

// RangeSessions iterates over all sessions
func (m *ProxySessionManager) RangeSessions(fn func(sessionID string, session *ProxySession) bool) {
	m.sessions.Range(func(key, value interface{}) bool {
		sessionID, ok := key.(string)
		if !ok {
			return true
		}
		session, ok := value.(*ProxySession)
		if !ok {
			return true
		}
		return fn(sessionID, session)
	})
}

// ClearSessionsForProxy clears all sessions associated with a proxy configuration
func (m *ProxySessionManager) ClearSessionsForProxy(proxyID string) {
	if proxyID == "" {
		return
	}

	clearedCount := 0

	m.sessions.Range(func(key, value interface{}) bool {
		sessionID, ok := key.(string)
		if !ok {
			return true
		}
		session, ok := value.(*ProxySession)
		if !ok {
			return true
		}

		// check if this session's domain belongs to the proxy
		if session.Domain != nil && session.Domain.ProxyID != nil {
			if session.Domain.ProxyID.String() == proxyID {
				m.DeleteSession(sessionID)
				clearedCount++
				m.Logger.Debugw("cleared session for proxy",
					"sessionID", sessionID,
					"proxyID", proxyID,
					"domain", session.Domain.Name,
				)
			}
		}
		return true
	})

	if clearedCount > 0 {
		m.Logger.Infow("cleared all sessions for proxy",
			"count", clearedCount,
			"proxyID", proxyID,
		)
	}
}

// ClearSessionsForDomains clears all sessions associated with specific phishing domains
func (m *ProxySessionManager) ClearSessionsForDomains(phishingDomains []string) {
	if len(phishingDomains) == 0 {
		return
	}

	// create a map for fast lookup
	domainMap := make(map[string]bool)
	for _, domain := range phishingDomains {
		domainMap[domain] = true
	}

	clearedCount := 0

	m.sessions.Range(func(key, value interface{}) bool {
		sessionID, ok := key.(string)
		if !ok {
			return true
		}
		session, ok := value.(*ProxySession)
		if !ok {
			return true
		}

		// check if this session's domain matches any of the affected domains
		if session.Domain != nil {
			domainName := session.Domain.Name
			if domainMap[domainName] {
				m.DeleteSession(sessionID)
				clearedCount++
				m.Logger.Debugw("cleared session for affected domain",
					"sessionID", sessionID,
					"domain", domainName,
				)
			}
		}
		return true
	})

	if clearedCount > 0 {
		m.Logger.Infow("cleared sessions for affected domains",
			"count", clearedCount,
			"domains", phishingDomains,
		)
	}
}

// CleanupExpiredSessions removes sessions older than maxAge
func (m *ProxySessionManager) CleanupExpiredSessions(maxAge time.Duration) int {
	now := time.Now()
	cleanedCount := 0

	m.sessions.Range(func(key, value interface{}) bool {
		sessionID, ok := key.(string)
		if !ok {
			return true
		}
		session, ok := value.(*ProxySession)
		if !ok {
			m.sessions.Delete(sessionID)
			cleanedCount++
			return true
		}

		sessionAge := now.Sub(session.CreatedAt)
		if sessionAge > maxAge {
			m.DeleteSession(sessionID)
			cleanedCount++
		}
		return true
	})

	if cleanedCount > 0 {
		m.Logger.Debugw("cleaned up expired sessions", "count", cleanedCount)
	}

	return cleanedCount
}
