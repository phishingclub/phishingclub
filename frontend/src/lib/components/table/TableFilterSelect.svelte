<script context="module">
	let instanceCount = 0;
</script>

<script>
	import { createEventDispatcher } from 'svelte';

	// a list filter in the table toolbar, styled like the rows per page select
	export let label;
	/** @type {Array<{value: string, label: string}>} */
	export let options = [];
	// the label of the empty value that shows every row
	export let allLabel = 'All';
	export let value = '';

	const dispatch = createEventDispatcher();
	const selectId = `table-filter-${++instanceCount}`;
</script>

<div class="flex items-center gap-2">
	<label for={selectId} class="text-gray-600 dark:text-gray-400 transition-colors duration-200"
		>{label}:</label
	>
	<div class="relative">
		<select
			class="pc-table-filter appearance-none bg-grayblue-light dark:bg-gray-900/60 py-1.5 pl-3 pr-9 rounded-md text-slate-800 dark:text-gray-300 border border-transparent dark:border-gray-700 cursor-pointer transition-colors duration-200 focus:outline-none focus:ring-2 focus:border-cta-blue focus:ring-cta-blue/30 dark:focus:border-highlight-blue dark:focus:ring-highlight-blue/30"
			id={selectId}
			bind:value
			on:change={() => dispatch('change', value)}
		>
			<option value="">{allLabel}</option>
			{#each options as option}
				<option value={option.value}>{option.label}</option>
			{/each}
		</select>
		<img
			class="absolute pointer-events-none w-3 right-3 top-1/2 -translate-y-1/2"
			src="/arrow.svg"
			alt=""
			aria-hidden="true"
		/>
	</div>
</div>
