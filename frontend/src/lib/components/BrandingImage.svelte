<script>
	// Renders a branding image inside a fixed box, applying the persisted display
	// settings (fit, scale, background, position). The box is always bounded so an
	// oversized upload can never break the layout.
	import { brandingImageStyle, brandingBoxStyle } from '$lib/store/branding';

	export let src;
	export let alt = '';
	export let display = {};
	// sizing and rounding classes for the box, e.g. 'h-12 w-40 rounded-md'
	export let boxClass = '';
</script>

<div class="flex {boxClass}" style={brandingBoxStyle(display)}>
	<img {src} {alt} draggable="false" style={brandingImageStyle(display)} />
</div>
