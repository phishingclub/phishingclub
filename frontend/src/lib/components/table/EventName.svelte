<script>
	import { toEvent } from '$lib/utils/events';
	export let eventName;

	// reactive statement to update event when eventName prop changes
	$: event = toEvent(eventName);
</script>

<div
	class="!flex items-center text-ellipsis text-gray-900 dark:text-gray-200 transition-colors duration-200"
>
	<div class="w-4 h-4 flex-shrink-0 {event.color} mr-2 rounded-sm"></div>
	{event.name}
</div>
