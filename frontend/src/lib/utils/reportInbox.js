// Status of the reporting inbox that covers a campaign or a program: the
// company inbox, or the global one when there is no company.

// checks run every 2 minutes and one can take up to 3 minutes, so a healthy
// inbox is checked within about 6 minutes. older than the first limit is a
// delay, older than the second means the checker is not running
export const REPORT_INBOX_WARN_MINUTES = 8;
export const REPORT_INBOX_STALE_MINUTES = 15;

const toneOf = {
	Active: 'success',
	Delayed: 'warning',
	'Not checked yet': 'warning',
	Error: 'danger',
	'Not checking': 'danger'
};

// compact date and time in the browser zone
const shortDateTime = (instant) =>
	instant
		? new Date(instant).toLocaleString(undefined, {
				year: '2-digit',
				month: 'numeric',
				day: 'numeric',
				hour: 'numeric',
				minute: '2-digit'
			})
		: '';

/**
 * the status label, badge tone and last check text of an inbox, null without one
 * @param {any} inbox
 * @param {number} now in ms
 */
export const reportInboxStatus = (inbox, now) => {
	if (!inbox) return null;
	// counts from the last check, or from when it was turned on if it never checked
	const checkedAt = inbox.lastPolledAt || inbox.readSince || null;
	const age = checkedAt
		? Math.max(0, Math.floor((now - new Date(checkedAt).getTime()) / 60000))
		: null;
	const label = !inbox.enabled
		? 'Disabled'
		: inbox.lastError
			? 'Error'
			: age === null
				? 'Not checked yet'
				: age > REPORT_INBOX_STALE_MINUTES
					? 'Not checking'
					: age >= REPORT_INBOX_WARN_MINUTES
						? 'Delayed'
						: 'Active';
	const lastCheck = !inbox.enabled
		? ''
		: age === null
			? 'Last check never'
			: `Last check ${shortDateTime(checkedAt)}`;
	return { label, tone: toneOf[label] ?? 'neutral', lastCheck };
};

/**
 * where the inbox settings are. The global inbox is only managed in the global
 * context, so a global item viewed in a company context gets no link.
 * @param {string|null} companyID of the campaign or program
 * @param {string|null} contextCompanyID of the current view
 */
export const reportInboxSettingsHref = (companyID, contextCompanyID) =>
	companyID ? `/company/${companyID}#reporting` : contextCompanyID ? null : '/settings#reporting';
