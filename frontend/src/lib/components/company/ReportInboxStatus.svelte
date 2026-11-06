<script>
	import StatusBadge from '$lib/components/StatusBadge.svelte';

	// status pill for a report inbox, shared by the settings card, the company card and the modal.
	// inbox is the GET response data, or null when no inbox is configured.
	export let inbox = null;
	// show the last error under the pill
	export let showError = true;

	$: status = !inbox ? 'none' : !inbox.enabled ? 'disabled' : inbox.lastError ? 'error' : 'active';

	const LABELS = {
		none: 'Not configured',
		disabled: 'Disabled',
		error: 'Error',
		active: 'Active'
	};

	const TONES = {
		none: 'neutral',
		disabled: 'warning',
		error: 'danger',
		active: 'success'
	};
</script>

<div>
	<div class="flex items-center gap-2">
		<span class="text-sm text-gray-500 dark:text-gray-400">Status</span>
		<StatusBadge tone={TONES[status]}>{LABELS[status]}</StatusBadge>
	</div>
	{#if showError && status === 'error'}
		<p
			class="mt-2 text-xs text-red-700 dark:text-red-400 line-clamp-2 break-words"
			title={inbox.lastError}
		>
			{inbox.lastError}
		</p>
	{/if}
</div>
