<script>
	// reusable settings panel card: title, optional description, body slot, optional footer slot.
	// fixed comfortable width so panels can wrap cards consistently.
	export let title = '';
	// short text under the title; set it here so every card has the same gap to the first field
	export let description = '';
	export let widthClass = 'w-full sm:w-96';
</script>

<div
	class="bg-white dark:bg-gray-800 p-6 rounded-lg shadow-sm dark:shadow-gray-900/50 border border-gray-100 dark:border-gray-700 flex flex-col {widthClass} transition-colors duration-200"
>
	<h2
		class="text-lg font-semibold text-pc-darkblue dark:text-white mb-4 pb-2 border-b border-gray-200 dark:border-gray-700 transition-colors duration-200"
	>
		{title}
	</h2>
	{#if description}
		<p class="text-sm text-gray-600 dark:text-gray-300 transition-colors duration-200">
			{description}
		</p>
	{/if}
	<!-- hidden when empty so a card with only a description and a footer has no extra gap -->
	<div class="flex flex-col flex-1 empty:hidden" class:mt-2={description}>
		<slot />
	</div>
	{#if $$slots.footer}
		<!-- empty when the caller shows its footer only sometimes -->
		<div class="mt-auto pt-6 flex justify-end empty:hidden">
			<slot name="footer" />
		</div>
	{/if}
</div>
