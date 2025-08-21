<script>
	import { onMount } from 'svelte';
	import TableHead from './TableHead.svelte';
	import TableHeadCell from './TableHeadCell.svelte';
	import TableHeadCellAction from './TableHeadCellAction.svelte';
	import TableHeadCellEmpty from './TableHeadCellEmpty.svelte';
	import TableRow from './TableRow.svelte';
	import TableRowEmpty from './TableRowEmpty.svelte';
	import GhostText from '../GhostText.svelte';

	export let columns = [];
	export let sortable = [];
	export let isGhost = false;
	export let hasActions = true;
	/** @type {*|null} */
	export let pagination = null;

	$: sortableMap = {};

	onMount(() => {
		sortable.forEach((column) => {
			sortableMap[column.toLowerCase()] = true;
		});
	});
</script>

<TableHead>
	<TableRow>
		{#each columns as column, i (i)}
			{#if typeof column === 'object'}
				<TableHeadCell
					{...column}
					{...columns.length === 1 ? { size: '' } : {}}
					{pagination}
					{isGhost}
					sortable={sortableMap[column.column.toLowerCase()]}
					last={i === columns.length - 1}
					fillRest={i === columns.length - 1 && !hasActions}
				/>
			{:else}
				<TableHeadCell
					{column}
					{pagination}
					sortable={sortableMap[column.toLowerCase()]}
					last={i === columns.length - 1}
					fillRest={i === columns.length - 1 && !hasActions}
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
