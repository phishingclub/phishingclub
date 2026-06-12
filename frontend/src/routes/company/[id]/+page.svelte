<script>
	import { page } from '$app/stores';
	import { goto } from '$app/navigation';
	import { onMount } from 'svelte';
	import { api } from '$lib/api/apiProxy.js';
	import { addToast } from '$lib/store/toast';
	import { showIsLoading, hideIsLoading } from '$lib/store/loading.js';
	import HeadTitle from '$lib/components/HeadTitle.svelte';
	import SettingsCard from '$lib/components/SettingsCard.svelte';
	import SettingsLoading from '$lib/components/SettingsLoading.svelte';
	import RadioOption from '$lib/components/RadioOption.svelte';
	import FormButton from '$lib/components/FormButton.svelte';
	import FormError from '$lib/components/FormError.svelte';
	import TextField from '$lib/components/TextField.svelte';
	import Alert from '$lib/components/Alert.svelte';
	import DeleteAlert from '$lib/components/modal/DeleteAlert.svelte';
	import ScimModal from '$lib/components/modal/ScimModal.svelte';
	import CompanyReportTemplateModal from '$lib/components/modal/CompanyReportTemplateModal.svelte';
	import CompanyCustomStats from '$lib/components/company/CompanyCustomStats.svelte';

	$: companyId = $page.params.id;

	let loaded = false;
	let company = null;

	// general form
	let formValues = {
		name: '',
		comment: ''
	};
	let generalError = '';
	let isSaving = false;

	// auto-prune (saved on change, like display mode in settings)
	let autoPruneEnabled = false;
	let isSavingAutoPrune = false;

	// SCIM status shown in the Integrations tab
	let scimStatus = 'none'; // 'none' | 'disabled' | 'enabled'

	// modals
	let isScimModalVisible = false;
	let isReportTemplateModalVisible = false;
	let isExportAlertVisible = false;
	let isDeleteAlertVisible = false;

	// tabs
	const tabs = [
		{ id: 'general', label: 'General' },
		{ id: 'stats', label: 'Custom Stats' },
		{ id: 'integrations', label: 'Integrations' },
		{ id: 'reports', label: 'Reports' },
		{ id: 'data', label: 'Data' },
		{ id: 'danger', label: 'Danger Zone' }
	];
	let active = 'general';

	onMount(async () => {
		const hash = window.location.hash.replace('#', '');
		if (tabs.some((t) => t.id === hash)) {
			active = hash;
		}
		await load();
		loaded = true;
	});

	const selectTab = (id) => {
		active = id;
		// replace the current history entry instead of pushing a new one, so
		// switching tabs does not stack up history and the back button leaves
		// the page rather than walking back through each visited tab
		history.replaceState(history.state, '', `#${id}`);
	};

	const load = async () => {
		await Promise.all([loadCompany(), loadAutoPrune(), loadScimStatus()]);
	};

	const loadCompany = async () => {
		try {
			const res = await api.company.getByID(companyId);
			if (!res.success) {
				throw res.error;
			}
			company = res.data;
			formValues.name = company.name || '';
			formValues.comment = company.comment || '';
		} catch (e) {
			addToast('Failed to get company', 'Error');
			console.error('failed to get company', e);
		}
	};

	const loadAutoPrune = async () => {
		try {
			const res = await api.company.getAutoPrune(companyId);
			autoPruneEnabled = res.success && res.data?.enabled === true;
		} catch (_) {
			autoPruneEnabled = false;
		}
	};

	const loadScimStatus = async () => {
		try {
			const res = await api.company.scim.getByCompanyID(companyId);
			if (res.success && res.data) {
				scimStatus = res.data.enabled ? 'enabled' : 'disabled';
			} else {
				scimStatus = 'none';
			}
		} catch (_) {
			scimStatus = 'none';
		}
	};

	const onSaveGeneral = async () => {
		generalError = '';
		isSaving = true;
		try {
			const res = await api.company.update(companyId, formValues.name, formValues.comment);
			if (!res.success) {
				generalError = res.error;
				return;
			}
			addToast('Company updated', 'Success');
			await loadCompany();
		} catch (e) {
			addToast('Failed to update company', 'Error');
			console.error('failed to update company', e);
		} finally {
			isSaving = false;
		}
	};

	const setAutoPrune = async (enabled) => {
		if (enabled === autoPruneEnabled) {
			return;
		}
		isSavingAutoPrune = true;
		try {
			const res = await api.company.setAutoPrune(companyId, enabled);
			if (res.success) {
				autoPruneEnabled = enabled;
				addToast('Auto-prune setting updated', 'Success');
			} else {
				addToast('Failed to save auto-prune setting', 'Error');
			}
		} catch (e) {
			addToast('Failed to save auto-prune setting', 'Error');
			console.error('failed to save auto-prune', e);
		} finally {
			isSavingAutoPrune = false;
		}
	};

	const onConfirmExport = async () => {
		try {
			showIsLoading();
			api.company.export(companyId);
			isExportAlertVisible = false;
			return { success: true };
		} catch (e) {
			addToast('Failed to export company events', 'Error');
			console.error('failed to export company events', e);
			return { success: false, error: e };
		} finally {
			hideIsLoading();
		}
	};

	const onConfirmDelete = async () => {
		const res = await api.company.delete(companyId);
		if (res.success) {
			addToast('Company deleted', 'Success');
			goto('/company');
		}
		return res;
	};
