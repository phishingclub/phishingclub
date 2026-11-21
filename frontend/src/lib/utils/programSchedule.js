// the schedule maths of the program wizard forecast. v is the wizard form:
// weekDays (seven booleans, index 0 is Sunday), blackout ranges, startTime and
// endTime as "HH:MM" and timeZone

import {
	DEFAULT_INTERVAL_MINUTES,
	MINUTES_PER_DAY,
	blackoutContains,
	fromZoneLocal,
	toZoneLocal
} from './programText.js';

// an hour and a day in milliseconds
const HOUR_MS = 3600000;
const DAY_MS = 24 * HOUR_MS;
import { browserTimeZone } from './dailySlots.js';

// the first sending day on or after a day, as "YYYY-MM-DD", skipping days
// outside the sending days and skip dates. Empty when none is found in a year.
export const nextSendingDay = (v, fromKey, skipToday = () => false) => {
	const [y, m, d] = fromKey.split('-').map(Number);
	for (let i = 0; i < 400; i++) {
		const day = new Date(Date.UTC(y, m - 1, d + i));
		const key = day.toISOString().slice(0, 10);
		if (i === 0 && skipToday()) continue;
		if (!v.weekDays[day.getUTCDay()]) continue;
		if (v.blackout.some((r) => blackoutContains(r, key))) continue;
		return key;
	}
	return '';
};

export const dayDate = (key) => {
	const [y, m, d] = key.split('-').map(Number);
	return new Date(Date.UTC(y, m - 1, d));
};
export const addDays = (key, n) => {
	const day = dayDate(key);
	day.setUTCDate(day.getUTCDate() + n);
	return day.toISOString().slice(0, 10);
};

// the first sending time at or after a moment, in the sending days and
// hours and outside the skip dates, null when none is found in a year
export const nextSendingTime = (v, at) => {
	const zone = v.timeZone || browserTimeZone();
	const local = toZoneLocal(at, zone);
	if (!local) return null;
	const open = clockMinutes(v.startTime);
	const close = v.endTime === '23:59' ? MINUTES_PER_DAY : clockMinutes(v.endTime);
	const clock = clockMinutes(local.slice(11, 16));
	const key = local.slice(0, 10);
	const day = nextSendingDay(v, key);
	if (!day) return null;
	if (day === key && clock < close) {
		return clock >= open ? at : fromZoneLocal(`${key}T${v.startTime}`, zone);
	}
	// past the sending hours, the next sending day after today
	const next = day === key ? nextSendingDay(v, key, () => true) : day;
	return next ? fromZoneLocal(`${next}T${v.startTime}`, zone) : null;
};

// the moments of the sending hours of a sending day, as [from, to) in ms,
// null when a time change falls in them or at either end, then the wall
// clock is read for each moment instead
const sendingHours = (v, key, zone) => {
	const endKey = v.endTime === '23:59' ? `${addDays(key, 1)}T00:00` : `${key}T${v.endTime}`;
	const from = fromZoneLocal(`${key}T${v.startTime}`, zone);
	const to = fromZoneLocal(endKey, zone);
	if (!from || !to) return null;
	if (toZoneLocal(from, zone) !== `${key}T${v.startTime}`) return null;
	if (toZoneLocal(to, zone) !== endKey) return null;
	const close = v.endTime === '23:59' ? MINUTES_PER_DAY : clockMinutes(v.endTime);
	if (to.getTime() - from.getTime() !== (close - clockMinutes(v.startTime)) * 60000) return null;
	return { from: from.getTime(), to: to.getTime() };
};

// the start of the round after one that started at the moment t, in ms: an
// interval later. An interval of whole days adds days on the calendar of the
// program zone, so a time change keeps the hour, as the backend does.
export const nextRoundStart = (v, t, interval) => {
	if (interval > 0 && interval % MINUTES_PER_DAY === 0) {
		const zone = v.timeZone || browserTimeZone();
		const local = toZoneLocal(new Date(t), zone);
		const at = local
			? fromZoneLocal(`${addDays(local.slice(0, 10), interval / MINUTES_PER_DAY)}${local.slice(10)}`, zone)
			: null;
		// the seconds past the minute are kept
		if (at) return at.getTime() + (((t % 60000) + 60000) % 60000);
	}
	return t + interval * 60000;
};

