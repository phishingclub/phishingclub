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
	// indents the first column by the checkbox width on tables without
	// selection, so Name lines up with selectable tables
	export let reserveSelect = false;

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

	$: columnsLength = shownColumns.length + (hasActions ? 2 : 0) + (selectColumn ? 1 : 0);
	$: skeletonRows = pagination?.perPage || 10;

	// table-fixed ignores min-width on cells, so the table gets a min width from
	// the column sizes and scrolls instead of squashing columns to nothing
	const COLUMN_WIDTHS = { '': 160, small: 128, medium: 176, large: 256, xlarge: 288 };
	const ACTIONS_WIDTH = 96;
	const SELECT_WIDTH = 48;
	const widthOf = (column) =>
		COLUMN_WIDTHS[typeof column === 'object' && columns.length > 1 ? column.size || '' : ''] ??
		COLUMN_WIDTHS[''];

	// below sm the empty checkbox gutter only costs space, so it is not reserved
	let isPhone = false;
	// visible width of the scroll box
	let boxWidth = 0;
	// an empty selectable table on a phone drops the checkbox gutter, it has nothing to select
	$: selectColumn = selectable && (hasData || isGhost || !isPhone);
	$: selectWidth = selectColumn ? SELECT_WIDTH : 0;
	$: actionsWidth = hasActions ? ACTIONS_WIDTH : 0;
	$: columnsMinWidth =
		columns.reduce((sum, column) => sum + widthOf(column), 0) + selectWidth + actionsWidth;
	// the reserved gutter of a table without checkboxes is first column padding
	$: reserveGutter = reserveSelect && !selectable && !isPhone;
	// same rule as TableHeader: long columns share the free space, else the
	// columns of the widest size do; any other column has a fixed width
	const SIZE_RANK = { small: 1, medium: 2, large: 3, xlarge: 4 };
	const sizeOf = (column) =>
		typeof column === 'object' && columns.length > 1 ? column.size || '' : '';
	const isLong = (column) => ['', 'large', 'xlarge'].includes(sizeOf(column));
	const rankOf = (column) => SIZE_RANK[sizeOf(column)] ?? 0;
	$: firstIsFixed =
		columns.length > 1 &&
		(columns.some(isLong)
			? !isLong(columns[0])
			: rankOf(columns[0]) < Math.max(...columns.map(rankOf)));
	// a fixed first column is widened by the gutter, so its text keeps its room
	$: gutterColumnWidth = reserveGutter && firstIsFixed ? widthOf(columns[0]) + SELECT_WIDTH : null;
	// for a fluid first column the gutter is not counted while the columns fit,
	// so a fitting table keeps fitting; it is counted once the table scrolls
	// anyway, so the first column keeps its width
	$: reserveWidth =
		reserveGutter && !firstIsFixed && boxWidth > 0 && columnsMinWidth > boxWidth
			? SELECT_WIDTH
			: 0;
	$: baseMinWidth = columnsMinWidth + reserveWidth;
	// on phones a scrolling table with an actions column gives the first column
	// the rest of the visible width, so the second column starts behind the
	// actions column instead of a clipped sliver; without actions the columns
	// keep their sizes so the next one peeks in as a scroll cue
	$: firstColumnWidth =
		isPhone && hasActions && columns.length > 1 && boxWidth > 0 && baseMinWidth > boxWidth
			? Math.max(120, boxWidth - selectWidth - actionsWidth)
			: gutterColumnWidth;
	$: minTableWidth = baseMinWidth + (firstColumnWidth ? firstColumnWidth - widthOf(columns[0]) : 0);
	// on desktop an empty table fits the box, so no header label is cut or hides
	// behind the pinned actions column and no edge fade shows; on phones it keeps
	// its width like a table with rows, or fixed columns squeeze the others to nothing
	$: isEmptyView = !hasData && !showSkeleton;
	// an empty phone table without an actions column has nothing pinned to
	// cover the extra columns, so it shows only the leading columns that fit
	// (at least two) and fits the box with a round right end
	$: phoneEmptyFit =
		isPhone && isEmptyView && !hasActions && columns.length > 1 && boxWidth > 0 && baseMinWidth > boxWidth;
	const fitCount = (available) => {
		let used = 0;
		let count = 0;
		for (const column of columns) {
			used += widthOf(column);
			if (used > available) {
				break;
			}
			count++;
		}
		return Math.max(2, count);
	};
	$: shownColumns = phoneEmptyFit ? columns.slice(0, fitCount(boxWidth - selectWidth)) : columns;
	$: tableMinWidth = isEmptyView && (!isPhone || phoneEmptyFit) ? 0 : minTableWidth;

	onMount(() => {
		if (!pagination && sortable?.length) {
			console.warn('You need to pass a pagination object to make the column sortable');
		}
	});

	onDestroy(() => {
		clearTimeout(showTimer);
	});

	let currentPage = pagination && pagination.currentPage;

	// a page with fewer rows than the page size is the last page
	let tbodyRef = null;
	let rowCount = null;
	const countRows = () => {
		if (!tbodyRef) {
			return;
		}
		rowCount = tbodyRef.querySelectorAll(':scope > tr:not([data-empty-result])').length;
	};
	let perPage = Number(pagination?.perPage) || 0;
	$: nextPageAvailable =
		hasNextPage && !(rowCount !== null && perPage > 0 && !showSkeleton && rowCount < perPage);

	// shows an edge shadow on the pinned actions column while columns are hidden to its left
	let scrollRef = null;
	let tableRef = null;
	let canScrollRight = false;
	const updateScrollCue = () => {
		if (!scrollRef) {
			return;
		}
		canScrollRight = scrollRef.scrollLeft + scrollRef.clientWidth < scrollRef.scrollWidth - 1;
		boxWidth = scrollRef.clientWidth;
		// the empty state centers on the visible part of the table
		scrollRef.style.setProperty('--pc-scroll-w', `${scrollRef.clientWidth}px`);
	};

	onMount(() => {
		pagination?.onChange((key, value) => {
			if (key === 'page') {
				currentPage = value;
			} else if (key === 'perPage') {
				perPage = Number(value) || 0;
			}
		});
		countRows();
		updateScrollCue();
		const mutationObserver = new MutationObserver(() => {
			countRows();
			updateScrollCue();
		});
		if (tbodyRef) {
			mutationObserver.observe(tbodyRef, { childList: true });
		}
		const resizeObserver = new ResizeObserver(updateScrollCue);
		if (scrollRef) {
			resizeObserver.observe(scrollRef);
		}
		if (tableRef) {
			resizeObserver.observe(tableRef);
		}
		const phoneQuery = window.matchMedia('(max-width: 639.98px)');
		const onPhoneChange = () => {
			isPhone = phoneQuery.matches;
		};
		onPhoneChange();
		phoneQuery.addEventListener('change', onPhoneChange);
		return () => {
			mutationObserver.disconnect();
			resizeObserver.disconnect();
			phoneQuery.removeEventListener('change', onPhoneChange);
		};
	});
