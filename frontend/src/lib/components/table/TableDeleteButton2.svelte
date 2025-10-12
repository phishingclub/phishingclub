<script>
	export let disabled = false;
	export let title = '';
	export let name = 'Delete';
	if (disabled && !title) {
		title = 'Delete is disabled';
	}
</script>

{#if disabled}
	<button
		class="w-full px py text-slate-300 dark:text-gray-600 cursor-not-allowed text-left transition-colors duration-200"
		{disabled}
		{title}
	>
		<p class="ml-2 text-left">{name}</p>
	</button>
{:else}
	<button
		class="w-full px py-1 text-slate-600 dark:text-gray-200 hover:bg-red-400 dark:hover:bg-red-600/70 hover:text-white cursor-pointer text-left transition-colors duration-200"
		on:click
		{title}
	>
		<p class="ml-2 text-left">{name}</p>
	</button>
{/if}
