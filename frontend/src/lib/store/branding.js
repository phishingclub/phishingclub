import { writable } from 'svelte/store';
import { api } from '$lib/api/apiProxy';
import { API } from '$lib/api/api.js';

// slot names, must match the backend data.BrandingSlot* constants
export const BRANDING_SLOT = {
	headerLogo: 'header-logo',
	loginLogo: 'login-logo',
	loginSideImage: 'login-side-image'
};

// default assets bundled with the app, used when no custom image is uploaded
export const BRANDING_DEFAULTS = {
	headerLogo: '/logo-white.svg',
	loginLogoLight: '/logo-blue.svg',
	loginLogoDark: '/logo-white.svg',
	loginSideImage: '/login-graphics.svg'
};

// default display settings per slot, mirrors the backend defaults so the UI
// renders correctly before the state loads
const defaultDisplay = (slot) => ({
	fit: slot === BRANDING_SLOT.loginSideImage ? 'cover' : 'contain',
	scale: 100,
	background: 'none',
	// the login logo sits top left like the original; other slots center
	positionX: slot === BRANDING_SLOT.loginLogo ? 'left' : 'center',
	positionY: slot === BRANDING_SLOT.loginLogo ? 'top' : 'center'
});

export const BRANDING_DEFAULT_DISPLAY = {
	[BRANDING_SLOT.headerLogo]: defaultDisplay(BRANDING_SLOT.headerLogo),
	[BRANDING_SLOT.loginLogo]: defaultDisplay(BRANDING_SLOT.loginLogo),
	[BRANDING_SLOT.loginSideImage]: defaultDisplay(BRANDING_SLOT.loginSideImage)
};

// branding holds the current branding state. Each slot mode is 'default' or
// 'custom'; the login side image can also be hidden.
export const branding = writable({
	loaded: false,
	headerLogo: 'default',
	loginLogo: 'default',
	loginSideImage: 'default',
	loginSideImageHidden: false,
	display: { ...BRANDING_DEFAULT_DISPLAY },
	// version busts the image cache after an upload or reset
	version: 0
});

// backgroundColor maps the background setting to a CSS color
const BRANDING_BG = { none: 'transparent', light: '#ffffff', dark: '#111827' };

// brandingImageStyle builds the inline style for an image element from its
// display settings. The image fills its box; object-fit and object-position
// place it, transform scales it, and the box supplies the background.
export const brandingImageStyle = (display) => {
	const d = display || {};
	const fit = d.fit || 'contain';
	const scale = (d.scale ?? 100) / 100;
	const posX = d.positionX || 'center';
	const posY = d.positionY || 'center';
	return (
		`width:100%;height:100%;object-fit:${fit};` +
		`object-position:${posX} ${posY};` +
		`transform:scale(${scale});transform-origin:${posX} ${posY};`
	);
};

// brandingBoxStyle builds the inline style for the box wrapping the image
export const brandingBoxStyle = (display) => {
	const bg = BRANDING_BG[(display && display.background) || 'none'] || 'transparent';
	return `background:${bg};overflow:hidden;`;
};

// brandingDisplayFor returns the display settings for a slot with defaults
export const brandingDisplayFor = (state, slot) =>
	(state.display && state.display[slot]) || defaultDisplay(slot);

// guards against firing more than one initial fetch when several components
// mount at once
let inFlight = null;

// loadBranding fetches the branding state. Safe to call on the login screen
// before authentication as the endpoint is public. Concurrent calls share one
// in flight request; the state endpoint is never cached so each fetch is fresh.
export const loadBranding = async () => {
	if (inFlight) {
		return inFlight;
	}
	inFlight = (async () => {
		try {
			const res = await api.branding.getState();
			if (res.success && res.data) {
				branding.set({
					loaded: true,
					headerLogo: res.data.headerLogo,
					loginLogo: res.data.loginLogo,
					loginSideImage: res.data.loginSideImage,
					loginSideImageHidden: !!res.data.loginSideImageHidden,
					display: { ...BRANDING_DEFAULT_DISPLAY, ...(res.data.display || {}) },
					version: Date.now()
				});
			} else {
				branding.update((b) => ({ ...b, loaded: true }));
			}
		} catch (e) {
			console.error('failed to load branding', e);
			branding.update((b) => ({ ...b, loaded: true }));
		} finally {
			inFlight = null;
		}
	})();
	return inFlight;
};

// brandingImageURL builds the public image URL for a slot. It must use the raw
// API instance, not the apiProxy: the proxy wraps every method as an async
// response handler, so calling this synchronous URL builder through it would
// return a Promise and the image would fail to load.
export const brandingImageURL = (slot, version) =>
	API.instance.branding.imageURL(slot, version);

// headerLogoSrc resolves the header logo URL for the current state.
export const headerLogoSrc = (state) =>
	state.headerLogo === 'custom'
		? brandingImageURL(BRANDING_SLOT.headerLogo, state.version)
		: BRANDING_DEFAULTS.headerLogo;
