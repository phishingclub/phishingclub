<script>
	// The reporting inbox row of a Reporting card: its last check, status and a
	// link to its settings, with the last error under it.
	import StatusBadge from '$lib/components/StatusBadge.svelte';
	import ToolTip from '$lib/components/ToolTip.svelte';
	import { reportInboxStatus } from '$lib/utils/reportInbox.js';

	export let inbox;
	// the time the last check is measured against, in ms
	export let now = Date.now();
	// null when the settings can not be opened from this view
	export let settingsHref = null;

	$: status = reportInboxStatus(inbox, now);
</script>

{#if status}
	<div class="py-3 first:pt-0 last:pb-0">
		<div class="flex items-center justify-between gap-x-4">
			<div class="min-w-0">
				<p class="flex items-center text-gray-600 dark:text-gray-400">
					Reporting inbox
					<ToolTip
						>Reports sent to the Reporting Inbox are matched to recipients automatically.</ToolTip
					>
				</p>
				{#if status.lastCheck}
					<p class="text-xs text-gray-500 dark:text-gray-400">{status.lastCheck}</p>
				{/if}
			</div>
			<div class="flex shrink-0 items-center gap-x-3">
				<span data-report-inbox>
					<StatusBadge tone={status.tone}>{status.label}</StatusBadge>
				</span>
				{#if settingsHref}
					<a
						href={settingsHref}
						class="pc-focus rounded-sm text-xs text-cta-blue dark:text-highlight-blue hover:underline"
						>Settings</a
					>
				{/if}
			</div>
		</div>
		{#if inbox.lastError}
			<p
				class="mt-1 text-xs text-red-700 dark:text-red-400 line-clamp-2 break-words"
				title={inbox.lastError}
			>
				{inbox.lastError}
			</p>
		{/if}
	</div>
{/if}
