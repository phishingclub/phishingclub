<script>
	import { onMount } from 'svelte';
	import { get } from 'svelte/store';
	import { api } from '$lib/api/apiProxy.js';
	import { addToast } from '$lib/store/toast';
	import {
		branding,
		loadBranding,
		brandingImageURL,
		brandingDisplayFor,
		BRANDING_SLOT,
		BRANDING_DEFAULTS,
		BRANDING_DEFAULT_DISPLAY
	} from '$lib/store/branding';
	import SettingsCard from '$lib/components/SettingsCard.svelte';
	import { showIsLoading, hideIsLoading } from '$lib/store/loading';
	import Button from '$lib/components/Button.svelte';
	import FileField from '$lib/components/FileField.svelte';
	import CheckboxField from '$lib/components/CheckboxField.svelte';
	import BrandingImage from '$lib/components/BrandingImage.svelte';
	import BrandingAdjust from '$lib/components/BrandingAdjust.svelte';

	let loaded = false;
	// per slot busy flag so controls disable during a request
	let busy = { 'header-logo': false, 'login-logo': false, 'login-side-image': false };
	// hide state, driven one way from the store and updated by the toggle handler
	// so it never fights a reactive assignment
	let sideHidden = false;
	// local editable copy of the display settings so the preview updates live
	let forms = { 'header-logo': null, 'login-logo': null, 'login-side-image': null };

	function syncForms() {
		const b = get(branding);
		sideHidden = b.loginSideImageHidden;
		forms = {
			'header-logo': { ...brandingDisplayFor(b, BRANDING_SLOT.headerLogo) },
			'login-logo': { ...brandingDisplayFor(b, BRANDING_SLOT.loginLogo) },
			'login-side-image': { ...brandingDisplayFor(b, BRANDING_SLOT.loginSideImage) }
		};
	}

	onMount(async () => {
		showIsLoading();
		try {
			await loadBranding();
			syncForms();
		} finally {
			loaded = true;
			hideIsLoading();
		}
	});

	// preview resolves the image shown in a card for a slot
	$: headerPreview =
		$branding.headerLogo === 'custom'
			? brandingImageURL(BRANDING_SLOT.headerLogo, $branding.version)
			: BRANDING_DEFAULTS.headerLogo;
	$: loginPreview =
		$branding.loginLogo === 'custom'
			? brandingImageURL(BRANDING_SLOT.loginLogo, $branding.version)
			: BRANDING_DEFAULTS.loginLogoLight;
	$: sidePreview =
		$branding.loginSideImage === 'custom'
			? brandingImageURL(BRANDING_SLOT.loginSideImage, $branding.version)
			: BRANDING_DEFAULTS.loginSideImage;

	async function handleUpload(slot, event) {
		const input = event.target;
		const file = input.files && input.files[0];
		if (!file) {
			return;
		}
		if (file.type !== 'image/png') {
			addToast('Only PNG images are allowed', 'Error');
			input.value = '';
			return;
		}
		busy[slot] = true;
		try {
			const res = await api.branding.upload(slot, file);
			if (!res.success) {
				addToast(res.error || 'Failed to upload image', 'Error');
				return;
			}
			addToast('Image updated', 'Success');
			await loadBranding();
			syncForms();
		} catch (e) {
			addToast('Failed to upload image', 'Error');
			console.error(e);
		} finally {
			busy[slot] = false;
			input.value = '';
		}
	}

	async function handleReset(slot) {
		busy[slot] = true;
		try {
			const res = await api.branding.reset(slot);
			if (!res.success) {
				addToast(res.error || 'Failed to reset image', 'Error');
				return;
			}
			addToast('Reset to default', 'Success');
			await loadBranding();
			syncForms();
		} catch (e) {
			addToast('Failed to reset image', 'Error');
			console.error(e);
		} finally {
			busy[slot] = false;
		}
	}

	async function handleToggleSideHidden(event) {
		const hidden = event.target.checked;
		const slot = BRANDING_SLOT.loginSideImage;
		busy[slot] = true;
		try {
			const res = await api.branding.setSideImageHidden(hidden);
			if (!res.success) {
				addToast(res.error || 'Failed to update image', 'Error');
			} else {
				addToast(hidden ? 'Login side image hidden' : 'Login side image shown', 'Success');
			}
		} catch (e) {
			addToast('Failed to update image', 'Error');
			console.error(e);
		} finally {
			busy[slot] = false;
			await loadBranding();
			sideHidden = get(branding).loginSideImageHidden;
		}
	}

	// live preview while adjusting
	function onAdjustInput(slot, event) {
		forms[slot] = event.detail;
		forms = forms;
	}

	// persist a committed adjustment
	async function onAdjustChange(slot, event) {
		await persistDisplay(slot, event.detail);
	}

	// reset the adjustments for a slot back to its defaults
	async function onAdjustReset(slot) {
		const def = { ...BRANDING_DEFAULT_DISPLAY[slot] };
		forms[slot] = def;
		forms = forms;
		await persistDisplay(slot, def);
	}

	async function persistDisplay(slot, display) {
		forms[slot] = display;
		forms = forms;
		busy[slot] = true;
		try {
			const res = await api.branding.setDisplay(slot, display);
			if (!res.success) {
				addToast(res.error || 'Failed to save adjustment', 'Error');
			}
			await loadBranding();
			forms[slot] = { ...brandingDisplayFor(get(branding), slot) };
			forms = forms;
		} catch (e) {
			addToast('Failed to save adjustment', 'Error');
			console.error(e);
		} finally {
			busy[slot] = false;
		}
	}
