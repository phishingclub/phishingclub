<script>
	import { api } from '$lib/api/apiProxy.js';
	import { immediateResponseHandler } from '$lib/api/middleware.js';
	import Button from '$lib/components/Button.svelte';
	import FileField from '$lib/components/FileField.svelte';
	import Form from '$lib/components/Form.svelte';
	import FormButton from '$lib/components/FormButton.svelte';
	import FormColumn from '$lib/components/FormColumn.svelte';
	import FormColumns from '$lib/components/FormColumns.svelte';
	import FormError from '$lib/components/FormError.svelte';
	import FormFooter from '$lib/components/FormFooter.svelte';
	import FormGrid from '$lib/components/FormGrid.svelte';
	import Headline from '$lib/components/Headline.svelte';
	import HeadTitle from '$lib/components/HeadTitle.svelte';
	import Modal from '$lib/components/Modal.svelte';
	import DeleteAlert from '$lib/components/modal/DeleteAlert.svelte';
	import PasswordField from '$lib/components/PasswordField.svelte';
	import TextField from '$lib/components/TextField.svelte';
	import TextFieldSelect from '$lib/components/TextFieldSelect.svelte';
	import { AppStateService } from '$lib/service/appState';
	import { hideIsLoading, showIsLoading } from '$lib/store/loading';
	import { addToast } from '$lib/store/toast';
	import { onMount } from 'svelte';
	import { onClickCopy } from '$lib/utils/common';
	import SelectSquare from '$lib/components/SelectSquare.svelte';

	// services
	const appStateService = AppStateService.instance;

	const logLevels = ['debug', 'info', 'warn', 'error'];
	const dbLogLevels = ['silent', 'info', 'warn', 'error'];
	// local state
	let logLevel = '';
	let dbLogLevel = '';
	let isInitiallyLoaded = false;
	let updateSettingsError = '';
	let formValues = {
		maxFileSize: null,
		repeatOffenderMonths: null
	};
	let version = '';
	let isSubmitting = false;

	// Update checking variables
	let updateAvailable = false;
	let isCheckingUpdate = false;

	let ssoForm = null;
	let isSSOModalVisible = false;
	let updateSSOError = '';
	let ssoSettingsFormValues = {
		clientID: null,
		tenantID: null,
		redirectURL: null,
		clientSecret: null
	};
	let isSSOEnabled = false;
	let isSSODeleteAlertVisible = false;

	// Import functionality
	let importError = '';
	let isImportSubmitting = false;
	let importFile = null;
	let importResult = null;
	let isImportResultModalVisible = false;

	// Company context for import
	const appState = AppStateService.instance;
	let isCompanyContext = false;
	let importForCompany = false;
	let contextCompanyID = null;

	$: {
		isCompanyContext = appState.isCompanyContext();
		importForCompany = isCompanyContext;
		if (appState.getContext()) {
			contextCompanyID = appState.getContext().companyID;
		} else {
			contextCompanyID = null;
		}
	}

	// hooks
	onMount(() => {
		(async () => {
			showIsLoading();

			await refreshLogLevel();
			await refreshSettings();
			await refreshSSO();
			await refreshVersion();
			await refreshUpdateCached();
			if (!ssoSettingsFormValues.redirectURL) {
				ssoSettingsFormValues.redirectURL = `${location.origin}/api/v1/sso/entra-id/auth`;
			}
			hideIsLoading();
			isInitiallyLoaded = true;
		})();
	});

	// component logic
	async function refreshSettings() {
		try {
			const res = immediateResponseHandler(await api.option.get('max_file_upload_size_mb'));
			if (res.success) {
				formValues.maxFileSize = res.data.value;
			} else {
				throw res.error;
			}
			const resRepeat = immediateResponseHandler(await api.option.get('repeat_offender_months'));
			if (resRepeat.success) {
				formValues.RepeatOffenderMonths = resRepeat.data.value;
			} else {
				throw res.error;
			}
		} catch (err) {
			console.error(err);
		}
	}

	async function refreshLogLevel() {
		try {
			const res = immediateResponseHandler(await api.log.getLevel());
			if (res.success) {
				logLevel = res.data.level;
				dbLogLevel = res.data.dbLevel;
			} else {
				console.error(res);
			}
		} catch (err) {
			console.error(err);
		}
	}

	async function setLogLevel() {
		try {
			const res = await api.log.setLevel(logLevel, dbLogLevel);
			if (res.success) {
				console.log('success');
			} else {
				console.error(res);
			}
		} catch (err) {
			console.error(err);
		}
	}

	async function refreshVersion() {
		try {
			const res = await api.version.get();
			if (!res.success) {
				throw res.error;
			}
			version = res.data;
		} catch (e) {
			console.error('failed to check version', e);
		}
	}

	async function refreshUpdateCached() {
		try {
			const res = await api.application.isUpdateAvailableCached();
			if (!res.success) {
				throw res.error;
			}
			updateAvailable = res.data.updateAvailable;
		} catch (e) {
			console.error('failed to check cached update status', e);
		}
	}

	async function checkForUpdate() {
		isCheckingUpdate = true;
		try {
			const res = await api.application.isUpdateAvailable();
			if (!res.success) {
				throw res.error;
			}
			updateAvailable = res.data.updateAvailable;
			if (updateAvailable) {
				addToast('Update available!', 'Success');
			} else {
				addToast('No updates available', 'Info');
			}
		} catch (e) {
			addToast('Failed to check for updates', 'Error');
			console.error('failed to check for updates', e);
		} finally {
			isCheckingUpdate = false;
		}
	}

	// License functionality removed

	async function refreshSSO() {
		try {
			const res = await api.option.get('sso_login');
			if (!res.success) {
				throw res.error;
			}
			const sso = JSON.parse(res.data.value);
			sso.clientSecret = '';
			ssoSettingsFormValues = sso;
			isSSOEnabled = sso.enabled;
		} catch (e) {
			console.error('failed to get SSO configuration', e);
		}
	}

	async function testLogLevel() {
		try {
			const res = await api.log.testLevels();
			if (res.success) {
				// do nothing
			} else {
				console.error(res);
			}
		} catch (err) {
			console.error(err);
		}
	}

	const onClickUpdateSettings = async () => {
		updateSettingsError = '';
		try {
			const res = await api.option.set('max_file_upload_size_mb', formValues.maxFileSize);
			if (!res.success) {
				updateSettingsError = res.error;
				return;
			}
			const resRepeat = await api.option.set(
				'repeat_offender_months',
				formValues.RepeatOffenderMonths
			);
			if (!resRepeat.success) {
				updateSettingsError = resRepeat.error;
				return;
			}
			addToast('Settings updated', 'Success');
		} catch (e) {
			addToast('Failed to update settings', 'Error');
			console.error('failed to update settings', e);
		}
	};

	// License functionality removed

	const onSubmitSSO = async () => {
		updateSSOError = '';
		isSubmitting = true;
		try {
			const res = await api.sso.upsert(ssoSettingsFormValues);
			if (!res.success) {
				updateSSOError = res.error;
				return;
			}
			closeSSOModal();
			refreshSSO();
		} catch (e) {
			addToast('Failed to update SSO configuration', 'Error');
			console.error('failed to update SSO configuration', e);
		} finally {
			isSubmitting = false;
		}
	};

	// License modal functions removed

	const openSSOModal = async (e) => {
		e.preventDefault();
		/*
		updateSSOError = '';
		showIsLoading();
		try {
			const res = await api.option.get('sso_login');
			if (!res.success) {
				updateSSOError = res.error;
				return;
			}
			const sso = JSON.parse(res.data.value);
			ssoSettingsFormValues = sso;
		} catch (e) {
			addToast('Failed to get SSO options', 'Error');
			console.error('failed to get SSO options', e);
		} finally {
			hideIsLoading();
		}
		*/
		isSSOModalVisible = true;
	};

	const closeSSOModal = () => {
		updateSSOError = '';
		ssoSettingsFormValues = {
			clientID: null,
			tenantID: null,
			redirectURL: null,
			clientSecret: null
		};
		isSSOModalVisible = false;
	};

	const onClickDisableSSO = async () => {
		const action = api.sso.upsert({
			clientID: '',
			tenantID: '',
			redirectURL: '',
			clientSecret: ''
		});
		action
			.then((res) => {
				if (!res.success) {
					throw res.error;
				}
				refreshSSO();
			})
			.catch((e) => {
				console.error('failed to remove SSO configuration:', e);
			});
		return action;
	};

	/**
	 * @param {*} event
	 */
	// License file handling removed

	/**
	 * @param {*} event
	 */
	const onSetImportFile = (event) => {
		importFile = event.target.files[0];
	};

	/**
	 * Submit import file
	 */
	const onSubmitImport = async () => {
		if (!importFile) {
			importError = 'Please select a file to import';
			return;
		}

		isImportSubmitting = true;
		importError = '';
		importResult = null;
		isImportResultModalVisible = false;

		try {
			const formData = new FormData();
			formData.append('file', importFile);
			formData.append('forCompany', importForCompany ? '1' : '0');
			if (importForCompany && contextCompanyID) {
				formData.append('companyID', contextCompanyID);
			}
			const response = await api.import.import(formData);

			if (response.success) {
				addToast('File has been imported successfully', 'Success');
				importFile = null;
				importResult = response.data;
				isImportResultModalVisible = true;
				// Reset file input
				const fileInput = document.querySelector('input[type="file"][name="importFile"]');
				if (fileInput) /** @type {HTMLInputElement} */ (fileInput).value = '';
			} else {
				importError = response.error || 'Import failed';
				importResult = response.data || null;
				isImportResultModalVisible = !!importResult;
			}
		} catch (error) {
			console.error('Import error:', error);
			importError = 'An error occurred during import';
			importResult = null;
			isImportResultModalVisible = false;
		} finally {
			isImportSubmitting = false;
		}
	};
