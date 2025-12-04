package proxy

// aitm.go - adversary-in-the-middle websocket communications
//
// this file handles real-time bidirectional websocket communication between
// aitm static/phishing pages and the backend server. it provides:
//
// - websocket connection management for campaign recipients
// - event tracking from client-side javascript
// - connection health monitoring (ping/pong)
// - message size limits and timeout enforcement
// - future: remote browser control, session hijacking, live credential injection
//
// the websocket endpoint is available at: wss://<domain>/ws/aitm/<recipient-id>
// and is exposed to templates via the {{.AITMWebSocket}} variable.

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/phishingclub/phishingclub/cache"
	"github.com/phishingclub/phishingclub/data"
	"github.com/phishingclub/phishingclub/model"
	"github.com/phishingclub/phishingclub/repository"
	"github.com/phishingclub/phishingclub/vo"
)

const (
	// websocket timeout and health check constants
	websocketReadTimeout    = 60 * time.Second
	websocketWriteTimeout   = 10 * time.Second
	websocketPingInterval   = 30 * time.Second
	websocketMaxMessageSize = 1024 * 1024 // 1MB max message size
)

// aitmwebsocketmessage represents a message sent over the aitm websocket
type AITMWebSocketMessage struct {
	Type  string                 `json:"type"`
	Event string                 `json:"event,omitempty"`
	Data  map[string]interface{} `json:"data,omitempty"`
	Error string                 `json:"error,omitempty"`
}

// checkaitmwebsocketorigin validates the origin of aitm websocket connections
func checkAITMWebSocketOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		// no origin header - allow for same-origin requests
		return true
	}

	// parse the origin
	// allow connections only from the same host
	host := r.Host
	if strings.Contains(host, ":") {
		host = strings.Split(host, ":")[0]
	}

	// check if origin matches the host
	allowedOrigins := []string{
		"https://" + host,
		"http://" + host,
	}

	for _, allowed := range allowedOrigins {
		if strings.HasPrefix(origin, allowed) {
			return true
		}
	}

	return false
}

var aitmUpgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin:     checkAITMWebSocketOrigin,
}

// HandleAITMWebSocket handles websocket connections for aitm static pages
func (m *ProxyHandler) HandleAITMWebSocket(c *gin.Context) {
	recipientIDStr := c.Param("recipientID")
	if recipientIDStr == "" {
		m.logger.Errorw("missing recipient id in websocket request")
		c.JSON(http.StatusBadRequest, gin.H{"error": "missing recipient id"})
		return
	}

	recipientID, err := uuid.Parse(recipientIDStr)
	if err != nil {
		m.logger.Errorw("invalid recipient id in websocket request",
			"recipient_id", recipientIDStr,
			"error", err,
		)
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid recipient id"})
		return
	}

	// verify campaign recipient exists
	campaignRecipient, err := m.CampaignRecipientRepository.GetByID(
		c.Request.Context(),
		&recipientID,
		&repository.CampaignRecipientOption{},
	)
	if err != nil {
		m.logger.Errorw("failed to get campaign recipient for websocket",
			"recipient_id", recipientIDStr,
			"error", err,
		)
		c.JSON(http.StatusNotFound, gin.H{"error": "recipient not found"})
		return
	}

	// upgrade connection to websocket
	conn, err := aitmUpgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		m.logger.Errorw("failed to upgrade websocket connection",
			"recipient_id", recipientIDStr,
			"error", err,
		)
		return
	}
	defer conn.Close()

	// set message size limit to prevent memory exhaustion
	conn.SetReadLimit(websocketMaxMessageSize)

	m.logger.Infow("websocket connection established",
		"recipient_id", recipientIDStr,
		"remote_addr", c.Request.RemoteAddr,
		"origin", c.Request.Header.Get("Origin"),
	)

	// set up pong handler to handle client responses to our pings
	conn.SetPongHandler(func(string) error {
		conn.SetReadDeadline(time.Now().Add(websocketReadTimeout))
		return nil
	})

	// send initial connection success message
	conn.SetWriteDeadline(time.Now().Add(websocketWriteTimeout))
	err = conn.WriteJSON(AITMWebSocketMessage{
		Type: "connected",
	})
	if err != nil {
		m.logger.Errorw("failed to send connection message",
			"recipient_id", recipientIDStr,
			"error", err,
		)
		return
	}

	// channel to signal when to stop ping ticker
	done := make(chan struct{})
	defer close(done)

	// start ping ticker in a goroutine for connection health monitoring
	go func() {
		ticker := time.NewTicker(websocketPingInterval)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				conn.SetWriteDeadline(time.Now().Add(websocketWriteTimeout))
				if err := conn.WriteMessage(websocket.PingMessage, nil); err != nil {
					return
				}
			case <-done:
				return
			case <-c.Request.Context().Done():
				return
			}
		}
	}()

	// set initial read deadline
	conn.SetReadDeadline(time.Now().Add(websocketReadTimeout))

	// read messages from client
	for {
		// check if context is cancelled
		select {
		case <-c.Request.Context().Done():
			m.logger.Infow("websocket context cancelled",
				"recipient_id", recipientIDStr,
			)
			return
		default:
		}

		var msg AITMWebSocketMessage
		err := conn.ReadJSON(&msg)
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				m.logger.Errorw("websocket read error",
					"recipient_id", recipientIDStr,
					"error", err,
				)
			}
			break
		}

		// reset read deadline after successful read
		conn.SetReadDeadline(time.Now().Add(websocketReadTimeout))

		m.logger.Infow("received websocket event from static page",
			"recipient_id", recipientIDStr,
			"event", msg.Event,
			"type", msg.Type,
		)

		// handle the message and create event
		userAgent := c.Request.UserAgent()
		ipAddress := c.ClientIP()
		err = m.handleAITMWebSocketMessage(c.Request.Context(), conn, campaignRecipient, &msg, userAgent, ipAddress)
		if err != nil {
			m.logger.Errorw("failed to handle websocket message",
				"recipient_id", recipientIDStr,
				"error", err,
			)
			conn.SetWriteDeadline(time.Now().Add(websocketWriteTimeout))
			conn.WriteJSON(AITMWebSocketMessage{
				Type:  "error",
				Error: fmt.Sprintf("failed to handle message: %s", err.Error()),
			})
		}
	}

	m.logger.Infow("websocket connection closed",
		"recipient_id", recipientIDStr,
	)
}

