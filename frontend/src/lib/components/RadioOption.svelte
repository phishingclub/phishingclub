<script>
	// selectable bordered radio row with label and optional description
	export let checked = false;
	export let label = '';
	export let description = '';
</script>

<label
	class="flex items-start gap-3 p-3 border rounded-lg cursor-pointer transition-colors has-[:focus-visible]:ring-2 has-[:focus-visible]:ring-cta-blue dark:has-[:focus-visible]:ring-highlight-blue {checked
		? 'bg-blue-50 dark:bg-highlight-blue/20 border-cta-blue dark:border-highlight-blue'
		: 'border-gray-300 dark:border-gray-600 hover:border-blue-300 dark:hover:border-highlight-blue/80'}"
>
	<input
		type="radio"
		{checked}
		on:change
		class="mt-0.5 w-4 h-4 shrink-0 accent-cta-blue dark:accent-highlight-blue dark:[color-scheme:dark] focus:outline-none"
	/>
	<div class="text-left flex-1 min-w-0">
		<span class="text-sm font-medium text-gray-900 dark:text-gray-100 block">{label}</span>
		{#if description}
			<span class="text-xs text-gray-500 dark:text-gray-400 block mt-0.5">{description}</span>
		{/if}
	</div>
</label>
