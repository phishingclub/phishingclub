// compact relative time such as "in 7d 9h" or "3h 5m ago", so date columns stay narrow
export const shortRelative = (value, now = Date.now()) => {
	const diff = new Date(value).getTime() - now;
	if (isNaN(diff)) {
		return '';
	}
	const minutes = Math.floor(Math.abs(diff) / 60000);
	const hours = Math.floor(minutes / 60);
	const days = Math.floor(hours / 24);
	let text = '';
	if (days > 0) {
		text = `${days}d${hours % 24 ? ` ${hours % 24}h` : ''}`;
	} else if (hours > 0) {
		text = `${hours}h${minutes % 60 ? ` ${minutes % 60}m` : ''}`;
	} else if (minutes > 0) {
		text = `${minutes}m`;
	} else {
		return 'now';
	}
	return diff > 0 ? `in ${text}` : `${text} ago`;
};
