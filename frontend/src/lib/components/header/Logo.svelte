<script>
	import { onMount } from 'svelte';
	import { branding, loadBranding, headerLogoSrc, brandingDisplayFor, BRANDING_SLOT } from '$lib/store/branding';
	import BrandingImage from '$lib/components/BrandingImage.svelte';

	onMount(() => loadBranding());
	$: logoSrc = headerLogoSrc($branding);
	$: display = brandingDisplayFor($branding, BRANDING_SLOT.headerLogo);
</script>

<a
	href="/dashboard/"
	aria-label="Go to dashboard"
	class="logo-link flex items-center min-w-0 w-32 sm:w-40 lg:w-56 justify-center py-2 ml-3 sm:ml-4 rounded pc-focus-dark"
>
	<BrandingImage src={logoSrc} alt="logo" {display} ready={$branding.loaded} boxClass="h-12 w-full rounded" />
</a>

<style>
	.logo-link,
	.logo-link:hover {
		background: none !important;
	}
</style>