// the earliest moments, in ms, the sends of one recipient can go out from a
// start until an end, the way the backend advances: one send per interval,
// each sent at the first sending time in it but at least half an interval
// after the last send, and an overdue interval starts over from that send.
// Each send is the earliest the draw can give, so no send of the backend is
// earlier. Without an end it stops at max sends.
export const earliestSends = (v, from, until, interval, spread, max = Infinity) => {
	const zone = v.timeZone || browserTimeZone();
	const step = interval * 60000;
	const reach = (spread || 0) * 60000;
	const end = until ? until.getTime() : Infinity;
	let roundStart = from.getTime();
	let last = null;
	const out = [];
	// the sending hours the last send was in. A moment in them is its own
	// sending time, so intervals shorter than a day do not read the wall
	// clock each interval
	const short = interval < MINUTES_PER_DAY;
	let hours = null;
	for (let i = 0; i < 5000 && roundStart < end && out.length < max; i++) {
		const earliest = last == null ? roundStart : Math.max(roundStart, last + step / 2);
		let sent;
		if (hours && earliest >= hours.from && earliest < hours.to) {
			sent = earliest;
		} else {
			const at = nextSendingTime(v, new Date(earliest));
			if (!at) break;
			sent = at.getTime();
			hours = null;
			if (short && sent === earliest) {
				const local = toZoneLocal(at, zone);
				if (local) hours = sendingHours(v, local.slice(0, 10), zone);
				if (hours && !(sent >= hours.from && sent < hours.to)) hours = null;
			}
		}
		if (sent >= end) break;
		out.push(sent);
		last = sent;
		const next = nextRoundStart(v, roundStart, interval);
		roundStart = next + reach < last ? last : next;
	}
	return out;
};

// how many templates one recipient gets from a start until an end, see
// earliestSends. Counting stops at max.
export const countSends = (v, from, until, interval, spread, max = Infinity) =>
	earliestSends(v, from, until, interval, spread, max).length;

// minutes since midnight of a "HH:MM" clock
export const clockMinutes = (hhmm) => {
	const [h, m] = (hhmm || '00:00').split(':').map(Number);
	return h * 60 + m;
};

// the number of days a minute offset from startKey midnight falls on
const dayOf = (min) => Math.floor(min / MINUTES_PER_DAY);

// whether the day startKey plus i days is a sending day
const isSendingDay = (v, startKey, i) => {
	const key = addDays(startKey, i);
	return nextSendingDay(v, key) === key;
};

// the sending minutes inside [from, to), counted in minutes from startKey
// midnight, over the sending days and hours, as programWindowSpans does
const heldMinutes = (v, startKey, from, to, open, close) => {
	if (to <= from) return 0;
	let total = 0;
	for (let i = dayOf(from); i <= dayOf(to - 1); i++) {
		if (!isSendingDay(v, startKey, i)) continue;
		const s = Math.max(i * MINUTES_PER_DAY + open, from);
		const e = Math.min(i * MINUTES_PER_DAY + close, to);
		if (e > s) total += e - s;
	}
	return total;
};

// the opening of the sending hours the minute offset from falls in, or of the
// next ones when from is outside them, as programWindowOpening does
const windowOpening = (v, startKey, from, open, close) => {
	const i = dayOf(from);
	const dayClose = i * MINUTES_PER_DAY + close;
	if (isSendingDay(v, startKey, i) && from < dayClose) {
		return i * MINUTES_PER_DAY + open;
	}
	// the next sending day opening, skipping the start day when it is past its
	// sending hours
	const next = nextSendingDay(v, startKey, () => from >= close);
	return next ? dayIndex(startKey, next) * MINUTES_PER_DAY + open : null;
};

// the earliest minute offset at which [from, end) holds amount sending minutes,
// as programWindowEndAfter does
const windowEndAfter = (v, startKey, from, amount, open, close) => {
	let held = 0;
	for (let i = dayOf(from), n = 0; n < programForecastDays; i++, n++) {
		if (!isSendingDay(v, startKey, i)) continue;
		const s = Math.max(i * MINUTES_PER_DAY + open, from);
		const e = i * MINUTES_PER_DAY + close;
		if (e <= s) continue;
		const length = e - s;
		if (held + length >= amount) return s + (amount - held);
		held += length;
	}
	return from + amount;
};

