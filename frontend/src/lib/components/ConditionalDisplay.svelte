<script>
	import { displayMode, isShown } from '$lib/store/displayMode';

	/**
	 * what mode should this content be visible in, see isShown
	 * @type {'whitebox' | 'blackbox' | 'simulation' | 'both'}
	 */
	export let show = 'blackbox';

	$: shouldShow = isShown(show, $displayMode);
</script>

{#if shouldShow}
	<slot />
{/if}