// handleaitmwebsocketmessage processes incoming aitm websocket messages and creates events
func (m *ProxyHandler) handleAITMWebSocketMessage(
	ctx context.Context,
	conn *websocket.Conn,
	campaignRecipient *model.CampaignRecipient,
	msg *AITMWebSocketMessage,
	userAgent string,
	ipAddress string,
) error {
	if msg.Type != "event" {
		return fmt.Errorf("invalid message type: %s", msg.Type)
	}

	if msg.Event == "" {
		return fmt.Errorf("missing event name")
	}

	// get event id for websocket event type
	eventID, exists := cache.EventIDByName[data.EVENT_CAMPAIGN_RECIPIENT_WEBSOCKET_EVENT]
	if !exists || eventID == nil {
		return fmt.Errorf("websocket event type not found")
	}

	// prepare event data with event name and data
	eventData := map[string]interface{}{
		"event": msg.Event,
	}
	if msg.Data != nil {
		eventData["data"] = msg.Data
	}

	eventDataJSON, err := json.Marshal(eventData)
	if err != nil {
		return fmt.Errorf("failed to marshal event data: %w", err)
	}

	// get campaign id
	campaignID, err := campaignRecipient.CampaignID.Get()
	if err != nil {
		return fmt.Errorf("failed to get campaign id: %w", err)
	}

	// get recipient id
	recipientID, err := campaignRecipient.RecipientID.Get()
	if err != nil {
		return fmt.Errorf("failed to get recipient id: %w", err)
	}

	// get campaign recipient id
	crID, err := campaignRecipient.ID.Get()
	if err != nil {
		return fmt.Errorf("failed to get campaign recipient id: %w", err)
	}

	// create campaign event using model
	newEventID := uuid.New()
	campaignEvent := &model.CampaignEvent{
		ID:          &newEventID,
		CampaignID:  &campaignID,
		RecipientID: &recipientID,
		EventID:     eventID,
		Data:        vo.NewOptionalString1MBMust(string(eventDataJSON)),
		UserAgent:   vo.NewOptionalString255Must(userAgent),
		IP:          vo.NewOptionalString64Must(ipAddress),
		Metadata:    vo.NewOptionalString1MBMust(""),
	}

	err = m.CampaignRepository.SaveEvent(ctx, campaignEvent)
	if err != nil {
		return fmt.Errorf("failed to save event: %w", err)
	}

	m.logger.Infow("created websocket event",
		"campaign_id", campaignID.String(),
		"recipient_id", recipientID.String(),
		"campaign_recipient_id", crID.String(),
		"event", msg.Event,
	)

	// send success response
	conn.SetWriteDeadline(time.Now().Add(websocketWriteTimeout))
	err = conn.WriteJSON(AITMWebSocketMessage{
		Type:  "success",
		Event: msg.Event,
	})
	if err != nil {
		return fmt.Errorf("failed to send success response: %w", err)
	}

	return nil
}