</script>

<HeadTitle title={company ? company.name : 'Company'} />
<main>
	{#if !loaded}
		<SettingsLoading />
	{:else}
		<nav class="mt-2 mb-1 text-sm">
			<a
				href="/company"
				class="text-gray-500 dark:text-gray-400 hover:text-cta-blue dark:hover:text-highlight-blue transition-colors"
			>
				Companies
			</a>
			<span class="text-gray-400 dark:text-gray-600 mx-2">/</span>
			<span class="text-gray-700 dark:text-gray-300">{company?.name}</span>
		</nav>

		<nav class="mt-4 mb-6 border-b border-gray-200 dark:border-gray-700">
			<div class="flex">
				{#each tabs as tab}
					<button
						on:click={() => selectTab(tab.id)}
						class="px-6 py-3 text-sm font-medium border-b-2 transition-colors
							{active === tab.id
							? 'border-cta-blue dark:border-highlight-blue text-cta-blue dark:text-highlight-blue'
							: 'border-transparent text-gray-500 dark:text-gray-400 hover:text-gray-700 dark:hover:text-gray-300 hover:border-gray-300 dark:hover:border-gray-600'}"
					>
						{tab.label}
					</button>
				{/each}
			</div>
		</nav>

		<div class="pb-8">
			{#if active === 'general'}
				<div class="flex flex-wrap gap-6">
					<SettingsCard title="Company Details" widthClass="w-full lg:w-[34rem]">
						<form on:submit|preventDefault={onSaveGeneral} class="flex flex-col flex-1">
							<TextField
								required
								width="full"
								minLength={1}
								maxLength={64}
								bind:value={formValues.name}>Company Name</TextField
							>
							<div class="flex flex-col py-2">
								<p class="font-semibold text-slate-600 dark:text-gray-400 py-2">Comment</p>
								<textarea
									bind:value={formValues.comment}
									maxlength={1000000}
									rows="6"
									placeholder="Add notes about this company..."
									class="w-full p-3 rounded-md text-gray-600 dark:text-gray-300 border border-transparent dark:border-gray-700/60 bg-grayblue-light dark:bg-gray-900/60 focus:outline-none focus:border-slate-400 dark:focus:border-highlight-blue/80 focus:bg-gray-100 dark:focus:bg-gray-700/60 resize-y transition-colors duration-200"
								/>
							</div>
							<FormError message={generalError} />
							<div class="mt-6 flex justify-end">
								<FormButton size="medium" isSubmitting={isSaving}>Save Changes</FormButton>
							</div>
						</form>
					</SettingsCard>

					<SettingsCard title="Auto-Prune Orphaned Recipients">
						<p class="text-gray-600 dark:text-gray-300 text-sm mb-4">
							Choose whether orphaned recipients are removed automatically.
						</p>
						<div class="space-y-2">
							<RadioOption
								checked={autoPruneEnabled}
								label="Enabled"
								description="Orphaned recipients are deleted automatically each hour"
								on:change={() => setAutoPrune(true)}
							/>
							<RadioOption
								checked={!autoPruneEnabled}
								label="Disabled"
								description="Orphaned recipients are kept until manually deleted"
								on:change={() => setAutoPrune(false)}
							/>
						</div>
					</SettingsCard>
				</div>
			{:else if active === 'stats'}
				<CompanyCustomStats {companyId} />
			{:else if active === 'integrations'}
				<div class="flex flex-wrap gap-6">
					<SettingsCard title="SCIM Provisioning">
						<p class="text-gray-600 dark:text-gray-300 text-sm mb-4">
							Automatically provision recipients from your identity provider.
						</p>
						<div class="flex items-center gap-2 mb-6">
							<span class="text-sm text-gray-500 dark:text-gray-400">Status</span>
							<span
								class="text-xs font-semibold px-2 py-1 rounded-full
									{scimStatus === 'enabled'
									? 'bg-green-100 text-green-700 dark:bg-green-900/40 dark:text-green-300'
									: scimStatus === 'disabled'
										? 'bg-yellow-100 text-yellow-700 dark:bg-yellow-900/40 dark:text-yellow-300'
										: 'bg-gray-100 text-gray-600 dark:bg-gray-700 dark:text-gray-300'}"
							>
								{scimStatus === 'enabled'
									? 'Enabled'
									: scimStatus === 'disabled'
										? 'Disabled'
										: 'Not configured'}
							</span>
						</div>
						<div class="mt-auto flex justify-end">
							<FormButton size="medium" on:click={() => (isScimModalVisible = true)}
								>Configure SCIM</FormButton
							>
						</div>
					</SettingsCard>
				</div>
			{:else if active === 'reports'}
				<div class="flex flex-wrap gap-6">
					<SettingsCard title="Report Template">
						<p class="text-gray-600 dark:text-gray-300 text-sm mb-4">
							Override the global report template for this company.
						</p>
						<div class="mt-auto flex justify-end">
							<FormButton size="medium" on:click={() => (isReportTemplateModalVisible = true)}
								>Edit template</FormButton
							>
						</div>
					</SettingsCard>
				</div>
			{:else if active === 'data'}
				<div class="flex flex-wrap gap-6">
					<SettingsCard title="Export">
						<p class="text-gray-600 dark:text-gray-300 text-sm mb-4">
							Download a ZIP with all company data, recipients, and campaign events.
						</p>
						<div class="mt-auto flex justify-end">
							<FormButton size="medium" on:click={() => (isExportAlertVisible = true)}
								>Export data</FormButton
							>
						</div>
					</SettingsCard>
				</div>
			{:else if active === 'danger'}
				<div class="flex flex-wrap gap-6">
					<SettingsCard title="Delete Company">
						<p class="text-gray-600 dark:text-gray-300 text-sm mb-4">
							Permanently removes this company and all of its domains, campaigns, and recipients.
							This cannot be undone.
						</p>
						<div class="mt-auto flex justify-end">
							<button
								type="button"
								on:click={() => (isDeleteAlertVisible = true)}
								class="px-4 py-2 bg-red-600 hover:bg-red-700 text-white text-sm font-bold uppercase rounded-md transition-colors duration-200"
							>
								Delete
							</button>
						</div>
					</SettingsCard>
				</div>
			{/if}
		</div>
	{/if}

	<ScimModal bind:visible={isScimModalVisible} {company} />
	<CompanyReportTemplateModal bind:visible={isReportTemplateModalVisible} {company} />

	<Alert
		headline="Export Company Data"
		bind:visible={isExportAlertVisible}
		onConfirm={onConfirmExport}
	>
		<div>
			<p class="mb-4">Are you sure you want to export all data for:</p>
			<div class="bg-gray-50 dark:bg-gray-700 p-3 rounded mb-4">
				<p class="font-medium">{company?.name}</p>
			</div>
			<p class="text-sm text-gray-500">
				This will download a ZIP file containing all company data, recipients, and campaign events.
			</p>
		</div>
	</Alert>

	<DeleteAlert
		list={['All data related to the company such as domains, campaign, recipients will be lost']}
		name={company?.name}
		onClick={onConfirmDelete}
		confirm
		bind:isVisible={isDeleteAlertVisible}
	/>
</main>
