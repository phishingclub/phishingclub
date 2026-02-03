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
</script>

<td
	class={`pl-4 font-regular text-slate-600 dark:text-gray-200 text-ellipsis whitespace-nowrap overflow-hidden pr-4 transition-colors duration-200 max-w-0 [&>*]:text-ellipsis [&>*]:overflow-hidden [&>*]:whitespace-nowrap [&>*]:block`}
	title={isDate ? '' : value}
	class:text-left={alignText === 'left'}
	class:text-center={alignText === 'center'}
	class:text-right={alignText === 'right'}
>
	{#if value}
		{#if isDate}
			{#if isRelative}
				<RelativeTime {value} />
			{:else}
				<Datetime {value} {hideHours} />
			{/if}
		{:else}
			{value ?? ''}
		{/if}
	{:else}
		<slot />
	{/if}
</td>
