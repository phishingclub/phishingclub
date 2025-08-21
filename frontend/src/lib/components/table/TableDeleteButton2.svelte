<script>
	export let disabled = false;
	export let title = '';
	export let name = 'Delete';
	if (disabled && !title) {
		title = 'Delete is disabled';
	}
</script>

{#if disabled}
	<button class="px py text-slate-300 cursor-not-allowed" {disabled} {title}>
		<p class="ml-2 text-left">{name}</p>
	</button>
{:else}
	<button
		class="px py-1 text-slate-600 hover:bg-red-400 hover:text-white cursor-pointer"
		on:click
		{title}
	>
		<p class="ml-2 text-left">{name}</p>
	</button>
{/if}
