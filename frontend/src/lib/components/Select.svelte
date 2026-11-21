<script context="module">
	let instanceCount = 0;
</script>

<script>
	import { onMount } from 'svelte';
	import { setPerPagePreference } from '$lib/store/preferences';

	/**
	 * @type {*|null}
	 */
	export let pagination = null;

	let value = pagination?.perPage;
	const selectId = `per-page-${++instanceCount}`;

	const setSearch = () => {
		pagination.perPage = value;
		// save to localStorage for persistence across pages
		setPerPagePreference(value);
	};

	onMount(() => {
		pagination.onChange((k, v, changes) => {
			if ('perPage' in changes) {
				value = changes.perPage;
			}
		});
	});
</script>

<div class="flex items-center gap-2">
	<label for={selectId} class="text-gray-600 dark:text-gray-400 transition-colors duration-200"
		>Show:</label
	>
	<div class="relative">
		<select
			class="pc-per-page appearance-none bg-grayblue-light dark:bg-gray-900/60 py-1.5 pl-3 pr-9 rounded-md text-slate-800 dark:text-gray-300 border border-transparent dark:border-gray-700 cursor-pointer transition-colors duration-200 focus:outline-none focus:ring-2 focus:border-cta-blue focus:ring-cta-blue/30 dark:focus:border-highlight-blue dark:focus:ring-highlight-blue/30"
			name="entries"
			id={selectId}
			aria-label="Rows per page"
			bind:value
			on:change={() => {
				if (pagination && pagination.perPage !== null) {
					setSearch();
				}
			}}
		>
			{#each [10, 25, 50] as option}
				{#if option === value}
					<option value={option} selected>{option}</option>
				{:else}
					<option value={option}>{option}</option>
				{/if}
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
