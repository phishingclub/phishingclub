// Daily Slots are picked as a date range, week days and daily hours in a time
// zone. The backend stores the zone with them and builds every delivery day's
// window in that zone, so these helpers follow its rules: each delivery day
// gets the hours on that local date, and an hour that does not exist or happens
// twice because of a clock change on a delivery day is rejected.
//
// Campaigns saved before the zone was stored have their week days and hours in
// UTC. They are read with fromLegacyUTC and saved again with a zone.

const DAY_MS = 24 * 60 * 60 * 1000;

// the longest delivery range in dates. The backend allows 3 x 366 days between
// the first slot start and the last slot end; two days less here keeps a range
// that passes in the browser from failing on the server.
const MAX_RANGE_MS = (3 * 366 - 2) * DAY_MS;

const pad = (n) => String(n).padStart(2, '0');

const minutesOf = (time) => {
	const [h, m] = time.split(':').map(Number);
	return h * 60 + m;
};

export const browserTimeZone = () => Intl.DateTimeFormat().resolvedOptions().timeZone || 'UTC';

const formatters = new Map();
const formatterFor = (timeZone) => {
	if (!formatters.has(timeZone)) {
		formatters.set(
			timeZone,
			new Intl.DateTimeFormat('en-US', {
				timeZone,
				hourCycle: 'h23',
				year: 'numeric',
				month: 'numeric',
				day: 'numeric',
				hour: 'numeric',
				minute: 'numeric',
				second: 'numeric'
			})
		);
	}
	return formatters.get(timeZone);
};

// the wall clock of an instant in a zone
const wallParts = (ms, timeZone) => {
	const parts = {};
	for (const p of formatterFor(timeZone).formatToParts(new Date(ms))) {
		parts[p.type] = p.value;
	}
	return {
		y: Number(parts.year),
		m: Number(parts.month),
		d: Number(parts.day),
		h: Number(parts.hour),
		mi: Number(parts.minute),
		s: Number(parts.second)
	};
};

// the zone's offset from UTC at an instant, in ms
const offsetAt = (ms, timeZone) => {
	const p = wallParts(ms, timeZone);
	return Date.UTC(p.y, p.m - 1, p.d, p.h, p.mi, p.s) - Math.floor(ms / 1000) * 1000;
};

/**
 * The instant of a wall clock time in a zone, or null when that wall time does
 * not exist (the clock jumps forward) or exists twice (the clock goes back).
 * Same rule as the backend.
 */
const zonedInstant = (y, m, d, h, mi, timeZone) => {
	const wall = Date.UTC(y, m - 1, d, h, mi);
	// offsets are within 14h of UTC, so every offset that can apply is in effect
	// somewhere in the 36h around the wall time
	const offsets = new Set();
	for (let k = -6; k <= 6; k++) {
		offsets.add(offsetAt(wall + k * 6 * 3600000, timeZone));
	}
	const found = [];
	for (const offset of offsets) {
		const candidate = wall - offset;
		const p = wallParts(candidate, timeZone);
		if (p.y === y && p.m === m && p.d === d && p.h === h && p.mi === mi) {
			found.push(candidate);
		}
	}
	return found.length === 1 ? new Date(found[0]) : null;
};

// "YYYY-MM-DD" of an instant's wall date in a zone
const zonedDateString = (instant, timeZone) => {
	const p = wallParts(new Date(instant).getTime(), timeZone);
	return `${p.y}-${pad(p.m)}-${pad(p.d)}`;
};

export const weekDaysToBinary = (weekDays) => {
	if (!weekDays?.length) return null;
	return weekDays.reduce((binary, day) => binary | (1 << day), 0);
};

export const binaryToWeekDays = (binary) => {
	if (!binary) return [];
	return Array.from({ length: 7 }, (_, i) => i).filter((day) => binary & (1 << day));
};

/**
 * The delivery window of a Daily Slots choice, built the same way as the backend.
 * start is the first selected day at the start hour and end the last selected
 * day at the end hour, both in the zone.
 * Returns null while the choice is incomplete, and { error } when it can not run.
 * @returns {{start: Date, end: Date, deliveryDays: number} | {error: string} | null}
 */
