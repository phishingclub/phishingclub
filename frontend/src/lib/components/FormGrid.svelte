<script>
	export let bindTo = null;
	export let isSubmitting = false;
	export let novalidate = false;
</script>

<form
	on:submit|preventDefault
	inert={isSubmitting}
	class="grid grid-cols-3 grid-rows-1 w-full h-full flex-col bg-white dark:bg-gray-800 text-gray-900 dark:text-gray-100 transition-colors duration-200"
	class:opacity-70={isSubmitting}
	bind:this={bindTo}
	{novalidate}
>
	<slot />
</form>
