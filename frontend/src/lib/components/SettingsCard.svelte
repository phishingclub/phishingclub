<script>
	// reusable settings panel card: title, body slot, optional footer slot.
	// fixed comfortable width so panels can wrap cards consistently.
	export let title = '';
</script>

<div
	class="bg-white dark:bg-gray-800 p-6 rounded-lg shadow-sm dark:shadow-gray-900/50 border border-gray-100 dark:border-gray-700 flex flex-col w-full sm:w-96 transition-colors duration-200"
>
	<h2
		class="text-xl font-semibold text-gray-700 dark:text-gray-200 mb-6 transition-colors duration-200"
	>
		{title}
	</h2>
	<div class="flex flex-col flex-1">
		<slot />
	</div>
	{#if $$slots.footer}
		<div class="mt-auto pt-6 flex justify-end">
			<slot name="footer" />
		</div>
	{/if}
</div>