// how many sending days windowEndAfter searches before it gives up
const programForecastDays = 400;

// the whole number of days from startKey to key
const dayIndex = (startKey, key) => Math.round((dayDate(key) - dayDate(startKey)) / 86400000);

// the first and the last sending day of an interval, the way the backend picks
// a send time: a sending time in [interval start, interval start + spread), or
// the next sending time when that holds none. When the hours left in the
// interval hold less than the spread, the interval is stretched from the next
// sending hours opening until it holds as much as a full one, as the backend
// does in programRoundEnd, so for example an interval starting late Friday with
// a one day spread runs into Monday. local is the interval start as
// "YYYY-MM-DDTHH:MM" in the program zone, minutes count from its midnight
export const roundWindow = (v, local, spread, late = 0) => {
	const startKey = local.slice(0, 10);
	const from = clockMinutes(local.slice(11, 16));
	const open = clockMinutes(v.startTime);
	// an end of 23:59 is the end of the day, as in the backend
	const close = v.endTime === '23:59' ? MINUTES_PER_DAY : clockMinutes(v.endTime);
	// local is where the draw starts. late is how many minutes after the round
	// start that is, as the backend draws from the later of the round start and
	// the moment it plans the round, while the spread still counts from the
	// round start
	let to = from - (late || 0) + (spread || 0);
	// a full interval holds the sending time the spread measured from a window
	// opening holds. When this interval holds less, it is stretched to hold that
	// much, as programRoundEnd does.
	const opening = windowOpening(v, startKey, from, open, close);
	const full =
		opening == null ? 0 : heldMinutes(v, startKey, opening, opening + (spread || 0), open, close);
	if (full > 0 && heldMinutes(v, startKey, from, to, open, close) < full) {
		to = windowEndAfter(v, startKey, from, full, open, close);
	}
	let first = '';
	let last = '';
	for (let i = dayOf(from); i * MINUTES_PER_DAY < to; i++) {
		if (!isSendingDay(v, startKey, i)) continue;
		const dayFrom = Math.max(i * MINUTES_PER_DAY + open, from);
		const dayTo = Math.min(i * MINUTES_PER_DAY + close, to);
		if (dayFrom < dayTo) {
			first = first || addDays(startKey, i);
			last = addDays(startKey, i);
		}
	}
	if (first) return { first, last };
	const next = nextSendingDay(v, startKey, () => from >= close);
	return next ? { first: next, last: next } : null;
};

// the send windows of the first templates, as the first and the last day the
// backend can send each one on. The first day is the earliest send, see
// earliestSends, the last day the latest, see sendSchedule. A template that
// cannot go out before the end is left out.
export const sendWindows = (v, start, interval, spread, count, end) => {
	const zone = v.timeZone || browserTimeZone();
	const latest = sendSchedule(v, start, interval, spread, count);
	const early = earliestSends(v, start, end, interval, spread, latest.length);
	const out = [];
	for (let k = 0; k < early.length; k++) {
		const local = toZoneLocal(new Date(early[k]), zone);
		if (!local) break;
		const last = latest[k].last;
		const first = local.slice(0, 10);
		out.push({ first: first < last ? first : last, last });
	}
	return out;
};

// the most send windows sendSchedule works out, which covers three years of daily
// templates
const SCHEDULE_MAX = 1200;