</script>

<HeadTitle title="Profile" />
<main class="pb-8">
	<Headline>Profile</Headline>
	{#if isInitiallyLoaded}
		<div class="max-w-7xl pt-4 space-y-8">
			<!-- Settings Grid -->
			<div class="grid grid-cols-1 lg:grid-cols-2 xl:grid-cols-4 gap-8">
				<!-- SSO Card -->
				<div
					class="bg-white p-6 rounded-lg shadow-sm border border-gray-100 min-h-[300px] flex flex-col"
				>
					<h2 class="text-xl font-semibold text-gray-700 mb-6">Single Sign-On</h2>
					<div class="flex flex-col h-full pt-4">
						<div class="bg-gray-50 rounded-md p-3">
							{#if isSSOEnabled}
								<p class="text-sm font-medium text-green-600">
									<span class="inline-block w-2 h-2 rounded-full bg-green-500 mr-2"></span>
									Enabled
								</p>
							{:else}
								<p class="text-sm text-gray-600">
									<span class="inline-block w-2 h-2 rounded-full bg-gray-400 mr-2"></span>
									Disabled
								</p>
							{/if}
						</div>
						<div class="mt-auto pt-4">
							{#if isSSOEnabled}
								<Button
									size={'large'}
									on:click={() => {
										isSSODeleteAlertVisible = true;
									}}>Disable SSO</Button
								>
							{:else}
								<Button size={'large'} on:click={openSSOModal}>Configure SSO</Button>
							{/if}
						</div>
					</div>
				</div>

				<!-- General Settings Card -->
				<div
					class="bg-white p-6 rounded-lg shadow-sm border border-gray-100 min-h-[300px] flex flex-col"
				>
					<h2 class="text-xl font-semibold text-gray-700 mb-6">General Settings</h2>
					<Form on:submit={onClickUpdateSettings} fullHeight>
						<div class="flex flex-col h-full">
							<div>
								<TextField required type="number" min="1" bind:value={formValues.maxFileSize}
									>Upload max file size (MB)</TextField
								>
								<TextField
									required
									type="number"
									min={1}
									max={1000}
									bind:value={formValues.RepeatOffenderMonths}
									>Repeat Offender Memory (Months)</TextField
								>
								<FormError message={updateSettingsError} />
							</div>
							<div class="mt-auto pt-4">
								<FormButton size={'large'}>Save Changes</FormButton>
							</div>
						</div>
					</Form>
				</div>

				<!-- Logging Card -->
				<div
					class="bg-white p-6 rounded-lg shadow-sm border border-gray-100 min-h-[300px] flex flex-col"
				>
					<h2 class="text-xl font-semibold text-gray-700 mb-6">Logging</h2>
					<Form fullHeight>
						<div class="flex flex-col h-full">
							<div>
								<TextFieldSelect
									id="appLogLevel"
									required
									bind:value={logLevel}
									onSelect={setLogLevel}
									options={logLevels}>Application log level</TextFieldSelect
								>

								<TextFieldSelect
									id="dbLogLevel"
									required
									bind:value={dbLogLevel}
									onSelect={setLogLevel}
									options={dbLogLevels}>Database log level</TextFieldSelect
								>
							</div>
						</div>
					</Form>
				</div>

				<!-- Import Section -->
				<div
					class="bg-white p-6 rounded-lg shadow-sm border border-gray-100 min-h-[300px] flex flex-col"
				>
					<h2 class="text-xl font-semibold text-gray-700 mb-6">Import</h2>
					<div class="flex flex-col h-full">
						<div class="space-y-4">
							<FileField name="importFile" accept=".zip" on:change={(e) => onSetImportFile(e)}>
								Select ZIP file to import
							</FileField>
							<label class="flex items-center gap-2 mt-2">
								<input
									type="checkbox"
									bind:checked={importForCompany}
									disabled={!isCompanyContext}
								/>
								Import pages and emails as company templates
							</label>
							{#if importForCompany}
								<div class="bg-blue-50 p-3 rounded-md text-sm text-blue-700">
									<strong>Company Import:</strong><br /> Pages and emails will be imported for this company.
									Assets will be imported as global/shared resources.
								</div>
							{:else}
								<div class="bg-gray-50 p-3 rounded-md text-sm text-gray-600">
									<strong>Global Import:</strong> All templates and assets will be imported as shared
									resources.
								</div>
							{/if}
							<FormError message={importError} />
						</div>
						<div class="mt-auto pt-4">
							<FormButton
								size={'large'}
								isSubmitting={isImportSubmitting}
								on:click={importFile ? onSubmitImport : undefined}
							>
								{#if isImportSubmitting}
									Importing...
								{:else}
									Import File
								{/if}
							</FormButton>
						</div>
					</div>
				</div>
			</div>
		</div>

		{#if isImportResultModalVisible && importResult}
			<Modal headerText="Import Summary" bind:visible={isImportResultModalVisible}>
				<div class="p-6 max-h-[80vh] overflow-y-auto">
					<div class="space-y-6">
						<!-- Statistics Section -->
						<div class="grid grid-cols-3 gap-6">
							<!-- Assets -->
							<div>
								<h3 class="font-semibold text-gray-900 mb-2">Assets (Global/Shared)</h3>
								<ul class="space-y-1">
									<li>Created: {importResult.assets_created}</li>
									<li>Skipped: {importResult.assets_skipped}</li>
									<li>Errors: {importResult.assets_errors}</li>
								</ul>
								<p class="text-xs text-gray-500 mt-1">
									Assets are always imported as global resources
								</p>
							</div>

							<!-- Pages -->
							<div>
								<h3 class="font-semibold text-gray-900 mb-2">Pages</h3>
								<ul class="space-y-1">
									<li>Created: {importResult.pages_created}</li>
									<li>Updated: {importResult.pages_updated}</li>
									<li>Skipped: {importResult.pages_skipped}</li>
									<li>Errors: {importResult.pages_errors}</li>
								</ul>
							</div>

							<!-- Emails -->
							<div>
								<h3 class="font-semibold text-gray-900 mb-2">Emails</h3>
								<ul class="space-y-1">
									<li>Created: {importResult.emails_created}</li>
									<li>Updated: {importResult.emails_updated}</li>
									<li>Skipped: {importResult.emails_skipped}</li>
									<li>Errors: {importResult.emails_errors}</li>
								</ul>
							</div>
						</div>

						<!-- Details Section -->
						<div class="border-t pt-6">
							<div class="space-y-6">
								<!-- Assets Details -->
								{#if importResult.assets_skipped_list?.length > 0}
									<div>
										<h3 class="font-semibold text-gray-900 mb-2">Assets (Global/Shared)</h3>
										<p class="text-sm text-gray-500 mb-3">
											All assets are imported as global resources regardless of import context
										</p>
										<div class="space-y-4">
											{#if importResult.assets_skipped_list?.length > 0}
												<div>
													<p class="text-sm text-gray-600 font-medium mb-1">Skipped:</p>
													<ul class="list-disc list-inside text-sm text-gray-600 ml-2">
														{#each importResult.assets_skipped_list || [] as asset}
															<li>{asset}</li>
														{/each}
													</ul>
												</div>
											{/if}
										</div>
									</div>
								{/if}
								{#if importResult.assets_errors_list?.length > 0}
									<div class="mt-6 border-t pt-6">
										<h4 class="font-semibold text-red-600 mb-1">Errors:</h4>
										<ul class="list-disc list-inside text-red-700 text-sm">
											{#each importResult.assets_errors_list as err}
												<li>
													<strong>{err.type}:</strong>
													{err.name} — {err.message}
												</li>
											{/each}
										</ul>
									</div>
								{/if}

								<!-- Pages Details -->
								{#if importResult.pages_created_list?.length > 0 || importResult.pages_updated_list?.length > 0 || importResult.pages_skipped_list?.length > 0 || importResult.pages_errors_list?.length > 0}
									<div>
										<h3 class="font-semibold text-gray-900 mb-2">Pages</h3>
										<div class="space-y-4">
											{#if importResult.pages_created_list?.length > 0}
												<div>
													<p class="text-sm text-gray-600 font-medium mb-1">Created:</p>
													<ul class="list-disc list-inside text-sm text-gray-600 ml-2">
														{#each importResult.pages_created_list || [] as page}
															<li>{page}</li>
														{/each}
													</ul>
												</div>
											{/if}
											{#if importResult.pages_updated_list?.length > 0}
												<div>
													<p class="text-sm text-gray-600 font-medium mb-1">Updated:</p>
													<ul class="list-disc list-inside text-sm text-gray-600 ml-2">
														{#each importResult.pages_updated_list || [] as page}
															<li>{page}</li>
														{/each}
													</ul>
												</div>
											{/if}
											{#if importResult.pages_skipped_list?.length > 0}
												<div>
													<p class="text-sm text-gray-600 font-medium mb-1">Skipped:</p>
													<ul class="list-disc list-inside text-sm text-gray-600 ml-2">
														{#each importResult.pages_skipped_list || [] as page}
															<li>{page}</li>
														{/each}
													</ul>
												</div>
											{/if}
											{#if importResult.pages_errors_list?.length > 0}
												<div class="mt-6 border-t pt-6">
													<h4 class="font-semibold text-red-600 mb-1">Errors:</h4>
													<ul class="list-disc list-inside text-red-700 text-sm">
														{#each importResult.pages_errors_list as err}
															<li>
																<strong>{err.type}:</strong>
																{err.name} — {err.message}
															</li>
														{/each}
													</ul>
												</div>
											{/if}
										</div>
									</div>
								{/if}

								<!-- Emails Details -->
								{#if importResult.emails_created_list?.length > 0 || importResult.emails_updated_list?.length > 0 || importResult.emails_errors_list?.length > 0 || importResult.emails_skipped_list?.length > 0}
									<div>
										<h3 class="font-semibold text-gray-900 mb-2">Emails</h3>
										<div class="space-y-4">
											{#if importResult.emails_created_list?.length > 0}
												<div>
													<p class="text-sm text-gray-600 font-medium mb-1">Created:</p>
													<ul class="list-disc list-inside text-sm text-gray-600 ml-2">
														{#each importResult.emails_created_list || [] as email}
															<li>{email}</li>
														{/each}
													</ul>
												</div>
											{/if}
											{#if importResult.emails_updated_list?.length > 0}
												<div>
													<p class="text-sm text-gray-600 font-medium mb-1">Updated:</p>
													<ul class="list-disc list-inside text-sm text-gray-600 ml-2">
														{#each importResult.emails_updated_list || [] as email}
															<li>{email}</li>
														{/each}
													</ul>
												</div>
											{/if}
											{#if importResult.emails_errors_list?.length > 0}
												<div class="mt-6 border-t pt-6">
													<h4 class="font-semibold text-red-600 mb-1">Errors:</h4>
													<ul class="list-disc list-inside text-red-700 text-sm">
														{#each importResult.emails_errors_list as err}
															<li>
																<strong>{err.type}:</strong>
																{err.name} — {err.message}
															</li>
														{/each}
													</ul>
												</div>
											{/if}
										</div>
									</div>
								{/if}
							</div>
						</div>
					</div>
					{#if importResult.errors && importResult.errors.length > 0}
						<div class="mt-6 border-t pt-6">
							<h4 class="font-semibold text-red-600 mb-1">Errors:</h4>
							<ul class="list-disc list-inside text-red-700 text-sm">
								{#each importResult.errors as err}
									<li>
										<strong>{err.type}:</strong>
										{err.name} — {err.message}
									</li>
								{/each}
							</ul>
						</div>
					{/if}
					<div class="mt-4 flex justify-end">
						<Button on:click={() => (isImportResultModalVisible = false)}>Close</Button>
					</div>
				</div>
			</Modal>
		{/if}

		<!-- Version and Update Info -->
		<div class="mt-8 text-sm text-gray-600 border-t border-gray-100 pt-4">
			<div class="flex items-center gap-4 flex-wrap">
				<button
					on:click|preventDefault={() => onClickCopy('.version-text')}
					class="flex items-center hover:bg-gray-100 py-2 px-4 rounded-md text-gray-700 transition-colors"
				>
					<span class="version-text font-mono">Version: {version}</span>
					<img class="ml-2 w-4 h-4" src="/icon-copy.svg" alt="copy version" />
				</button>
				<span>|</span>
				{#if updateAvailable}
					<a href="/settings/update/" class="text-blue-600 hover:underline">Update Available</a>
				{:else}
					<span class="text-gray-500">Up to date</span>
				{/if}
				<span>|</span>
				<button
					on:click={checkForUpdate}
					disabled={isCheckingUpdate}
					class="text-blue-600 hover:underline disabled:opacity-50 disabled:cursor-not-allowed"
				>
					{#if isCheckingUpdate}
						Checking...
					{:else}
						Check for Updates
					{/if}
				</button>
				<span>|</span>
				<a href="/licenses.txt" class="text-blue-600 hover:underline">View Licenses</a>
			</div>
		</div>
	{/if}

	<!-- License modal removed -->

	{#if isSSOModalVisible}
		<Modal bind:visible={isSSOModalVisible} headerText="SSO configuration" onClose={closeSSOModal}>
			<div class="mt-4">
				<!-- Introduction Section -->
				<div>
					<h3 class="text-xl font-semibold text-gray-700">Microsoft SSO Setup</h3>
					<p class="text-gray-600 mb-4">Configure Single Sign-On with Microsoft Azure AD.</p>
				</div>

				<!-- Warning Message -->
				<div class="bg-yellow-50 p-4 rounded-md">
					<p class="text-yellow-800 font-bold">Important:</p>
					<p class="text-yellow-700">
						Accounts that login with SSO will no longer be able to use password login.
					</p>
				</div>
			</div>
			<FormGrid on:submit={onSubmitSSO} bind:bindTo={ssoForm} {isSubmitting}>
				<FormColumns>
					<FormColumn>
						<!-- Configuration Fields -->
						<TextField
							required
							bind:value={ssoSettingsFormValues.clientID}
							placeholder="e.g., 8adf8e7c-d3ef-4a1b-b6c5-12345678abcd">Client ID</TextField
						>
						<TextField
							required
							bind:value={ssoSettingsFormValues.tenantID}
							placeholder="e.g., contoso.onmicrosoft.com">Tenant ID</TextField
						>
					</FormColumn>
					<FormColumn>
						<TextField
							required
							type="url"
							bind:value={ssoSettingsFormValues.redirectURL}
							placeholder="https://your-domain.com/auth/callback">Redirect URL</TextField
						>

						<PasswordField
							required
							bind:value={ssoSettingsFormValues.clientSecret}
							placeholder="Enter your client secret">Client Secret</PasswordField
						>
					</FormColumn>
					<FormError message={updateSSOError} />
				</FormColumns>
				<FormFooter
					closeModal={closeSSOModal}
					okText="Enable SSO"
					closeText="Cancel"
					{isSubmitting}
				/>
			</FormGrid>
		</Modal>
	{/if}

	{#if isSSODeleteAlertVisible}
		<DeleteAlert
			list={[
				'SSO will be disabled',
				'Configuration will be deleted',
				'SSO users will no longer be able to log in',
				'Be sure there is a administrative user without SSO'
			]}
			confirm
			name={'SSO configuration'}
			onClick={() => onClickDisableSSO()}
			bind:isVisible={isSSODeleteAlertVisible}
		></DeleteAlert>
	{/if}

	{#if isSSODeleteAlertVisible}
		<DeleteAlert
			list={[
				'SSO will be disabled',
				'Configuration will be deleted',
				'SSO users will no longer be able to log in',
				'Be sure there is a administrative user without SSO'
			]}
			confirm
			name={'SSO configuration'}
			onClick={() => onClickDisableSSO()}
			bind:isVisible={isSSODeleteAlertVisible}
		/>
	{/if}
</main>
