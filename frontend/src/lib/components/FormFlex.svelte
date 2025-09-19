<script>
	export let bindTo = null;
</script>

<form
	on:submit|preventDefault
	class="col-start-1 col-span-3 row-start-1 row-span-5 py-8 flex-col flex sm:flex-col md:flex-col lg:flex-row justify-start bg-white dark:bg-gray-800 text-gray-900 dark:text-gray-100 transition-colors duration-200"
	bind:this={bindTo}
>
	<slot />
</form>
