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

<div >
	<label for="pet-select">Show:</label>
	<select
		class="bg-grayblue-light px-2 py-1 rounded-md text-gray-600 border border-transparent focus:outline-none focus:border-solid focus:border focus:border-slate-400 focus:bg-gray-100"
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
