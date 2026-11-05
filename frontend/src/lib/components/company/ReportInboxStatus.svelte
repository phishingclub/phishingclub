<script>
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

	const CLASSES = {
		none: 'bg-gray-100 text-gray-600 dark:bg-gray-700 dark:text-gray-300',
		disabled: 'bg-yellow-100 text-yellow-700 dark:bg-yellow-900/40 dark:text-yellow-300',
		error: 'bg-red-100 text-red-700 dark:bg-red-900/40 dark:text-red-300',
		active: 'bg-green-100 text-green-700 dark:bg-green-900/40 dark:text-green-300'
	};
</script>

<div>
	<div class="flex items-center gap-2">
		<span class="text-sm text-gray-500 dark:text-gray-400">Status</span>
		<span class="text-xs font-semibold px-2 py-1 rounded-full {CLASSES[status]}">
			{LABELS[status]}
		</span>
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
