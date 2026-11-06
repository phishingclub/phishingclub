<script>
	export let size = 'large';
	export let isSubmitting = false;
	export let disabled = false;
	/** @type {'primary'|'danger'} */
	export let variant = 'primary';
	/** @type {'submit'|'button'} */
	export let type = 'submit';
</script>

<button
	{type}
	disabled={isSubmitting || disabled}
	aria-busy={isSubmitting}
	class:w-32={size === 'small'}
	class:w-36={size === 'medium'}
	class:min-w-[8rem]={size === 'large'}
	class:px-6={size === 'large'}
	class:px-2={size !== 'large'}
	class:bg-cta-blue={variant !== 'danger'}
	class:hover:bg-active-blue={variant !== 'danger'}
	class:dark:hover:bg-highlight-blue={variant !== 'danger'}
	class:bg-red-600={variant === 'danger'}
	class:hover:bg-red-700={variant === 'danger'}
	class="pc-focus py-2 border border-transparent self-end text-white text-sm font-bold flex justify-center items-center uppercase rounded-md transition-colors duration-200 disabled:opacity-50 dark:disabled:opacity-60 disabled:cursor-not-allowed"
	on:click
>
	{#if isSubmitting}
		<div
			class="mr-2 w-4 h-4 border-t-2 border-t-white border-r-2 border-r-white border-b-white border-b-2 border-l-transparent border-l-2 rounded-xl animate-spin"
			aria-hidden="true"
		></div>
	{:else}
		<slot />
	{/if}
</button>
