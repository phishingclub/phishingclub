<script>
	export let id = null;
</script>

<div
	class="scrollX col-start-1 col-span-3 row-start-1 row-span-5 py-8 flex-col flex sm:flex-col md:flex-col lg:flex-row justify-center bg-white dark:bg-gray-800 text-gray-900 dark:text-gray-100 transition-colors duration-200"
	{id}
>
	<slot />
</div>
