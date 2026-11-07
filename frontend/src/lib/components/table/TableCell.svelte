<!-- TableCell.svelte -->
<script>
	import Datetime from '../Datetime.svelte';
	import RelativeTime from '../RelativeTime.svelte';

	/** @type {string} */
	export let value = '';
	export let isDate = false;
	export let hideHours = false;
	export let isRelative = false; // relative time
	export let alignText = '';
	/** on phones show the text on up to two lines; use only on the main name column */
	export let wrap = false;
	// hideBelow 'md' hides the cell on small screens
	export let hideBelow = '';

	$: hasValue = value !== null && value !== undefined && value !== '' && value !== false;
</script>

<td
	class={`pc-cell pl-4 font-regular text-slate-600 dark:text-gray-200 text-ellipsis whitespace-nowrap overflow-hidden pr-4 transition-colors duration-200 max-w-0 [&>*]:text-ellipsis [&>*]:overflow-hidden [&>*]:whitespace-nowrap`}
	title={isDate || !hasValue ? '' : value}
	class:pc-cell-wrap={wrap}
	class:text-left={alignText === 'left'}
	class:text-center={alignText === 'center'}
	class:text-right={alignText === 'right'}
	class:hidden={hideBelow === 'md'}
	class:md:table-cell={hideBelow === 'md'}
>
	{#if hasValue}
		{#if isDate}
			{#if isRelative}
				<RelativeTime {value} short />
			{:else}
				<Datetime {value} {hideHours} />
			{/if}
		{:else}
			<span class="pc-cell-text">{value ?? ''}</span>
		{/if}
	{:else if $$slots.default}
		<slot />
	{:else}
		<span class="text-slate-400 dark:text-gray-500">-</span>
	{/if}
</td>

<style>
	/* a direct child is a block so long text can end in an ellipsis; zero
	   specificity so any display class on the child (inline-flex, sm:hidden) wins */
	:global(:where(td.pc-cell) > :where(*)) {
		display: block;
	}
	/* on phones a wrap cell shows up to two lines instead of cutting the name short */
	@media (max-width: 639px) {
		:global(td.pc-cell-wrap > button),
		:global(td.pc-cell-wrap > a),
		:global(td.pc-cell-wrap > .pc-cell-text),
		:global(td.pc-cell-wrap > * > .truncate),
		:global(td.pc-cell-wrap > * > * > .truncate) {
			display: -webkit-box;
			-webkit-box-orient: vertical;
			-webkit-line-clamp: 2;
			line-clamp: 2;
			white-space: normal;
			overflow: hidden;
			overflow-wrap: anywhere;
		}
	}
</style>
