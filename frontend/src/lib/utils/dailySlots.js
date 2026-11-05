// Daily Slots are picked in local time: a date range, week days and daily hours.
// The backend runs them in UTC: it reads the week day and the daily hours of
// every delivery day in UTC. These helpers convert between the two so the days
// and hours the user picks are the ones that get sent.

const DAY_MS = 24 * 60 * 60 * 1000;

const pad = (n) => String(n).padStart(2, '0');

const minutesOf = (time) => {
	const [h, m] = time.split(':').map(Number);
	return h * 60 + m;
};

// "YYYY-MM-DD" of the UTC date of an instant, moved by a number of days
const utcDateString = (instant, addDays = 0) => {
	const d = new Date(
		Date.UTC(instant.getUTCFullYear(), instant.getUTCMonth(), instant.getUTCDate() + addDays)
	);
	return `${d.getUTCFullYear()}-${pad(d.getUTCMonth() + 1)}-${pad(d.getUTCDate())}`;
};

// a "YYYY-MM-DD" string at local midnight
const parseLocalDate = (dateStr) => new Date(dateStr + 'T00:00:00');

// a copy of a local date with a "HH:MM" local time set on it
export const withLocalTime = (date, time) => {
	const d = new Date(date);
	if (time) {
		const [h, m] = time.split(':').map(Number);
		d.setHours(h, m, 0, 0);
	}
	return d;
};

// how many days the UTC date of an instant is ahead of its local date: -1, 0 or 1
const utcDayShift = (instant) => {
	const local = Date.UTC(instant.getFullYear(), instant.getMonth(), instant.getDate());
	const utc = Date.UTC(instant.getUTCFullYear(), instant.getUTCMonth(), instant.getUTCDate());
	return Math.round((utc - local) / DAY_MS);
};

const shiftWeekDays = (weekDays, shift) =>
	[...new Set(weekDays.map((d) => (((d + shift) % 7) + 7) % 7))].sort((a, b) => a - b);

export const weekDaysToBinary = (weekDays) => {
	if (!weekDays?.length) return null;
	return weekDays.reduce((binary, day) => binary | (1 << day), 0);
};

export const binaryToWeekDays = (binary) => {
	if (!binary) return [];
	return Array.from({ length: 7 }, (_, i) => i).filter((day) => binary & (1 << day));
};

/**
 * The local delivery window for a Daily Slots choice.
 * start is the first selected day in the range at the daily start time, end is
 * the last selected day at the daily end time.
 * @returns {{start: Date, end: Date, firstDay: Date, lastDay: Date, deliveryDays: number} | null}
 */
export const dailySlotWindow = ({ startDate, endDate, weekDays, startTime, endTime }) => {
	if (!startDate || !endDate || !weekDays?.length) return null;
	const end = parseLocalDate(endDate);
	let firstDay = null;
	let lastDay = null;
	let deliveryDays = 0;
	for (let d = parseLocalDate(startDate); d <= end; d.setDate(d.getDate() + 1)) {
		if (weekDays.includes(d.getDay())) {
			if (!firstDay) firstDay = new Date(d);
			lastDay = new Date(d);
			deliveryDays++;
		}
	}
	if (!firstDay) return null;
	return {
		start: withLocalTime(firstDay, startTime),
		end: withLocalTime(lastDay, endTime),
		firstDay,
		lastDay,
		deliveryDays
	};
};

/**
 * The backend fields for a Daily Slots choice: the exact first and last send
 * time, and the week days and daily hours moved to UTC.
 * Returns { error } when the daily hours cross midnight in UTC, which the
 * backend can not run as one slot per day, or when a daily hour does not exist
 * on the first delivery day because the clock jumps forward that night.
 */
export const dailySlotsToUTC = (slot) => {
	const win = dailySlotWindow(slot);
	if (!win || !slot.startTime || !slot.endTime) return null;
	const dayStart = win.start;
	const dayEnd = withLocalTime(win.firstDay, slot.endTime);
	const localMinutes = (d) => d.getHours() * 60 + d.getMinutes();
	if (
		localMinutes(dayStart) !== minutesOf(slot.startTime) ||
		localMinutes(dayEnd) !== minutesOf(slot.endTime)
	) {
		return {
			error:
				'The daily hours do not exist on the first delivery day because of a clock change. Choose other hours or another start date.'
		};
	}
	const shift = utcDayShift(dayStart);
	if (utcDayShift(dayEnd) !== shift) {
		return {
			error:
				'The daily hours cross midnight in UTC. Choose hours that start and end on the same UTC day.'
		};
	}
	// the backend runs every day at the UTC hours of the first day, so the last
	// send is the last day's UTC date at that UTC end time. Using the local end time
	// instead would drop the last day when a clock change falls inside the range.
	const lastSend = new Date(
		Date.UTC(
			win.lastDay.getFullYear(),
			win.lastDay.getMonth(),
			win.lastDay.getDate() + shift,
			dayEnd.getUTCHours(),
			dayEnd.getUTCMinutes()
		)
	);
	return {
		sendStartAt: dayStart.toISOString(),
		sendEndAt: lastSend.toISOString(),
		constraintWeekDays: weekDaysToBinary(shiftWeekDays(slot.weekDays, shift)),
		constraintStartTime: `${pad(dayStart.getUTCHours())}:${pad(dayStart.getUTCMinutes())}`,
		constraintEndTime: `${pad(dayEnd.getUTCHours())}:${pad(dayEnd.getUTCMinutes())}`
	};
};

/**
 * The local week days and daily hours of a saved Daily Slots campaign.
 * The UTC fields are read against the campaign send start, so the shift and
 * the time zone offset are the ones of its first delivery day.
 * startDate and endDate are the first and last local delivery day. The end date
 * is read from the UTC date of the last send, as that is the day the backend
 * runs it on, so it stays the same when the last send falls after a clock change.
 * @returns {{weekDays: number[], startTime: string, endTime: string, startDate: string|null, endDate: string|null} | null}
 */
export const dailySlotsFromUTC = ({
	sendStartAt,
	sendEndAt,
	constraintWeekDays,
	constraintStartTime,
	constraintEndTime
}) => {
	if (!constraintWeekDays || !constraintStartTime || !constraintEndTime) return null;
	const ref = sendStartAt ? new Date(sendStartAt) : new Date();
	const toLocal = (utcTime) => {
		const [h, m] = utcTime.split(':').map(Number);
		return new Date(Date.UTC(ref.getUTCFullYear(), ref.getUTCMonth(), ref.getUTCDate(), h, m));
	};
	const start = toLocal(constraintStartTime);
	const end = toLocal(constraintEndTime);
	const shift = utcDayShift(start);
	return {
		startDate: sendStartAt ? utcDateString(start, -shift) : null,
		endDate: sendEndAt ? utcDateString(new Date(sendEndAt), -shift) : null,
		weekDays: shiftWeekDays(binaryToWeekDays(constraintWeekDays), -shift),
		startTime: `${pad(start.getHours())}:${pad(start.getMinutes())}`,
		endTime: `${pad(end.getHours())}:${pad(end.getMinutes())}`
	};
};
