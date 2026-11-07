// text helpers shared by the program list, wizard and detail pages

export const MINUTES_PER_HOUR = 60;
export const MINUTES_PER_DAY = 1440;
export const MINUTES_PER_WEEK = 10080;
export const MINUTES_PER_MONTH = 43200;
// time between emails for a new program, 2 weeks
export const DEFAULT_INTERVAL_MINUTES = 2 * MINUTES_PER_WEEK;
// the weekday bitmask with all seven days set
export const ALL_WEEK_DAYS = 127;
// groups and periods with fewer people or emails than this are hidden
export const PROGRAM_MIN_GROUP = 5;

/**
 * "1 email", "3 emails", "2 people"
 * @param {number} n
 * @param {string} one
 * @param {string} [many]
 */
export const plural = (n, one, many = `${one}s`) => `${n} ${n === 1 ? one : many}`;

/**
 * a length of time in minutes as "2 weeks", "1 month", "3 days"
 * @param {number} minutes
 */
export const describeDuration = (minutes) => {
	if (!minutes) return '';
	if (minutes % MINUTES_PER_MONTH === 0) return plural(minutes / MINUTES_PER_MONTH, 'month');
	if (minutes % MINUTES_PER_WEEK === 0) return plural(minutes / MINUTES_PER_WEEK, 'week');
	if (minutes % MINUTES_PER_DAY === 0) return plural(minutes / MINUTES_PER_DAY, 'day');
	if (minutes % MINUTES_PER_HOUR === 0) return plural(minutes / MINUTES_PER_HOUR, 'hour');
	return plural(minutes, 'minute');
};

/**
 * the time between emails as "every week", "every 2 weeks"
 * @param {number} minutes
 */
export const describeInterval = (minutes) => {
	if (minutes === MINUTES_PER_WEEK) return 'every week';
	if (minutes === MINUTES_PER_MONTH) return 'every month';
	return `every ${describeDuration(minutes)}`;
};

// week days in Monday first order, index 0 is Sunday like the backend bitmask
export const weekDayOrder = [1, 2, 3, 4, 5, 6, 0];
const weekDayLabels = ['Sun', 'Mon', 'Tue', 'Wed', 'Thu', 'Fri', 'Sat'];

/**
 * the backend weekday bitmask as a list of seven booleans, index 0 is Sunday
 * @param {number|null} mask
 */
export const weekDaysFromMask = (mask) =>
	weekDayLabels.map((_, i) => mask != null && (mask & (1 << i)) !== 0);

/**
 * the chosen days as "Mon to Fri" or "Mon, Wed"
 * @param {boolean[]} weekDays seven booleans, index 0 is Sunday
 */
export const describeDays = (weekDays) => {
	const picked = weekDayOrder.filter((d) => weekDays[d]);
	if (picked.length === 0) return 'no days';
	if (picked.length === 7) return 'every day';
	const first = weekDayOrder.indexOf(picked[0]);
	const last = weekDayOrder.indexOf(picked[picked.length - 1]);
	if (picked.length >= 3 && last - first + 1 === picked.length) {
		return `${weekDayLabels[picked[0]]} to ${weekDayLabels[picked[picked.length - 1]]}`;
	}
	return picked.map((d) => weekDayLabels[d]).join(', ');
};

/** upper case the first letter */
export const capitalize = (text) => (text ? text[0].toUpperCase() + text.slice(1) : text);

// the time zone of the browser
export const browserTimeZone = Intl.DateTimeFormat().resolvedOptions().timeZone || 'UTC';

// the wall clock parts of a moment in a zone
const zoneParts = (date, zone) => {
	const parts = new Intl.DateTimeFormat('en-GB', {
		timeZone: zone,
		year: 'numeric',
		month: '2-digit',
		day: '2-digit',
		hour: '2-digit',
		minute: '2-digit',
		hourCycle: 'h23'
	}).formatToParts(date);
	const get = (type) => parts.find((p) => p.type === type)?.value || '00';
	return {
		year: get('year'),
		month: get('month'),
		day: get('day'),
		hour: get('hour'),
		minute: get('minute')
	};
};

/**
 * a moment as "YYYY-MM-DDTHH:MM" on the wall clock of a zone, for a
 * datetime-local input
 * @param {string|Date} value
 * @param {string} zone
 */
export const toZoneLocal = (value, zone) => {
	try {
		const p = zoneParts(new Date(value), zone);
		return `${p.year}-${p.month}-${p.day}T${p.hour}:${p.minute}`;
	} catch (e) {
		return '';
	}
};

/**
 * a "YYYY-MM-DDTHH:MM" wall clock time in a zone as a Date, null when invalid
 * @param {string} local
 * @param {string} zone
 */
export const fromZoneLocal = (local, zone) => {
	const m = /^(\d{4})-(\d{2})-(\d{2})T(\d{2}):(\d{2})$/.exec(local || '');
	if (!m) return null;
	try {
		const wanted = Date.UTC(+m[1], +m[2] - 1, +m[3], +m[4], +m[5]);
		// guess, then correct by the zone offset twice so a DST change is handled
		let t = wanted;
		for (let i = 0; i < 2; i++) {
			const p = zoneParts(new Date(t), zone);
			const seen = Date.UTC(+p.year, +p.month - 1, +p.day, +p.hour, +p.minute);
			t += wanted - seen;
		}
		return new Date(t);
	} catch (e) {
		return null;
	}
};

