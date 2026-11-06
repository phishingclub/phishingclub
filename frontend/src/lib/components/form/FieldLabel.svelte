<script>
	import ToolTip from '../ToolTip.svelte';

	export let toolTipText = '';
	export let optional = false;
	// id of the label text, for aria-labelledby on controls the label does not wrap
	export let id = undefined;
</script>

<span class="flex flex-wrap items-center gap-y-1 py-2">
	<span
		{id}
		class="font-semibold text-slate-600 dark:text-gray-400 leading-6 transition-colors duration-200"
	>
		<slot />
	</span>
	{#if optional === true}
		<span
			class="ml-2 inline-flex items-center h-5 px-1.5 rounded text-xs font-normal leading-none bg-gray-100 text-slate-600 dark:bg-gray-700 dark:text-gray-300 transition-colors duration-200"
		>
			optional
		</span>
	{/if}
	{#if toolTipText && toolTipText.length > 0}
		<ToolTip>
			{toolTipText}
		</ToolTip>
	{/if}
</span>
