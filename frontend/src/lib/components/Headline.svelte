<script>
	import { guideURL } from '$lib/consts/docs';

	// When set, a help icon linking to the matching guide page is shown.
	export let docSlug = '';
	export let docAnchor = '';
	// keeps data such as emails and domains in their real case
	export let preserveCase = false;
</script>

<!-- the heading is inline so the help icon follows the last word when the title wraps.
     the row is as tall as a form field so titles line up with or without Auto-Refresh -->
<div class="min-w-0 min-h-[2.625rem] flex items-center">
	<div class="min-w-0 break-words">
		<h1
			class="inline text-2xl sm:text-3xl text-gray-600 dark:text-gray-300 font-bold transition-colors duration-200"
			class:uppercase={!preserveCase}
		>
			<slot />
		</h1>
		{#if docSlug}
			<a
				class="group pc-focus ml-1 inline-flex h-7 w-7 items-center justify-center rounded-full align-middle"
				href={guideURL(docSlug, docAnchor)}
				target="_blank"
				rel="noopener noreferrer"
				title="Open the documentation for this page"
				aria-label="Open the documentation for this page"
			>
				<span
					class="inline-flex h-4 w-4 items-center justify-center rounded-full border border-gray-400 text-[10px] font-medium leading-none text-gray-500 group-hover:border-gray-500 group-hover:text-gray-600 dark:border-gray-500 dark:text-gray-400 dark:group-hover:border-gray-300 dark:group-hover:text-gray-200 transition-colors duration-200"
					aria-hidden="true">?</span
				>
			</a>
		{/if}
	</div>
</div>
