import { writable } from 'svelte/store';

const DAYS = [
	{ short: 'Sun', full: 'Sunday', num: '1' },
	{ short: 'Mon', full: 'Monday', num: '2' },
	{ short: 'Tue', full: 'Tuesday', num: '3' },
	{ short: 'Wed', full: 'Wednesday', num: '4' },
	{ short: 'Thu', full: 'Thursday', num: '5' },
	{ short: 'Fri', full: 'Friday', num: '6' },
	{ short: 'Sat', full: 'Saturday', num: '7' }
];

export const timeFormat = writable(false); // false = 12h, true = 24h

export const formatWeekDays = (binaryDays) => {
	const days = DAYS.map((day) => ({
		...day,
		isActive: !!(binaryDays & (1 << DAYS.indexOf(day)))
	}));

	const activeDays = days.filter((d) => d.isActive);
	const isWeekdaysOnly =
		activeDays.length === 5 &&
		!days[0].isActive && // Sunday inactive
		!days[6].isActive; // Saturday inactive
	const isAllDays = activeDays.length === 7;

	return {
		days,
		summary: isAllDays ? 'Every day' : isWeekdaysOnly ? 'Weekdays only' : null
	};
};

export const formatTimeConstraint = (time, use24Hour = false) => {
	if (!time) return '';

	const [hours, minutes] = time.split(':').map(Number);

	if (use24Hour) {
		return `${hours.toString().padStart(2, '0')}:${minutes.toString().padStart(2, '0')}`;
	} else {
		const h = hours % 12 || 12;
		const ampm = hours >= 12 ? 'PM' : 'AM';
		return `${h}:${minutes.toString().padStart(2, '0')} ${ampm}`;
	}
};

export const isCurrentlyActive = (startTime, endTime, activeDays) => {
	if (!startTime || !endTime || !activeDays) return false;

	const now = new Date();
	const currentDay = now.getDay(); // 0-6
	const currentTime = now.getHours() * 60 + now.getMinutes();

	const [startHour, startMin] = startTime.split(':').map(Number);
	const [endHour, endMin] = endTime.split(':').map(Number);

	const scheduleStart = startHour * 60 + startMin;
	const scheduleEnd = endHour * 60 + endMin;

	const isDayActive = !!(activeDays & (1 << currentDay));
	const isTimeActive = currentTime >= scheduleStart && currentTime <= scheduleEnd;

	return isDayActive && isTimeActive;
};
