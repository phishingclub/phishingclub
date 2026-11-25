<script>
	// The allow listing row of a Reporting card: the sync status of one
	// campaign or program and a link to the sync settings.
	import StatusBadge from '$lib/components/StatusBadge.svelte';
	import ToolTip from '$lib/components/ToolTip.svelte';
	import { allowListingStatus, allowListingStatusTip } from '$lib/utils/allowListingSync.js';

	// the status response of the campaign or program
	export let status;
	// a closed campaign or program is no longer needed
	export let closed = false;
	// null when the settings can not be opened from this view
	export let settingsHref = null;

	$: badge = allowListingStatus(status?.status, closed);
	$: tip = allowListingStatusTip(status);
</script>

{#if status && status.status !== 'noTarget'}
	<div class="py-3 first:pt-0 last:pb-0" data-allow-listing-row>
		<div class="flex items-center justify-between gap-x-4">
			<div class="min-w-0">
				<p class="flex items-center text-gray-600 dark:text-gray-400">
					Allow listing
					<ToolTip
						>The domains, sending IPs and simulation URLs are kept in the Microsoft Defender
						Advanced Delivery lists of the tenant.</ToolTip
					>
				</p>
			</div>
			<div class="flex shrink-0 items-center gap-x-3">
				<span class="inline-flex" data-allow-listing-status>
					<StatusBadge tone={badge.tone} title={tip}>{badge.label}</StatusBadge>
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
	</div>
{/if}
