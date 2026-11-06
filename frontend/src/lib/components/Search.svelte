<script>
	import { onMount } from 'svelte';

	/** @type {*|null} */
	export let pagination = null;
	export let plural = '';
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

<div class="relative flex items-center w-full sm:w-64">
	<img
		class="pointer-events-none w-4 h-4 absolute left-3 z-10 dark:invert"
		src="/search-icon.svg"
		alt=""
		aria-hidden="true"
	/>
	<input
		type="search"
		aria-label={plural ? `Search ${plural}` : 'Search'}
		bind:value
		on:input={() => {
			if (pagination && pagination.search !== null) {
				setSearch();
			}
		}}
		class="w-full h-10 pl-9 pr-3 bg-grayblue-light dark:bg-gray-900/60 border text-slate-800 dark:text-gray-200 border-gray-300 dark:border-gray-700 placeholder:text-slate-500 dark:placeholder:text-gray-400 rounded-lg focus:outline-none focus:border-cta-blue focus:ring-2 focus:ring-cta-blue/30 dark:focus:border-highlight-blue dark:focus:ring-highlight-blue/30 transition-colors duration-200"
		placeholder="Search"
	/>
</div>
