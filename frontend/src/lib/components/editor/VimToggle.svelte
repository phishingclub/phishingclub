<script>
	import { vimModeEnabled, toggleVimMode } from '$lib/store/vimMode.js';
</script>

<button
	type="button"
	on:click={toggleVimMode}
	class="h-8 border-2 border-gray-300 dark:border-gray-600 rounded-md px-3 text-center cursor-pointer hover:opacity-80 flex items-center justify-center gap-2 bg-white dark:bg-gray-700 text-gray-700 dark:text-gray-200 transition-colors duration-200"
	class:font-bold={$vimModeEnabled}
	class:bg-cta-blue={$vimModeEnabled}
	class:dark:bg-indigo-600={$vimModeEnabled}
	class:text-white={$vimModeEnabled}
	title={$vimModeEnabled ? 'Disable vim mode' : 'Enable vim mode'}
>
	<span>Vim</span>
</button>
