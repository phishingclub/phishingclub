<script>
	import { addToast } from '$lib/store/toast';
	import Modal from '../Modal.svelte';
	import Alert from '../Alert.svelte';
	import { api } from '$lib/api/apiProxy.js';

	// external
	export let visible = false;
	/** @type {{ id: string, name: string } | null} */
	export let company = null;

	// local state
	let scimConfig = null;
	// the global domain SCIM is served on; SCIM lives on the phishing server, not
	// this admin origin, so the base URL must use the configured domain
	let scimDomain = '';
	let isLoading = false;
	let isTogglingEnabled = false;
	let isRotating = false;
	let isSettingUp = false;
	let isDeleting = false;

	// token reveal — only populated immediately after create or rotate
	let revealedToken = '';
	let showTokenReveal = false;

	// confirmation dialogs
	let isRotateAlertVisible = false;
	let isDeleteAlertVisible = false;

	// reactive: reload when modal opens
	$: {
		if (visible && company) {
			loadAll();
		}
	}

	// reactive: clean up when modal closes
	$: {
		if (!visible) {
			resetState();
		}
	}

	const resetState = () => {
		scimConfig = null;
		revealedToken = '';
		showTokenReveal = false;
		isRotateAlertVisible = false;
		isDeleteAlertVisible = false;
	};

	const loadAll = async () => {
		isLoading = true;
		try {
			await Promise.all([loadScimConfig(), loadScimDomain()]);
		} finally {
			isLoading = false;
		}
	};

	const loadScimDomain = async () => {
		try {
			const res = await api.option.getScimDomain();
			if (res.success) {
				scimDomain = res.data.domain || '';
			}
		} catch (err) {
			console.error('failed to load SCIM domain', err);
		}
	};

	const loadScimConfig = async () => {
		try {
			const res = await api.company.scim.getByCompanyID(company.id);
			if (res && res.success && res.data) {
				scimConfig = res.data;
			} else {
				scimConfig = null;
			}
		} catch (e) {
			console.error('failed to load scim config', e);
			scimConfig = null;
		}
	};

	// called once — creates the config and reveals the token
	const onSetUp = async () => {
		isSettingUp = true;
		try {
			const res = await api.company.scim.upsert(company.id, { enabled: true });
			if (!res || !res.success) {
				addToast(res?.error ?? 'Failed to set up SCIM', 'Error');
				return;
			}
			scimConfig = res.data;
			if (res.data?.token) {
				revealedToken = res.data.token;
				showTokenReveal = true;
			}
			addToast('SCIM set up', 'Success');
		} catch (e) {
			console.error('failed to set up scim', e);
			addToast('Failed to set up SCIM', 'Error');
		} finally {
			isSettingUp = false;
		}
	};

	// inline toggle — immediately persists the new enabled state
	const onToggleEnabled = async () => {
		if (!scimConfig) return;
		isTogglingEnabled = true;
		const newEnabled = !scimConfig.enabled;
		try {
			const res = await api.company.scim.upsert(company.id, { enabled: newEnabled });
			if (!res || !res.success) {
				addToast(res?.error ?? 'Failed to update SCIM', 'Error');
				return;
			}
			scimConfig = res.data;
			addToast(newEnabled ? 'SCIM enabled' : 'SCIM disabled', 'Success');
		} catch (e) {
			console.error('failed to toggle scim enabled', e);
			addToast('Failed to update SCIM', 'Error');
		} finally {
			isTogglingEnabled = false;
		}
	};

	const onConfirmRotateToken = async () => {
		isRotating = true;
		try {
			const res = await api.company.scim.rotateToken(company.id);
			if (!res || !res.success) {
				addToast(res?.error ?? 'Failed to rotate token', 'Error');
				return { success: false };
			}
			scimConfig = res.data;
			revealedToken = res.data?.token ?? '';
			showTokenReveal = true;
			addToast('SCIM token rotated', 'Success');
			return { success: true };
		} catch (e) {
			console.error('failed to rotate scim token', e);
			addToast('Failed to rotate SCIM token', 'Error');
			return { success: false };
		} finally {
			isRotating = false;
		}
	};

	const onConfirmDelete = async () => {
		isDeleting = true;
		try {
			const res = await api.company.scim.delete(company.id);
			if (!res || !res.success) {
				addToast(res?.error ?? 'Failed to delete SCIM config', 'Error');
				return { success: false };
			}
			scimConfig = null;
			showTokenReveal = false;
			revealedToken = '';
			addToast('SCIM config deleted', 'Success');
			return { success: true };
		} catch (e) {
			console.error('failed to delete scim config', e);
			addToast('Failed to delete SCIM config', 'Error');
			return { success: false };
		} finally {
			isDeleting = false;
		}
	};

	const copyToClipboard = async (text) => {
		try {
			await navigator.clipboard.writeText(text);
			addToast('Copied to clipboard', 'Success');
		} catch (e) {
			console.error('failed to copy to clipboard', e);
		}
	};

	const dismissToken = () => {
		showTokenReveal = false;
		revealedToken = '';
	};

	const formatDate = (dateStr) => {
		if (!dateStr) return 'Never';
		try {
			return new Date(dateStr).toLocaleString();
		} catch {
			return dateStr;
		}
	};

	$: scimBaseURL = company && scimDomain ? `https://${scimDomain}/api/v1/scim/v2/${company.id}` : '';

	$: isBusy = isSettingUp || isTogglingEnabled || isRotating || isDeleting;
