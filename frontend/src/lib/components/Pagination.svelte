<script>
	import { onMount } from 'svelte';

	export let paginator;

	let currentPage = paginator.currentPage;
	let urlWithoutParams = window.location.href.split('?')[0];

	export let hasNextPage = true;

	const nextPage = async () => {
		currentPage = paginator.next();
	};

	const previousPage = async () => {
		currentPage = paginator.previous();
	};

	const popStateHandler = () => {
		// only handle popstate if the url matches the current page
		// this is to avoid handling popstate events from other pages
		// upon a browser back/forward navigation as popstate event happens
		// before the component is unmounted
		if (window.location.href.split('?')[0] !== urlWithoutParams) {
			return;
		}
		currentPage = paginator.currentPage;
	};

	onMount(() => {
		// listen for browser back/forward navigation
		window.addEventListener('popstate', popStateHandler);
		// cleanup on component unmount
		paginator.onChange((key, value) => {
			if (key === 'page') {
				currentPage = value;
			}
		});

		return () => {
			window.removeEventListener('popstate', popStateHandler);
		};
	});
</script>

<div class="flex items-center mb-8 mt-4">
	<button
		class="bg-highlight-blue dark:bg-highlight-blue/80 w-8 text-white hover:bg-active-blue dark:hover:bg-highlight-blue m-1 rounded-md py-1 px-1 transition-colors duration-200"
		disabled={currentPage === 1}
		class:opacity-50={currentPage === 1}
		on:click|preventDefault={previousPage}>&lt;&lt;</button
	>
	<div
		class="w-8 text-center bg-grayblue-light dark:bg-gray-800/60 text-gray-700 dark:text-gray-300 rounded-md py-1 px-1 transition-colors duration-200"
	>
		{currentPage}
	</div>
	<button
		disabled={!hasNextPage}
		class:opacity-50={!hasNextPage}
		class=" bg-highlight-blue dark:bg-highlight-blue/80 w-8 text-white hover:bg-active-blue dark:hover:bg-highlight-blue m-1 rounded-md py-1 px-1 transition-colors duration-200"
		on:click|preventDefault={nextPage}>&gt;&gt;</button
	>
</div>
