<script>
	// Marks a feature that is still in beta. Use onDark on the blue menus.
	export let onDark = false;
</script>

<span
	class="ml-2 inline-flex shrink-0 items-center rounded px-1.5 py-0.5 text-[10px] font-semibold uppercase leading-none tracking-wide {onDark
		? 'bg-white/20 text-white dark:bg-highlight-blue/20 dark:text-highlight-blue'
		: 'bg-grayblue-light text-gray-500 dark:bg-gray-800/60 dark:text-gray-400'}"
	title="This feature is in beta"
>
	Beta
</span>
