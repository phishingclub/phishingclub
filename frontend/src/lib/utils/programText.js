// text helpers shared by the program list, wizard and detail pages

export const MINUTES_PER_HOUR = 60;
export const MINUTES_PER_DAY = 1440;
export const MINUTES_PER_WEEK = 10080;
export const MINUTES_PER_MONTH = 43200;
// time between templates for a new program, 2 weeks
export const DEFAULT_INTERVAL_MINUTES = 2 * MINUTES_PER_WEEK;
// the weekday bitmask with all seven days set
export const ALL_WEEK_DAYS = 127;
// groups and periods with fewer recipients or emails than this are hidden
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
 * the time between templates as "every week", "every 2 weeks"
 * @param {number} minutes
 */
export const describeInterval = (minutes) => {
	if (minutes === MINUTES_PER_DAY) return 'every day';
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

// one formatter per zone, making a formatter is slow and the schedule maths
// reads the wall clock many times
const zoneFormats = new Map();
const zoneFormat = (zone) => {
	let format = zoneFormats.get(zone);
	if (!format) {
		format = new Intl.DateTimeFormat('en-GB', {
			timeZone: zone,
			year: 'numeric',
			month: '2-digit',
			day: '2-digit',
			hour: '2-digit',
			minute: '2-digit',
			hourCycle: 'h23'
		});
		zoneFormats.set(zone, format);
	}
	return format;
};

// the wall clock parts of a moment in a zone
const zoneParts = (date, zone) => {
	const parts = zoneFormat(zone).formatToParts(date);
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
 * a moment as a short date and time in the browser locale, like the campaign
 * Timeline card, in a zone or in the browser zone
 * @param {string|Date} value
 * @param {string} [zone]
 */
export const formatDateTime = (value, zone) => {
	if (!value) return '';
	try {
		return new Date(value).toLocaleString(undefined, {
			timeZone: zone || undefined,
			year: '2-digit',
			month: 'numeric',
			day: 'numeric',
			hour: 'numeric',
			minute: '2-digit'
		});
	} catch (e) {
		return String(value);
	}
};

/**
 * a moment as a short date without the time in the browser locale, in a zone
 * or in the browser zone
 * @param {string|Date} value
 * @param {string} [zone]
 */
export const formatDate = (value, zone) => {
	if (!value) return '';
	try {
		return new Date(value).toLocaleDateString(undefined, {
			timeZone: zone || undefined,
			year: '2-digit',
			month: 'numeric',
			day: 'numeric'
		});
	} catch (e) {
		return String(value);
	}
};

/**
 * a calendar day with its week day in the browser locale, the Date is read in UTC
 * @param {Date} day
 */
export const formatWeekDay = (day) =>
	new Intl.DateTimeFormat(undefined, {
		timeZone: 'UTC',
		weekday: 'short',
		day: 'numeric',
		month: 'short'
	}).format(day);

/**
 * the month and year of a time in a time zone, in the browser locale
 * @param {string|Date} at
 * @param {string} timeZone
 */
export const formatMonthYear = (at, timeZone) =>
	new Intl.DateTimeFormat(undefined, {
		timeZone: timeZone || 'UTC',
		month: 'long',
		year: 'numeric'
	}).format(new Date(at));

/**
 * a "YYYY-MM-DD" date with the month name in the browser locale
 * @param {string} day
 */
const formatDay = (day) => {
	const m = /^(\d{4})-(\d{2})-(\d{2})$/.exec(day || '');
	if (!m) return day || '';
	return new Intl.DateTimeFormat(undefined, {
		timeZone: 'UTC',
		day: 'numeric',
		month: 'short',
		year: 'numeric'
	}).format(new Date(Date.UTC(+m[1], +m[2] - 1, +m[3])));
};

/**
 * whether a "YYYY-MM-DD" day falls in a skip range. A yearly range compares
 * the month and day only and may run over the new year, like the backend
 * @param {{from: string, to: string, yearly?: boolean}} range
 * @param {string} key
 */
export const blackoutContains = (range, key) => {
	if (!range?.from || !range?.to) return false;
	if (!range.yearly) return key >= range.from && key <= range.to;
	const day = key.slice(5);
	const from = range.from.slice(5);
	const to = range.to.slice(5);
	if (range.from.slice(0, 4) === range.to.slice(0, 4)) return day >= from && day <= to;
	return day >= from || day <= to;
};

// holidays offered as yearly skip dates, as month and day
export const HOLIDAY_PRESETS = [
	{ name: "New Year's Day", day: '01-01' },
	{ name: 'Christmas Eve', day: '12-24' },
	{ name: 'Christmas Day', day: '12-25' },
	{ name: 'Boxing Day', day: '12-26' },
	{ name: "New Year's Eve", day: '12-31' }
];

/**
 * a "YYYY-MM-DD" date as day and month name in the browser locale, without the year
 * @param {string} day
 */
const formatMonthDay = (day) => {
	const m = /^(\d{4})-(\d{2})-(\d{2})$/.exec(day || '');
	if (!m) return day || '';
	return new Intl.DateTimeFormat(undefined, {
		timeZone: 'UTC',
		day: 'numeric',
		month: 'short'
	}).format(new Date(Date.UTC(+m[1], +m[2] - 1, +m[3])));
};

/**
 * a yearly skip range as "25 Dec every year" or "24 Dec to 1 Jan every year"
 * @param {{from: string, to: string}} range
 */
const describeYearly = (range) =>
	range.from === range.to
		? `${formatMonthDay(range.from)} every year`
		: `${formatMonthDay(range.from)} to ${formatMonthDay(range.to)} every year`;

/**
 * skip dates as one day or "from to", or for a yearly range "25 Dec every year"
 * @param {{from: string, to: string, yearly?: boolean}} range
 */
export const describeBlackout = (range) => {
	if (range.yearly) return describeYearly(range);
	return range.from === range.to
		? formatDay(range.from)
		: `${formatDay(range.from)} to ${formatDay(range.to)}`;
};

/**
 * a part of the sent emails as a percentage, null when nothing was sent
 * @param {number|null} part
 * @param {number} sent
 */
export const ratePercent = (part, sent) => (sent > 0 && part != null ? (part / sent) * 100 : null);

// the list status filter values the backend accepts with their labels
export const PROGRAM_LIST_STATUSES = [
	{ value: 'draft', label: 'Draft' },
	{ value: 'scheduled', label: 'Scheduled' },
	{ value: 'active', label: 'Active' },
	{ value: 'needs_attention', label: 'Needs attention' },
	{ value: 'paused', label: 'Paused' },
	{ value: 'closed', label: 'Closed' }
];

/**
 * the close or delete a program is in, 'close', 'delete' or ''. A close or
 * delete runs in the background with closingAt or deletingAt set and the
 * program paused. One that stopped half way keeps them, and only that action
 * can be tried again.
 * @param {{closedAt?: string, closingAt?: string, deletingAt?: string}} program
 */
export const programStoppingAction = (program) => {
	if (program?.deletingAt) return 'delete';
	if (program?.closingAt && !program.closedAt) return 'close';
	return '';
};

/**
 * the state of a program as a StatusBadge label, tone and tooltip. The status
 * field is draft, scheduled, active, paused or closed, needs attention comes
 * from needsAttention or warnings. A close or delete that did not finish
 * shows as Closing or Deleting, a resume that runs as Resuming.
 * @param {{status?: string, closedAt?: string, pausedAt?: string, closingAt?: string, deletingAt?: string, resuming?: boolean, needsAttention?: boolean, warnings?: string[]}} program
 */
export const programStatus = (program) => {
	if (!program) return { label: '', tone: 'neutral' };
	const stopping = programStoppingAction(program);
	if (stopping === 'delete') {
		return {
			label: 'Deleting',
			tone: 'danger',
			tip: 'The program is stopped and deleted in the background. If it stays here, use Retry Delete.'
		};
	}
	if (stopping === 'close') {
		return {
			label: 'Closing',
			tone: 'warning',
			tip: 'Sending has stopped and the close finishes in the background. If it stays here, use Retry Close.'
		};
	}
	if (program.closedAt || program.status === 'closed') return { label: 'Closed', tone: 'neutral' };
	if (program.status === 'draft') return { label: 'Draft', tone: 'neutral' };
	if (program.status === 'scheduled') return { label: 'Scheduled', tone: 'info' };
	if (program.resuming) {
		return {
			label: 'Resuming',
			tone: 'info',
			tip: 'Held templates are planned again in the background, then the program runs.'
		};
	}
	if (program.pausedAt || program.status === 'paused') return { label: 'Paused', tone: 'warning' };
	if (program.needsAttention || program.warnings?.length) {
		return { label: 'Needs attention', tone: 'warning' };
	}
	return { label: 'Active', tone: 'success' };
};

// tooltip for a value hidden to protect anonymity
export const HIDDEN_TIP = `A month in this range has too few recipients to show this. Each month needs at least ${PROGRAM_MIN_GROUP} who did it and ${PROGRAM_MIN_GROUP} who did not. Small numbers can point to a person.`;
// why a send count of an anonymous program is hidden
export const SENT_HIDDEN_TIP = `A month, or a template in a month, was sent to fewer than ${PROGRAM_MIN_GROUP} recipients. Small numbers can point to a person.`;
// why the month and template rows of an anonymous program show no results
export const ROW_HIDDEN_TIP = 'An anonymous program shows results only in the totals above, not per month or template.';
// why the next send of an anonymous program is not shown
export const NEXT_HIDDEN_TIP = 'Hidden for anonymous programs, as it shows when someone is sent';
// why a result tile of an anonymous program can show Hidden
export const HIDDEN_RESULTS_NOTE = `A result is hidden when a month in the range has fewer than ${PROGRAM_MIN_GROUP} recipients who did it, or who did not, or when two linked results, such as Clicked link and Data submitted, differ by fewer than ${PROGRAM_MIN_GROUP}. So a month needs at least ${PROGRAM_MIN_GROUP * 2} recipients before its results show.`;
// shown for a rate or score that has nothing counted yet
export const NONE_YET = 'None yet';

/**
 * the report delivery check of a program as a StatusBadge label, tone and
 * tooltip, from GET /program/report-delivery
 * @param {{ready: boolean, reason?: string}} d
 */
export const reportDeliveryStatus = (d) => {
	if (d.ready) {
		return { label: 'Ready', tone: 'success', tip: 'The automated report can be sent.' };
	}
	if (d.reason === 'pdfDisabled') {
		return {
			label: 'PDF reports off',
			tone: 'warning',
			tip: 'PDF reports are turned off in the settings, so no report is sent.'
		};
	}
	if (d.reason === 'incomplete') {
		return {
			label: 'Incomplete',
			tone: 'warning',
			tip: 'Report delivery has no recipient group, SMTP configuration or sender email, so no report is sent.'
		};
	}
	return {
		label: 'Not set up',
		tone: 'warning',
		tip: 'Report delivery is not set up, so no report is sent.'
	};
};

/**
 * where the report delivery of a program scope is set up
 * @param {{reason?: string}} d
 * @param {string|null} companyID
 */
export const reportDeliveryHref = (d, companyID) =>
	d.reason === 'pdfDisabled' || !companyID ? '/settings#reports' : `/company/${companyID}#reports`;