let lastWindowKey = null;
let lastWindow = null;
export const dailySlotWindow = (slot) => {
	// the wizard asks again on every form change; only rebuild when the choice changed
	const key = JSON.stringify([
		slot.startDate,
		slot.endDate,
		slot.weekDays,
		slot.startTime,
		slot.endTime,
		slot.timeZone
	]);
	if (key !== lastWindowKey) {
		try {
			lastWindow = buildWindow(slot);
		} catch (e) {
			// Intl throws for a zone the browser does not know
			lastWindow = { error: `The time zone ${slot.timeZone} is not known by this browser.` };
		}
		lastWindowKey = key;
	}
	return lastWindow;
};

const buildWindow = ({ startDate, endDate, weekDays, startTime, endTime, timeZone }) => {
	if (!startDate || !endDate || !weekDays?.length || !startTime || !endTime || !timeZone) {
		return null;
	}
	if (minutesOf(endTime) <= minutesOf(startTime)) {
		return { error: 'The daily end time must be after the daily start time.' };
	}
	const [sy, sm, sd] = startDate.split('-').map(Number);
	const [ey, em, ed] = endDate.split('-').map(Number);
	const [sh, smi] = startTime.split(':').map(Number);
	const [eh, emi] = endTime.split(':').map(Number);
	// Date.UTC reads years below 100 as 19xx, and older years are typos anyway
	if (sy < 1970 || ey < 1970) {
		return { error: 'Choose valid delivery dates.' };
	}
	const first = Date.UTC(sy, sm - 1, sd);
	const last = Date.UTC(ey, em - 1, ed);
	if (last < first) {
		return { error: 'The delivery end date must be on or after the start date.' };
	}
	if (last - first > MAX_RANGE_MS) {
		return { error: 'The delivery dates can be at most 3 years apart.' };
	}
	let start = null;
	let end = null;
	let deliveryDays = 0;
	// walk calendar dates in UTC, which has no clock changes
	for (let day = first; day <= last; day += DAY_MS) {
		const date = new Date(day);
		if (!weekDays.includes(date.getUTCDay())) continue;
		const y = date.getUTCFullYear();
		const m = date.getUTCMonth() + 1;
		const d = date.getUTCDate();
		const dayStart = zonedInstant(y, m, d, sh, smi, timeZone);
		const dayEnd = zonedInstant(y, m, d, eh, emi, timeZone);
		if (!dayStart || !dayEnd || dayEnd <= dayStart) {
			return {
				error: `The daily hours ${startTime} to ${endTime} do not exist or happen twice on ${y}-${pad(m)}-${pad(d)} in ${timeZone} because of a clock change. Choose other hours or dates.`
			};
		}
		if (!start) start = dayStart;
		end = dayEnd;
		deliveryDays++;
	}
	if (!start) {
		return { error: 'None of the selected days fall inside the date range.' };
	}
	return { start, end, deliveryDays };
};

/**
 * The backend fields for a Daily Slots choice, or null / { error } like
 * dailySlotWindow
 */
export const dailySlotsPayload = (slot) => {
	const win = dailySlotWindow(slot);
	if (!win || win.error) return win;
	return {
		sendStartAt: win.start.toISOString(),
		sendEndAt: win.end.toISOString(),
		constraintWeekDays: weekDaysToBinary(slot.weekDays),
		constraintStartTime: slot.startTime,
		constraintEndTime: slot.endTime,
		constraintTimeZone: slot.timeZone
	};
};

// how many days the UTC date of an instant is ahead of its date in a zone: -1, 0 or 1
const utcDayShift = (instant, timeZone) => {
	const p = wallParts(instant.getTime(), timeZone);
	const local = Date.UTC(p.y, p.m - 1, p.d);
	const utc = Date.UTC(instant.getUTCFullYear(), instant.getUTCMonth(), instant.getUTCDate());
	return Math.round((utc - local) / DAY_MS);
};

const shiftWeekDays = (weekDays, shift) =>
	[...new Set(weekDays.map((d) => (((d + shift) % 7) + 7) % 7))].sort((a, b) => a - b);

const wallTime = (instant, timeZone) => {
	const p = wallParts(instant.getTime(), timeZone);
	return `${pad(p.h)}:${pad(p.mi)}`;
};

