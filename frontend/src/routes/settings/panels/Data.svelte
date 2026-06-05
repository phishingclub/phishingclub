<script>
	import { onMount } from 'svelte';
	import { api } from '$lib/api/apiProxy.js';
	import { addToast } from '$lib/store/toast';
	import { AppStateService } from '$lib/service/appState';
	import SettingsCard from '$lib/components/SettingsCard.svelte';
	import SettingsLoading from '$lib/components/SettingsLoading.svelte';
	import RadioOption from '$lib/components/RadioOption.svelte';
	import Button from '$lib/components/Button.svelte';
	import FileField from '$lib/components/FileField.svelte';
	import FormButton from '$lib/components/FormButton.svelte';
	import FormError from '$lib/components/FormError.svelte';
	import FormGrid from '$lib/components/FormGrid.svelte';
	import FormColumns from '$lib/components/FormColumns.svelte';
	import FormColumn from '$lib/components/FormColumn.svelte';
	import FormFooter from '$lib/components/FormFooter.svelte';
	import Modal from '$lib/components/Modal.svelte';

	let loaded = false;

	// auto-prune settings
	let autoPruneOption = { enabled: false, companies: [] };
	let autoPruneEnabled = false;
	let autoPruneError = '';

	// backup
	let isBackupModalVisible = false;
	let isCreatingBackup = false;
	let availableBackups = [];
	let isLoadingBackups = false;

	// import
	let importError = '';
	let isImportSubmitting = false;
	let importFile = null;
	let importResult = null;
	let isImportResultModalVisible = false;
	let importModalContent = null;

	// company context for import
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

	onMount(async () => {
		try {
			await refreshAutoPrune();
			await refreshBackupList();
		} finally {
			loaded = true;
		}
	});

	async function refreshAutoPrune() {
		try {
			const res = await api.option.getAutoPrune();
			if (res.success) {
				autoPruneOption = res.data;
				autoPruneEnabled = res.data.enabled === true;
			}
		} catch (e) {
			console.error('failed to load auto-prune setting', e);
		}
	}

	async function setAutoPruneValue(enabled) {
		autoPruneError = '';
		// read-modify-write: preserve per-company entries
		const updated = { ...autoPruneOption, enabled };
		try {
			const res = await api.option.setAutoPrune(updated);
			if (!res.success) {
				autoPruneError = res.error;
				return;
			}
			autoPruneOption = updated;
			autoPruneEnabled = enabled;
			addToast('Auto-prune setting saved', 'Success');
		} catch (e) {
			autoPruneError = 'Failed to save auto-prune setting';
			console.error('failed to set auto-prune setting', e);
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

	const openBackupModal = () => {
		isBackupModalVisible = true;
	};

	const closeBackupModal = () => {
		isBackupModalVisible = false;
	};

	async function createBackup() {
		isCreatingBackup = true;
		try {
			const res = await api.application.createBackup();
			if (res.success) {
				addToast('Backup created', 'Success');
				closeBackupModal();
				await refreshBackupList();
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

	const onSetImportFile = (event) => {
		importFile = event.target.files[0];
	};

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
				setTimeout(() => {
					if (importModalContent) {
						importModalContent.scrollTop = 0;
					}
				}, 0);
				const fileInput = document.querySelector('input[type="file"][name="importFile"]');
				if (fileInput) /** @type {HTMLInputElement} */ (fileInput).value = '';
			} else {
				importError = response.error || 'Import failed';
				importResult = response.data || null;
				isImportResultModalVisible = !!importResult;
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
</script>

{#if !loaded}
	<SettingsLoading />
{:else}
<div class="flex flex-wrap gap-6">
	<SettingsCard title="Import">
		<div class="space-y-4">
			<FileField name="importFile" accept=".zip" on:change={(e) => onSetImportFile(e)}>
				Select ZIP file to import
			</FileField>
			<label class="flex items-center gap-2 mt-2">
				<input type="checkbox" bind:checked={importForCompany} disabled={!isCompanyContext} />
				Import pages and emails as company templates
			</label>
			{#if importForCompany}
				<div
					class="bg-blue-50 dark:bg-blue-900/30 p-3 rounded-md text-sm text-blue-700 dark:text-blue-200 transition-colors duration-200"
				>
					<strong>Company Import:</strong><br /> Pages and emails will be imported for this company. Assets
					will be imported as global/shared resources.
				</div>
			{:else}
				<div
					class="bg-gray-50 dark:bg-gray-700 p-3 rounded-md text-sm text-gray-600 dark:text-gray-300 transition-colors duration-200"
				>
					<strong>Global Import:</strong> All templates and assets will be imported as shared resources.
				</div>
			{/if}
			<FormError message={importError} />
		</div>
		<svelte:fragment slot="footer">
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
		</svelte:fragment>
	</SettingsCard>

	<SettingsCard title="Backup">
		<div class="space-y-4">
			<p class="text-gray-600 dark:text-gray-300 text-sm transition-colors duration-200">
				Create a backup of database, assets, attachments and certificates.
			</p>

			{#if availableBackups.length > 0}
				<div class="bg-gray-50 dark:bg-gray-700 p-3 rounded-md transition-colors duration-200">
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
		<svelte:fragment slot="footer">
			<Button size={'large'} on:click={openBackupModal} disabled={isCreatingBackup}>
				Create Backup
			</Button>
		</svelte:fragment>
	</SettingsCard>

	<SettingsCard title="Auto-Prune Recipients">
		<div class="space-y-4">
			<p class="text-gray-600 dark:text-gray-300 text-sm transition-colors duration-200">
				Automatically delete orphaned recipients (not in any group) on a hourly schedule.
			</p>
			<div class="space-y-3">
				<RadioOption
					checked={autoPruneEnabled}
					label="Enabled"
					description="Orphaned recipients are deleted automatically each hour"
					on:change={() => setAutoPruneValue(true)}
				/>
				<RadioOption
					checked={!autoPruneEnabled}
					label="Disabled"
					description="Orphaned recipients are kept until manually deleted"
					on:change={() => setAutoPruneValue(false)}
				/>
			</div>
			<FormError message={autoPruneError} />
		</div>
	</SettingsCard>
</div>
{/if}

{#if isImportResultModalVisible && importResult}
	<Modal headerText="Import Summary" bind:visible={isImportResultModalVisible}>
		<div
			class="p-6 max-h-[80vh] overflow-y-auto bg-white dark:bg-gray-800 text-gray-900 dark:text-gray-100 transition-colors duration-200"
			bind:this={importModalContent}
		>
			<div class="space-y-6">
				<div class="grid grid-cols-3 gap-6">
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
						<p class="text-xs text-gray-500 dark:text-gray-400 mt-1 transition-colors duration-200">
							Assets are always imported as global resources
						</p>
					</div>

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

				<div class="border-t pt-6">
					<div class="space-y-6">
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
							<strong>Note:</strong> This is not a substitute for having proper automated and tested backup
							and recovery plans at the operating system level.
						</p>
						<div class="bg-gray-50 dark:bg-gray-700 p-4 rounded-md">
							<h3 class="font-semibold text-gray-900 dark:text-white mb-2">What will be backed up:</h3>
							<ul class="text-sm text-gray-700 dark:text-gray-300 space-y-1">
								<li>• SQLite database (including WAL files)</li>
								<li>• Asset files</li>
								<li>• Attachment files</li>
								<li>• Certificate files</li>
							</ul>
						</div>

						<div class="bg-gray-50 dark:bg-gray-700 p-4 rounded-md">
							<h3 class="font-semibold text-gray-900 dark:text-white mb-2">Important:</h3>
							<ul class="text-sm text-gray-700 dark:text-gray-300 space-y-1">
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
