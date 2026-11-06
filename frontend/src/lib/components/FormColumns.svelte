<script>
	export let id = null;
</script>

<div
	class="scrollX col-start-1 col-span-3 row-start-1 row-span-5 py-6 sm:py-8 gap-y-2 flex-col flex lg:flex-row justify-center min-w-0 bg-white dark:bg-gray-800 text-gray-900 dark:text-gray-100 transition-colors duration-200"
	{id}
>
	<slot />
</div>
