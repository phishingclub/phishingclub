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

{#if disabled}
	<button
		class="px py-1 text-slate-300 dark:text-gray-500 cursor-not-allowed transition-colors duration-200"
		{disabled}
		{title}
	>
		<p class="ml-2 text-left">{name}</p>
	</button>
{:else}
	<button
		class="px py-1 text-slate-600 dark:text-gray-300 hover:bg-highlight-blue dark:hover:bg-blue-600 hover:text-white cursor-pointer transition-colors duration-200"
		on:click
		on:keydown={handleKeydown}
		{title}
	>
		<p class="ml-2 text-left">{name}</p>
	</button>
{/if}
