<script>
	import { onDestroy, createEventDispatcher } from 'svelte';
	import { addToast } from '$lib/store/toast';
	import Modal from '../Modal.svelte';
	import Alert from '../Alert.svelte';
	import TextField from '../TextField.svelte';
	import TextFieldSelect from '../TextFieldSelect.svelte';
	import FormButton from '../FormButton.svelte';
	import Button from '../Button.svelte';
	import FormError from '../FormError.svelte';
	import { api } from '$lib/api/apiProxy.js';
	import { fetchAllRows } from '$lib/utils/api-utils.js';
	import ReportInboxStatus from '../company/ReportInboxStatus.svelte';
	import StatusBadge from '../StatusBadge.svelte';

	// change fires with the saved inbox, or null after delete, so cards can update their status
	const dispatch = createEventDispatcher();

	// external
	export let visible = false;
	/** @type {{ id: string, name: string } | null} */
	export let company = null;
	// when true the modal edits the global inbox, which only covers global campaigns
	export let isGlobal = false;

	// the scope id passed to the api: a company id, or null for the global inbox
	$: companyId = isGlobal ? null : (company?.id ?? null);
	$: ready = isGlobal || !!company;

	const AUTH_MODE_OPTIONS = [
		{ value: 'app', label: 'App (recommended)' },
		{ value: 'delegated', label: 'OAuth provider' }
	];

	// local state
	let config = null;
	let oauthProviders = [];
	// authorized providers, plus the stored one when it lost its authorization, so
	// the select still shows it and why it fails
	$: oauthProviderOptions = oauthProviders
		.filter((p) => p.isAuthorized || p.id === form.oauthProviderID)
		.map((p) => ({
			value: p.id,
			label: !p.isAuthorized
				? `${p.name} (not authorized)`
				: p.authorizedEmail
					? `${p.name} (${p.authorizedEmail})`
					: p.name
		}));
	let isLoading = false;
	let isSaving = false;
	let isDeleting = false;
	let isTesting = false;
	let isPolling = false;
	let isDeleteAlertVisible = false;
	let formError = '';
	let testResult = null;
	let pollRefreshTimer = null;

	// a new inbox starts switched on so one save activates it
	const emptyForm = () => ({
		enabled: true,
		authMode: 'app',
		mailbox: '',
		tenantID: '',
		clientID: '',
		clientSecret: '',
		oauthProviderID: ''
	});
	let form = emptyForm();

	// reactive: reload when modal opens
	$: {
		if (visible && ready) {
			loadAll();
		}
	}

	// reactive: clean up when modal closes
	$: {
		if (!visible) {
			resetState();
		}
	}

	onDestroy(() => clearTimeout(pollRefreshTimer));

	const resetState = () => {
		config = null;
		form = emptyForm();
		formError = '';
		testResult = null;
		isDeleteAlertVisible = false;
		clearTimeout(pollRefreshTimer);
	};

	const loadAll = async () => {
		isLoading = true;
		try {
			await Promise.all([loadConfig(), loadOAuthProviders()]);
		} finally {
			isLoading = false;
		}
	};

	const applyConfig = (data) => {
		config = data;
		form = {
			...form,
			enabled: !!data.enabled,
			authMode: data.authMode || 'app',
			mailbox: data.mailbox ?? '',
			tenantID: data.tenantID ?? '',
			clientID: data.clientID ?? '',
			clientSecret: '',
			oauthProviderID: data.oauthProviderID ?? ''
		};
	};

	const loadConfig = async () => {
		try {
			const res = await api.company.reportInbox.getByCompanyID(companyId);
			if (res && res.success && res.data) {
				applyConfig(res.data);
			} else if (res && res.statusCode === 404) {
				// no inbox for this scope yet
				config = null;
			} else {
				// an empty form here could overwrite the stored settings on save
				addToast('Failed to load reporting inbox', 'Error');
				visible = false;
				return;
			}
		} catch (e) {
			console.error('failed to load reporting inbox', e);
			addToast('Failed to load reporting inbox', 'Error');
			visible = false;
			return;
		}
		dispatch('change', config);
	};

	// refresh only the status numbers so unsaved edits in the form are kept
	const refreshStatus = async () => {
		try {
			const res = await api.company.reportInbox.getByCompanyID(companyId);
			if (res && res.success && res.data) {
				config = res.data;
				dispatch('change', config);
			}
		} catch (e) {
			console.error('failed to refresh report inbox', e);
		}
	};

	// global providers, plus the company's own in company view
	const loadOAuthProviders = async () => {
		try {
			const providers = await fetchAllRows((options) => {
				return api.oauthProvider.getAll(options, companyId);
			});
			oauthProviders = providers;
		} catch (e) {
			console.error('failed to load oauth providers', e);
			oauthProviders = [];
		}
	};

	// only send the fields that belong to the chosen sign in method
	const payload = () => {
		const isApp = form.authMode === 'app';
		return {
			enabled: form.enabled,
			authMode: form.authMode,
			mailbox: form.mailbox.trim(),
			tenantID: isApp ? form.tenantID.trim() : '',
			clientID: isApp ? form.clientID.trim() : '',
			clientSecret: isApp ? form.clientSecret : '',
			oauthProviderID: isApp ? null : form.oauthProviderID || null
		};
	};

	const onSave = async () => {
		formError = '';
		isSaving = true;
		try {
			const res = await api.company.reportInbox.upsert(companyId, payload());
			if (!res || !res.success) {
				formError = res?.error ?? 'Failed to save reporting inbox';
				return;
			}
			// the save returns the stored inbox, with server set fields like readSince
			if (res.data) {
				applyConfig(res.data);
				dispatch('change', config);
			}
			addToast(
				config?.enabled ? 'Reporting inbox saved and active' : 'Reporting inbox saved, not active',
				'Success'
			);
		} catch (e) {
			console.error('failed to save report inbox', e);
			formError = 'Failed to save reporting inbox';
		} finally {
			isSaving = false;
		}
	};

	const onTest = async () => {
		formError = '';
		testResult = null;
		isTesting = true;
		try {
			const res = await api.company.reportInbox.test(companyId, payload());
			if (!res || !res.success) {
				formError = res?.error ?? 'Failed to run the test';
				return;
			}
			testResult = res.data;
		} catch (e) {
			console.error('failed to test report inbox', e);
			formError = 'Failed to run the test';
		} finally {
			isTesting = false;
		}
	};

	const onPollNow = async () => {
		isPolling = true;
		try {
			const res = await api.company.reportInbox.pollNow(companyId);
			if (!res || !res.success) {
				addToast(res?.error ?? 'Failed to start a check', 'Error');
				return;
			}
			addToast('Check started', 'Success');
			await refreshStatus();
			// the poller picks it up on its next round, refresh again shortly after
			clearTimeout(pollRefreshTimer);
			pollRefreshTimer = setTimeout(() => {
				if (visible) {
					refreshStatus();
				}
			}, 5000);
		} catch (e) {
			console.error('failed to poll report inbox', e);
			addToast('Failed to start a check', 'Error');
		} finally {
			isPolling = false;
		}
	};

	const onConfirmDelete = async () => {
		isDeleting = true;
		try {
			const res = await api.company.reportInbox.delete(companyId);
			if (!res || !res.success) {
				addToast(res?.error ?? 'Failed to delete reporting inbox', 'Error');
				return { success: false };
			}
			resetState();
			dispatch('change', null);
			addToast('Reporting inbox deleted', 'Success');
			return { success: true };
		} catch (e) {
			console.error('failed to delete report inbox', e);
			addToast('Failed to delete reporting inbox', 'Error');
			return { success: false };
		} finally {
			isDeleting = false;
		}
	};


	// same format as the Datetime component
	const formatDate = (dateStr) => (dateStr ? new Date(dateStr).toLocaleString() : 'Never');

	$: isApp = form.authMode === 'app';
	$: isBusy = isSaving || isDeleting || isTesting || isPolling;
	$: isEnabledUnsaved = !!config && form.enabled !== !!config.enabled;
	$: secretPlaceholder = config?.hasClientSecret ? 'Stored, leave empty to keep' : '';

	const secondaryButtonClass =
		'bg-cta-blue px-4 py-2 hover:opacity-80 dark:hover:opacity-90 text-white text-sm font-bold uppercase rounded-md transition-all duration-200 disabled:opacity-50 dark:disabled:opacity-60';
