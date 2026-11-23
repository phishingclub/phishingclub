<script>
	import StatsCard from './StatsCard.svelte';

	// one row of stat tiles, the same on every page:
	// { key, title, tip?, value, color, icon, href?, percentages?, hiddenTip? }
	// A tile with href links to where its number is listed. A value of null is
	// withheld and shows Hidden, with hiddenTip as its title.
	export let tiles = [];
	export let id = null;
	let className = '';
	export { className as class };

	// columns by tile count. Tiles with a percentage line need more width, so
	// five of them wait for wide screens
	$: hasContext = tiles.some((t) => (t.percentages ?? []).some((p) => p.baseValue > 0));
	$: cols =
		{
			3: 'sm:grid-cols-3',
			4: 'lg:grid-cols-4',
			5: hasContext ? 'lg:grid-cols-3 2xl:grid-cols-5' : 'lg:grid-cols-5',
			6: 'sm:grid-cols-3 2xl:grid-cols-6'
		}[tiles.length] ?? 'lg:grid-cols-4';

	const hiddenCls =
		'text-sm font-normal italic text-gray-500 dark:text-gray-400 cursor-help underline decoration-dotted underline-offset-2';
</script>

<div {id} class="pc-stats-grid grid grid-cols-2 gap-3 sm:gap-4 {cols} {className}">
	{#each tiles as tile (tile.key)}
		{#if tile.value == null}
			<StatsCard
				title={tile.title}
				tip={tile.tip ?? ''}
				icon={tile.icon}
				iconColor={tile.color}
				reserveContext={hasContext}
			>
				<svelte:fragment slot="value"
					><span title={tile.hiddenTip ?? ''} class={hiddenCls}>Hidden</span></svelte:fragment
				>
			</StatsCard>
		{:else}
			<StatsCard
				title={tile.title}
				tip={tile.tip ?? ''}
				value={tile.value}
				icon={tile.icon}
				iconColor={tile.color}
				percentages={tile.percentages ?? []}
				href={tile.href ?? ''}
				tileKey={tile.key}
				reserveContext={hasContext}
			/>
		{/if}
	{/each}
</div>
