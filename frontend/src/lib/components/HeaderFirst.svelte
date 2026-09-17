<script>
	import { onMount } from 'svelte';
	import { branding, loadBranding, headerLogoSrc, brandingDisplayFor, BRANDING_SLOT } from '$lib/store/branding';
	import BrandingImage from '$lib/components/BrandingImage.svelte';

	onMount(() => loadBranding());
	$: logoSrc = headerLogoSrc($branding);
	$: display = brandingDisplayFor($branding, BRANDING_SLOT.headerLogo);
</script>

<div
	class="bg-gray-300 dark:bg-gray-700 w-full h-1/10 flex justify-center transition-colors duration-200"
>
	<BrandingImage src={logoSrc} alt="logo" {display} boxClass="h-16 w-64 max-w-full" />
</div>
