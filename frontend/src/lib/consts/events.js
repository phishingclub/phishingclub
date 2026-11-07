// Human-readable display names for campaign events, shared so the campaign
// wizard and the script editor label events the same way instead of showing
// the raw internal names.
export const eventDisplayNames = {
	campaign_closed: 'Campaign Closed',
	campaign_paused: 'Sending Paused',
	campaign_resumed: 'Sending Resumed',
	campaign_recipient_message_sent: 'Message Sent',
	campaign_recipient_message_failed: 'Message Failed',
	campaign_recipient_message_read: 'Message Read',
	campaign_recipient_submitted_data: 'Submitted Data',
	campaign_recipient_reported: 'Reported',
	campaign_recipient_evasion_page_visited: 'Evasion Page Visited',
	campaign_recipient_before_page_visited: 'Before Page Visited',
	campaign_recipient_page_visited: 'Page Visited',
	campaign_recipient_after_page_visited: 'After Page Visited',
	campaign_recipient_deny_page_visited: 'Deny Page Visited',
	campaign_recipient_training_started: 'Training Started',
	campaign_recipient_training_completed: 'Training Completed'
};

// eventDisplayLabel returns the friendly label for an event name, falling back
// to the raw name for anything unmapped.
export const eventDisplayLabel = (name) => eventDisplayNames[name] || name;

// events a webhook can listen to, in display order
export const webhookEventOptions = [
	'campaign_recipient_message_sent',
	'campaign_recipient_message_failed',
	'campaign_recipient_message_read',
	'campaign_recipient_submitted_data',
	'campaign_recipient_page_visited',
	'campaign_recipient_before_page_visited',
	'campaign_recipient_after_page_visited',
	'campaign_recipient_evasion_page_visited',
	'campaign_recipient_deny_page_visited',
	'campaign_recipient_training_started',
	'campaign_recipient_training_completed',
	'campaign_closed'
];

// bit of each webhook event, must match backend data.WebhookEventToBit
const webhookEventToBit = {
	campaign_closed: 1 << 0,
	campaign_recipient_message_sent: 1 << 1,
	campaign_recipient_message_failed: 1 << 2,
	campaign_recipient_message_read: 1 << 3,
	campaign_recipient_submitted_data: 1 << 4,
	campaign_recipient_evasion_page_visited: 1 << 5,
	campaign_recipient_before_page_visited: 1 << 6,
	campaign_recipient_page_visited: 1 << 7,
	campaign_recipient_after_page_visited: 1 << 8,
	campaign_recipient_deny_page_visited: 1 << 9,
	campaign_recipient_training_started: 1 << 10,
	campaign_recipient_training_completed: 1 << 11
};
const webhookEventAllBits = 4095;

// event names to the stored bit mask, 0 means all events
export const webhookEventsToBinary = (events) => {
	if (!events?.length) return 0;
	const binary = events.reduce((acc, event) => acc | (webhookEventToBit[event] || 0), 0);
	return binary === webhookEventAllBits ? 0 : binary;
};

// stored bit mask to event names, 0 means all events
export const webhookEventsFromBinary = (binary) => {
	if (!binary) return [...webhookEventOptions];
	return webhookEventOptions.filter((event) => binary & webhookEventToBit[event]);
};

// how much data a webhook gets
export const webhookDataLevelOptions = [
	{ label: 'Minimum', value: 'none' },
	{ label: 'Basic', value: 'basic' },
	{ label: 'Full', value: 'full' }
];
