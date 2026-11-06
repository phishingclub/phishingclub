<script>
	import { toEvent } from '$lib/utils/events';
	export let eventName;

	// reactive statement to update event when eventName prop changes
	$: event = toEvent(eventName);
</script>

{#if event.name}
	<div
		class="!flex min-w-0 items-center text-gray-900 dark:text-gray-200 transition-colors duration-200"
		title={event.name}
	>
		<div
			class="w-4 h-4 flex-shrink-0 {event.color} mr-2 rounded-sm ring-1 ring-inset ring-black/10 dark:ring-white/50"
		></div>
		<span class="min-w-0 truncate">{event.name}</span>
	</div>
{:else}
	<span class="text-slate-400 dark:text-gray-500">-</span>
{/if}
