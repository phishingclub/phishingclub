<script>
	export let backgroundColor = 'bg-cta-blue';
	export let css = '';
	export let size = 'medium';
	export let disabled = false;
	/** @type {'primary'|'secondary'|'danger'|'danger-outline'} */
	export let variant = 'primary';
	/** @type {'button'|'submit'|'reset'|undefined} */
	export let type = undefined;
	export let title = undefined;

	$: colorClass =
		variant === 'danger'
			? 'bg-red-600 hover:bg-red-700 text-white border border-transparent'
			: variant === 'danger-outline'
				? 'bg-white hover:bg-red-50 border border-red-600 text-red-700 dark:bg-transparent dark:border-red-500 dark:text-red-400 dark:hover:bg-red-900/30'
				: variant === 'secondary'
				? 'bg-white hover:bg-gray-100 border border-gray-300 text-slate-700 dark:bg-gray-700 dark:hover:bg-gray-600 dark:border-gray-500 dark:text-gray-100'
				: `${backgroundColor} text-white hover:opacity-90 border border-transparent`;
</script>

<button
	{type}
	{title}
	class="pc-focus {colorClass} px-6 py-2 rounded-md uppercase font-semibold text-sm transition-colors duration-200 disabled:opacity-50 dark:disabled:opacity-60 disabled:cursor-not-allowed {css}"
	class:w-32={size === 'small'}
	class:w-36={size === 'medium'}
	class:min-w-[9rem]={size === 'large'}
	{disabled}
	on:click
>
	<slot />
</button>
