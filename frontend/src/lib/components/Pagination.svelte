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

{#if currentPage === 1 && !hasNextPage}
	<div class="mb-8 mt-4" />
{:else}
	<nav aria-label="Pagination" class="flex items-center gap-2 mb-8 mt-4">
		<button
			type="button"
			aria-label="Previous page"
			title="Previous page"
			class="pc-focus inline-flex items-center justify-center w-10 h-10 rounded-md bg-cta-blue text-white hover:bg-active-blue disabled:bg-gray-200 disabled:text-gray-500 dark:disabled:bg-gray-800 dark:disabled:text-gray-500 disabled:cursor-not-allowed transition-colors duration-200"
			disabled={currentPage === 1}
			on:click|preventDefault={previousPage}
		>
			<svg class="w-5 h-5" viewBox="0 0 20 20" fill="currentColor" aria-hidden="true">
				<path
					fill-rule="evenodd"
					d="M12.79 5.23a.75.75 0 0 1-.02 1.06L8.832 10l3.938 3.71a.75.75 0 1 1-1.04 1.08l-4.5-4.25a.75.75 0 0 1 0-1.08l4.5-4.25a.75.75 0 0 1 1.06.02Z"
					clip-rule="evenodd"
				/>
			</svg>
		</button>
		<span
			aria-current="page"
			class="inline-flex items-center h-10 px-3 rounded-md bg-grayblue-light dark:bg-gray-800 text-slate-700 dark:text-gray-200 whitespace-nowrap transition-colors duration-200"
		>
			Page {currentPage}
		</span>
		<button
			type="button"
			aria-label="Next page"
			title="Next page"
			class="pc-focus inline-flex items-center justify-center w-10 h-10 rounded-md bg-cta-blue text-white hover:bg-active-blue disabled:bg-gray-200 disabled:text-gray-500 dark:disabled:bg-gray-800 dark:disabled:text-gray-500 disabled:cursor-not-allowed transition-colors duration-200"
			disabled={!hasNextPage}
			on:click|preventDefault={nextPage}
		>
			<svg class="w-5 h-5" viewBox="0 0 20 20" fill="currentColor" aria-hidden="true">
				<path
					fill-rule="evenodd"
					d="M7.21 14.77a.75.75 0 0 1 .02-1.06L11.168 10 7.23 6.29a.75.75 0 1 1 1.04-1.08l4.5 4.25a.75.75 0 0 1 0 1.08l-4.5 4.25a.75.75 0 0 1-1.06-.02Z"
					clip-rule="evenodd"
				/>
			</svg>
		</button>
	</nav>
{/if}
