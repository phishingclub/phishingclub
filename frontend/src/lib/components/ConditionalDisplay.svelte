<script>
	import { displayMode, DISPLAY_MODE } from '$lib/store/displayMode';

	/**
	 * what mode should this content be visible in?
	 * @type {'whitebox' | 'blackbox' | 'both'}
	 */
	export let show = 'blackbox';

	$: shouldShow =
		show === 'both' ||
		(show === 'whitebox' && $displayMode === DISPLAY_MODE.WHITEBOX) ||
		(show === 'blackbox' && $displayMode === DISPLAY_MODE.BLACKBOX);
</script>

{#if shouldShow}
	<slot />
{/if}
