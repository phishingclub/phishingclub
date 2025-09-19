<script>
	let showToolTip = false;
	let tooltipStyle = '';
	let tooltipId = 'tooltip-' + Math.random().toString(36).substr(2, 5);

	function updateTooltipPosition(event) {
		// Position the tooltip near the element using clientX / clientY from the event
		tooltipStyle = `position: fixed; left: ${event.clientX + 10}px; top: ${event.clientY + 10}px;`;
	}
</script>

<div
	class="rounded-full bg-gray-600 dark:bg-gray-500 text-white w-4 h-4 z-30 text-center ml-2 relative cursor-pointer hover:bg-gray-500 dark:hover:bg-gray-400 transition-colors duration-200"
	role="tooltip"
	aria-describedby={tooltipId}
	on:mouseenter={(e) => {
		updateTooltipPosition(e);
		showToolTip = true;
	}}
	on:mouseleave={() => {
		showToolTip = false;
	}}
>
	<p class="text-xs">?</p>
</div>

{#if showToolTip}
	<div
		id={tooltipId}
		class="bg-gray-600 dark:bg-gray-700 text-white w-max mt-2 px-2 py-2 rounded-md shadow-xl dark:shadow-gray-900/70 z-40 transition-colors duration-200"
		style={tooltipStyle}
	>
		<p><slot /></p>
	</div>
{/if}