// the send windows of the first count templates of one recipient, from the
// start. Each window has from, the start of its round, its first and last
// sending day and by, the moment its last sending day closes, which is the
// latest a send in it goes out. Round k
// starts an interval after round k-1, as in the backend, and starts at the
// last send instead when that is later than its spread allows. A send is at
// least half an interval after the one before, and a round is drawn from no
// earlier than the send before, as the backend plans it then. All use the
// latest send of the window before, so the windows never come out earlier
// than the backend.
export const sendSchedule = (v, start, interval, spread, count) => {
	const zone = v.timeZone || browserTimeZone();
	const step = (interval || 0) * 60000;
	const reach = (spread || 0) * 60000;
	const out = [];
	// the backend starts at a time with seconds while the windows here count
	// whole minutes, so the start is taken at the next whole minute, which can
	// only move the latest send later
	let roundStart = Math.ceil(start.getTime() / 60000) * 60000;
	for (let k = 0; k < Math.min(count, SCHEDULE_MAX); k++) {
		if (k > 0 && !(step > 0)) break;
		const prev = out[out.length - 1];
		// the backend plans a round right after the send before it, so the draw
		// starts no earlier than that send, at the latest just before its window
		// closes. A send late in its window then stretches the next round, for
		// example over a weekend.
		const base = prev ? Math.max(roundStart, prev.by - 60000) : roundStart;
		const local = toZoneLocal(new Date(base), zone);
		const w = local ? roundWindow(v, local, spread, Math.round((base - roundStart) / 60000)) : null;
		if (!w) break;
		let last = w.last;
		if (prev) {
			// the half interval gap can move a send past its window
			const gap = nextSendingTime(v, new Date(prev.by + step / 2));
			const gapDay = gap ? toZoneLocal(gap, zone).slice(0, 10) : '';
			if (gapDay > last) last = gapDay;
		}
		const by = fromZoneLocal(`${last}T${v.endTime}`, zone);
		if (!by) break;
		out.push({ from: roundStart, first: w.first, last, by: by.getTime() });
		const next = nextRoundStart(v, roundStart, interval);
		// the backend starts an overdue round at the send, inside the sending
		// hours, so it is taken just before they close as for base above
		roundStart = next + reach < by.getTime() - 60000 ? by.getTime() - 60000 : next;
	}
	return out;
};

// the end that gives the last send as long as the time between templates
// before links stop: the close of its last sending day plus the interval,
// moved to the end of the sending hours of that day, or of the next sending
// day when it is not one. Whole days are counted on the calendar so a time
// change does not move the hour. lastDay is "YYYY-MM-DD" in the program zone.
export const responseEnd = (v, lastDay, interval) => {
	const zone = v.timeZone || browserTimeZone();
	let key;
	if (interval % MINUTES_PER_DAY === 0) {
		key = addDays(lastDay, interval / MINUTES_PER_DAY);
	} else {
		const close = fromZoneLocal(`${lastDay}T${v.endTime}`, zone);
		if (!close) return null;
		const local = toZoneLocal(new Date(close.getTime() + interval * 60000), zone);
		key = local.slice(0, 10);
		if (local.slice(11, 16) > v.endTime) key = addDays(key, 1);
	}
	const day = nextSendingDay(v, key) || key;
	return fromZoneLocal(`${day}T${v.endTime}`, zone);
};

// the close of the latest sending day that closes at or before a moment and
// after another one, or the moment itself when there is none
const closeBefore = (v, at, after) => {
	const zone = v.timeZone || browserTimeZone();
	const local = toZoneLocal(at, zone);
	if (!local) return at;
	let key = local.slice(0, 10);
	for (let i = 0; i < 400; i++, key = addDays(key, -1)) {
		if (nextSendingDay(v, key) !== key) continue;
		const close = fromZoneLocal(`${key}T${v.endTime}`, zone);
		if (!close || close.getTime() > at.getTime()) continue;
		return close.getTime() > after ? close : at;
	}
	return at;
};

