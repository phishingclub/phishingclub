<script>
	import { createEventDispatcher, tick } from 'svelte';
	import CheckboxField from './CheckboxField.svelte';
	import AutoRefresh from './AutoRefresh.svelte';
	import { dashboardIncludeTest } from '$lib/store/dashboardIncludeTest.js';

	// the controls under the dashboard tab bar, the same on every tab
	export let isLoading = false;
	export let pageId;
	export let onRefresh;
	// no company selected, so every figure covers all companies. A selected
	// company is named in the banner.
	export let allCompanies = false;
	// the test campaign switch, left out on a tab it does not change
	export let includeTest = true;

	const dispatch = createEventDispatcher();
	// the store has the new value once the binding settled
	const onChange = async () => {
		await tick();
		dispatch('change');
	};
</script>

<div class="flex flex-wrap items-center justify-between gap-x-6 gap-y-1">
	<p class="text-sm text-slate-600 dark:text-gray-300 transition-colors duration-200">
		{#if allCompanies}All companies{/if}
	</p>
	<div class="flex flex-wrap items-center gap-x-6 gap-y-1 text-sm">
		{#if includeTest}
			<div class="[&>label]:w-auto">
				<CheckboxField
					bind:value={$dashboardIncludeTest}
					on:change={onChange}
					id="includeTestCampaigns"
					inline={true}
					plain={true}
					toolTipText="Counts, lists and the calendar include test campaigns. The trend, Training completed and Repeat offenders never count test campaigns."
				>
					Include test campaigns
				</CheckboxField>
			</div>
		{/if}
		<AutoRefresh {isLoading} {pageId} {onRefresh} plain={true} />
	</div>
</div>
