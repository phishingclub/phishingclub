<script>
	import { theme, toggleMode, modeLight, modeDark } from '$lib/theme.js';

	// reactive statement to determine current theme
	$: isDark = $theme === modeDark;

	const handleToggle = () => {
		toggleMode();
	};
</script>

<button
	on:click={handleToggle}
	class="relative inline-flex items-center justify-center w-6 h-6 rounded transition-colors duration-200 focus:outline-none"
	title={isDark ? 'Switch to light mode' : 'Switch to dark mode'}
	aria-label={isDark ? 'Switch to light mode' : 'Switch to dark mode'}
>
	<!-- sun icon for light mode -->
	<svg
		class="w-4 h-4 text-yellow-400 transition-all duration-200 {isDark
			? 'opacity-0 rotate-90 scale-0'
			: 'opacity-100 rotate-0 scale-100'} absolute"
		fill="currentColor"
		viewBox="0 0 20 20"
		xmlns="http://www.w3.org/2000/svg"
	>
		<path
			fill-rule="evenodd"
			d="M10 2a1 1 0 011 1v1a1 1 0 11-2 0V3a1 1 0 011-1zm4 8a4 4 0 11-8 0 4 4 0 018 0zm-.464 4.95l.707.707a1 1 0 001.414-1.414l-.707-.707a1 1 0 00-1.414 1.414zm2.12-10.607a1 1 0 010 1.414l-.706.707a1 1 0 11-1.414-1.414l.707-.707a1 1 0 011.414 0zM17 11a1 1 0 100-2h-1a1 1 0 100 2h1zm-7 4a1 1 0 011 1v1a1 1 0 11-2 0v-1a1 1 0 011-1zM5.05 6.464A1 1 0 106.465 5.05l-.708-.707a1 1 0 00-1.414 1.414l.707.707zm1.414 8.486l-.707.707a1 1 0 01-1.414-1.414l.707-.707a1 1 0 011.414 1.414zM4 11a1 1 0 100-2H3a1 1 0 000 2h1z"
			clip-rule="evenodd"
		/>
	</svg>

	<!-- moon icon for dark mode -->
	<svg
		class="w-4 h-4 text-blue-300 transition-all duration-200 {isDark
			? 'opacity-100 rotate-0 scale-100'
			: 'opacity-0 -rotate-90 scale-0'} absolute"
		fill="currentColor"
		viewBox="0 0 20 20"
		xmlns="http://www.w3.org/2000/svg"
	>
		<path d="M17.293 13.293A8 8 0 016.707 2.707a8.001 8.001 0 1010.586 10.586z" />
	</svg>

	<!-- invisible placeholder to maintain button size -->
	<div class="w-4 h-4 opacity-0">
		<svg class="w-4 h-4" viewBox="0 0 20 20">
			<path d="M10 2a1 1 0 011 1v1a1 1 0 11-2 0V3a1 1 0 011-1z" />
		</svg>
	</div>
</button>
