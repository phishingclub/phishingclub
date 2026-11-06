<script>
	export let disabled = false;
	export let title = '';
	export let name = 'Delete';
	if (disabled && !title) {
		title = 'Delete is disabled';
	}

	const handleKeydown = (e) => {
		if (e.key === 'Enter' || e.key === ' ') {
			e.preventDefault();
			e.stopPropagation();
			e.target.click();
		}
	};
</script>

<li
	role="none"
	class="pc-menu-danger [&:not(:first-child)]:mt-1 [&:not(:first-child)]:pt-1 [&:not(:first-child)]:border-t border-gray-200 dark:border-gray-600"
>
	{#if disabled}
		<button
			type="button"
			role="menuitem"
			aria-disabled="true"
			class="w-full min-h-10 px-4 py-2 text-left text-red-400 dark:text-red-400/60 cursor-not-allowed transition-colors duration-200"
			{disabled}
			{title}
		>
			{name}
		</button>
	{:else}
		<button
			type="button"
			role="menuitem"
			class="w-full min-h-10 px-4 py-2 text-left text-red-600 dark:text-red-400 hover:bg-red-600 hover:text-white dark:hover:bg-red-600 dark:hover:text-white focus:outline-none focus-visible:bg-red-600 focus-visible:text-white cursor-pointer transition-colors duration-200"
			on:click
			on:keydown={handleKeydown}
			title={title || undefined}
		>
			{name}
		</button>
	{/if}
</li>

<style>
	/* one separator above a run of delete items, not one per item */
	:global(li.pc-menu-danger + li.pc-menu-danger) {
		margin-top: 0;
		padding-top: 0;
		border-top-width: 0;
	}
</style>
