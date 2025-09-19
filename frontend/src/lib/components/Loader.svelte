<script>
	import { isLoading } from '$lib/store/loading.js';
	import { blur } from 'svelte/transition';

	let isAnimating = false;
	const duration = 250; //ms

	isLoading.subscribe((s) => {
		const throttle = setTimeout(() => {
			isAnimating = s;
		}, 150);

		return () => clearTimeout(throttle);
	});
</script>

{#if $isLoading && isAnimating}
	<div
		class="fixed top-0 left-0 w-full h-full bg-black dark:bg-gray-900 opacity-[0.5] transition-colors duration-200"
		transition:blur={{ duration }}
	/>
	<div
		transition:blur={{ duration }}
		class="fixed top-0 left-0 w-full h-full flex justify-center items-center backdrop-blur-sm z-50"
	>
		<div
			class="w-20 h-20 border-t-8 border-t-cta-blue dark:border-t-blue-500 border-r-8 border-r-cta-blue dark:border-r-blue-500 border-b-cta-blue dark:border-b-blue-500 border-b-8 border-l-transparent border-l-8 rounded-full animate-spin transition-colors duration-200"
		></div>
	</div>
{/if}
