<script>
	export let bindTo = null;
	export let fullHeight = false;
	export let fullWidth = false;
</script>

<form
	on:submit|preventDefault
	class="flex w-60 flex-col"
	class:h-full={fullHeight}
	class:w-full={fullWidth}
	bind:this={bindTo}
>
	<slot />
</form>
