<script>
	export let center = false;
	export let square = false;
	const widths = ['w-16', 'w-20', 'w-24'];
	let height = 'h-4';
	let width = widths[Math.floor(Math.random() * widths.length)];
	let gradient = 'from-gray-300 to-transparent bg-gradient-to-r';
	if (square) {
		width = 'h-4';
		height = 'w-4';
		gradient = 'bg-gray-300';
	}
</script>

{#if center}
	<div class="flex items-center justify-center">
		<div class="{width} {height} {gradient} ">&nbsp;</div>
	</div>
{:else}
	<div class="{width} {height} {gradient}">&nbsp;</div>
{/if}