// the end that lets the first sends templates of a recipient go out and gives
// the last one time to respond before its links stop: the time between
// templates, see responseEnd. When the program starts over, the template after
// them would go out in that time, so the end is the close of the last sending
// day before the earliest moment it can go out, see earliestSends, or that
// moment. Then no template of the next round goes out in any draw. by is the
// latest the last send goes out and capped tells when the end is the one
// before the next template. overlap tells when that end comes before the
// latest the last send can go out, as the spread lets the last send and the
// next one fall on the same days, so the last send can miss the end. Null
// when the sends cannot be worked out.
export const safeEnd = (v, start, interval, spread, sends, loops) => {
	if (!(sends > 0) || interval == null) return null;
	const s = sendSchedule(v, start, interval, spread, sends);
	if (s.length < sends) return null;
	const last = s[sends - 1];
	let end = responseEnd(v, last.last, interval);
	if (!end) return null;
	let capped = false;
	if (loops) {
		const early = earliestSends(v, start, null, interval, spread, sends + 1);
		const after = early[sends];
		if (after != null && after < end.getTime()) {
			end = closeBefore(v, new Date(after), last.by);
			capped = true;
			if (end.getTime() < last.by) {
				// the last send can go out up to the end, so the end is the opening of
				// the sending hours the next send can first go out in. A send then
				// never goes out right before the end, only before the night or the
				// days without sending hours.
				const zone = v.timeZone || browserTimeZone();
				const local = toZoneLocal(new Date(after), zone);
				const opening = local ? fromZoneLocal(`${local.slice(0, 10)}T${v.startTime}`, zone) : null;
				if (opening && opening.getTime() <= after && opening.getTime() > early[sends - 1]) {
					end = opening;
				}
			}
		}
	}
	const overlap = capped && end.getTime() < last.by;
	return { end, by: last.by, last: last.last, capped, overlap };
};

// the schedule fields of a stored program in the form the forecast reads. A
// program saved without sending hours sends on every day at any hour.
export const programScheduleForm = (p) => {
	const mask = p?.constraintWeekDays;
	const has = mask != null;
	return {
		weekDays: [0, 1, 2, 3, 4, 5, 6].map((d) => !has || ((mask >> d) & 1) === 1),
		startTime: has ? p.constraintStartTime || '09:00' : '00:00',
		endTime: has ? p.constraintEndTime || '17:00' : '23:59',
		timeZone: p?.timeZone || 'UTC',
		blackout: (p?.blackoutDates || []).map((r) => ({ from: r.from, to: r.to, yearly: !!r.yearly }))
	};
};

// the end check of the wizard for a stored program that starts at a moment.
// Null when the program has no end or no templates or the sends cannot be
// worked out. count is how many sends go out before the end, every round
// counted when the program starts over, and total how many templates there
// are. gap is set when the last send gets less time to respond than the
// others: the time from its latest send to the end. fitEnd is the end that
// lets the templates go out with that time, null when they already fit. The
// end never lets a template of the next round go out, see safeEnd. overlap is
// set when the spread lets the last send go out as late as the next one could,
// so no end fits. A program without an interval uses the default one.
export const startEndCheck = (p, at = new Date()) => {
	const total = (p?.steps || []).length;
	const interval = p?.stepIntervalMinutes > 0 ? p.stepIntervalMinutes : DEFAULT_INTERVAL_MINUTES;
	if (!p?.endAt || !total) return null;
	const end = new Date(p.endAt);
	if (!(end.getTime() > at.getTime())) return null;
	const v = programScheduleForm(p);
	if (!v.weekDays.some(Boolean) || !(v.endTime > v.startTime)) return null;
	const spread = Math.min(p.spreadMinutes ?? 0, interval);
	const loops = p.loopMode === 'loop';
	const count = countSends(v, at, end, interval, spread, loops ? Infinity : total);
	const out = { count, total, gap: null, lastBy: null, fitEnd: null, overlap: false };
	const got = count > 0 ? safeEnd(v, at, interval, spread, count, loops) : null;
	if (got) {
		out.lastBy = got.by;
		const gap = end.getTime() - got.by;
		// the time the safe end gives the last send, at most the interval
		const needed = Math.min(interval * 60000, got.end.getTime() - got.by);
		if (gap + HOUR_MS < needed || (got.capped && gap < Math.min(DAY_MS, (interval * 60000) / 2))) {
			out.gap = Math.max(0, gap);
			// no end fits the last send and keeps out the next one, see safeEnd
			out.overlap = got.overlap && !(gap + HOUR_MS < needed);
		}
	}
	if (count < total || out.gap != null) {
		const target = count < total ? safeEnd(v, at, interval, spread, total, loops) : got;
		if (target && Math.abs(target.end.getTime() - end.getTime()) > 60000) {
			out.fitEnd = target.end;
		}
	}
	return out;
};