</script>

<Modal
	headerText={isGlobal ? 'Global reporting inbox' : 'Reporting inbox'}
	docSlug="reporting"
	docAnchor="report-inbox"
	bind:visible
>
	<div class="w-[640px] max-w-full py-4 sm:p-6 space-y-6">
		{#if isLoading}
			<div class="flex items-center justify-center py-10">
				<p class="text-gray-500 dark:text-gray-400">Loading...</p>
			</div>
		{:else}
			<p class="text-sm text-gray-500 dark:text-gray-400">
				{#if isGlobal}
					Read the mailbox your users report phishing to and mark matching recipients as reported.
					Only global campaigns are matched. Each company needs its own reporting inbox.
				{:else}
					Read the mailbox your users report phishing to and mark matching recipients as reported.
					Only this company's campaigns are matched.
				{/if}
			</p>

			{#if config}
				<!-- status -->
				<div
					class="rounded-md border border-gray-200 dark:border-gray-700/60 bg-gray-50 dark:bg-gray-800/40 p-4 space-y-3"
				>
					<div class="flex items-center justify-between gap-4">
						<ReportInboxStatus inbox={config} showError={false} />
						<button
							type="button"
							disabled={isBusy || !config.enabled}
							title={config.enabled ? '' : 'Turn on the inbox and save first'}
							on:click={onPollNow}
							class={secondaryButtonClass}
						>
							{isPolling ? 'Checking...' : 'Check now'}
						</button>
					</div>
					{#if config.enabled && config.readSince}
						<p class="text-sm text-gray-500 dark:text-gray-400">
							Reading reports since
							<span class="text-gray-800 dark:text-gray-200">{formatDate(config.readSince)}</span>
						</p>
					{/if}
					<div class="grid grid-cols-2 gap-x-6 gap-y-2 text-sm">
						<div class="flex justify-between gap-2">
							<span class="text-gray-500 dark:text-gray-500">Last checked</span>
							<span class="text-gray-800 dark:text-gray-200">{formatDate(config.lastPolledAt)}</span>
						</div>
						<div class="flex justify-between gap-2">
							<span class="text-gray-500 dark:text-gray-500">Last success</span>
							<span class="text-gray-800 dark:text-gray-200">{formatDate(config.lastSuccessAt)}</span>
						</div>
						<div class="flex justify-between gap-2">
							<span class="text-gray-500 dark:text-gray-500">Processed</span>
							<span class="text-gray-800 dark:text-gray-200">{config.processedCount ?? 0}</span>
						</div>
						<div class="flex justify-between gap-2">
							<span class="text-gray-500 dark:text-gray-500">Matched</span>
							<span class="text-gray-800 dark:text-gray-200">{config.matchedCount ?? 0}</span>
						</div>
					</div>
					{#if config.lastError}
						<div
							class="rounded-md border border-red-300 dark:border-red-500/60 bg-red-50 dark:bg-red-900/20 p-3"
						>
							<p class="text-sm text-red-700 dark:text-red-400 break-words">
								<span class="font-semibold">Last error:</span>
								{config.lastError}
								{#if config.failureCount > 1}
									<span class="text-xs">({config.failureCount} failures in a row)</span>
								{/if}
							</p>
						</div>
					{/if}
				</div>
			{/if}

			<!-- enabled toggle -->
			<div class="flex items-center justify-between">
				<div>
					<p class="text-sm font-medium text-gray-700 dark:text-gray-300">Enabled</p>
					<p class="text-xs text-gray-500 dark:text-gray-400 mt-0.5">
						{#if isEnabledUnsaved}
							Click Save to {form.enabled ? 'turn on' : 'turn off'} the inbox.
						{:else if config}
							Check the mailbox every 2 minutes.
						{:else}
							Check the mailbox every 2 minutes once saved.
						{/if}
					</p>
				</div>
				<button
					type="button"
					role="switch"
					aria-checked={form.enabled}
					aria-label="Enabled"
					disabled={isBusy}
					on:click={() => (form.enabled = !form.enabled)}
					class="relative inline-flex h-6 w-11 flex-shrink-0 cursor-pointer rounded-full border-2 border-transparent transition-colors duration-200 focus:outline-none disabled:opacity-50
						{form.enabled ? 'bg-cta-blue dark:bg-highlight-blue/80' : 'bg-gray-300 dark:bg-gray-600'}"
				>
					<span
						class="pointer-events-none inline-block h-5 w-5 transform rounded-full bg-white shadow transition duration-200
							{form.enabled ? 'translate-x-5' : 'translate-x-0'}"
					/>
				</button>
			</div>

			<TextField
				id="report-inbox-mailbox"
				width="full"
				placeholder="reports@company.com"
				bind:value={form.mailbox}
				toolTipText="The mailbox users report phishing to.">Mailbox</TextField
			>

			<TextFieldSelect
				id="report-inbox-auth-mode"
				bind:value={form.authMode}
				options={AUTH_MODE_OPTIONS}>Sign in method</TextFieldSelect
			>

			{#if isApp}
				<TextField id="report-inbox-tenant" width="full" bind:value={form.tenantID}
					>Tenant ID</TextField
				>
				<TextField id="report-inbox-client" width="full" bind:value={form.clientID}
					>Client ID</TextField
				>
				<TextField
					id="report-inbox-secret"
					type="password"
					width="full"
					placeholder={secretPlaceholder}
					bind:value={form.clientSecret}>Client secret</TextField
				>
			{:else}
				<div>
					<TextFieldSelect
						id="report-inbox-oauth-provider"
						placeholder={oauthProviderOptions.length
							? 'Select an OAuth provider'
							: 'None authorized'}
						bind:value={form.oauthProviderID}
						options={oauthProviderOptions}>OAuth provider</TextFieldSelect
					>
					<p class="text-xs text-gray-500 dark:text-gray-400 mt-1">
						Authorize the provider as an account that can only open the report mailbox. The guide
						lists the scopes to use.
					</p>
				</div>
			{/if}

			<!-- test -->
			<div class="border-t border-gray-200 dark:border-gray-700/60 pt-4 space-y-2">
				<p class="font-semibold text-slate-600 dark:text-gray-400">Test</p>
				<p class="text-xs text-gray-500 dark:text-gray-400">
					Checks the values above against Microsoft 365 without saving them: sign in, read the
					report mailbox, and check that another mailbox on the same domain can not be opened.
				</p>
				<div>
					<button
						type="button"
						disabled={isBusy}
						on:click={onTest}
						class="{secondaryButtonClass} w-36 flex justify-center"
					>
						{isTesting ? 'Testing...' : 'Test'}
					</button>
				</div>

				{#if testResult}
					<div
						class="rounded-md border border-gray-200 dark:border-gray-700/60 bg-gray-50 dark:bg-gray-800/40 p-4 space-y-2"
					>
						<StatusBadge tone={testResult.ok ? 'success' : 'danger'}>
							{testResult.ok ? 'Test passed' : 'Test failed'}
						</StatusBadge>
						<ul class="space-y-2">
							{#each testResult.steps ?? [] as step}
								<li class="flex gap-2 text-sm">
									<span
										class="font-bold w-4 flex-shrink-0 {step.ok
											? 'text-green-700 dark:text-green-400'
											: 'text-red-600 dark:text-red-400'}"
										aria-label={step.ok ? 'passed' : 'failed'}>{step.ok ? '✓' : '✗'}</span
									>
									<div class="min-w-0">
										<p class="text-gray-800 dark:text-gray-200">{step.name}</p>
										{#if step.message}
											<p class="text-xs text-gray-500 dark:text-gray-400 break-words">
												{step.message}
											</p>
										{/if}
									</div>
								</li>
							{/each}
						</ul>
					</div>
				{/if}

				{#if testResult?.warnings?.length}
					<div
						class="rounded-md border border-amber-400 dark:border-amber-500/60 bg-amber-50 dark:bg-amber-900/20 p-4 space-y-1"
					>
						{#each testResult.warnings as warning}
							<p class="text-sm text-amber-700 dark:text-amber-400">{warning}</p>
						{/each}
					</div>
				{/if}
			</div>

			<FormError message={formError} />

			<!-- actions -->
			<div
				class="border-t border-gray-200 dark:border-gray-700/60 pt-4 flex gap-3 justify-end items-center"
			>
				{#if config}
					<Button
						size="medium"
						variant="danger-outline"
						disabled={isBusy}
						on:click={() => (isDeleteAlertVisible = true)}
					>
						Delete
					</Button>
				{/if}
				<FormButton size="medium" isSubmitting={isSaving} disabled={isBusy} on:click={onSave}
					>Save</FormButton
				>
			</div>
		{/if}
	</div>
</Modal>

<Alert
	headline="Delete Reporting Inbox"
	bind:visible={isDeleteAlertVisible}
	onConfirm={onConfirmDelete}
	verification="delete"
>
	<p>
		Are you sure you want to delete the reporting inbox and its stored credentials for
		<strong>{isGlobal ? 'global campaigns' : company?.name}</strong>?
	</p>
</Alert>
