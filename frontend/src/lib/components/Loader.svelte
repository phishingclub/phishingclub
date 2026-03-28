<script>
	import { isLoading } from '$lib/store/loading.js';
	import { fade } from 'svelte/transition';

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
		transition:fade={{ duration }}
		class="fixed top-0 left-0 w-full h-full flex justify-center items-center z-50"
	>
		<div
			class="w-20 h-20 border-t-8 border-t-cta-blue border-r-8 border-r-cta-blue border-b-cta-blue border-b-8 border-l-transparent border-l-8 rounded-full animate-spin"
		></div>
	</div>
{/if}
