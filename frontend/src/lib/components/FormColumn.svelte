<script>
	export let overflowX = false;
</script>

<div
	class="px-6 flex flex-col items-start sm:items-start md:items-start lg:items-start xl:items-start 2xl:items-start text-gray-900 dark:text-gray-100 transition-colors duration-200"
	class:overflow-x-scroll={overflowX}
>
	<slot />
</div>
