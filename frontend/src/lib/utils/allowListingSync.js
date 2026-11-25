// Labels and tones for the allow listing sync: the health of a target, and
// the status of one campaign or program on its target.

// the names Microsoft Defender Advanced Delivery uses for each list
export const ALLOW_LISTING_KIND_LABELS = {
	domain: 'Domain',
	ip: 'Sending IP',
	url: 'Simulation URL'
};

export const kindLabel = (kind) => ALLOW_LISTING_KIND_LABELS[kind] ?? kind;

const targetTones = {
	'Not configured': 'neutral',
	'Removing values': 'warning',
	Disabled: 'warning',
	Error: 'danger',
	'Waiting for first report': 'warning',
	'Report too old': 'danger',
	Synced: 'success'
};

/**
 * the health label and badge tone of a target, null means not configured
 * @param {any} target
 * @param {number} now in ms
 */
export const allowListingTargetStatus = (target, now = Date.now()) => {
	let label;
	if (!target) {
		label = 'Not configured';
	} else if (target.deleteRequestedAt) {
		label = 'Removing values';
	} else if (!target.enabled) {
		label = 'Disabled';
	} else if (target.lastError) {
		label = 'Error';
	} else if (!target.lastReportAt) {
		label = 'Waiting for first report';
	} else if (
		now - new Date(target.lastReportAt).getTime() >
		(target.maxReportAgeMinutes ?? 0) * 60000
	) {
		label = 'Report too old';
	} else {
		label = 'Synced';
	}
	return { label, tone: targetTones[label] ?? 'neutral' };
};

const sourceStatus = {
	confirmed: { label: 'Confirmed', tone: 'success' },
	waiting: { label: 'Waiting for sync', tone: 'warning' },
	stale: { label: 'Report too old', tone: 'danger' },
	overLimit: { label: 'Over Microsoft limit', tone: 'danger' },
	incomplete: { label: 'Missing values', tone: 'danger' },
	disabled: { label: 'Sync turned off', tone: 'neutral' },
	notCounting: { label: 'Not needed yet', tone: 'neutral' }
};

/**
 * the label and tone of a campaign, program or pin status
 * @param {string} status
 * @param {boolean} [closed] a closed campaign or program is no longer needed
 */
export const allowListingStatus = (status, closed = false) => {
	if (status === 'notCounting' && closed) {
		return { label: 'Not needed', tone: 'neutral' };
	}
	return sourceStatus[status] ?? { label: status, tone: 'neutral' };
};

/**
 * the tooltip of a campaign or program status: the values not in the
 * tenant yet and why values are missing
 * @param {any} s the status response
 */
export const allowListingStatusTip = (s) => {
	if (!s) return '';
	const lines = [];
	if (s.missing?.length) {
		lines.push('Not in the tenant yet:');
		for (const m of s.missing) lines.push(`${kindLabel(m.kind)}: ${m.value}`);
	}
	if (s.incomplete?.length) {
		lines.push(...s.incomplete);
	}
	return lines.join('\n');
};

/**
 * where the sync settings of a campaign or program are. The global target is
 * only managed in the global context, so a global item viewed in a company
 * context gets no link.
 * @param {string|null} companyID of the campaign or program
 * @param {string|null} contextCompanyID of the current view
 */
export const allowListingSettingsHref = (companyID, contextCompanyID) =>
	companyID
		? `/company/${companyID}#integrations`
		: contextCompanyID
			? null
			: '/settings#allowlisting';

// time choices in minutes for the target settings
const HOUR = 60;
const DAY = 1440;
export const LEAD_PRESETS = [0, HOUR, 6 * HOUR, 12 * HOUR, DAY, 2 * DAY, 3 * DAY, 7 * DAY, 14 * DAY, 30 * DAY];
export const GRACE_PRESETS = [0, HOUR, 6 * HOUR, DAY, 2 * DAY, 3 * DAY, 7 * DAY, 14 * DAY, 30 * DAY];
// the schedules the runner may run on, an hour is the shortest Azure
// Automation allows
export const INTERVAL_PRESETS = [HOUR, 3 * HOUR, 6 * HOUR, 12 * HOUR, DAY];

/**
 * how often the schedule in the tenant seems to run when the time between
 * the last two reports does not match the interval, else null
 * @param {any} target
 * @returns {number|null} the minutes between the last two reports
 */
export const allowListingScheduleMismatch = (target) => {
	const gap = target?.reportGapMinutes ?? 0;
	const interval = target?.intervalMinutes ?? 0;
	if (!gap || !interval) return null;
	if (gap >= interval * 0.6 && gap <= interval * 1.6) return null;
	// whole hours read better than a count of minutes
	return gap >= 120 ? Math.round(gap / 60) * 60 : gap;
};

/**
 * a length of time in minutes as "3 days", "6 hours", "15 minutes"
 * @param {number} minutes
 */
export const describeMinutes = (minutes) => {
	const unit = (n, word) => `${n} ${word}${n === 1 ? '' : 's'}`;
	if (!minutes) return 'None';
	if (minutes % DAY === 0) return unit(minutes / DAY, 'day');
	if (minutes % HOUR === 0) return unit(minutes / HOUR, 'hour');
	return unit(minutes, 'minute');
};

/**
 * the presets plus a stored value that is not one of them, as select options
 * @param {number[]} presets
 * @param {number|null|undefined} value
 */
export const minuteOptions = (presets, value) => {
	const list =
		value == null || presets.includes(value) ? presets : [...presets, value].sort((a, b) => a - b);
	return list.map((m) => ({ value: String(m), label: describeMinutes(m) }));
};
