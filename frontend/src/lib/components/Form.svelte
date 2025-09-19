<script>
	export let bindTo = null;
	export let fullHeight = false;
	export let fullWidth = false;
</script>

<form
	on:submit|preventDefault
	class="flex w-60 flex-col bg-white dark:bg-gray-800 text-gray-900 dark:text-gray-100 transition-colors duration-200"
	class:h-full={fullHeight}
	class:w-full={fullWidth}
	bind:this={bindTo}
>
	<slot />
</form>
