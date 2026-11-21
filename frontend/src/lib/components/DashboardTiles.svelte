<script>
	import StatsCard from './StatsCard.svelte';
	import { dashboardIcons } from '$lib/utils/dashboardIcons.js';

	// the one tile row of a dashboard tab. Each tile links to where its number
	// is listed: { key, title, tip?, value, border, color, icon, href }
	export let tiles = [];

	$: grid = tiles.length === 5 ? 'lg:grid-cols-5' : 'lg:grid-cols-4';
</script>

<div class="pc-stats-grid grid grid-cols-2 gap-3 sm:gap-6 mt-4 {grid}">
	{#each tiles as tile (tile.key)}
		<a href={tile.href} class="pc-focus rounded-lg block h-full min-w-0" data-tile={tile.key}>
			<StatsCard
				title={tile.title}
				tip={tile.tip ?? ''}
				value={tile.value}
				borderColor={tile.border}
				iconColor={tile.color}
			>
				<svg
					slot="icon"
					class="h-5 w-5"
					fill="none"
					viewBox="0 0 24 24"
					stroke="currentColor"
					stroke-width="1.5"
					aria-hidden="true"
				>
					{#each dashboardIcons[tile.icon] as part}
						<path
							stroke-linecap="round"
							stroke-linejoin="round"
							d={part.d}
							fill={part.fill ? 'currentColor' : 'none'}
						/>
					{/each}
				</svg>
			</StatsCard>
		</a>
	{/each}
</div>