</script>

<Modal headerText={`SCIM`} bind:visible>
	<div class="w-[600px] p-6 space-y-6">
		{#if isLoading}
			<div class="flex items-center justify-center py-10">
				<p class="text-gray-500 dark:text-gray-400 transition-colors duration-200">Loading...</p>
			</div>
		{:else if showTokenReveal && revealedToken}
			<!-- ── step 2: token reveal — nothing else until dismissed ── -->
			<div
				class="rounded-md border border-amber-400 dark:border-amber-500/60 bg-amber-50 dark:bg-amber-900/20 p-4 space-y-3 transition-colors duration-200"
			>
				<p class="text-sm font-semibold text-amber-700 dark:text-amber-400">
					⚠ Copy this token now — it will not be shown again.
				</p>
				<div class="flex items-center gap-2">
					<input
						type="text"
						readonly
						value={revealedToken}
						class="flex-1 px-3 py-2 text-sm rounded-md bg-white dark:bg-gray-900/60 border border-amber-300 dark:border-amber-600/60 text-gray-800 dark:text-gray-200 font-mono focus:outline-none transition-colors duration-200"
					/>
					<button
						type="button"
						on:click={() => copyToClipboard(revealedToken)}
						class="px-3 py-2 text-sm bg-amber-500 hover:bg-amber-400 dark:bg-amber-600/80 dark:hover:bg-amber-500/80 text-white rounded-md transition-colors duration-200"
					>
						Copy
					</button>
				</div>
				<button
					type="button"
					class="text-xs text-amber-600 dark:text-amber-500 underline hover:no-underline"
					on:click={dismissToken}
				>
					I have copied the token, dismiss
				</button>
			</div>
		{:else if !scimConfig}
			<!-- ── step 1: no config yet ── -->
			<p class="text-sm text-gray-500 dark:text-gray-400 transition-colors duration-200">
				Set up SCIM provisioning for <strong class="text-gray-700 dark:text-gray-300"
					>{company?.name}</strong
				>. <br /> A bearer token will be generated once and must be copied into your identity provider.
			</p>
			<div class="flex justify-end">
				<button
					type="button"
					disabled={isBusy}
					on:click={onSetUp}
					class="bg-cta-blue dark:bg-highlight-blue/80 hover:bg-blue-700 dark:hover:bg-highlight-blue text-sm uppercase font-bold px-4 py-2 text-white rounded-md disabled:opacity-50 disabled:cursor-not-allowed transition-colors duration-200"
				>
					{isSettingUp ? 'Setting up...' : 'Configure SCIM'}
				</button>
			</div>
		{:else}
			<!-- ── step 3: config exists ── -->

			<!-- status panel -->
			<div
				class="rounded-md border border-gray-200 dark:border-gray-700/60 p-4 space-y-3 transition-colors duration-200"
			>
				<div class="grid grid-cols-2 gap-x-6 gap-y-2 text-sm">
					<span class="text-gray-500 dark:text-gray-500">Token prefix</span>
					<span class="text-gray-800 dark:text-gray-200 font-mono">
						{scimConfig.tokenPrefix ? scimConfig.tokenPrefix + '...' : '—'}
					</span>
					<span class="text-gray-500 dark:text-gray-500">Last sync</span>
					<span class="text-gray-800 dark:text-gray-200">{formatDate(scimConfig.lastSyncAt)}</span>
				</div>

				<!-- scim base url -->
				<div class="pt-3 border-t border-gray-200 dark:border-gray-700/60 space-y-1">
					<p class="text-xs font-semibold text-gray-500 dark:text-gray-400">
						SCIM Base URL - provide this to your identity provider
					</p>
					{#if scimDomain}
						<div class="flex items-center gap-2">
							<input
								type="text"
								readonly
								value={scimBaseURL}
								class="flex-1 px-3 py-2 text-xs rounded-md bg-gray-50 dark:bg-gray-900/60 border border-gray-200 dark:border-gray-700/60 text-gray-700 dark:text-gray-300 font-mono focus:outline-none transition-colors duration-200"
							/>
							<button
								type="button"
								on:click={() => copyToClipboard(scimBaseURL)}
								class="px-3 py-2 text-sm bg-slate-500 hover:bg-slate-400 dark:bg-gray-700/80 dark:hover:bg-gray-600/80 text-white rounded-md transition-colors duration-200"
							>
								Copy
							</button>
						</div>
					{:else}
						<p class="text-xs text-amber-600 dark:text-amber-400">
							No SCIM domain is configured. Set a global SCIM domain under Settings → System before
							connecting an identity provider.
						</p>
					{/if}
				</div>
			</div>

			<!-- enabled toggle -->
			<div class="flex items-center justify-between">
				<div>
					<p class="text-sm font-medium text-gray-700 dark:text-gray-300">Provisioning</p>
					<p class="text-xs text-gray-500 dark:text-gray-400 mt-0.5">
						{scimConfig.enabled
							? 'Active - IdP can push changes.'
							: 'Paused - incoming SCIM requests will be rejected.'}
					</p>
				</div>
				<button
					type="button"
					role="switch"
					aria-checked={scimConfig.enabled}
					disabled={isBusy}
					on:click={onToggleEnabled}
					class="relative inline-flex h-6 w-11 flex-shrink-0 cursor-pointer rounded-full border-2 border-transparent transition-colors duration-200 focus:outline-none disabled:opacity-50 disabled:cursor-not-allowed
						{scimConfig.enabled ? 'bg-cta-blue dark:bg-highlight-blue/80' : 'bg-gray-300 dark:bg-gray-600'}"
				>
					<span
						class="pointer-events-none inline-block h-5 w-5 transform rounded-full bg-white shadow ring-0 transition duration-200
							{scimConfig.enabled ? 'translate-x-5' : 'translate-x-0'}"
					/>
				</button>
			</div>

			<!-- actions -->
			<div
				class="border-t border-gray-200 dark:border-gray-700/60 pt-4 flex gap-3 justify-end transition-colors duration-200"
			>
				<button
					type="button"
					disabled={isBusy}
					on:click={() => (isRotateAlertVisible = true)}
					class="bg-slate-400 dark:bg-gray-700/80 hover:bg-slate-300 dark:hover:bg-gray-600/80 text-sm uppercase font-bold px-4 py-2 text-white rounded-md disabled:opacity-50 disabled:cursor-not-allowed transition-colors duration-200"
				>
					Rotate Token
				</button>
				<button
					type="button"
					disabled={isBusy}
					on:click={() => (isDeleteAlertVisible = true)}
					class="bg-red-600 dark:bg-red-700/80 hover:bg-red-500 dark:hover:bg-red-600/80 text-sm uppercase font-bold px-4 py-2 text-white rounded-md disabled:opacity-50 disabled:cursor-not-allowed transition-colors duration-200"
				>
					Delete
				</button>
			</div>
		{/if}
	</div>
</Modal>

<!-- rotate token confirmation -->
<Alert
	headline="Rotate SCIM Token"
	bind:visible={isRotateAlertVisible}
	onConfirm={onConfirmRotateToken}
>
	<p>
		Are you sure you want to rotate the SCIM bearer token for
		<strong>{company?.name}</strong>?
	</p>
	<p class="mt-2 text-sm text-gray-500 dark:text-gray-400">
		the existing token will be immediately invalidated. your identity provider must be updated with
		the new token before provisioning can resume.
	</p>
</Alert>

<!-- delete config confirmation -->
<Alert
	headline="Delete SCIM Config"
	bind:visible={isDeleteAlertVisible}
	onConfirm={onConfirmDelete}
>
	<p>
		Are you sure you want to delete the SCIM configuration for
		<strong>{company?.name}</strong>?
	</p>
	<p class="mt-2 text-sm text-gray-500 dark:text-gray-400">
		the bearer token will be invalidated and future syncs will stop. previously provisioned
		recipients will not be removed.
	</p>
</Alert>
