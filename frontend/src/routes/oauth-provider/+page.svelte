<script>
	import { page } from '$app/stores';
	import { api } from '$lib/api/apiProxy.js';
	import { onMount, tick } from 'svelte';
	import { newTableURLParams } from '$lib/service/tableURLParams.js';
	import { safeExternalHref } from '$lib/utils/url.js';
	import { globalButtonDisabledAttributes } from '$lib/utils/form.js';
	import Headline from '$lib/components/Headline.svelte';
	import TextField from '$lib/components/TextField.svelte';
	import TableRow from '$lib/components/table/TableRow.svelte';
	import TableCell from '$lib/components/table/TableCell.svelte';
	import StatusBadge from '$lib/components/StatusBadge.svelte';
	import TableUpdateButton from '$lib/components/table/TableUpdateButton.svelte';
	import TableDeleteButton from '$lib/components/table/TableDeleteButton2.svelte';
	import FormError from '$lib/components/FormError.svelte';
	import { addToast } from '$lib/store/toast';
	import PasswordField from '$lib/components/PasswordField.svelte';
	import { AppStateService } from '$lib/service/appState';
	import TableCellAction from '$lib/components/table/TableCellAction.svelte';
	import TableCellEmpty from '$lib/components/table/TableCellEmpty.svelte';
	import FormGrid from '$lib/components/FormGrid.svelte';
	import Modal from '$lib/components/Modal.svelte';
	import InstalledResourceBadge from '$lib/components/InstalledResourceBadge.svelte';
	import BigButton from '$lib/components/BigButton.svelte';
	import FormColumns from '$lib/components/FormColumns.svelte';
	import FormColumn from '$lib/components/FormColumn.svelte';
	import FormFooter from '$lib/components/FormFooter.svelte';
	import Table from '$lib/components/table/Table.svelte';
	import HeadTitle from '$lib/components/HeadTitle.svelte';
	import { getModalText } from '$lib/utils/common';
	import TableCopyButton from '$lib/components/table/TableCopyButton.svelte';
	import { showIsLoading, hideIsLoading } from '$lib/store/loading.js';
	import TableDropDownEllipsis from '$lib/components/table/TableDropDownEllipsis.svelte';
	import DeleteAlert from '$lib/components/modal/DeleteAlert.svelte';
	import SelectSquare from '$lib/components/SelectSquare.svelte';
	import TableCellCheckbox from '$lib/components/table/TableCellCheckbox.svelte';
	import BulkActionBar from '$lib/components/table/BulkActionBar.svelte';
	import {
		createTableSelection,
		headerSelectionState,
		runBulkDelete
	} from '$lib/service/tableSelection.js';
	import TableCellScope from '$lib/components/table/TableCellScope.svelte';
	import TextareaField from '$lib/components/TextareaField.svelte';
	import TableDropDownButton from '$lib/components/table/TableDropDownButton.svelte';
	import FieldLabel from '$lib/components/form/FieldLabel.svelte';
	import FormButton from '$lib/components/FormButton.svelte';

	const secondaryButtonClass =
		'pc-focus inline-flex items-center justify-center gap-2 px-4 py-2 rounded-md border border-gray-300 dark:border-gray-500 bg-white dark:bg-gray-700 text-sm font-medium text-slate-700 dark:text-gray-100 hover:bg-gray-50 dark:hover:bg-gray-600 cursor-pointer transition-colors duration-200';
	const tokenTextareaClass =
		'w-full h-72 sm:h-96 px-3 py-2 text-sm font-mono rounded-md border bg-grayblue-light dark:bg-gray-900/60 text-slate-800 dark:text-gray-300 border-transparent dark:border-gray-700/60 focus:outline-none focus:ring-2 focus:border-cta-blue focus:ring-cta-blue/30 dark:focus:border-highlight-blue dark:focus:ring-highlight-blue/30 resize-y transition-colors duration-200';

	// services
	const appStateService = AppStateService.instance;

	// data
	let form = null;
	let formValues = {
		id: null,
		name: null,

		clientID: null,
		clientSecret: null,
		authURL: null,
		tokenURL: null,
		scopes: null
	};
	let providers = [];
	let providersHasNextPage = false;

	// multi select
	const selection = createTableSelection();
	let isBulkDeleteAlertVisible = false;
	const isRowDisabled = (p) => globalButtonDisabledAttributes(p, contextCompanyID).disabled;
	$: selectablePageIds = providers
		.filter((p) => !globalButtonDisabledAttributes(p, contextCompanyID).disabled)
		.map((p) => p.id);
	$: headerState = headerSelectionState($selection, selectablePageIds);
	$: showMultiSelect = providers.length > 0;
	const onClickBulkDelete = async () => {
		await runBulkDelete({
			ids: [...$selection],
			deleteFn: api.oauthProvider.delete,
			noun: 'OAuth provider'
		});
		await refreshProviders();
		return { success: true };
	};
	let formError = '';
	let contextCompanyID = null;
	const tableURLParams = newTableURLParams();
	let isModalVisible = false;
	let isProviderTableLoading = false;
	let isSubmitting = false;
	let modalMode = null;
	let modalText = '';
	let isDeleteAlertVisible = false;
	let deleteValues = {
		id: null,
		name: null
	};
	let isRemoveAuthAlertVisible = false;
	let removeAuthValues = {
		id: null,
		name: null
	};
	let isImportModalVisible = false;
	let importTokensText = '';
	let importFormError = '';
	let isExportModalVisible = false;
	let exportTokensText = '';
	let exportTokenExpiry = '';

	$: {
		modalText = getModalText('OAuth', modalMode);
	}

	// hooks
	onMount(() => {
		if (appStateService.getContext()) {
			contextCompanyID = appStateService.getContext().companyID;
		}
		refreshProviders();
		tableURLParams.onChange(refreshProviders);

		// the callback page posts its result on a same origin BroadcastChannel,
		// since the popup is opened without an opener
		if (typeof BroadcastChannel !== 'undefined') {
			oauthChannel = new BroadcastChannel(OAUTH_CALLBACK_CHANNEL);
			oauthChannel.onmessage = (event) => {
				if (!event.data || event.data.type !== 'oauth-callback') {
					return;
				}
				// only act on the flow this page started
				if (
					!pendingAuthorization ||
					event.data.providerID !== pendingAuthorization.providerID
				) {
					return;
				}
				stopAuthorizationPolling();
				if (event.data.status === 'success') {
					addToast('OAuth authorization successful!', 'Success');
					refreshProviders();
				} else if (event.data.status === 'error') {
					addToast('OAuth authorization failed', 'Error');
				}
			};
		}

		(async () => {
			const editID = $page.url.searchParams.get('edit');
			if (editID) {
				await openUpdateModal(editID);
			}
		})();

		return () => {
			tableURLParams.unsubscribe();
			stopAuthorizationPolling();
			oauthChannel?.close();
			oauthChannel = null;
		};
	});

	// component logic
	const refreshProviders = async () => {
		try {
			isProviderTableLoading = true;
			selection.clear();
			const data = await getProviders();
			providers = data.rows;
			providersHasNextPage = data.hasNextPage;
		} catch (e) {
			addToast('Failed to get OAuth providers', 'Error');
			console.error(e);
		} finally {
			isProviderTableLoading = false;
		}
	};

	/**
	 * Gets a provider by ID
	 * @param {string} id
	 */
	const getProvider = async (id) => {
		try {
			showIsLoading();
			const res = await api.oauthProvider.getByID(id);
			if (res.success) {
				return res.data;
			} else {
				throw res.error;
			}
		} catch (e) {
			addToast('Failed to get OAuth provider', 'Error');
			console.error('failed to get OAuth provider', e);
		} finally {
			hideIsLoading();
		}
	};

	const getProviders = async () => {
		try {
			const res = await api.oauthProvider.getAll(tableURLParams, contextCompanyID);
			if (!res.success) {
				throw res.error;
			}
			return res.data;
		} catch (e) {
			addToast('Failed to get OAuth providers', 'Error');
			console.error('failed to get OAuth providers', e);
		}
		return [];
	};

	const onClickSubmit = async () => {
		try {
			isSubmitting = true;
			if (modalMode === 'create' || modalMode === 'copy') {
				await create();
				return;
			} else {
				await update();
				return;
			}
		} finally {
			isSubmitting = false;
		}
	};

	const create = async () => {
		formError = '';
		try {
			const res = await api.oauthProvider.create({
				name: formValues.name,
				clientID: formValues.clientID,
				clientSecret: formValues.clientSecret,
				authURL: formValues.authURL,
				tokenURL: formValues.tokenURL,
				scopes: formValues.scopes,
				companyID: contextCompanyID
			});
			if (!res.success) {
				formError = res.error;
				return;
			}
			addToast('Created OAuth provider', 'Success');
			closeModal();
		} catch (err) {
			addToast('Failed to create OAuth provider', 'Error');
			console.error('failed to create OAuth provider:', err);
		}
		refreshProviders();
	};

	const update = async () => {
		formError = '';
		try {
			const res = await api.oauthProvider.update({
				id: formValues.id,
				name: formValues.name,
				clientID: formValues.clientID,
				clientSecret: formValues.clientSecret,
				authURL: formValues.authURL,
				tokenURL: formValues.tokenURL,
				scopes: formValues.scopes,
				companyID: formValues.companyID
			});
			if (res.success) {
				addToast('Updated OAuth provider', 'Success');
				closeModal();
			} else {
				formError = res.error;
			}
		} catch (e) {
			addToast('Failed to update OAuth provider', 'Error');
			console.error('failed to update OAuth provider', e);
		}
		refreshProviders();
	};

	const openDeleteAlert = async (provider) => {
		isDeleteAlertVisible = true;
		deleteValues.id = provider.id;
		deleteValues.name = provider.name;
	};

	/**
	 * Deletes an OAuth provider
	 * @param {string} id
	 */
	const onClickDelete = async (id) => {
		const action = api.oauthProvider.delete(id);

		action
			.then((res) => {
				if (res.success) {
					refreshProviders();
					return;
				}
				throw res.error;
			})
			.catch((e) => {
				console.error('failed to delete oauth provider', e);
			});
		return action;
	};

	const openCreateModal = () => {
		modalMode = 'create';
		formError = '';
		formValues = {
			id: null,
			name: null,
			clientID: null,
			clientSecret: null,
			authURL: null,
			tokenURL: null,
			scopes: null
		};
		isModalVisible = true;
	};

	const openImportModal = async () => {
		importTokensText = '';
		importFormError = '';
		isImportModalVisible = true;
		// start on the JSON field, not the hidden file input behind "Load from file"
		await tick();
		document.getElementById('import-token-textarea')?.focus();
	};

	const closeImportModal = () => {
		isImportModalVisible = false;
		importTokensText = '';
		importFormError = '';
	};

	const onSetImportFile = (event) => {
		// read file from event
		const file = event.target.files[0];
		if (!file) return;

		const reader = new FileReader();
		reader.onload = (e) => {
			importTokensText = /** @type {string} */ (e.target.result);
		};
		reader.readAsText(file);
		// reset field
		event.target.value = '';
	};

	const onClickImport = async () => {
		importFormError = '';
		try {
			isSubmitting = true;
			const tokens = JSON.parse(importTokensText);
			if (!Array.isArray(tokens)) {
				importFormError = 'Input must be an array of tokens';
				return;
			}
			const res = await api.oauthProvider.importTokens(tokens);
			if (!res.success) {
				importFormError = res.error;
				return;
			}
			addToast(`Imported ${res.data.count} OAuth token(s)`, 'Success');
			closeImportModal();
			refreshProviders();
		} catch (err) {
			if (err instanceof SyntaxError) {
				importFormError = 'Invalid JSON format';
			} else {
				importFormError = 'Failed to import tokens';
				console.error('failed to import tokens:', err);
			}
		} finally {
			isSubmitting = false;
		}
	};

	const onClickExport = async (id) => {
		try {
			showIsLoading();
			const res = await api.oauthProvider.exportTokens(id);
			if (res.success) {
				// format as array for consistency with import format
				exportTokensText = JSON.stringify([res.data], null, 2);

				// calculate expiry date
				if (res.data.expires_at) {
					const expiryDate = new Date(res.data.expires_at);
					exportTokenExpiry = expiryDate.toLocaleString();
				} else {
					exportTokenExpiry = 'Unknown';
				}

				isExportModalVisible = true;
			} else {
				throw res.error;
			}
		} catch (e) {
			addToast('Failed to export OAuth token', 'Error');
			console.error('failed to export oauth token', e);
		} finally {
			hideIsLoading();
		}
	};

	const closeExportModal = () => {
		isExportModalVisible = false;
		exportTokensText = '';
		exportTokenExpiry = '';
	};

	const onClickCopyExport = () => {
		navigator.clipboard
			.writeText(exportTokensText)
			.then(() => {
				addToast('Copied to clipboard', 'Success');
			})
			.catch(() => {
				addToast('Failed to copy to clipboard', 'Error');
			});
	};

	const openUpdateModal = async (id) => {
		modalMode = 'update';
		formError = '';
		const provider = await getProvider(id);
		if (!provider) {
			return;
		}
		formValues = {
			id: provider.id,
			name: provider.name,
			clientID: provider.clientID,
			clientSecret: provider.clientSecret,
			authURL: provider.authURL,
			tokenURL: provider.tokenURL,
			scopes: provider.scopes,
			companyID: provider.companyID,
			isImported: provider.isImported
		};
		isModalVisible = true;
	};

	const openCopyModal = async (id) => {
		modalMode = 'copy';
		formError = '';
		const provider = await getProvider(id);
		if (!provider) {
			return;
		}
		formValues = {
			id: null,
			name: provider.name + ' (copy)',
			clientID: provider.clientID,
			clientSecret: provider.clientSecret,
			authURL: provider.authURL,
			tokenURL: provider.tokenURL,
			scopes: provider.scopes
		};
		isModalVisible = true;
	};

	const openRemoveAuthAlert = async (provider) => {
		isRemoveAuthAlertVisible = true;
		removeAuthValues.id = provider.id;
		removeAuthValues.name = provider.name;
	};

	/**
	 * Removes authorization from an OAuth provider
	 * @param {string} id
	 */
	const onClickRemoveAuth = async (id) => {
		const action = api.oauthProvider.removeAuthorization(id);

		action
			.then((res) => {
				if (res.success) {
					addToast('Removed authorization from OAuth provider', 'Success');
					refreshProviders();
					return;
				}
				throw res.error;
			})
			.catch((e) => {
				addToast('Failed to remove authorization', 'Error');
				console.error('failed to remove authorization from oauth provider', e);
			});
		return action;
	};

	const closeModal = () => {
		isModalVisible = false;
		form?.reset();
	};

	// name shared with the backend oauth callback page
	const OAUTH_CALLBACK_CHANNEL = 'phishingclub-oauth-callback';
	const AUTHORIZATION_POLL_INTERVAL_MS = 2000;
	const AUTHORIZATION_POLL_TIMEOUT_MS = 5 * 60 * 1000;
	/** @type {BroadcastChannel|null} */
	let oauthChannel = null;
	/** @type {ReturnType<typeof setInterval>|null} */
	let authorizationPollTimer = null;
	/** @type {{providerID: string, state: string}|null} */
	let pendingAuthorization = null;

	const stopAuthorizationPolling = () => {
		if (authorizationPollTimer) {
			clearInterval(authorizationPollTimer);
			authorizationPollTimer = null;
		}
		pendingAuthorization = null;
	};

	// Poll the flow status until it is no longer pending. This is how the page
	// learns the result when the callback is served from another origin than
	// this page, which a BroadcastChannel can not cross.
	const startAuthorizationPolling = (id, state) => {
		stopAuthorizationPolling();
		pendingAuthorization = { providerID: id, state: state };
		const startedAt = Date.now();
		let inFlight = false;
		authorizationPollTimer = setInterval(async () => {
			if (Date.now() - startedAt > AUTHORIZATION_POLL_TIMEOUT_MS) {
				stopAuthorizationPolling();
				addToast('OAuth authorization did not complete in time', 'Error');
				return;
			}
			if (inFlight) {
				return;
			}
			inFlight = true;
			try {
				const res = await api.oauthProvider.getAuthorizationStatus(id, state);
				if (!res.success) {
					return;
				}
				const status = res.data?.status;
				if (status === 'success') {
					stopAuthorizationPolling();
					addToast('OAuth authorization successful!', 'Success');
					refreshProviders();
				} else if (status === 'failed') {
					stopAuthorizationPolling();
					addToast('OAuth authorization failed', 'Error');
				} else if (status === 'expired') {
					stopAuthorizationPolling();
					addToast('OAuth authorization expired, please try again', 'Error');
				}
			} catch (e) {
				console.error('failed to poll oauth authorization status', e);
			} finally {
				inFlight = false;
			}
		}, AUTHORIZATION_POLL_INTERVAL_MS);
	};

	const onClickAuthorize = async (id) => {
		try {
			showIsLoading();
			const res = await api.oauthProvider.getAuthorizationURL(id);
			if (res.success && res.data.authorizationURL) {
				// the url comes from the provider discovery document, so only open
				// it when it is http or https, never a javascript or data scheme
				const authURL = safeExternalHref(res.data.authorizationURL);
				if (!authURL) {
					addToast('Provider returned an invalid authorization URL', 'Error');
					return;
				}
				// open authorization url in popup window
				const width = 600;
				const height = 700;
				const left = window.screenX + (window.outerWidth - width) / 2;
				const top = window.screenY + (window.outerHeight - height) / 2;
				// the authorization page belongs to the provider, so it gets no
				// opener and can not navigate or message this admin window. With
				// noopener window.open returns null, so a blocked popup can not be
				// detected here.
				let state = '';
				try {
					state = new URL(authURL).searchParams.get('state') ?? '';
				} catch (e) {
					state = '';
				}
				if (!state) {
					addToast('Provider returned an invalid authorization URL', 'Error');
					return;
				}
				window.open(
					authURL,
					'_blank',
					`noopener,noreferrer,popup,width=${width},height=${height},left=${left},top=${top}`
				);
				startAuthorizationPolling(id, state);
			} else {
				throw res.error || 'No authorization URL returned';
			}
		} catch (e) {
			addToast('Failed to get authorization URL', 'Error');
			console.error('failed to get authorization URL', e);
		} finally {
			hideIsLoading();
		}
	};

	// shown as the import placeholder and copied by "Copy format example"
	const tokenFormatExample = `[
  {
    "refresh_token": "1.AXkAwC...",
    "client_id": "1fec8e78-bce4-4aaf-ab1b-5451cc387264",
    "name": "optional: auto-generated if omitted",
    "token_url": "optional: defaults to Microsoft",
    "user": "optional: for display only",
    "scope": "optional: populated from refresh",
    "access_token": "optional: refreshed automatically",
    "expires_at": 0
  }
]`;
</script>

