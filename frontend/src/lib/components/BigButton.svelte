<script>
	export let disabled = false;
	/** @type {'button'|'submit'} */
	export let type = 'button';
	/** @type {'primary'|'secondary'|'danger'|'danger-outline'} */
	export let variant = 'primary';

	const variantClasses = {
		primary:
			'border-transparent text-white bg-gradient-to-b from-blue-600 to-indigo-500 hover:from-blue-500 hover:to-indigo-500 dark:hover:from-blue-500 dark:hover:to-indigo-400',
		secondary:
			'border-gray-300 bg-white hover:bg-gray-100 text-slate-700 dark:bg-gray-700 dark:hover:bg-gray-600 dark:border-gray-500 dark:text-gray-100',
		danger: 'border-transparent text-white bg-red-600 hover:bg-red-700',
		'danger-outline':
			'border-red-600 bg-white hover:bg-red-50 text-red-700 dark:bg-transparent dark:border-red-500 dark:text-red-400 dark:hover:bg-red-900/30'
	};
	$: colorClass = variantClasses[variant] || variantClasses.primary;
</script>

<button
	{disabled}
	class="pc-focus self-start mt-6 px-4 w-full sm:w-56 py-2 border font-bold uppercase rounded-md mb-10 transition-colors duration-200 disabled:opacity-50 dark:disabled:opacity-60 disabled:cursor-not-allowed {colorClass}"
	on:click
	{type}
>
	<slot />
</button>
