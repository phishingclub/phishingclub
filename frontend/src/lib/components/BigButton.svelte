<script>
	export let disabled = false;
	/** @type {'button'|'submit'} */
	export let type = 'button';
</script>

<button
	{disabled}
	class="self-start mt-6 bg-gradient-to-b from-blue-500 to-indigo-400 dark:from-blue-600 dark:to-indigo-500 px-4 w-56 py-2 hover:from-blue-400 hover:to-indigo-400 dark:hover:from-blue-500 dark:hover:to-indigo-400 text-white font-bold uppercase rounded-md mb-10 transition-all duration-200"
	on:click
	class:opacity-50={disabled}
	class:dark:opacity-60={disabled}
	class:cursor-not-allowed={disabled}
	{type}
>
	<slot />
</button>
