<script>
	/** @type {String | Date} */
	export let value;
	export let hideHours = false;
	let date = null;
	$: {
		if (typeof value === 'string') {
			if (value.length > 0) {
				date = new Date(value);
			}
		} else {
			date = value;
		}
	}
</script>

<div class="text-gray-900 dark:text-gray-100 transition-colors duration-200">
	{#if date}
		{#if !hideHours}
			{date.toLocaleString()}
		{:else}
			{date.toLocaleDateString()}
		{/if}
	{/if}
</div>