</script>

{#if loaded}
	<div class="flex flex-wrap gap-6">
		<SettingsCard title="Header logo">
			<p class="text-gray-600 dark:text-gray-300 text-sm mb-4 transition-colors duration-200">
				Shown in the top navigation. Upload a PNG, ideally with a transparent background.
			</p>
			<div class="h-24 rounded-md bg-gray-400 dark:bg-gray-900 mb-2 overflow-hidden">
				<BrandingImage
					src={headerPreview}
					alt="header logo preview"
					display={forms['header-logo']}
					boxClass="h-full w-full"
				/>
			</div>
			<FileField
				accept="image/png"
				resets={false}
				disabled={busy['header-logo']}
				on:change={(e) => handleUpload(BRANDING_SLOT.headerLogo, e)}
			/>
			<BrandingAdjust
				display={forms['header-logo']}
				disabled={busy['header-logo']}
				on:input={(e) => onAdjustInput(BRANDING_SLOT.headerLogo, e)}
				on:change={(e) => onAdjustChange(BRANDING_SLOT.headerLogo, e)}
				on:reset={() => onAdjustReset(BRANDING_SLOT.headerLogo)}
			/>
			<svelte:fragment slot="footer">
				{#if $branding.headerLogo === 'custom'}
					<Button
						size={'large'}
						disabled={busy['header-logo']}
						on:click={() => handleReset(BRANDING_SLOT.headerLogo)}
					>
						Reset to default
					</Button>
				{/if}
			</svelte:fragment>
		</SettingsCard>

		<SettingsCard title="Login logo">
			<p class="text-gray-600 dark:text-gray-300 text-sm mb-4 transition-colors duration-200">
				Shown on the login screen. Upload a PNG, ideally with a transparent background.
			</p>
			<div class="h-24 rounded-md bg-gray-400 dark:bg-gray-900 mb-2 overflow-hidden">
				<BrandingImage
					src={loginPreview}
					alt="login logo preview"
					display={forms['login-logo']}
					boxClass="h-full w-full"
				/>
			</div>
			<FileField
				accept="image/png"
				resets={false}
				disabled={busy['login-logo']}
				on:change={(e) => handleUpload(BRANDING_SLOT.loginLogo, e)}
			/>
			<BrandingAdjust
				display={forms['login-logo']}
				disabled={busy['login-logo']}
				on:input={(e) => onAdjustInput(BRANDING_SLOT.loginLogo, e)}
				on:change={(e) => onAdjustChange(BRANDING_SLOT.loginLogo, e)}
				on:reset={() => onAdjustReset(BRANDING_SLOT.loginLogo)}
			/>
			<svelte:fragment slot="footer">
				{#if $branding.loginLogo === 'custom'}
					<Button
						size={'large'}
						disabled={busy['login-logo']}
						on:click={() => handleReset(BRANDING_SLOT.loginLogo)}
					>
						Reset to default
					</Button>
				{/if}
			</svelte:fragment>
		</SettingsCard>

		<SettingsCard title="Login side image">
			<p class="text-gray-600 dark:text-gray-300 text-sm mb-4 transition-colors duration-200">
				The image beside the login form. Upload a PNG, or hide it to center the login box.
			</p>
			<div class="relative h-24 rounded-md bg-gray-400 dark:bg-gray-900 mb-2 overflow-hidden">
				<BrandingImage
					src={sidePreview}
					alt="login side image preview"
					display={forms['login-side-image']}
					boxClass="h-full w-full {sideHidden ? 'opacity-30' : ''}"
				/>
				{#if sideHidden}
					<span
						class="absolute inset-0 flex items-center justify-center text-white text-xs font-semibold"
					>
						<span class="px-2 py-1 rounded bg-gray-900/70">Hidden, login box centered</span>
					</span>
				{/if}
			</div>
			<FileField
				accept="image/png"
				resets={false}
				disabled={busy['login-side-image']}
				on:change={(e) => handleUpload(BRANDING_SLOT.loginSideImage, e)}
			/>
			<BrandingAdjust
				display={forms['login-side-image']}
				disabled={busy['login-side-image']}
				on:input={(e) => onAdjustInput(BRANDING_SLOT.loginSideImage, e)}
				on:change={(e) => onAdjustChange(BRANDING_SLOT.loginSideImage, e)}
				on:reset={() => onAdjustReset(BRANDING_SLOT.loginSideImage)}
			/>
			<CheckboxField
				inline
				value={sideHidden}
				disabled={busy['login-side-image']}
				on:change={handleToggleSideHidden}>Hide image</CheckboxField
			>
			<svelte:fragment slot="footer">
				{#if $branding.loginSideImage === 'custom'}
					<Button
						size={'large'}
						disabled={busy['login-side-image']}
						on:click={() => handleReset(BRANDING_SLOT.loginSideImage)}
					>
						Reset to default
					</Button>
				{/if}
			</svelte:fragment>
		</SettingsCard>
	</div>
{/if}
