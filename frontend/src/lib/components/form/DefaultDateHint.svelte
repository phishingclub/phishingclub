<script>
	// The line under a date field that has a default. It says what the date
	// follows, and offers to clear the date or to follow the default again.
	export let id;
	export let text = '';
	// true while the date follows its default
	export let following = false;
	export let hasValue = false;
	export let onClear = () => {};
	export let onUseDefault = () => {};

	const linkClass =
		'pc-focus rounded-sm text-sm font-medium text-cta-blue dark:text-highlight-blue hover:underline';
</script>

{#if text}
	<div class="mt-1 mb-2 flex flex-wrap items-center gap-x-3 gap-y-1">
		<span {id} class="text-sm text-slate-600 dark:text-gray-300">{text}</span>
		{#if hasValue}
			<button type="button" class={linkClass} on:click={onClear}>Clear</button>
		{/if}
		{#if !following}
			<button type="button" class={linkClass} on:click={onUseDefault}>Use default</button>
		{/if}
	</div>
{/if}
