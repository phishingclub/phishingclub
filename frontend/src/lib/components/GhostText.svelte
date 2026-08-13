<script>
	export let center = false;
	export let square = false;
	// deterministic placeholder width so the skeleton keeps the same calm shape
	// across reloads instead of reshuffling on every mount
	export let index = 0;
	const widths = ['w-16', 'w-20', 'w-24'];
	let height = 'h-4';
	let width = widths[index % widths.length];
	let gradient = 'from-gray-300 dark:from-gray-600 to-transparent bg-gradient-to-r';
	if (square) {
		width = 'h-4';
		height = 'w-4';
		gradient = 'bg-gray-300 dark:bg-gray-600';
	}
</script>

{#if center}
	<div class="flex items-center justify-center">
		<div class="{width} {height} {gradient} transition-colors duration-200">&nbsp;</div>
	</div>
{:else}
	<div class="{width} {height} {gradient} transition-colors duration-200">&nbsp;</div>
{/if}
