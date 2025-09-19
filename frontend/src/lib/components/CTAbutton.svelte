<script>
	export let disabled = false;
	/**
     * @type {"submit" | "button" | "reset"}

     */
	export let type = 'submit';
</script>

<div class="flex flex-col items-center justify-center w-full p-4 mt-8 h-16">
	<button
		{disabled}
		{type}
		class="w-full p-2 text-white text-2xl bg-cta-blue dark:bg-blue-600 hover:bg-blue-700 dark:hover:bg-blue-700 rounded-lg font-phudu font-bold transition-colors duration-200 disabled:opacity-50 dark:disabled:opacity-60"
		>Login</button
	>
</div>
