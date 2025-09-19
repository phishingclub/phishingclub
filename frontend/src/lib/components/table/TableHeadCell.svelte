<script>
	import { onMount } from 'svelte';
	import GhostText from '../GhostText.svelte';

	export let column;
	/** @type {*|null} */
	export let pagination = null;
	export let alignText = 'left';
	export let sortable = false;
	export let size = '';
	// last tells if it the last field column before the actions column
	export let last = false;
	export let fillRest = false;
	export let isGhost = false;
	export let title = '';

	let sortBy = pagination?.sortBy;
	let sortOrder = pagination?.sortOrder;
	const setSortAndSortBy = () => {
		if (!sortable || !pagination) {
			return;
		}
		pagination.sort(column.toLowerCase(), sortOrder.toLowerCase());
	};

	onMount(() => {
		if (pagination) {
			pagination.onChange(() => {
				sortBy = pagination.sortBy;
				sortOrder = pagination.sortOrder;
			});
		}
	});
</script>

<th
	class="pl-4 bg-grayblue-light dark:bg-gray-700 py-4 border-hidden first:rounded-tl-lg first:rounded-bl-lg last:rounded-tr-lg last:border-4 min-w-48 transition-colors duration-200"
	class:rounded-br-lg={last}
	class:rounded-tr-lg={last}
	class:w-48={size === 'small'}
	class:w-56={size === 'medium'}
	class:w-80={size === 'large'}
	class:w-full={fillRest}
>
	<button
		class="flex group cursor-pointer"
		class:pointer-events-none={!sortable}
		class:table-cell={alignText === 'center'}
		on:click|preventDefault={setSortAndSortBy}
	>
		<div class="w-full">
			<p
				class="font-bold text-slate-600 dark:text-gray-200 text-{alignText} flex transition-colors duration-200"
			>
				{#if !isGhost}
					{title.length ? title : column}
				{:else}
					<GhostText />
				{/if}
				{#if sortable && column.toLowerCase() === sortBy.toLowerCase() && !isGhost}
					<div
						class:bg-transparent={sortOrder === ''}
						class="flex justify-center items-center w-6 h-6 ml-2 rounded-md bg-cta-blue dark:bg-blue-600 transition-colors duration-200"
					>
						{#if sortOrder === 'asc'}
							<div>
								<img src="/arrow-up.svg" alt="arrow up" />
							</div>
						{/if}
						{#if sortOrder === 'desc'}
							<div>
								<img src="/arrow-down.svg" alt="arrow down" />
							</div>
						{/if}
						{#if sortOrder === ''}
							<p class="text-white"></p>
						{/if}
					</div>
				{/if}
			</p>
		</div>
	</button>
</th>
