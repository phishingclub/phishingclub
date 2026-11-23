import { toEvent } from './events.js';

// the status word and badge tone of a campaign, the same on every page.
// Paused wins while the campaign is open. The tones are the campaign colors
// of the calendar legend: active, self managed, closed.
export const campaignStatus = (c) => {
	if (c.pausedAt && !c.closedAt) return { label: 'Paused', tone: 'warning' };
	const event = c.notableEventName;
	const label = event ? toEvent(event).name : '';
	if (c.closedAt || event === 'campaign_closed' || event === 'campaign_pending_schedule') {
		return { label, tone: 'neutral' };
	}
	if (event === 'campaign_active') return { label, tone: 'active' };
	if (event === 'campaign_self_managed') return { label, tone: 'managed' };
	return { label, tone: 'info' };
};
