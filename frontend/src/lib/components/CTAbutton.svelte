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
		class="w-full p-2 text-white text-2xl bg-cta-blue rounded-lg font-phudu font-bold"
		>Login</button
	>
</div>
