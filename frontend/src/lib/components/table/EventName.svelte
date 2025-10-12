<script>
	import { toEvent } from '$lib/utils/events';
	import { onMount } from 'svelte';
	export let eventName;

	let event = { name: '', priority: null, color: null };

	onMount(() => {
		event = toEvent(eventName);
	});
</script>

<div
	class="flex items-center text-ellipsis text-gray-900 dark:text-gray-200 transition-colors duration-200"
>
	<div class="w-4 h-4 {event.color} mr-2 rounded-sm"></div>
	{event.name}
</div>
