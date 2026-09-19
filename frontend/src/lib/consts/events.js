// Human-readable display names for campaign events, shared so the campaign
// wizard and the script editor label events the same way instead of showing
// the raw internal names.
export const eventDisplayNames = {
	campaign_closed: 'Campaign Closed',
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
