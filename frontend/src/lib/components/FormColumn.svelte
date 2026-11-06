<script>
	export let overflowX = false;
	// fill the available width so width="full" fields and long notices use the column
	// instead of sizing it to their content
	export let stretch = false;
</script>

<div
	class="px-1 sm:px-6 max-w-full flex flex-col text-gray-900 dark:text-gray-100 transition-colors duration-200"
	class:items-start={!stretch}
	class:items-stretch={stretch}
	class:w-full={stretch}
	class:lg:flex-1={stretch}
	class:overflow-x-scroll={overflowX}
	class:min-w-0={overflowX || stretch}
	class:flex-1={overflowX}
>
	<slot />
</div>
