<script>
	// trail above a detail page title, the last entry is the current page
	/** @type {{ label: string, href?: string }[]} */
	export let crumbs = [];
</script>

<nav class="mt-2 mb-1 text-sm min-w-0 break-words" aria-label="Breadcrumb">
	{#each crumbs as crumb, i}
		{#if i > 0}
			<span class="text-gray-400 dark:text-gray-600 mx-2" aria-hidden="true">/</span>
		{/if}
		{#if crumb.href && i < crumbs.length - 1}
			<a
				href={crumb.href}
				class="pc-focus rounded text-gray-500 dark:text-gray-400 hover:text-cta-blue dark:hover:text-highlight-blue transition-colors"
			>
				{crumb.label}
			</a>
		{:else}
			<span
				class="text-gray-700 dark:text-gray-300"
				aria-current={i === crumbs.length - 1 ? 'page' : undefined}>{crumb.label}</span
			>
		{/if}
	{/each}
</nav>
