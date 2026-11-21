<script>
	import { createEventDispatcher } from 'svelte';
	import CellCopy from '$lib/components/table/CopyCell.svelte';
	import { addToast } from '$lib/store/toast';
	import { formatEventData, isSessionCapture, stripCaptureEmoji } from '$lib/utils/eventData.js';

	// data is the raw event data and eventName the event type name
	export let data = '';
	export let eventName = '';

	const dispatch = createEventDispatcher();

	$: formatted = formatEventData(data, eventName) || '';
	$: capture = isSessionCapture(formatted, eventName);

	// copies captured cookies or tokens and asks the page to show the import help
	const onClickCopyCapture = async () => {
		try {
			await navigator.clipboard.writeText(stripCaptureEmoji(formatted));
			dispatch('capture', formatted);
			addToast('Copied to clipboard', 'Success');
		} catch (e) {
			addToast('Failed to copy data to clipboard', 'Error');
			console.error('failed to copy data to clipboard', e);
		}
	};
</script>

{#if capture}
	<button
		class="hover:bg-gray-100 dark:hover:bg-gray-700 px-2 py-1 rounded-md transition-colors w-full text-left text-ellipsis overflow-hidden text-gray-900 dark:text-gray-100"
		title={formatted}
		on:click={onClickCopyCapture}
	>
		{formatted}
	</button>
{:else}
	<CellCopy text={formatted} />
{/if}
