<script>
	import StatusBadge from '$lib/components/StatusBadge.svelte';
	import { allowListingTargetStatus } from '$lib/utils/allowListingSync.js';

	// status pill of an allow listing sync target, shared by the card and the modal.
	// target is the GET response data, or null when none is set up.
	export let target = null;
	// show the last error under the pill
	export let showError = true;

	$: status = allowListingTargetStatus(target);
</script>

<div>
	<div class="flex items-center gap-2">
		<span class="text-sm text-gray-500 dark:text-gray-400">Status</span>
		<span class="inline-flex" data-allow-listing-status>
			<StatusBadge tone={status.tone}>{status.label}</StatusBadge>
		</span>
	</div>
	{#if showError && target?.lastError}
		<p
			class="mt-2 text-xs text-red-700 dark:text-red-400 line-clamp-2 break-words"
			title={target.lastError}
		>
			{target.lastError}
		</p>
	{/if}
</div>
