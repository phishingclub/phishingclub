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
	<button class="px py-1 text-slate-300 cursor-not-allowed" {disabled} {title}>
		<p class="ml-2 text-left">{name}</p>
	</button>
{:else}
	<button
		class="px py-1 text-slate-600 hover:bg-highlight-blue hover:text-white cursor-pointer"
		on:click
		on:keydown={handleKeydown}
		{title}
	>
		<p class="ml-2 text-left">{name}</p>
	</button>
{/if}
