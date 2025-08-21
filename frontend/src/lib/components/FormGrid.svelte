<script>
	export let bindTo = null;
	export let isSubmitting = false;
	export let novalidate = false;
</script>

<form
	on:submit|preventDefault
	inert={isSubmitting}
	class="grid grid-cols-3 grid-rows-1 w-full h-full flex-col"
	class:opacity-70={isSubmitting}
	bind:this={bindTo}
	{novalidate}
>
	<slot />
</form>
