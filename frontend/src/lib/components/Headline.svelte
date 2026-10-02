<script>
	import { guideURL } from '$lib/consts/docs';

	// When set, a help icon linking to the matching guide page is shown.
	export let docSlug = '';
	export let docAnchor = '';
</script>

<div class="flex items-start gap-2">
	<h1
		class="text-3xl text-gray-600 dark:text-gray-300 font-bold uppercase transition-colors duration-200"
	>
		<slot />
	</h1>
	{#if docSlug}
		<a
			class="mt-1 inline-flex h-4 w-4 shrink-0 items-center justify-center rounded-full border border-gray-300 text-[10px] font-medium leading-none text-gray-400 hover:border-gray-500 hover:text-gray-600 dark:border-gray-600 dark:text-gray-500 dark:hover:border-gray-400 dark:hover:text-gray-300 transition-colors duration-200"
			href={guideURL(docSlug, docAnchor)}
			target="_blank"
			rel="noopener noreferrer"
			title="Open the documentation for this page"
			aria-label="Open the documentation for this page"
		>
			?
		</a>
	{/if}
</div>
