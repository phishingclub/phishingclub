// map between system event name and a human readable event name
const eventNameMap = {
	// campaign recipient events
	campaign_recipient_scheduled: { name: 'Scheduled', priority: 10, color: 'bg-scheduled' },
	campaign_recipient_cancelled: { name: 'Cancelled', priority: 15, color: 'bg-black' },
	campaign_recipient_message_sent: { name: 'Message Sent', priority: 30, color: 'bg-message-sent' },
	campaign_recipient_message_failed: {
		name: 'Failed Sending',
		priority: 80,
		color: 'bg-failed-sending'
	},
	campaign_recipient_message_read: { name: 'Message Read', priority: 40, color: 'bg-message-read' },
	campaign_recipient_evasion_page_visited: {
		name: 'Evasion Page Visited',
		priority: 45,
		color: 'bg-evasion-page-visited'
	},
	campaign_recipient_before_page_visited: {
		name: 'Before Page Visited',
		priority: 50,
		color: 'bg-before-page-visited'
	},
	campaign_recipient_page_visited: { name: 'Page Visited', priority: 60, color: 'bg-page-visited' },
	campaign_recipient_after_page_visited: {
		name: 'After Page Visited',
		priority: 70,
		color: 'bg-after-page-visited'
	},
	campaign_recipient_submitted_data: {
		name: 'Submitted Data',
		priority: 90,
		color: 'bg-submitted-data'
	},
	campaign_recipient_reported: {
		name: 'Reported',
		priority: 95,
		color: 'bg-reported'
	},
	// campaign events
	campaign_scheduled: { name: 'Scheduled', priority: 10 },
	campaign_active: { name: 'Active', priority: 20 },
	campaign_self_managed: { name: 'Self managed', priority: 20 },
	campaign_closed: { name: 'Closed', priority: 30, color: 'bg-closed' }
};

/**
 * @param {string} systemEventName
 * @returns {{name: string, priority: number, color: string}}
 */
export const toEvent = (systemEventName) => {
	if (systemEventName === '') {
		return { name: '', priority: 0, color: '' };
	}
	return eventNameMap[systemEventName] ?? { name: 'Unknown Event', priority: 80, color: '' };
};
