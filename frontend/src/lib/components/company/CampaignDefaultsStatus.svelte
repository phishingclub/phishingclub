<script>
	import StatusBadge from '$lib/components/StatusBadge.svelte';
	import { api } from '$lib/api/apiProxy.js';

	// status and short summary of the stored campaign defaults, for the settings cards.
	// companyId null shows the global defaults.
	export let companyId = null;
	// the card's modal visibility; the row loads again each time the modal closes
	export let modalVisible = false;

	let row = null;
	let loaded = false;

	const load = async (id) => {
		try {
			const res = await api.company.campaignDefaults.getByCompanyID(id);
			row = res && res.success && res.data ? res.data : null;
		} catch (e) {
			console.error('failed to load campaign defaults', e);
			row = null;
		} finally {
			loaded = true;
		}
	};

	// runs on mount and again when the modal closes or the company changes
	$: if (!modalVisible) load(companyId);

	const isSet = (v) => v !== null && v !== undefined;
	const yesNo = (v) => (v ? 'Yes' : 'No');

	$: items = row
		? [
				isSet(row.closeAfterDays) && {
					label: 'Close',
					value: `${row.closeAfterDays} days after last send`
				},
				isSet(row.anonymizeAfterDays) && {
					label: 'Anonymize all data',
					value: `${row.anonymizeAfterDays} days after close`
				},
				isSet(row.dataAnonymizeAfterDays) && {
					label: 'Anonymize submitted data',
					value: `${row.dataAnonymizeAfterDays} days after close`
				},
				isSet(row.isAnonymous) && { label: 'Anonymous', value: yesNo(row.isAnonymous) },
				isSet(row.saveSubmittedData) && {
					label: 'Save submitted data',
					value: yesNo(row.saveSubmittedData)
				}
			].filter(Boolean)
		: [];

	$: isGlobal = !companyId;
	$: configured = items.length > 0;
</script>

{#if loaded}
	<div>
		<div class="text-sm flex items-center gap-2">
			<span class="text-gray-500 dark:text-gray-400">Status</span>
			{#if configured}
				<StatusBadge tone="info">{isGlobal ? 'Configured' : 'Company overrides'}</StatusBadge>
			{:else}
				<StatusBadge tone="neutral"
					>{isGlobal ? 'Not configured' : 'Global defaults'}</StatusBadge
				>
			{/if}
		</div>
		{#if configured}
			<dl class="mt-3 space-y-1 text-sm">
				{#each items as item}
					<div class="flex flex-wrap gap-x-2">
						<dt class="text-gray-500 dark:text-gray-400">{item.label}</dt>
						<dd class="text-gray-800 dark:text-gray-200">{item.value}</dd>
					</div>
				{/each}
			</dl>
			{#if !isGlobal}
				<p class="mt-2 text-xs text-gray-500 dark:text-gray-400">
					Fields not listed use the global defaults.
				</p>
			{/if}
		{/if}
	</div>
{/if}
