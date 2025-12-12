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
	import SimpleCodeEditor from '$lib/components/editor/SimpleCodeEditor.svelte';
	import { AppStateService } from '$lib/service/appState';
	import { hideIsLoading, showIsLoading } from '$lib/store/loading';
	import { addToast } from '$lib/store/toast';
	import { onMount } from 'svelte';
	import { onClickCopy } from '$lib/utils/common';
	import { displayMode, DISPLAY_MODE } from '$lib/store/displayMode';
	import ConditionalDisplay from '$lib/components/ConditionalDisplay.svelte';

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

	// Backup functionality
	let isBackupModalVisible = false;
	let isCreatingBackup = false;
	let availableBackups = [];
	let isLoadingBackups = false;

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
	let importModalContent = null;

	// display mode settings
	let currentDisplayMode = DISPLAY_MODE.WHITEBOX;
	let displayModeError = '';

	// Company context for import
	const appState = AppStateService.instance;
	let isCompanyContext = false;
	let importForCompany = false;
	let contextCompanyID = null;

	// obfuscation template editor
	let isObfuscationTemplateModalVisible = false;
	let obfuscationTemplate = '';
	let obfuscationTemplateError = '';
	let isObfuscationTemplateSubmitting = false;

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
			await refreshBackupList();
			await refreshDisplayMode();
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

	async function refreshDisplayMode() {
		try {
			const res = immediateResponseHandler(await api.option.get('display_mode'));
			if (res.success && res.data.value) {
				currentDisplayMode = res.data.value;
				displayMode.setMode(res.data.value);
			}
		} catch (e) {
			console.error('failed to refresh display mode', e);
		}
	}

	async function setDisplayMode(mode) {
		try {
			showIsLoading();
			const res = await api.option.set('display_mode', mode);
			if (res.success) {
				currentDisplayMode = mode;
				displayMode.setMode(mode);
				addToast('Display mode updated', 'Success');
				displayModeError = '';
			} else {
				displayModeError = res.error || 'Failed to update display mode';
			}
		} catch (e) {
			console.error('failed to set display mode', e);
			displayModeError = 'Failed to update display mode';
		} finally {
			hideIsLoading();
		}
	}

	async function refreshBackupList() {
		isLoadingBackups = true;
		try {
			const res = await api.application.listBackups();
			if (res.success) {
				availableBackups = res.data || [];
			}
		} catch (e) {
			console.error('failed to refresh backup list', e);
			availableBackups = [];
		} finally {
			isLoadingBackups = false;
		}
	}

	async function downloadBackup(filename) {
		try {
			const blob = await api.application.downloadBackup(filename);

			// Create download link
			const url = window.URL.createObjectURL(blob);
			const a = document.createElement('a');
			a.href = url;
			a.download = filename;
			document.body.appendChild(a);
			a.click();
			window.URL.revokeObjectURL(url);
			document.body.removeChild(a);

			addToast('Backup downloaded', 'Success');
		} catch (e) {
			console.error('failed to download backup', e);
			addToast('Failed to download backup', 'Error');
		}
	}

	function openBackupModal() {
		isBackupModalVisible = true;
	}

	function closeBackupModal() {
		isBackupModalVisible = false;
	}

	async function createBackup() {
		isCreatingBackup = true;
		try {
			const res = await api.application.createBackup();
			if (res.success) {
				addToast('Backup created', 'Success');
				closeBackupModal();
				await refreshBackupList(); // Refresh backup list
			} else {
				addToast('Failed to create backup', 'Error');
			}
		} catch (e) {
			console.error('failed to create backup', e);
			addToast('Failed to create backup', 'Error');
		} finally {
			isCreatingBackup = false;
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

	const openSSOModal = async (e) => {
		e.preventDefault();
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
				addToast('File has been imported', 'Success');
				importFile = null;
				importResult = response.data;
				isImportResultModalVisible = true;
				// reset scroll position to top when modal becomes visible
				setTimeout(() => {
					if (importModalContent) {
						importModalContent.scrollTop = 0;
					}
				}, 0);
				// Reset file input
				const fileInput = document.querySelector('input[type="file"][name="importFile"]');
				if (fileInput) /** @type {HTMLInputElement} */ (fileInput).value = '';
			} else {
				importError = response.error || 'Import failed';
				importResult = response.data || null;
				isImportResultModalVisible = !!importResult;
				// reset scroll position to top when modal becomes visible
				setTimeout(() => {
					if (importModalContent) {
						importModalContent.scrollTop = 0;
					}
				}, 0);
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

	/**
	 * Open obfuscation template modal
	 */
	const openObfuscationTemplateModal = async () => {
		try {
			showIsLoading();
			const response = await api.option.get('obfuscation_template');
			if (response.success) {
				obfuscationTemplate = response.data.value || '';
			} else {
				obfuscationTemplateError = 'Failed to load template';
			}
		} catch (error) {
			console.error('Failed to load obfuscation template:', error);
			obfuscationTemplateError = 'Failed to load template';
		} finally {
			hideIsLoading();
			isObfuscationTemplateModalVisible = true;
		}
	};

	/**
	 * Close obfuscation template modal
	 */
	const closeObfuscationTemplateModal = () => {
		isObfuscationTemplateModalVisible = false;
		obfuscationTemplateError = '';
	};

	/**
	 * Submit obfuscation template
	 */
	const onSubmitObfuscationTemplate = async (event) => {
		const saveOnly = event?.detail?.saveOnly || false;
		isObfuscationTemplateSubmitting = true;
		obfuscationTemplateError = '';

		try {
			const response = await api.option.set('obfuscation_template', obfuscationTemplate);

			if (response.success) {
				addToast(
					saveOnly ? 'Obfuscation template saved' : 'Obfuscation template updated',
					'Success'
				);
				if (!saveOnly) {
					isObfuscationTemplateModalVisible = false;
				}
			} else {
				obfuscationTemplateError = response.error || 'Failed to update template';
			}
		} catch (error) {
			console.error('Failed to update obfuscation template:', error);
			obfuscationTemplateError = 'Failed to update template';
		} finally {
			isObfuscationTemplateSubmitting = false;
		}
	};
</script>

<HeadTitle title="Settings" />
<main class="pb-8">
	<Headline>Settings</Headline>
	{#if isInitiallyLoaded}
		<div class="max-w-7xl pt-4 space-y-8">
			<!-- Settings Grid -->
			<div class="grid grid-cols-1 lg:grid-cols-2 xl:grid-cols-4 gap-8">
				<!-- SSO Card -->
				<div
					class="bg-white dark:bg-gray-800 p-6 rounded-lg shadow-sm dark:shadow-gray-900/50 border border-gray-100 dark:border-gray-700 min-h-[300px] flex flex-col transition-colors duration-200"
				>
					<h2
						class="text-xl font-semibold text-gray-700 dark:text-gray-200 mb-6 transition-colors duration-200"
					>
						Single Sign-On
					</h2>
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
					class="bg-white dark:bg-gray-800 p-6 rounded-lg shadow-sm dark:shadow-gray-900/50 border border-gray-100 dark:border-gray-700 min-h-[300px] flex flex-col transition-colors duration-200"
				>
					<h2
						class="text-xl font-semibold text-gray-700 dark:text-gray-200 mb-6 transition-colors duration-200"
					>
						General Settings
					</h2>
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
					class="bg-white dark:bg-gray-800 p-6 rounded-lg shadow-sm dark:shadow-gray-900/50 border border-gray-100 dark:border-gray-700 min-h-[300px] flex flex-col transition-colors duration-200"
				>
					<h2
						class="text-xl font-semibold text-gray-700 dark:text-gray-200 mb-6 transition-colors duration-200"
					>
						Logging
					</h2>
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
					class="bg-white dark:bg-gray-800 p-6 rounded-lg shadow-sm dark:shadow-gray-900/50 border border-gray-100 dark:border-gray-700 min-h-[300px] flex flex-col transition-colors duration-200"
				>
					<h2
						class="text-xl font-semibold text-gray-700 dark:text-gray-200 mb-6 transition-colors duration-200"
					>
						Import
					</h2>
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
								<div
									class="bg-blue-50 dark:bg-blue-900/30 p-3 rounded-md text-sm text-blue-700 dark:text-blue-200 transition-colors duration-200"
								>
									<strong>Company Import:</strong><br /> Pages and emails will be imported for this company.
									Assets will be imported as global/shared resources.
								</div>
							{:else}
								<div
									class="bg-gray-50 dark:bg-gray-700 p-3 rounded-md text-sm text-gray-600 dark:text-gray-300 transition-colors duration-200"
								>
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
				<!-- Display Mode Card -->
				<div
					class="bg-white dark:bg-gray-800 p-6 rounded-lg shadow-sm dark:shadow-gray-900/50 border border-gray-100 dark:border-gray-700 min-h-[300px] flex flex-col transition-colors duration-200"
				>
					<h2
						class="text-xl font-semibold text-gray-700 dark:text-gray-200 mb-6 transition-colors duration-200"
					>
						Display Mode
					</h2>
					<div class="flex flex-col h-full">
						<div class="space-y-4">
							<p class="text-gray-600 dark:text-gray-300 text-sm transition-colors duration-200">
								Select which features are available
							</p>
							<div class="space-y-3">
								<label
									class="flex items-start gap-3 p-3 border rounded-lg cursor-pointer transition-colors {currentDisplayMode ===
									DISPLAY_MODE.WHITEBOX
										? 'bg-blue-50 dark:bg-blue-900/20 border-blue-500 dark:border-blue-600'
										: 'border-gray-300 dark:border-gray-600'}"
								>
									<input
										type="radio"
										checked={currentDisplayMode === DISPLAY_MODE.WHITEBOX}
										on:change={() => setDisplayMode(DISPLAY_MODE.WHITEBOX)}
										class="mt-0.5 w-4 h-4 text-blue-600 bg-gray-100 dark:bg-gray-700 border-gray-300 dark:border-gray-600 focus:ring-blue-500 focus:ring-2"
									/>
									<div class="text-left flex-1">
										<span class="text-sm font-medium text-gray-900 dark:text-gray-100 block">
											Phishing Simulation
										</span>
									</div>
								</label>
								<label
									class="flex items-start gap-3 p-3 border rounded-lg cursor-pointer transition-colors {currentDisplayMode ===
									DISPLAY_MODE.BLACKBOX
										? 'bg-blue-50 dark:bg-blue-900/20 border-blue-500 dark:border-blue-600'
										: 'border-gray-300 dark:border-gray-600'}"
								>
									<input
										type="radio"
										checked={currentDisplayMode === DISPLAY_MODE.BLACKBOX}
										on:change={() => setDisplayMode(DISPLAY_MODE.BLACKBOX)}
										class="mt-0.5 w-4 h-4 text-blue-600 bg-gray-100 dark:bg-gray-700 border-gray-300 dark:border-gray-600 focus:ring-blue-500 focus:ring-2"
									/>
									<div class="text-left flex-1">
										<span class="text-sm font-medium text-gray-900 dark:text-gray-100 block">
											Red Team Phishing
										</span>
									</div>
								</label>
								<p class="text-gray-600 dark:text-gray-300 text-sm transition-colors duration-200">
									Read about the difference between <a
										class="white underline"
										href="https://phishing.club/blog/phishing-simulation-vs-red-team-phishing/"
										target="_blank">phishing simulation and red team phishing</a
									>
								</p>
							</div>
							<FormError message={displayModeError} />
						</div>
					</div>
				</div>

				<!-- Backup Section -->
				<div
					class="bg-white dark:bg-gray-800 p-6 rounded-lg shadow-sm dark:shadow-gray-900/50 border border-gray-100 dark:border-gray-700 h-[420px] flex flex-col transition-colors duration-200"
				>
					<h2
						class="text-xl font-semibold text-gray-700 dark:text-gray-200 mb-6 transition-colors duration-200"
					>
						Backup
					</h2>
					<div class="flex flex-col h-full">
						<div class="space-y-4">
							<p class="text-gray-600 dark:text-gray-300 text-sm transition-colors duration-200">
								Create a backup of database, assets, attachments and certificates.
							</p>

							{#if availableBackups.length > 0}
								<div
									class="bg-gray-50 dark:bg-gray-700 p-3 rounded-md transition-colors duration-200"
								>
									<h4
										class="font-medium text-gray-900 dark:text-gray-100 mb-2 transition-colors duration-200"
									>
										Available:
									</h4>
									<div class="space-y-3">
										{#each availableBackups as backup}
											<div class="flex items-start justify-between gap-4 text-sm">
												<div class="flex flex-col min-w-0 flex-1">
													<span
														class="text-gray-700 dark:text-gray-200 text-xs font-medium transition-colors duration-200"
													>
														{new Date(backup.createdAt).toLocaleString()}
													</span>
													<span
														class="text-gray-400 dark:text-gray-400 text-xs transition-colors duration-200"
													>
														{(backup.size / 1024 / 1024).toFixed(1)} MB
													</span>
												</div>
												<button
													class="px-3 py-1 bg-blue-600 hover:bg-blue-700 dark:bg-blue-700 dark:hover:bg-blue-800 text-white text-xs rounded transition-colors flex-shrink-0"
													on:click={() => downloadBackup(backup.name)}
												>
													Download
												</button>
											</div>
										{/each}
									</div>
								</div>
							{:else if !isLoadingBackups}
								<div
									class="bg-gray-50 dark:bg-gray-700 p-3 rounded-md text-sm text-gray-600 dark:text-gray-300 transition-colors duration-200"
								>
									No backups available yet.
								</div>
							{/if}
						</div>
						<div class="mt-auto pt-4">
							<Button size={'large'} on:click={openBackupModal} disabled={isCreatingBackup}>
								Create Backup
							</Button>
						</div>
					</div>
				</div>

				<!-- Obfuscation Template Section -->
				<ConditionalDisplay show="blackbox">
					<div
						class="bg-white dark:bg-gray-800 p-6 rounded-lg shadow-sm dark:shadow-gray-900/50 border border-gray-100 dark:border-gray-700 h-[420px] flex flex-col transition-colors duration-200"
					>
						<h2
							class="text-xl font-semibold text-gray-700 dark:text-gray-200 mb-6 transition-colors duration-200"
						>
							Obfuscation Template
						</h2>
						<div class="flex flex-col h-full">
							<div class="space-y-4">
								<p class="text-gray-600 dark:text-gray-300 text-sm transition-colors duration-200">
									Customize the template used when obfuscation is enabled to.
								</p>
								<div
									class="bg-gray-50 dark:bg-gray-700 p-3 rounded-md transition-colors duration-200"
								>
									<p class="text-sm text-gray-700 dark:text-gray-300 mb-2">
										<strong>Internal obfuscation variable:</strong>
									</p>
									<p class="text-xs text-gray-600 dark:text-gray-400 font-mono">
										{'{{.Script}}'}
									</p>
								</div>
							</div>
							<div class="mt-auto pt-4">
								<Button size={'large'} on:click={openObfuscationTemplateModal}>Edit Template</Button
								>
							</div>
						</div>
					</div>
				</ConditionalDisplay>
			</div>
		</div>

		{#if isImportResultModalVisible && importResult}
			<Modal headerText="Import Summary" bind:visible={isImportResultModalVisible}>
				<div
					class="p-6 max-h-[80vh] overflow-y-auto bg-white dark:bg-gray-800 text-gray-900 dark:text-gray-100 transition-colors duration-200"
					on:scroll={() => {}}
					bind:this={importModalContent}
				>
					<div class="space-y-6">
						<!-- Statistics Section -->
						<div class="grid grid-cols-3 gap-6">
							<!-- Assets -->
							<div>
								<h3
									class="font-semibold text-gray-900 dark:text-gray-100 mb-2 transition-colors duration-200"
								>
									Assets (Global/Shared)
								</h3>
								<ul class="space-y-1">
									<li>Created: {importResult.assets_created}</li>
									<li>Skipped: {importResult.assets_skipped}</li>
									<li>Errors: {importResult.assets_errors}</li>
								</ul>
								<p
									class="text-xs text-gray-500 dark:text-gray-400 mt-1 transition-colors duration-200"
								>
									Assets are always imported as global resources
								</p>
							</div>

							<!-- Pages -->
							<div>
								<h3
									class="font-semibold text-gray-900 dark:text-gray-100 mb-2 transition-colors duration-200"
								>
									Pages
								</h3>
								<ul class="space-y-1">
									<li>Created: {importResult.pages_created}</li>
									<li>Updated: {importResult.pages_updated}</li>
									<li>Skipped: {importResult.pages_skipped}</li>
									<li>Errors: {importResult.pages_errors}</li>
								</ul>
							</div>

							<!-- Emails -->
							<div>
								<h3
									class="font-semibold text-gray-900 dark:text-gray-100 mb-2 transition-colors duration-200"
								>
									Emails
								</h3>
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
		<div
			class="mt-8 text-sm text-gray-600 dark:text-gray-300 border-t border-gray-100 dark:border-gray-700 pt-4 transition-colors duration-200"
		>
			<div class="flex items-center gap-4 flex-wrap">
				<button
					on:click|preventDefault={() => onClickCopy(version)}
					class="flex items-center hover:bg-gray-100 dark:hover:bg-gray-700 py-2 px-4 rounded-md text-gray-700 dark:text-gray-300 transition-colors duration-200"
				>
					<span class="version-text font-mono">Version: {version}</span>
					<img class="ml-2 w-4 h-4" src="/icon-copy.svg" alt="copy version" />
				</button>
				<span>|</span>
				{#if updateAvailable}
					<a
						href="/settings/update/"
						class="text-blue-600 dark:text-white hover:underline transition-colors duration-200"
						>Update Available</a
					>
				{:else}
					<span class="text-gray-500 dark:text-gray-400 transition-colors duration-200"
						>Up to date</span
					>
				{/if}
				<span>|</span>
				<button
					on:click={checkForUpdate}
					disabled={isCheckingUpdate}
					class="text-blue-600 dark:text-white hover:underline disabled:opacity-50 disabled:cursor-not-allowed transition-colors duration-200"
				>
					{#if isCheckingUpdate}
						Checking...
					{:else}
						Check for Updates
					{/if}
				</button>
				<span>|</span>
				<a
					href="/licenses.txt"
					class="text-blue-600 dark:text-white hover:underline transition-colors duration-200"
					>View Licenses</a
				>
			</div>
		</div>
	{/if}
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

	{#if isBackupModalVisible}
		<Modal
			headerText="Create Backup"
			bind:visible={isBackupModalVisible}
			onClose={closeBackupModal}
			isSubmitting={isCreatingBackup}
		>
			<FormGrid on:submit={createBackup} isSubmitting={isCreatingBackup}>
				<FormColumns>
					<FormColumn>
						<div class="space-y-4">
							<p>This will create a backup file that can be downloaded from the settings page.</p>
							<p>
								<strong>Note:</strong> This is not a substitute for having proper automated and tested
								backup and recovery plans at the operating system level.
							</p>
							<div class="bg-blue-50 p-4 rounded-md">
								<h3 class="font-semibold text-blue-800 mb-2">What will be backed up:</h3>
								<ul class="text-sm text-blue-700 space-y-1">
									<li>• SQLite database (including WAL files)</li>
									<li>• Asset files</li>
									<li>• Attachment files</li>
									<li>• Certificate files</li>
								</ul>
							</div>

							<div class="bg-yellow-50 p-4 rounded-md">
								<h3 class="font-semibold text-yellow-800 mb-2">Important:</h3>
								<ul class="text-sm text-yellow-700 space-y-1">
									<li>• Large databases may take significant time to backup</li>
									<li>• Operations may be affected during the backup process</li>
									<li>• Ensure you have sufficient disk space</li>
									<li>
										• Only the 3 most recent backups are kept (older ones are automatically deleted)
									</li>
									<li>• The backup does not include config.json or the application binary</li>
								</ul>
							</div>
						</div>
					</FormColumn>
				</FormColumns>
				<FormFooter
					closeModal={closeBackupModal}
					isSubmitting={isCreatingBackup}
					okText={isCreatingBackup ? 'Creating Backup...' : 'Create Backup'}
				/>
			</FormGrid>
		</Modal>
	{/if}

	{#if isObfuscationTemplateModalVisible}
		<Modal
			bind:visible={isObfuscationTemplateModalVisible}
			headerText="Edit Obfuscation Template"
			onClose={closeObfuscationTemplateModal}
			{isSubmitting}
		>
			<FormGrid
				on:submit={onSubmitObfuscationTemplate}
				isSubmitting={isObfuscationTemplateSubmitting}
				modalMode="update"
			>
				<div
					class="w-80vw col-start-1 col-end-4 row-start-1 py-8 px-6 flex flex-col bg-white dark:bg-gray-800 text-gray-900 dark:text-gray-100 transition-colors duration-200"
				>
					<SimpleCodeEditor
						bind:value={obfuscationTemplate}
						language="html"
						height="large"
						showVimToggle={true}
						showExpandButton={false}
					/>
					<p class="text-sm text-gray-600 dark:text-gray-300 my-4">
						Example <code class="bg-gray-200 dark:bg-gray-700 p-1 rounded text-xs"
							>{"eval(atob('{{base64 .Script}}'))"}</code
						>
					</p>
					<FormError message={obfuscationTemplateError} />
				</div>
				<FormFooter
					isSubmitting={isObfuscationTemplateSubmitting}
					closeModal={closeObfuscationTemplateModal}
				/>
			</FormGrid>
		</Modal>
	{/if}
</main>