<HeadTitle title="OAuth" />
<main>
	<Headline docSlug="oauth-providers">OAuth</Headline>
	<div class="flex flex-wrap gap-2 sm:gap-3 mt-6 mb-10 [&>button]:m-0">
		<BigButton on:click={openCreateModal}>New OAuth</BigButton>
		<BigButton variant="secondary" on:click={openImportModal}>Import Token</BigButton>
	</div>
	<BulkActionBar
		count={$selection.size}
		noun="OAuth provider"
		on:delete={() => (isBulkDeleteAlertVisible = true)}
		on:clear={() => selection.clear()}
	/>
	<Table
		selectable
		{headerState}
		on:toggleAll={(e) => selection.setPageSelection(selectablePageIds, e.detail)}
		columns={[
			{ column: 'Name', size: 'large' },
			{ column: 'Status', size: 'medium' }
		]}
		sortable={['name', 'is_authorized']}
		pagination={tableURLParams}
		hasData={isProviderTableLoading || providers.length > 0}
		hasNextPage={providersHasNextPage}
		plural="OAuth providers"
		isGhost={isProviderTableLoading}
		hasActions
	>
		{#each providers as provider}
			<TableRow>
				{#if showMultiSelect}
					<TableCellCheckbox
						checked={$selection.has(provider.id)}
						disabled={isRowDisabled(provider)}
						on:change={() => selection.toggle(provider.id)}
					/>
				{/if}
				<TableCell wrap>
					<button
						on:click={() => {
							openUpdateModal(provider.id);
						}}
						{...globalButtonDisabledAttributes(provider, contextCompanyID)}
						title={provider.name}
						class="block w-full py-1 text-left"
					>
						<span class="block truncate">{provider.name}</span>
						<span class="block sm:hidden mt-1">
							<StatusBadge tone={provider.isAuthorized ? 'success' : 'warning'}>
								{provider.isAuthorized ? 'Authorized' : 'Not Authorized'}
							</StatusBadge>
						</span>
					</button>
				</TableCell>
				<TableCell>
					<div>
						{#if provider.isAuthorized}
							<StatusBadge tone="success">Authorized</StatusBadge>
						{:else}
							<StatusBadge tone="warning">Not Authorized</StatusBadge>
						{/if}
					</div>
				</TableCell>
				<TableCellEmpty />
				<TableCellAction>
					<TableDropDownEllipsis>
						<TableUpdateButton
							on:click={() => openUpdateModal(provider.id)}
							{...globalButtonDisabledAttributes(provider, contextCompanyID)}
						/>
						{#if !provider.isImported}
							<TableCopyButton
								title="Copy"
								on:click={() => openCopyModal(provider.id)}
								{...globalButtonDisabledAttributes(provider, contextCompanyID)}
							/>
							{#if !provider.isAuthorized}
								<TableDropDownButton name="Authorize" on:click={() => onClickAuthorize(provider.id)} />
							{:else}
								<TableDropDownButton name="Authorize again" on:click={() => onClickAuthorize(provider.id)} />
							{/if}
						{/if}
						{#if provider.isAuthorized}
							<TableDropDownButton name="Read Token" on:click={() => onClickExport(provider.id)} />
						{/if}
						{#if !provider.isImported && provider.isAuthorized}
							<TableDropDownButton name="Remove Authorization" on:click={() => openRemoveAuthAlert(provider)} />
						{/if}
						<TableDeleteButton
							on:click={() => openDeleteAlert(provider)}
							{...globalButtonDisabledAttributes(provider, contextCompanyID)}
						/>
					</TableDropDownEllipsis>
				</TableCellAction>
			</TableRow>
		{/each}
	</Table>

	<Modal headerText={modalText} visible={isModalVisible} onClose={closeModal} {isSubmitting}>
		<div class="max-w-full sm:max-w-[40rem]">
			<FormGrid on:submit={onClickSubmit} bind:bindTo={form} {isSubmitting}>
				<FormColumns>
					<FormColumn>
						<div class="flex flex-col w-full sm:w-[28rem]">
							{#if modalMode === 'update'}
								<InstalledResourceBadge resourceType="oauthprovider" resourceId={formValues.id} variant="notice" />
							{/if}
							<TextField
								width="full"
								required
								minLength={1}
								maxLength={127}
								bind:value={formValues.name}
								placeholder="My OAuth Provider"
							>
								Name
							</TextField>
							{#if modalMode === 'update' && formValues.isImported}
								<p class="text-sm text-gray-600 dark:text-gray-400 italic">
									This is an imported provider. Only the name can be edited.
								</p>
							{:else}
								<TextField
									required
									minLength={1}
									maxLength={255}
									width="full"
									bind:value={formValues.clientID}
									placeholder="your-client-id"
								>
									Client ID
								</TextField>
								<PasswordField
									width="full"
									required={modalMode === 'create' || modalMode === 'copy'}
									minLength={modalMode === 'update' ? 0 : 1}
									maxLength={255}
									bind:value={formValues.clientSecret}
									placeholder={modalMode === 'update'
										? 'Leave empty to keep existing secret'
										: 'your-client-secret'}
								>
									Client Secret
								</PasswordField>
								<TextField
									required
									minLength={1}
									maxLength={512}
									width="full"
									bind:value={formValues.authURL}
									placeholder="https://example.com/oauth2/v2/auth"
								>
									Authorization URL
								</TextField>
								<TextField
									required
									minLength={1}
									maxLength={512}
									width="full"
									bind:value={formValues.tokenURL}
									placeholder="https://example.com/oauth2/token"
								>
									Token URL
								</TextField>
								<TextareaField
									required
									minLength={1}
									maxLength={512}
									bind:value={formValues.scopes}
									placeholder="https://example.com/auth/mail.send"
									height="small"
									fullWidth
									toolTipText="OAuth scopes separated by spaces">Scopes</TextareaField
								>
							{/if}
						</div>
					</FormColumn>
				</FormColumns>

				<FormError message={formError} />
				<FormFooter {closeModal} {isSubmitting} />
			</FormGrid>
		</div>
	</Modal>

	<Modal
		headerText="Import token"
		visible={isImportModalVisible}
		onClose={closeImportModal}
		{isSubmitting}
		noAutoFocus
	>
		<FormGrid on:submit={onClickImport} {isSubmitting}>
			<FormColumns>
				<FormColumn stretch>
					<div class="w-full md:w-[48rem] max-w-full">
						<div class="mb-2 flex flex-wrap gap-2">
							<input
								id="import-file-input"
								type="file"
								accept=".json"
								on:change={onSetImportFile}
								class="peer sr-only"
							/>
							<label
								for="import-file-input"
								class="{secondaryButtonClass} peer-focus-visible:ring-2 peer-focus-visible:ring-cta-blue"
							>
								<svg class="w-4 h-4" aria-hidden="true" fill="none" stroke="currentColor" viewBox="0 0 24 24">
									<path
										stroke-linecap="round"
										stroke-linejoin="round"
										stroke-width="2"
										d="M4 16v1a3 3 0 003 3h10a3 3 0 003-3v-1m-4-8l-4-4m0 0L8 8m4-4v12"
									></path>
								</svg>
								Load from file
							</label>
							<button
								type="button"
								class={secondaryButtonClass}
								on:click={() => {
									navigator.clipboard.writeText(tokenFormatExample);
									addToast('Copied format example to clipboard', 'Success');
								}}
							>
								<svg class="w-4 h-4" aria-hidden="true" fill="none" stroke="currentColor" viewBox="0 0 24 24">
									<path
										stroke-linecap="round"
										stroke-linejoin="round"
										stroke-width="2"
										d="M8 16H6a2 2 0 01-2-2V6a2 2 0 012-2h8a2 2 0 012 2v2m-6 12h8a2 2 0 002-2v-8a2 2 0 00-2-2h-8a2 2 0 00-2 2v8a2 2 0 002 2z"
									></path>
								</svg>
								Copy format example
							</button>
						</div>
						<label for="import-token-textarea" class="block">
							<FieldLabel>Token JSON</FieldLabel>
						</label>
						<textarea
							id="import-token-textarea"
							bind:value={importTokensText}
							required
							placeholder={tokenFormatExample}
							wrap={importTokensText ? 'off' : 'soft'}
							class={tokenTextareaClass}
						></textarea>
					</div>
				</FormColumn>
			</FormColumns>

			<FormError message={importFormError} />
			<FormFooter closeModal={closeImportModal} {isSubmitting} okText="Import" />
		</FormGrid>
	</Modal>

	<Modal headerText="Export token" visible={isExportModalVisible} onClose={closeExportModal}>
		<FormGrid on:submit={closeExportModal}>
			<div class="w-full md:w-[48rem] max-w-full pt-4">
				<p class="text-gray-600 dark:text-gray-400">
					<span class="font-semibold text-slate-600 dark:text-gray-400">Expires at:</span>
					<span class="text-pc-darkblue dark:text-white font-semibold ml-2">{exportTokenExpiry}</span>
				</p>
				<div class="mt-2 flex items-end justify-between gap-2">
					<label for="export-token-textarea" class="block">
						<FieldLabel>Token JSON</FieldLabel>
					</label>
					<button type="button" class="{secondaryButtonClass} mb-2" on:click={onClickCopyExport}>
						<svg class="w-4 h-4" aria-hidden="true" fill="none" stroke="currentColor" viewBox="0 0 24 24">
							<path
								stroke-linecap="round"
								stroke-linejoin="round"
								stroke-width="2"
								d="M8 16H6a2 2 0 01-2-2V6a2 2 0 012-2h8a2 2 0 012 2v2m-6 12h8a2 2 0 002-2v-8a2 2 0 00-2-2h-8a2 2 0 00-2 2v8a2 2 0 002 2z"
							></path>
						</svg>
						Copy to clipboard
					</button>
				</div>
				<textarea
					id="export-token-textarea"
					readonly
					value={exportTokensText}
					wrap="off"
					class={tokenTextareaClass}
				/>
			</div>
			<div
				class="form-footer py-4 border-t-2 border-gray-200 dark:border-gray-700 w-full flex justify-end bg-white dark:bg-gray-800 transition-colors duration-200"
			>
				<FormButton type="button" on:click={closeExportModal}>Close</FormButton>
			</div>
		</FormGrid>
	</Modal>

	<DeleteAlert
		name={deleteValues.name}
		onClick={() => onClickDelete(deleteValues.id)}
		bind:isVisible={isDeleteAlertVisible}
	/>
	<DeleteAlert
		title="Delete OAuth providers"
		name={`${$selection.size} OAuth provider${$selection.size === 1 ? '' : 's'}`}
		onClick={onClickBulkDelete}
		confirm
		bind:isVisible={isBulkDeleteAlertVisible}
	/>

	<DeleteAlert
		name={removeAuthValues.name}
		onClick={() => onClickRemoveAuth(removeAuthValues.id)}
		bind:isVisible={isRemoveAuthAlertVisible}
		title="Remove Authorization"
		actionMessage="Are you sure you want to remove authorization from"
		list={[
			'Access and refresh tokens will be deleted',
			'You will need to re-authorize to use this provider for sending'
		]}
		permanent={false}
	/>
</main>