</script>

<div class="min-w-0">
	<div class="flex flex-wrap items-center justify-between gap-3 pb-4">
		{#if pagination}
			<Select {pagination}></Select>
		{/if}
		{#if $$slots.toolbar || (pagination && !noSearch)}
			<div class="flex flex-wrap items-center gap-3">
				<slot name="toolbar" />
				{#if pagination && !noSearch}
					<Search {pagination} {plural}></Search>
				{/if}
			</div>
		{/if}
	</div>
	<div
		class="border-2 border-gray-200 dark:border-gray-700/60 rounded-md p-2 sm:p-4 bg-white dark:bg-gray-900 transition-colors duration-200"
	>
		<!-- the scroll box has no padding so rows never show beside the pinned actions column -->
		<div class="relative">
		<div
			bind:this={scrollRef}
			on:scroll={updateScrollCue}
			data-scroll-more={canScrollRight && !isEmptyView ? '' : undefined}
			data-scroll-rows={canScrollRight && hasData ? '' : undefined}
			class="pc-table-scroll overflow-x-auto {scrollBarClassesHorizontal}"
		>
		<table
			bind:this={tableRef}
			class="w-full table-fixed border-separate border-spacing-0 bg-white dark:bg-gray-900 transition-colors duration-200"
			style="min-width: {tableMinWidth}px"
			class:pc-reserve-select={reserveSelect && !selectable}
			class:animate-pulse={showSkeleton}
		>
			<TableHeader
				isGhost={showSkeleton}
				columns={shownColumns}
				{sortable}
				{hasActions}
				{pagination}
				selectable={selectColumn}
				{headerState}
				{firstColumnWidth}
				showSelect={hasData}
				on:toggleAll
			/>
			<tbody
				bind:this={tbodyRef}
				class="transition-opacity duration-200"
				class:animate-pulse={dimRows}
				class:pointer-events-none={dimRows}
			>
				{#if showSkeleton}
					{#each Array(skeletonRows) as _, row}
						<TableRow>
							{#if selectColumn}
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
		{#if canScrollRight && !hasActions && !isEmptyView}
			<!-- no pinned actions column to hint at more columns, so the box edge fades out -->
			<div
				aria-hidden="true"
				class="pointer-events-none absolute inset-y-0 right-0 w-8 bg-gradient-to-r from-transparent to-white dark:to-gray-900"
			></div>
		{/if}
		</div>
	</div>
	{#if pagination}
		<Pagination paginator={pagination} hasNextPage={nextPageAvailable} />
	{:else}
		<div class="flex items-center mb-8 mt-4" />
	{/if}
</div>

<style>
	/* the actions column stays pinned to the right edge, so its cells need an
	   opaque background that matches the row stripe and hover color */
	:global(.pc-table-scroll .pc-sticky-action) {
		position: sticky;
		right: 0;
		z-index: 1;
		background-color: var(--pc-sticky-bg);
	}
	:global(.pc-table-scroll tbody > tr > td.pc-sticky-action) {
		--pc-sticky-bg: #ffffff;
	}
	:global(.pc-table-scroll tbody > tr:nth-child(even) > td.pc-sticky-action) {
		--pc-sticky-bg: #f8f8ff; /* pleasant-gray */
	}
	:global(.pc-table-scroll tbody > tr:hover > td.pc-sticky-action) {
		--pc-sticky-bg: #d4d4ff; /* cta-light-blue */
	}
	:global(.dark .pc-table-scroll tbody > tr > td.pc-sticky-action) {
		--pc-sticky-bg: #111827; /* gray-900 */
	}
	:global(.dark .pc-table-scroll tbody > tr:nth-child(even) > td.pc-sticky-action) {
		--pc-sticky-bg: #151d2c; /* gray-800/30 on gray-900 */
	}
	:global(.dark .pc-table-scroll tbody > tr:hover > td.pc-sticky-action) {
		--pc-sticky-bg: #222e4e; /* highlight-blue/20 on gray-900 */
	}
	:global(.pc-table-scroll thead th.pc-sticky-action) {
		--pc-sticky-bg: #ebeaf9; /* grayblue-light */
	}
	:global(.dark .pc-table-scroll thead th.pc-sticky-action) {
		--pc-sticky-bg: #192231; /* gray-800/60 on gray-900, same as the other header cells */
	}
	/* when the table scrolls sideways other header cells sit under the pinned
	   header cell, so it paints the panel color and draws its rounded pill on
	   top, keeping the right end of the header as round as the left */
	:global(.pc-table-scroll thead th.pc-sticky-action) {
		background-color: #ffffff;
	}
	:global(.dark .pc-table-scroll thead th.pc-sticky-action) {
		background-color: #111827; /* gray-900 */
	}
	:global(.pc-table-scroll thead th.pc-sticky-action::after) {
		content: '';
		position: absolute;
		inset: 0;
		z-index: -1;
		border-radius: 0 0.5rem 0.5rem 0;
		background-color: var(--pc-sticky-bg);
	}
	@media (min-width: 640px) {
		:global(table.pc-reserve-select > thead > tr > th:first-child),
		:global(table.pc-reserve-select > tbody > tr:not([data-empty-result]) > td:first-child) {
			padding-left: 4rem;
		}
	}
	/* a short soft fade on the pinned actions column while columns are hidden
	   to its left, on body rows only */
	:global(.pc-table-scroll[data-scroll-rows] tbody td.pc-sticky-action::before) {
		content: '';
		position: absolute;
		top: 0;
		bottom: 0;
		left: -12px;
		width: 12px;
		pointer-events: none;
		background: linear-gradient(to right, transparent, rgba(15, 23, 42, 0.06));
	}
	:global(.dark .pc-table-scroll[data-scroll-rows] tbody td.pc-sticky-action::before) {
		background: linear-gradient(to right, transparent, rgba(0, 0, 0, 0.25));
	}
	/* header labels that run under the pinned header cell end in a band of the
	   header color, so no cut letters show */
	:global(.pc-table-scroll[data-scroll-more] thead th.pc-sticky-action::before) {
		content: '';
		position: absolute;
		top: 0;
		bottom: 0;
		left: -32px;
		width: 32px;
		pointer-events: none;
		background: linear-gradient(to right, transparent, var(--pc-sticky-bg) 50%);
	}
	/* one look for every clickable table cell: normal text color, underline on
	   hover, focus ring drawn inside so the cell clip does not hide it */
	:global(.pc-table-scroll tbody td > :is(a, button).block.w-full),
	:global(.pc-cell-link) {
		border-radius: 0.125rem;
		text-underline-offset: 2px;
		text-decoration-color: #94a3b8; /* slate-400 */
	}
	:global(.dark .pc-table-scroll tbody td > :is(a, button).block.w-full),
	:global(.dark .pc-cell-link) {
		text-decoration-color: #6b7280; /* gray-500 */
	}
	:global(.pc-table-scroll tbody td > :is(a, button).block.w-full:not(:disabled):hover),
	:global(.pc-cell-link:not(:disabled):hover) {
		text-decoration-line: underline;
	}
	:global(.pc-table-scroll tbody td > :is(a, button).block.w-full:focus-visible),
	:global(.pc-cell-link:focus-visible) {
		outline: 2px solid #445ecc; /* cta-blue */
		outline-offset: -2px;
	}
	:global(.dark .pc-table-scroll tbody td > :is(a, button).block.w-full:focus-visible),
	:global(.dark .pc-cell-link:focus-visible) {
		outline-color: #6887ea; /* highlight-blue */
	}
</style>
