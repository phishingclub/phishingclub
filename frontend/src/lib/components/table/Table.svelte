<script>
	import EmptyTableResult from './EmptyTableResult.svelte';
	import Pagination from '../Pagination.svelte';
	import TableHeader from './TableHeader.svelte';
	import Search from '$lib/components/Search.svelte';
	import Select from '$lib/components/Select.svelte';
	import { afterUpdate, onMount } from 'svelte';
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

	let tableWrapper = null;
	let columnsLength = columns.length;

	let rowsLength = 0;

	afterUpdate(() => {
		const elements = tableWrapper?.querySelectorAll('table > tr.table-row');
		rowsLength = elements?.length ?? 0;
	});

	onMount(() => {
		if (!pagination && sortable?.length) {
			console.warn('You need to pass a pagination object to make the column sortable');
		}
		columnsLength = columns.length + 2;
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
			bind:this={tableWrapper}
			class="
			border-2 border-gray-200 dark:border-gray-700/60 rounded-md px-4 py-4 overflow-x-auto bg-white dark:bg-gray-900/80 transition-colors duration-200
			{scrollBarClassesHorizontal}"
		>
			<table
				class="w-full table-fixed bg-white dark:bg-gray-900/80 transition-colors duration-200"
				class:animate-pulse={isGhost}
			>
				<TableHeader {isGhost} {columns} {sortable} {hasActions} {pagination} />
				{#if !hasData && !isGhost}
					<EmptyTableResult page={currentPage} {plural} colspan={columnsLength} />
				{/if}
				{#if !isGhost}
					<slot />
				{:else}
					{#each Array(rowsLength || pagination?.perPage) as _, row}
						<TableRow>
							{#each columns as column}
								<TableCell>
									<GhostText />
								</TableCell>
							{/each}
							<TableCellEmpty />
							<TableCellAction>
								<GhostText square center />
							</TableCellAction>
						</TableRow>
					{/each}
				{/if}
			</table>
		</div>
		{#if pagination}
			<Pagination paginator={pagination} {hasNextPage} />
		{:else}
			<div class="flex items-center mb-8 mt-4" />
		{/if}
	</div>
</div>
