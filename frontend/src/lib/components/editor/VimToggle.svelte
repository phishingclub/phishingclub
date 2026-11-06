<script>
	import { vimModeEnabled, toggleVimMode } from '$lib/store/vimMode.js';
</script>

<button
	type="button"
	on:click={toggleVimMode}
	aria-pressed={$vimModeEnabled}
	class="h-8 inline-flex items-center justify-center gap-2 px-3 rounded-md border text-sm whitespace-nowrap cursor-pointer transition-colors duration-200 focus:outline-none focus-visible:ring-2 focus-visible:ring-cta-blue dark:focus-visible:ring-highlight-blue {$vimModeEnabled
		? 'bg-cta-blue text-white border-cta-blue font-semibold hover:bg-active-blue'
		: 'bg-white dark:bg-gray-700 text-gray-700 dark:text-gray-200 border-gray-300 dark:border-gray-600 hover:bg-gray-50 dark:hover:bg-gray-600'}"
	title={$vimModeEnabled ? 'Disable vim mode' : 'Enable vim mode'}
>
	<span>Vim</span>
</button>
