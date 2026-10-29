import { writable } from 'svelte/store';
import { api } from '$lib/api/apiProxy';
import { AppStateService } from '$lib/service/appState';

// installedResources holds a map keyed by `${resourceType}:${resourceId}`
// pointing at the package the resource was installed from. It stays empty
// until loadInstalledResources runs and empty if the fetch fails, so a page
// that reads it never breaks when provenance is unavailable.
export const installedResources = writable({});

const keyOf = (resourceType, resourceId) => `${resourceType}:${resourceId}`;

// loaded marks that a successful fetch has filled the map at least once.
let loaded = false;

// inFlight shares one request between concurrent callers so several badges
// mounting together trigger a single fetch.
let inFlight = null;

// generation increases on every reset, so a fetch started before a reset does
// not write its result into the map afterwards.
let generation = 0;

// a failed fetch leaves the map empty and still marks the load done, so the
// app does not retry on every mount.
const fetchInstalledResources = async () => {
	const gen = generation;
	try {
		const res = await api.registry.installedResources();
		if (gen !== generation) {
			return;
		}
		if (res.success && res.data && Array.isArray(res.data.resources)) {
			const next = {};
			for (const r of res.data.resources) {
				next[keyOf(r.resourceType, r.resourceId)] = {
					packageId: r.packageId,
					registryId: r.registryId,
					sourceName: r.sourceName
				};
			}
			installedResources.set(next);
		}
	} catch (e) {
		console.error('failed to load installed resources', e);
	} finally {
		if (gen === generation) {
			loaded = true;
			inFlight = null;
		}
	}
};

// loadInstalledResources loads the provenance map once. Repeated calls reuse
// the cached map and the in flight request.
export const loadInstalledResources = async () => {
	if (loaded) {
		return;
	}
	if (inFlight) {
		return inFlight;
	}
	inFlight = fetchInstalledResources();
	return inFlight;
};

// refreshInstalledResources forces a fresh fetch, for example after an
// install or uninstall changes what is provisioned.
export const refreshInstalledResources = async () => {
	generation++;
	loaded = false;
	inFlight = null;
	return loadInstalledResources();
};

// resetInstalledResources empties the map and forgets the cached load, so the
// next reader fetches it again for the current session.
export const resetInstalledResources = () => {
	generation++;
	loaded = false;
	inFlight = null;
	installedResources.set({});
};

// the map belongs to one session: reset it whenever the login status or the
// logged in user changes, so a later session never sees stale provenance.
let sessionKey = null;
AppStateService.instance.subscribe((state) => {
	const key = `${state?.loginStatus}:${state?.user?.username ?? ''}`;
	if (sessionKey !== null && key !== sessionKey) {
		resetInstalledResources();
	}
	sessionKey = key;
});

// installedInfo returns the package entry for a resource or null when the
// resource was not installed from a package.
export const installedInfo = (map, resourceType, resourceId) => {
	if (!map || !resourceType || !resourceId) {
		return null;
	}
	return map[keyOf(resourceType, resourceId)] || null;
};
