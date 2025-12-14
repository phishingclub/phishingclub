<script>
	import { page } from '$app/stores';
	import { api } from '$lib/api/apiProxy.js';
	import { onMount } from 'svelte';
	import { newTableURLParams } from '$lib/service/tableURLParams.js';
	import { globalButtonDisabledAttributes } from '$lib/utils/form.js';
	import Headline from '$lib/components/Headline.svelte';
	import TextField from '$lib/components/TextField.svelte';
	import TableRow from '$lib/components/table/TableRow.svelte';
	import TableCell from '$lib/components/table/TableCell.svelte';
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
	import TableCellScope from '$lib/components/table/TableCellScope.svelte';
	import TextareaField from '$lib/components/TextareaField.svelte';

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

		// listen for oauth callback messages from popup window
		const handleMessage = (event) => {
			console.log('received message:', event.data, 'from origin:', event.origin);
			// verify message is from our origin or localhost (for dev with vite proxy)
			const isValidOrigin =
				event.origin === window.location.origin ||
				(window.location.hostname === 'localhost' &&
					new URL(event.origin).hostname === 'localhost');
			if (!isValidOrigin) {
				console.log('message origin does not match, ignoring');
				return;
			}
			// handle oauth callback result
			if (event.data && event.data.type === 'oauth-callback') {
				console.log('oauth callback message received with status:', event.data.status);
				if (event.data.status === 'success') {
					addToast('OAuth authorization successful!', 'Success');
					refreshProviders();
				} else if (event.data.status === 'error') {
					addToast('OAuth authorization failed', 'Error');
				}
			}
		};

		window.addEventListener('message', handleMessage);
		console.log('message listener added for oauth callbacks');

		(async () => {
			const editID = $page.url.searchParams.get('edit');
			if (editID) {
				await openUpdateModal(editID);
			}
		})();

		return () => {
			tableURLParams.unsubscribe();
			window.removeEventListener('message', handleMessage);
		};
	});

	// component logic
	const refreshProviders = async () => {
		try {
			isProviderTableLoading = true;
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

	const openImportModal = () => {
		importTokensText = '';
		importFormError = '';
		isImportModalVisible = true;
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

	const onClickAuthorize = async (id) => {
		try {
			showIsLoading();
			const res = await api.oauthProvider.getAuthorizationURL(id);
			if (res.success && res.data.authorizationURL) {
				// open authorization url in popup window
				const width = 600;
				const height = 700;
				const left = window.screenX + (window.outerWidth - width) / 2;
				const top = window.screenY + (window.outerHeight - height) / 2;
				const popup = window.open(
					res.data.authorizationURL,
					'OAuth Authorization',
					`width=${width},height=${height},left=${left},top=${top},toolbar=no,location=no,status=no,menubar=no,scrollbars=yes,resizable=yes`
				);
				if (!popup) {
					addToast('Failed to open authorization window. Please allow popups.', 'Error');
				}
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
</script>

<HeadTitle title="OAuth" />
<main>
	<Headline>OAuth</Headline>
	<div class="flex gap-2 mb-4">
		<BigButton on:click={openCreateModal}>New OAuth</BigButton>
		<BigButton on:click={openImportModal}>Import Token</BigButton>
	</div>
	<Table
		columns={['Name', 'Status']}
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
				<TableCell>
					<button
						on:click={() => {
							openUpdateModal(provider.id);
						}}
						{...globalButtonDisabledAttributes(provider, contextCompanyID)}
						title={provider.name}
						class="block w-full py-1 text-left"
					>
						{provider.name}
					</button>
				</TableCell>
				<TableCell>
					{#if provider.isAuthorized}
						<span class="text-green-600 dark:text-green-400">Authorized</span>
					{:else}
						<span class="text-yellow-600 dark:text-yellow-400">Not Authorized</span>
					{/if}
				</TableCell>
				<TableCellEmpty />
				<TableCellAction>
					<TableDropDownEllipsis>
						{#if !provider.isImported}
							<TableCopyButton
								title="Copy"
								on:click={() => openCopyModal(provider.id)}
								{...globalButtonDisabledAttributes(provider, contextCompanyID)}
							/>
						{/if}
						<TableUpdateButton
							on:click={() => openUpdateModal(provider.id)}
							{...globalButtonDisabledAttributes(provider, contextCompanyID)}
						/>
						{#if provider.isAuthorized}
							<button
								type="button"
								on:click={() => onClickExport(provider.id)}
								class="w-full px py-1 text-slate-600 dark:text-gray-200 hover:bg-highlight-blue dark:hover:bg-highlight-blue/50 hover:text-white cursor-pointer text-left transition-colors duration-200"
							>
								<p class="ml-2 text-left">Read Token</p>
							</button>
						{/if}
						{#if !provider.isImported}
							{#if !provider.isAuthorized}
								<button
									type="button"
									on:click={() => onClickAuthorize(provider.id)}
									class="w-full px py-1 text-slate-600 dark:text-gray-200 hover:bg-highlight-blue dark:hover:bg-highlight-blue/50 hover:text-white cursor-pointer text-left transition-colors duration-200"
								>
									<p class="ml-2 text-left">Authorize</p>
								</button>
							{:else}
								<button
									type="button"
									on:click={() => onClickAuthorize(provider.id)}
									class="w-full px py-1 text-slate-600 dark:text-gray-200 hover:bg-highlight-blue dark:hover:bg-highlight-blue/50 hover:text-white cursor-pointer text-left transition-colors duration-200"
								>
									<p class="ml-2 text-left">Re-authorize</p>
								</button>
								<button
									type="button"
									on:click={() => openRemoveAuthAlert(provider)}
									class="w-full px py-1 text-slate-600 dark:text-gray-200 hover:bg-highlight-blue dark:hover:bg-highlight-blue/50 hover:text-white cursor-pointer text-left transition-colors duration-200"
								>
									<p class="ml-2 text-left">Remove Authorization</p>
								</button>
							{/if}
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
		<FormGrid on:submit={onClickSubmit} bind:bindTo={form} {isSubmitting}>
			<FormColumns>
				<FormColumn>
					<TextField
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
							bind:value={formValues.clientID}
							placeholder="your-client-id"
						>
							Client ID
						</TextField>
						<PasswordField
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
							bind:value={formValues.authURL}
							placeholder="https://example.com/oauth2/v2/auth"
						>
							Authorization URL
						</TextField>
						<TextField
							required
							minLength={1}
							maxLength={512}
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
							toolTipText="Space-separated list of OAuth scopes">Scopes</TextareaField
						>
					{/if}
				</FormColumn>
			</FormColumns>

			<FormError message={formError} />
			<FormFooter {closeModal} {isSubmitting} />
		</FormGrid>
	</Modal>

	<Modal
		headerText="Import Token"
		visible={isImportModalVisible}
		onClose={closeImportModal}
		{isSubmitting}
	>
		<div class="mt-4 min-w-[800px]">
			<div class="mb-4">
				<p class="text-gray-600 dark:text-gray-400">
					Import a pre-authorized OAuth token. Only <strong>refresh_token</strong> and
					<strong>client_id</strong> are required. The system will automatically refresh to get a valid
					access token and populate metadata.
				</p>
			</div>

			<div class="mb-4">
				<label
					for="import-file-input"
					class="inline-flex items-center px-4 py-2 bg-white dark:bg-gray-700 border border-gray-300 dark:border-gray-600 rounded-md font-medium text-sm text-gray-700 dark:text-gray-300 hover:bg-gray-50 dark:hover:bg-gray-600 cursor-pointer transition-colors"
				>
					<svg class="w-4 h-4 mr-2" fill="none" stroke="currentColor" viewBox="0 0 24 24">
						<path
							stroke-linecap="round"
							stroke-linejoin="round"
							stroke-width="2"
							d="M4 16v1a3 3 0 003 3h10a3 3 0 003-3v-1m-4-8l-4-4m0 0L8 8m4-4v12"
						></path>
					</svg>
					Load from file
				</label>
				<input
					id="import-file-input"
					type="file"
					accept=".json"
					on:change={onSetImportFile}
					class="hidden"
				/>
			</div>

			<div class="bg-gray-50 dark:bg-gray-700 p-4 rounded-md mb-4">
				<button
					type="button"
					class="text-blue-600 dark:text-white hover:text-blue-800 dark:hover:text-gray-300 font-medium inline-flex items-center gap-2"
					on:click={() => {
						navigator.clipboard.writeText(`[
  {
    "refresh_token": "1.AXkAwC...",
    "client_id": "1fec8e78-bce4-4aaf-ab1b-5451cc387264"
  }
]`);
						addToast('Copied minimal format example to clipboard', 'Success');
					}}
				>
					<svg class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
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

			<div class="mb-4">
				<label
					for="import-token-textarea"
					class="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-2"
				>
					Token JSON
				</label>
				<textarea
					id="import-token-textarea"
					bind:value={importTokensText}
					required
					placeholder={`[
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
]`}
					class="w-full h-96 px-3 py-2 text-sm font-mono bg-white dark:bg-gray-800 border border-gray-300 dark:border-gray-600 rounded-md focus:ring-2 focus:ring-blue-500 focus:border-blue-500 dark:text-gray-200 resize-none"
				></textarea>
			</div>
		</div>
		<FormGrid on:submit={onClickImport} {isSubmitting}>
			<FormColumns>
				<FormColumn>
					<!-- Empty form column for structure -->
				</FormColumn>
			</FormColumns>

			<FormError message={importFormError} />
			<FormFooter closeModal={closeImportModal} {isSubmitting} />
		</FormGrid>
	</Modal>

	<Modal headerText="Export Token" visible={isExportModalVisible} onClose={closeExportModal}>
		<div class="mt-4 min-w-[800px]">
			<!-- Expiration Info Section -->
			<div class="mb-4">
				<h3 class="text-xl font-semibold text-gray-700 dark:text-gray-300">Token Information</h3>
				<p class="text-gray-600 dark:text-gray-400 mt-2">
					<span class="font-medium">Expires at:</span>
					<span class="text-pc-darkblue dark:text-white font-semibold ml-2"
						>{exportTokenExpiry}</span
					>
				</p>
			</div>

			<!-- Token JSON Section -->
			<div class="bg-gray-50 dark:bg-gray-700 p-4 rounded-md">
				<div class="mb-3">
					<label
						for="export-token-textarea"
						class="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-2"
					>
						Token JSON
					</label>
					<textarea
						id="export-token-textarea"
						readonly
						value={exportTokensText}
						class="w-full h-96 px-3 py-2 text-sm font-mono bg-white dark:bg-gray-800 border border-gray-300 dark:border-gray-600 rounded-md focus:ring-2 focus:ring-blue-500 focus:border-blue-500 dark:text-gray-200 resize-none"
					/>
				</div>
				<button
					type="button"
					class="text-blue-600 dark:text-white hover:text-blue-800 dark:hover:text-gray-300 font-medium inline-flex items-center gap-2"
					on:click={onClickCopyExport}
				>
					<svg class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
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
		</div>
		<FormGrid on:submit={closeExportModal}>
			<FormColumns>
				<FormColumn>
					<!-- Empty form column for structure -->
				</FormColumn>
			</FormColumns>
			<div
				class="py-4 row-span-2 col-start-1 col-span-3 border-t-2 border-gray-200 dark:border-gray-700 w-full flex flex-row justify-center items-center sm:justify-center md:justify-center lg:justify-end xl:justify-end 2xl:justify-end bg-white dark:bg-gray-800 text-gray-900 dark:text-gray-100 transition-colors duration-200"
			>
				<button
					type="button"
					on:click={closeExportModal}
					class="bg-blue-600 hover:bg-blue-500 dark:bg-blue-500 dark:hover:bg-blue-400 text-sm uppercase font-bold px-4 py-2 text-white rounded-md transition-colors duration-200"
				>
					Close
				</button>
			</div>
		</FormGrid>
	</Modal>

	<DeleteAlert
		name={deleteValues.name}
		onClick={() => onClickDelete(deleteValues.id)}
		bind:isVisible={isDeleteAlertVisible}
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
