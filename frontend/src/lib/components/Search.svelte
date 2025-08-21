<script>
	import { onMount } from 'svelte';

	/** @type {*|null} */
	export let pagination = null;
	let value = pagination?.search;
	let searchTimeoutID = null;

	const setSearch = () => {
		clearTimeout(searchTimeoutID);
		searchTimeoutID = setTimeout(() => {
			pagination.search = value;
		}, 400);
	};

	onMount(() => {
		// listen for browser back/forward navigation
		//window.addEventListener('popstate', popStateHandler);
		// cleanup on component unmount
		pagination.onChange((k, v) => {
			if (k === 'search') {
				value = v;
			}
		});

		return () => {
			//window.removeEventListener('popstate', popStateHandler);
		};
	});
</script>

<div class="relative flex items-center">
	<img class="ml-2 w-4 h-4 absolute z-10" src="/search-icon.svg" alt="search icon" />
	<input
		type="text"
		bind:value
		on:keyup={() => {
			if (pagination && pagination.search !== null) {
				setSearch();
			}
		}}
		class="bg-grayblue-light w-56 border text-gray-600 border-gray-300 pl-8 py-2 relative rounded-lg focus:outline-none focus:ring-0 focus:border-cta-blue focus:border"
		placeholder="Search"
	/>
</div>
