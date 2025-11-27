<script>
	export let size = 'large';
	export let isSubmitting = false;
</script>

<button
	type="submit"
	disabled={isSubmitting}
	class:w-32={size === 'small'}
	class:w-36={size === 'medium'}
	class:w-60={size === 'large'}
	class="bg-cta-blue px-2 py-2 self-end hover:opacity-80 dark:hover:opacity-90 text-white text-sm font-bold flex justify-center items-center uppercase rounded-md transition-all duration-200 disabled:opacity-50 dark:disabled:opacity-60"
	on:click
>
	{#if isSubmitting}
		<div
			class="mr-2 w-4 h-4 border-t-2 border-t-white border-r-2 border-r-white border-b-white border-b-2 border-l-transparent border-l-2 rounded-xl animate-spin"
		></div>
	{:else}
		<slot />
	{/if}
</button>