/**
 * a moment as "Mon 9 Nov 2026, 09:00" in a zone, or in the browser zone
 * @param {string|Date} value
 * @param {string} [zone]
 */
export const formatDateTime = (value, zone) => {
	if (!value) return '';
	try {
		const parts = new Intl.DateTimeFormat('en-GB', {
			timeZone: zone || undefined,
			weekday: 'short',
			day: 'numeric',
			month: 'short',
			year: 'numeric',
			hour: '2-digit',
			minute: '2-digit',
			hourCycle: 'h23'
		}).formatToParts(new Date(value));
		const get = (type) => parts.find((p) => p.type === type)?.value || '';
		return `${get('weekday')} ${get('day')} ${get('month')} ${get('year')}, ${get('hour')}:${get('minute')}`;
	} catch (e) {
		return String(value);
	}
};

/**
 * a calendar day as "Mon 9 Nov", the Date is read in UTC
 * @param {Date} day
 */
export const formatWeekDay = (day) =>
	new Intl.DateTimeFormat('en-GB', {
		timeZone: 'UTC',
		weekday: 'short',
		day: 'numeric',
		month: 'short'
	}).format(day);

/**
 * a "YYYY-MM-DD" date as "24 Dec 2026"
 * @param {string} day
 */
const formatDay = (day) => {
	const m = /^(\d{4})-(\d{2})-(\d{2})$/.exec(day || '');
	if (!m) return day || '';
	return new Intl.DateTimeFormat('en-GB', {
		timeZone: 'UTC',
		day: 'numeric',
		month: 'short',
		year: 'numeric'
	}).format(new Date(Date.UTC(+m[1], +m[2] - 1, +m[3])));
};

/**
 * skip dates as "24 Dec 2026" or "24 Dec 2026 to 2 Jan 2027"
 * @param {{from: string, to: string}} range
 */
export const describeBlackout = (range) =>
	range.from === range.to
		? formatDay(range.from)
		: `${formatDay(range.from)} to ${formatDay(range.to)}`;

// a "YYYY-MM-DD" day as "24 Dec", with the year when it is not within a year from now
const formatShortDay = (day) => {
	const m = /^(\d{4})-(\d{2})-(\d{2})$/.exec(day || '');
	if (!m) return day || '';
	const at = Date.UTC(+m[1], +m[2] - 1, +m[3]);
	const now = Date.now();
	const near = at > now - 30 * 86400000 && at < now + 330 * 86400000;
	return new Intl.DateTimeFormat('en-GB', {
		timeZone: 'UTC',
		day: 'numeric',
		month: 'short',
		year: near ? undefined : 'numeric'
	}).format(new Date(at));
};

/**
 * skip dates as "24 Dec" or "24 Dec to 1 Jan", the year shows only when the
 * dates are more than a year away
 * @param {{from: string, to: string}} range
 */
export const describeBlackoutShort = (range) =>
	range.from === range.to
		? formatShortDay(range.from)
		: `${formatShortDay(range.from)} to ${formatShortDay(range.to)}`;

/**
 * a moment as "3 Nov 2026" in the browser zone
 * @param {string|Date} value
 */
export const formatDate = (value) =>
	value
		? new Date(value).toLocaleDateString('en-GB', {
				day: 'numeric',
				month: 'short',
				year: 'numeric'
			})
		: '';

/**
 * a part of the sent emails as a percentage, null when nothing was sent
 * @param {number|null} part
 * @param {number} sent
 */
export const ratePercent = (part, sent) => (sent > 0 && part != null ? (part / sent) * 100 : null);

// StatusBadge tone for each risk level and template difficulty
export const riskTone = { low: 'success', medium: 'warning', high: 'danger' };
export const difficultyTone = { Easy: 'success', Medium: 'warning', 'Hard to spot': 'danger' };

/**
 * the state of a program as a StatusBadge label and tone
 * @param {{status?: string, closedAt?: string, pausedAt?: string, needsAttention?: boolean, warnings?: string[]}} program
 */
export const programStatus = (program) => {
	if (!program) return { label: '', tone: 'neutral' };
	if (program.closedAt || program.status === 'closed') return { label: 'Closed', tone: 'neutral' };
	if (program.status === 'draft') return { label: 'Draft', tone: 'neutral' };
	if (program.status === 'scheduled') return { label: 'Scheduled', tone: 'info' };
	if (program.pausedAt) return { label: 'Paused', tone: 'warning' };
	if (program.needsAttention || program.warnings?.length) {
		return { label: 'Needs attention', tone: 'warning' };
	}
	return { label: 'Active', tone: 'success' };
};

// tooltip for a value hidden to protect anonymity
export const HIDDEN_TIP = `Fewer than ${PROGRAM_MIN_GROUP} people or emails, hidden to protect anonymity`;
// how the risk score of a person is made
export const RISK_TIP =
	'Average over the last 12 months. Clicking or submitting raises it, reporting and training lower it. Needs 3 emails.';
// tooltip for a person without a risk score yet
export const RISK_NONE_TIP =
	'Needs 3 emails. Clicks and submits raise it, reports lower it. Last 12 months.';
// shown for a rate or score that has nothing counted yet
export const NONE_YET = 'None yet';