const utcDateString = (instant, addDays = 0) => {
	const d = new Date(
		Date.UTC(instant.getUTCFullYear(), instant.getUTCMonth(), instant.getUTCDate() + addDays)
	);
	return `${d.getUTCFullYear()}-${pad(d.getUTCMonth() + 1)}-${pad(d.getUTCDate())}`;
};

// a campaign saved before the zone was stored: its UTC week days and hours read
// in the browser's zone, against the send start so the offset is the one of its
// first delivery day. The end date is the UTC date of the last send moved by the
// same day shift, as that is the day the backend runs it on.
const fromLegacyUTC = ({
	sendStartAt,
	sendEndAt,
	constraintWeekDays,
	constraintStartTime,
	constraintEndTime
}) => {
	const ref = sendStartAt ? new Date(sendStartAt) : new Date();
	const toLocal = (utcTime) => {
		const [h, m] = utcTime.split(':').map(Number);
		return new Date(Date.UTC(ref.getUTCFullYear(), ref.getUTCMonth(), ref.getUTCDate(), h, m));
	};
	// the wall clock is read from the zone name the campaign will be saved with
	const timeZone = browserTimeZone();
	const start = toLocal(constraintStartTime);
	const end = toLocal(constraintEndTime);
	const shift = utcDayShift(start, timeZone);
	// hours that cross midnight in the browser's zone can not be one slot a day
	// there, so keep them in UTC, which is how the backend runs them now
	if (utcDayShift(end, timeZone) !== shift) {
		return {
			startDate: sendStartAt ? utcDateString(new Date(sendStartAt)) : null,
			endDate: sendEndAt ? utcDateString(new Date(sendEndAt)) : null,
			weekDays: binaryToWeekDays(constraintWeekDays),
			startTime: constraintStartTime,
			endTime: constraintEndTime,
			timeZone: 'UTC',
			legacy: true
		};
	}
	return {
		startDate: sendStartAt ? utcDateString(start, -shift) : null,
		endDate: sendEndAt ? utcDateString(new Date(sendEndAt), -shift) : null,
		weekDays: shiftWeekDays(binaryToWeekDays(constraintWeekDays), -shift),
		startTime: wallTime(start, timeZone),
		endTime: wallTime(end, timeZone),
		timeZone,
		legacy: true
	};
};

/**
 * The Daily Slots choice of a saved campaign: dates, week days and hours in its
 * zone. Campaigns without a zone are converted from UTC to the browser's zone.
 * @returns {{startDate: string|null, endDate: string|null, weekDays: number[], startTime: string, endTime: string, timeZone: string, legacy: boolean} | null}
 */
export const dailySlotsFromCampaign = (campaign) => {
	const { sendStartAt, sendEndAt, constraintWeekDays, constraintStartTime, constraintEndTime } =
		campaign;
	if (!constraintWeekDays || !constraintStartTime || !constraintEndTime) return null;
	const timeZone = campaign.constraintTimeZone;
	if (!timeZone) return fromLegacyUTC(campaign);
	// Intl throws for a zone the browser does not know; fall back to the UTC dates
	const dateIn = (instant) => {
		try {
			return zonedDateString(instant, timeZone);
		} catch (e) {
			return new Date(instant).toISOString().slice(0, 10);
		}
	};
	return {
		startDate: sendStartAt ? dateIn(sendStartAt) : null,
		endDate: sendEndAt ? dateIn(sendEndAt) : null,
		weekDays: binaryToWeekDays(constraintWeekDays),
		startTime: constraintStartTime,
		endTime: constraintEndTime,
		timeZone,
		legacy: false
	};
};

// an instant as date and time in a zone, e.g. "11/16/2026, 9:00:00 AM"
export const formatInZone = (instant, timeZone) => {
	try {
		return new Date(instant).toLocaleString(undefined, { timeZone });
	} catch (e) {
		return new Date(instant).toLocaleString();
	}
};

// the zone name, with the browser's zone noted when it is a different one
export const timeZoneLabel = (timeZone) => {
	const browser = browserTimeZone();
	return timeZone === browser ? timeZone : `${timeZone} (your browser is in ${browser})`;
};
