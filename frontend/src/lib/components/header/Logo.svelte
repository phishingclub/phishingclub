<script>
	import { goto } from '$app/navigation';
	import { onMount } from 'svelte';
	import { branding, loadBranding, headerLogoSrc, brandingDisplayFor, BRANDING_SLOT } from '$lib/store/branding';
	import BrandingImage from '$lib/components/BrandingImage.svelte';

	onMount(() => loadBranding());
	$: logoSrc = headerLogoSrc($branding);
	$: display = brandingDisplayFor($branding, BRANDING_SLOT.headerLogo);
</script>

<div
	on:click={() => goto('/dashboard/')}
	on:keydown={(e) => e.key === 'Enter' && goto('/dashboard/')}
	tabindex="0"
	role="button"
	class="flex items-center w-40 sm:w-40 md:w-42 lg:w-56 justify-center py-4 my-6 ml-4 cursor-pointer"
>
	<BrandingImage src={logoSrc} alt="logo" {display} boxClass="h-12 w-full rounded" />
</div>

<style>
	div {
		background: none !important;
	}
	div:hover {
		background: none !important;
		background-color: transparent !important;
	}
	div:focus {
		background: none !important;
		background-color: transparent !important;
		outline: none !important;
	}
</style>
