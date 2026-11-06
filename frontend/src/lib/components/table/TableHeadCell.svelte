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
	export let isGhost = false;
	export let title = '';
	// fluid drops the width so the column takes the space left by the others
	export let fluid = false;
	// fixed pixel width, wins over size and fluid
	/** @type {number|null} */
	export let width = null;

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

<!-- on phones labels stay on one line, so columns scrolled out of view
     do not make the header row taller -->
<th
	class="px-4 bg-grayblue-light dark:bg-gray-800/60 py-3 border-hidden first:rounded-tl-lg first:rounded-bl-lg transition-colors duration-200 align-middle"
	class:rounded-br-lg={last}
	class:rounded-tr-lg={last}
	class:w-40={!fluid && size === ''}
	class:w-32={!fluid && size === 'small'}
	class:w-44={!fluid && size === 'medium'}
	class:w-64={!fluid && size === 'large'}
	class:w-72={!fluid && size === 'xlarge'}
	style:width={width ? `${width}px` : null}
	aria-sort={sortable && !isGhost && sortBy && column.toLowerCase() === sortBy.toLowerCase()
		? sortOrder === 'asc'
			? 'ascending'
			: sortOrder === 'desc'
				? 'descending'
				: 'none'
		: undefined}
>
	{#if isGhost}
		<span class="flex py-1" class:justify-center={alignText === 'center'}><GhostText /></span>
	{:else if sortable}
		<button
			type="button"
			class="pc-focus group flex w-full items-center gap-2 -mx-1 px-1 py-1 rounded-md cursor-pointer hover:bg-white/70 dark:hover:bg-gray-700/40 transition-colors duration-200"
			class:justify-center={alignText === 'center'}
			class:justify-end={alignText === 'right'}
			title={column}
			on:click|preventDefault={setSortAndSortBy}
		>
			<span
				class="font-bold text-slate-600 dark:text-gray-200 leading-snug break-normal min-w-0 max-sm:truncate transition-colors duration-200 text-{alignText}"
			>
				{title?.length ? title : column}
			</span>
			{#if sortBy && column.toLowerCase() === sortBy.toLowerCase() && (sortOrder === 'asc' || sortOrder === 'desc')}
				<span
					class="flex flex-shrink-0 justify-center items-center w-6 h-6 rounded-md bg-cta-blue dark:bg-highlight-blue/80 transition-colors duration-200"
				>
					<img src={sortOrder === 'asc' ? '/arrow-up.svg' : '/arrow-down.svg'} alt="" />
				</span>
			{/if}
		</button>
	{:else}
		<span
			class="block py-1 font-bold text-slate-600 dark:text-gray-200 leading-snug break-normal max-sm:truncate transition-colors duration-200 text-{alignText}"
			title={column}
		>
			{title?.length ? title : column}
		</span>
	{/if}
</th>
