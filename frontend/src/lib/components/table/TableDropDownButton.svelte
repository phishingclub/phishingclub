<script>
	export let disabled = false;
	export let title = '';
	export let name;
	if (disabled && !title) {
		title = 'Disabled';
	}

	const handleKeydown = (e) => {
		if (e.key === 'Enter' || e.key === ' ') {
			e.preventDefault();
			e.stopPropagation();
			e.target.click();
		}
	};
</script>

<li role="none">
	{#if disabled}
		<button
			type="button"
			role="menuitem"
			aria-disabled="true"
			class="w-full min-h-10 px-4 py-2 text-left text-slate-400 dark:text-gray-500 cursor-not-allowed transition-colors duration-200"
			{disabled}
			{title}
		>
			{name}
		</button>
	{:else}
		<button
			type="button"
			role="menuitem"
			class="w-full min-h-10 px-4 py-2 text-left text-slate-700 dark:text-gray-100 hover:bg-cta-light-blue dark:hover:bg-highlight-blue/30 focus:outline-none focus-visible:bg-cta-light-blue dark:focus-visible:bg-highlight-blue/30 focus-visible:ring-2 focus-visible:ring-inset focus-visible:ring-cta-blue dark:focus-visible:ring-highlight-blue cursor-pointer transition-colors duration-200"
			on:click
			on:keydown={handleKeydown}
			title={title || undefined}
		>
			{name}
		</button>
	{/if}
</li>
