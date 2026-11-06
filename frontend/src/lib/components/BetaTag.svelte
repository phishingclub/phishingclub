<script>
	// Marks a feature that is not final yet (Beta, Experimental). Use onDark on the blue menus.
	export let onDark = false;
	export let label = 'Beta';
	export let title = `This feature is ${label === 'Beta' ? 'in beta' : label.toLowerCase()}`;
</script>

<span
	class="ml-2 inline-flex shrink-0 items-center rounded px-1.5 py-0.5 text-xs font-medium leading-none {onDark
		? 'bg-white/20 text-white dark:bg-highlight-blue/20 dark:text-highlight-blue'
		: 'bg-gray-100 text-gray-600 dark:bg-gray-700 dark:text-gray-300'}"
	{title}
>
	{label}
</span>
