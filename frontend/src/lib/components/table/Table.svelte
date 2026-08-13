<script>
	import EmptyTableResult from './EmptyTableResult.svelte';
	import Pagination from '../Pagination.svelte';
	import TableHeader from './TableHeader.svelte';
	import Search from '$lib/components/Search.svelte';
	import Select from '$lib/components/Select.svelte';
	import { onMount, onDestroy } from 'svelte';
	import TableCell from './TableCell.svelte';
	import TableRow from './TableRow.svelte';
	import TableCellEmpty from './TableCellEmpty.svelte';
	import TableCellAction from './TableCellAction.svelte';
	import GhostText from '../GhostText.svelte';
	import { scrollBarClassesHorizontal } from '$lib/utils/scrollbar';

	/** @type {Array<string>|*} */
	export let columns = [];
	/** @type {boolean} */
	export let hasData;
	/** @type {string} */
	export let plural;
	/** @type {*} */
	export let pagination = null;
	export let sortable = [];
	// key value map that should be switched on when selecting a sort by
	export let hasActions = true;
	export let isGhost = false;
	// if there is more data to paginate
	export let hasNextPage = true;
	export let noSearch = false;
	// adds a leading checkbox column for multi select
	export let selectable = false;
	/** @type {'none'|'some'|'all'} */
	export let headerState = 'none';

	// wait this long before revealing any loading state, so quick loads never
	// flash an indicator; the moment the load finishes we drop it again, so an
	// instant load leaves no lingering dim
	const LOADING_DELAY_MS = 150;

	// busy is the delayed view of isGhost: only true for loads slower than the delay
	let busy = false;
	// stays true once real rows have been on screen, so later loads dim the
	// existing rows in place instead of tearing them down for a skeleton
	let hasRenderedData = false;
	let showTimer = null;

	const startBusy = () => {
		if (busy || showTimer) {
			return;
		}
		showTimer = setTimeout(() => {
			showTimer = null;
			busy = true;
		}, LOADING_DELAY_MS);
	};

	const stopBusy = () => {
		if (showTimer) {
			clearTimeout(showTimer);
			showTimer = null;
		}
		busy = false;
	};

	$: isGhost ? startBusy() : stopBusy();
	$: if (hasData) {
		hasRenderedData = true;
	}

	// first load with nothing on screen yet shows the skeleton; any later load
	// keeps the current rows visible and just dims them
	$: showSkeleton = busy && !hasRenderedData;
	$: dimRows = busy && hasRenderedData;

	$: columnsLength = columns.length + (hasActions ? 2 : 0) + (selectable ? 1 : 0);
	$: skeletonRows = pagination?.perPage || 10;

	onMount(() => {
		if (!pagination && sortable?.length) {
			console.warn('You need to pass a pagination object to make the column sortable');
		}
	});

	onDestroy(() => {
		clearTimeout(showTimer);
	});

	let currentPage = pagination && pagination.currentPage;
</script>

<div>
	<div class="">
		<div class="flex justify-between items-center pb-4">
			{#if pagination}
				<Select {pagination}></Select>
				{#if !noSearch}
					<Search {pagination}></Search>
				{/if}
			{/if}
		</div>
		<div
			class="
			border-2 border-gray-200 dark:border-gray-700/60 rounded-md px-4 py-4 overflow-x-auto bg-white dark:bg-gray-900/80 transition-colors duration-200
			{scrollBarClassesHorizontal}"
		>
			<table
				class="w-full table-fixed bg-white dark:bg-gray-900/80 transition-colors duration-200"
				class:animate-pulse={showSkeleton}
			>
				<TableHeader
					isGhost={showSkeleton}
					{columns}
					{sortable}
					{hasActions}
					{pagination}
					{selectable}
					{headerState}
					showSelect={hasData}
					on:toggleAll
				/>
				<tbody
					class="transition-opacity duration-200"
					class:animate-pulse={dimRows}
					class:pointer-events-none={dimRows}
				>
					{#if showSkeleton}
						{#each Array(skeletonRows) as _, row}
							<TableRow>
								{#if selectable}
									<TableCellEmpty />
								{/if}
								{#each columns as column, col}
									<TableCell>
										<GhostText index={row * columns.length + col} />
									</TableCell>
								{/each}
								{#if hasActions}
									<TableCellEmpty />
									<TableCellAction>
										<GhostText square center />
									</TableCellAction>
								{/if}
							</TableRow>
						{/each}
					{:else}
						{#if !hasData}
							<EmptyTableResult page={currentPage} {plural} colspan={columnsLength} />
						{/if}
						<slot />
					{/if}
				</tbody>
			</table>
		</div>
		{#if pagination}
			<Pagination paginator={pagination} {hasNextPage} />
		{:else}
			<div class="flex items-center mb-8 mt-4" />
		{/if}
	</div>
</div>
