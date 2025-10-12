<script>
	import { onMount } from 'svelte';

	/**
	 * @type {*|null}
	 */
	export let pagination = null;

	let value = pagination?.perPage;

	const setSearch = () => {
		pagination.perPage = value;
	};

	onMount(() => {
		pagination.onChange((k, v) => {
			if (k === 'perPage') {
				value = v;
			}
		});
	});
</script>

<div>
	<label for="pet-select" class="text-gray-600 dark:text-gray-400 transition-colors duration-200"
		>Show:</label
	>
	<select
		class="bg-grayblue-light dark:bg-gray-900/60 px-2 py-1 rounded-md text-gray-600 dark:text-gray-300 border border-transparent dark:border-gray-700/60 focus:outline-none focus:border-solid focus:border focus:border-slate-400 dark:focus:border-highlight-blue/80 focus:bg-gray-100 dark:focus:bg-gray-700/60 transition-colors duration-200"
		name="entries"
		id="entries"
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
</div>