// a "YYYY-MM-DD" day plus years and days, a Feb 29 a year later is Mar 1 as
// in the backend
const addYears = (key, years, days = 0) => {
	const [y, m, d] = key.split('-').map(Number);
	return new Date(Date.UTC(y + years, m - 1, d + days)).toISOString().slice(0, 10);
};

// whether a yearly skip range covers every day of a year without Feb 29
const coversYear = (r) => {
	for (let key = '2023-01-01'; key < '2024-01-01'; key = addDays(key, 1)) {
		if (!blackoutContains(r, key)) return false;
	}
	return true;
};

// why a skip range with both dates in order is refused, the same rules as the
// backend: a yearly range must be shorter than a year and leave a day of the
// year free, a one off range can be at most a year. Empty when it is fine.
export const blackoutRangeError = (r) => {
	if (r.yearly) {
		return r.to >= addYears(r.from, 1, -1) || coversYear(r)
			? 'A yearly range must be shorter than a year'
			: '';
	}
	return r.to >= addYears(r.from, 1) ? 'A skip range can be at most a year' : '';
};

// how many days ahead the backend looks for a day to send on
const SEND_DAY_LOOK_AHEAD = 400 * 55;

// whether a day from today in the program zone on is a sending day outside
// the skip dates, as the backend checks before it saves
export const hasSendDay = (v, now = new Date()) => {
	if (!v.weekDays.some(Boolean)) return false;
	const zone = v.timeZone || browserTimeZone();
	const today = toZoneLocal(now, zone).slice(0, 10);
	if (!today) return true;
	const [y, m, d] = today.split('-').map(Number);
	for (let i = 0; i < SEND_DAY_LOOK_AHEAD; i++) {
		const day = new Date(Date.UTC(y, m - 1, d + i));
		if (!v.weekDays[day.getUTCDay()]) continue;
		const key = day.toISOString().slice(0, 10);
		if (!v.blackout.some((r) => blackoutContains(r, key))) return true;
	}
	return false;
};

// how many days ahead the backend checks the sending hours against clock changes
const CLOCK_CHECK_DAYS = 2 * 366;

// whether a wall clock time on a day exists exactly once in a zone
const wallTimeOnce = (key, hhmm, zone) => {
	const local = `${key}T${hhmm}`;
	const at = fromZoneLocal(local, zone);
	if (!at || toZoneLocal(at, zone) !== local) return false;
	// a clock set back shows the same time again 30 minutes to 2 hours later
	for (const minutes of [30, 60, 120]) {
		const shift = minutes * 60000;
		if (toZoneLocal(new Date(at.getTime() - shift), zone) === local) return false;
		if (toZoneLocal(new Date(at.getTime() + shift), zone) === local) return false;
	}
	return true;
};

// the first sending day from today on, within two years, on which the start or
// the end of the sending hours does not exist or exists twice because of a
// clock change, as { day, time }. Null when there is none. The backend refuses
// such hours. A start at 00:00 and an end at 23:59 always exist.
export const windowClockProblem = (v, now = new Date()) => {
	const zone = v.timeZone || browserTimeZone();
	const times = [v.startTime, v.endTime].filter((t) => t && t !== '00:00' && t !== '23:59');
	if (!times.length) return null;
	const today = toZoneLocal(now, zone).slice(0, 10);
	if (!today) return null;
	// the zone offset at noon UTC of each day. A clock change between two noons
	// can only touch those two days, the other days are not checked.
	const offsetAt = (key) => {
		const t = Date.parse(`${key}T12:00:00Z`);
		const local = toZoneLocal(new Date(t), zone);
		return local ? Date.parse(`${local}:00Z`) - t : 0;
	};
	const check = (key) => {
		if (!v.weekDays[dayDate(key).getUTCDay()]) return null;
		const time = times.find((t) => !wallTimeOnce(key, t, zone));
		return time ? { day: key, time } : null;
	};
	let before = offsetAt(today);
	let found = check(today);
	for (let i = 1; i < CLOCK_CHECK_DAYS && !found; i++) {
		const key = addDays(today, i);
		const offset = offsetAt(key);
		if (offset !== before) found = check(addDays(today, i - 1)) || check(key);
		before = offset;
	}
	return found;
};
