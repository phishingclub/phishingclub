package cache

import (
	"sync/atomic"

	"github.com/google/uuid"
	"github.com/phishingclub/phishingclub/data"
)

// EventIDByName is a map of event names to event IDs
var EventIDByName = map[string]*uuid.UUID{}

// EventNameByID is a map of event ids to names
// this is not safe before the API is up an running entirely
var EventNameByID = map[string]string{}

var isUpdateAvailable atomic.Bool

func init() {
	for _, name := range data.Events {
		EventIDByName[name] = nil
	}
	isUpdateAvailable.Store(false)
}

func SetUpdateAvailable(updateAvailable bool) {
	isUpdateAvailable.Store(updateAvailable)
}

func IsUpdateAvailable() bool {
	return isUpdateAvailable.Load()
}

// TODO all priority event functions should be in utils or something, and the priority in the data package.
// var CampaignEventPriority = map[]
// Add priority rankings (higher number = higher priority)
// readonly
var CampaignEventPriority = map[string]int{
	// campaign recipient events
	data.EVENT_CAMPAIGN_RECIPIENT_REPORTED:             90,
	data.EVENT_CAMPAIGN_RECIPIENT_CANCELLED:            80,
	data.EVENT_CAMPAIGN_RECIPIENT_SUBMITTED_DATA:       70,
	data.EVENT_CAMPAIGN_RECIPIENT_AFTER_PAGE_VISITED:   60,
	data.EVENT_CAMPAIGN_RECIPIENT_BEFORE_PAGE_VISITED:  40,
	data.EVENT_CAMPAIGN_RECIPIENT_PAGE_VISITED:         50,
	data.EVENT_CAMPAIGN_RECIPIENT_DENY_PAGE_VISITED:    38,
	data.EVENT_CAMPAIGN_RECIPIENT_EVASION_PAGE_VISITED: 35,
	data.EVENT_CAMPAIGN_RECIPIENT_MESSAGE_READ:         30,
	data.EVENT_CAMPAIGN_RECIPIENT_MESSAGE_FAILED:       20,
	data.EVENT_CAMPAIGN_RECIPIENT_MESSAGE_SENT:         20,
	data.EVENT_CAMPAIGN_RECIPIENT_SCHEDULED:            10,
	// campaign events
	data.EVENT_CAMPAIGN_CLOSED:       30,
	data.EVENT_CAMPAIGN_ACTIVE:       20,
	data.EVENT_CAMPAIGN_SELF_MANAGED: 20,
	data.EVENT_CAMPAIGN_SCHEDULED:    10,
}

// IsMoreNotableCampaignRecipientEvent returns true if newEvent is more notable than currentEvent
func IsMoreNotableCampaignRecipientEvent(newEvent, currentEvent string) bool {
	newPriority, newExists := CampaignEventPriority[newEvent]
	currentPriority, currentExists := CampaignEventPriority[currentEvent]

	// If either event doesn't exist in our priority map, treat it as lowest priority
	if !newExists || !currentExists {
		return false
	}

	return newPriority > currentPriority
}
func IsMoreNotableCampaignRecipientEventID(currentID, newID *uuid.UUID) bool {
	if currentID == nil || currentID.String() == uuid.Nil.String() {
		return true
	}
	if newID == nil {
		return false
	}

	newEventName := EventNameByID[newID.String()]
	currentEventName := EventNameByID[currentID.String()]

	return IsMoreNotableCampaignRecipientEvent(newEventName, currentEventName)
}

// GetEventPriority returns the priority for an event type id, or -1 if not found
func GetEventPriority(eventTypeID *uuid.UUID) int {
	if eventTypeID == nil {
		return -1
	}
	eventName := EventNameByID[eventTypeID.String()]
	priority, exists := CampaignEventPriority[eventName]
	if !exists {
		return -1
	}
	return priority
}
