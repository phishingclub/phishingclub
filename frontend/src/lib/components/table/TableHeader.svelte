<script>
	import { onMount } from 'svelte';
	import TableHead from './TableHead.svelte';
	import TableHeadCell from './TableHeadCell.svelte';
	import TableHeadCellAction from './TableHeadCellAction.svelte';
	import TableHeadCellEmpty from './TableHeadCellEmpty.svelte';
	import TableRow from './TableRow.svelte';
	import TableRowEmpty from './TableRowEmpty.svelte';
	import TableHeadCellCheckbox from './TableHeadCellCheckbox.svelte';
	import GhostText from '../GhostText.svelte';

	export let columns = [];
	export let sortable = [];
	export let isGhost = false;
	export let hasActions = true;
	/** @type {*|null} */
	export let pagination = null;
	export let selectable = false;
	/** @type {'none'|'some'|'all'} */
	export let headerState = 'none';
	// the checkbox column keeps its width whenever selectable is set; showSelect
	// controls whether the select all control is actually shown, so an empty or
	// loading table reserves the gutter without a dangling control
	export let showSelect = false;
	// pixel width for the first column, set by Table on phones and for a
	// fixed first column that holds the reserved select gutter
	/** @type {number|null} */
	export let firstColumnWidth = null;

	$: sortableMap = {};

	const sizeOf = (column) =>
		typeof column === 'object' && columns.length > 1 ? column.size || '' : '';
	// unsized, large and xlarge columns hold long text, so they share the free
	// space; when every column is small or medium, all columns of the widest
	// size share it, so the checkbox never stretches
	const SIZE_RANK = { small: 1, medium: 2, large: 3, xlarge: 4 };
	const isLong = (column) => ['', 'large', 'xlarge'].includes(sizeOf(column));
	$: hasLong = columns.some(isLong);
	$: widestRank = Math.max(0, ...columns.map((column) => SIZE_RANK[sizeOf(column)] ?? 0));
	$: isFluid = (column) =>
		hasLong ? isLong(column) : (SIZE_RANK[sizeOf(column)] ?? 0) === widestRank;

	onMount(() => {
		sortable.forEach((column) => {
			sortableMap[column.toLowerCase()] = true;
		});
	});
</script>

<TableHead>
	<TableRow>
		{#if selectable}
			<TableHeadCellCheckbox state={headerState} disabled={isGhost || !showSelect} on:toggleAll />
		{/if}
		{#each columns as column, i (typeof column === 'object' ? column.column : column)}
			{#if typeof column === 'object'}
				<TableHeadCell
					{...column}
					{...columns.length === 1 ? { size: '' } : {}}
					fluid={isFluid(column)}
					{pagination}
					{isGhost}
					sortable={sortableMap[column.column.toLowerCase()]}
					last={!hasActions && i === columns.length - 1}
					width={i === 0 ? firstColumnWidth : null}
				/>
			{:else}
				<TableHeadCell
					{column}
					fluid
					{pagination}
					{isGhost}
					sortable={sortableMap[column.toLowerCase()]}
					last={!hasActions && i === columns.length - 1}
					width={i === 0 ? firstColumnWidth : null}
				/>
			{/if}
		{/each}
		{#if hasActions}
			<TableHeadCellEmpty />
			<TableHeadCellAction>
				{#if !isGhost}
					Actions
				{:else}
					<GhostText center />
				{/if}
			</TableHeadCellAction>
		{/if}
	</TableRow>
	<TableRowEmpty />
</TableHead>
