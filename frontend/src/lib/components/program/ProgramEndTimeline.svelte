<script>
	// A bar from the start of a program until its end, or over 12 months when it
	// has none, with a mark per template send window, the time to respond after
	// the last send and the end. The forecast rows hold the same facts in text.
	// timeline comes from the wizard forecast, positions are in percent.
	export let timeline;
	export let id = '';

	$: hasCut = timeline.bars.some((b) => b.cut);
	// a short window keeps a visible mark and stays inside the bar
	const barStyle = (b) => `left:min(${b.left}%, calc(100% - 3px));width:max(${b.width}%, 3px)`;
</script>

<figure {id} class="mt-4 min-w-0">
	<div role="img" aria-label={timeline.summary} class="relative h-5 overflow-hidden">
		<div
			class="absolute inset-x-0 top-1/2 h-1.5 -translate-y-1/2 rounded-full bg-gray-200 dark:bg-gray-700 transition-colors duration-200"
		/>
		{#if timeline.response}
			<div
				class="absolute top-1/2 h-1.5 -translate-y-1/2 bg-blue-200 dark:bg-highlight-blue/40 transition-colors duration-200"
				style="left:{timeline.response.left}%;width:{timeline.response.width}%"
			/>
		{/if}
		{#each timeline.bars as b}
			<div
				class="absolute top-1/2 h-3.5 -translate-y-1/2 rounded-sm transition-colors duration-200 {b.cut
					? 'border border-dashed border-gray-400 dark:border-gray-500'
					: 'bg-cta-blue dark:bg-highlight-blue'}"
				style={barStyle(b)}
			/>
		{/each}
		{#if timeline.end != null}
			<div
				class="absolute inset-y-0 w-0.5 bg-pc-darkblue dark:bg-white transition-colors duration-200"
				style="left:min({timeline.end}%, calc(100% - 2px))"
			/>
		{/if}
	</div>
	<div
		class="flex justify-between gap-2 mt-1 text-xs tabular-nums text-gray-500 dark:text-gray-400"
		aria-hidden="true"
	>
		<span>{timeline.start}</span>
		<span>{timeline.end != null ? `Ends ${timeline.stop}` : timeline.stop}</span>
	</div>
	<ul
		class="flex flex-wrap gap-x-4 gap-y-1 mt-2 text-xs text-gray-600 dark:text-gray-300"
		aria-hidden="true"
	>
		<li class="flex items-center gap-1.5">
			<span class="w-3 h-3 rounded-sm bg-cta-blue dark:bg-highlight-blue" />Send window
		</li>
		{#if timeline.response}
			<li class="flex items-center gap-1.5">
				<span class="w-3 h-1.5 bg-blue-200 dark:bg-highlight-blue/40" />Time to respond
			</li>
		{/if}
		{#if hasCut}
			<li class="flex items-center gap-1.5">
				<span
					class="w-3 h-3 rounded-sm border border-dashed border-gray-400 dark:border-gray-500"
				/>After the end
			</li>
		{/if}
		{#if timeline.end != null}
			<li class="flex items-center gap-1.5">
				<span class="w-0.5 h-3 bg-pc-darkblue dark:bg-white" />End
			</li>
		{/if}
	</ul>
</figure>
