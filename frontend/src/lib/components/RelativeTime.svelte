<script>
	import { onMount, onDestroy } from 'svelte';

	/** @type {String | Date} */
	export let value;
	export let updateInterval = 60000; // default to 1 minute updates

	let formattedTime = '';
	let intervalId;

	function formatRelativeTime(date) {
		if (!date) return '';

		const now = new Date();
		const diff = date.getTime() - now.getTime();
		const seconds = Math.floor(Math.abs(diff) / 1000);
		const minutes = Math.floor(seconds / 60);
		const hours = Math.floor(minutes / 60);
		const days = Math.floor(hours / 24);
		const remainingMinutes = minutes % 60;

		// future
		if (diff > 0) {
			if (days > 0) {
				return `in ${days} day${days !== 1 ? 's' : ''}${hours % 24 ? `, ${hours % 24} hour${hours % 24 !== 1 ? 's' : ''}` : ''}`;
			}
			if (hours > 0) {
				return `in ${hours} hour${hours !== 1 ? 's' : ''}${remainingMinutes > 0 ? `, ${remainingMinutes} minute${remainingMinutes !== 1 ? 's' : ''}` : ''}`;
			}
			if (minutes > 0) {
				return `in ${minutes} minute${minutes !== 1 ? 's' : ''}`;
			}
			return 'in less than a minute';
		}

		// past
		if (days > 0) {
			return `${days} day${days !== 1 ? 's' : ''}${hours % 24 ? `, ${hours % 24} hour${hours % 24 !== 1 ? 's' : ''}` : ''} ago`;
		}
		if (hours > 0) {
			return `${hours} hour${hours !== 1 ? 's' : ''}${remainingMinutes > 0 ? `, ${remainingMinutes} minute${remainingMinutes !== 1 ? 's' : ''}` : ''} ago`;
		}
		if (minutes > 0) {
			return `${minutes} minute${minutes !== 1 ? 's' : ''} ago`;
		}
		return 'now';
	}

	function updateTime() {
		let date;
		if (typeof value === 'string') {
			date = new Date(value);
		} else if (value instanceof Date) {
			date = value;
		} else {
			return;
		}

		if (isNaN(date.getTime())) {
			formattedTime = 'Invalid date';
			return;
		}

		formattedTime = formatRelativeTime(date);
	}

	$: {
		if (value) {
			updateTime();
		}
	}

	onMount(() => {
		updateTime();
		intervalId = setInterval(updateTime, updateInterval);
	});

	onDestroy(() => {
		if (intervalId) {
			clearInterval(intervalId);
		}
	});
</script>

<span
	title={typeof value === 'string'
		? new Date(value).toLocaleString()
		: value instanceof Date
			? value.toLocaleString()
			: ''}
	class="text-gray-600 dark:text-gray-400 transition-colors duration-200"
>
	{formattedTime}
</span>
