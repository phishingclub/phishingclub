<script>
	import { page } from '$app/stores';
	import { goto } from '$app/navigation';
	import { onMount, onDestroy } from 'svelte';
	import { api } from '$lib/api/apiProxy.js';
	import { addToast } from '$lib/store/toast';
	import { showIsLoading, hideIsLoading } from '$lib/store/loading.js';
	import { companyColorOverride } from '$lib/store/companyColor';
	import HeadTitle from '$lib/components/HeadTitle.svelte';
	import SettingsCard from '$lib/components/SettingsCard.svelte';
	import StatusBadge from '$lib/components/StatusBadge.svelte';
	import SettingsLoading from '$lib/components/SettingsLoading.svelte';
	import RadioOption from '$lib/components/RadioOption.svelte';
	import FormButton from '$lib/components/FormButton.svelte';
	import FormError from '$lib/components/FormError.svelte';
	import TextField from '$lib/components/TextField.svelte';
	import TextareaField from '$lib/components/TextareaField.svelte';
	import FieldLabel from '$lib/components/form/FieldLabel.svelte';
	import Headline from '$lib/components/Headline.svelte';
	import SubHeadline from '$lib/components/SubHeadline.svelte';
	import Breadcrumb from '$lib/components/Breadcrumb.svelte';
	import TabBar from '$lib/components/TabBar.svelte';
	import Button from '$lib/components/Button.svelte';
	import Alert from '$lib/components/Alert.svelte';
	import DeleteAlert from '$lib/components/modal/DeleteAlert.svelte';
	import ScimModal from '$lib/components/modal/ScimModal.svelte';
	import CompanyReportTemplateModal from '$lib/components/modal/CompanyReportTemplateModal.svelte';
	import CompanyReportDeliveryModal from '$lib/components/modal/CompanyReportDeliveryModal.svelte';
	import CompanyReportInboxModal from '$lib/components/modal/CompanyReportInboxModal.svelte';
	import ReportInboxStatus from '$lib/components/company/ReportInboxStatus.svelte';
	import CompanyReportDeliveryLog from '$lib/components/company/CompanyReportDeliveryLog.svelte';
	import CompanyCustomStats from '$lib/components/company/CompanyCustomStats.svelte';

	$: companyId = $page.params.id;

	let loaded = false;
	let company = null;

	// default banner color used when a company has no custom color
	const DEFAULT_COMPANY_COLOR = '#1e3fa8';

	// general form
	let formValues = {
		name: '',
		comment: '',
		color: ''
	};
	let generalError = '';
	let isSaving = false;
	// the persisted color, used to revert an unsaved live preview on leave
	let savedColor = '';

	// push the chosen color to the banner store for a live preview while editing
	// only once loaded so the banner keeps its own value until we have the form
	$: if (loaded) {
		companyColorOverride.set({ companyID: companyId, color: formValues.color });
	}

	onDestroy(() => {
		// drop any unsaved preview, leaving the banner on the persisted color
		if (loaded) {
			companyColorOverride.set({ companyID: companyId, color: savedColor });
		} else {
			companyColorOverride.set(null);
		}
	});

	// auto-prune (saved on change, like display mode in settings)
	let autoPruneEnabled = false;
	let isSavingAutoPrune = false;

	// SCIM status shown in the Integrations tab
	let scimStatus = 'none'; // 'none' | 'disabled' | 'enabled'

	// report inbox shown in the Reporting tab, null when not configured
	let reportInbox = null;

	// whether PDF report generation is enabled globally; gates the Reports tab
	let reportPDFEnabled = false;

	// modals
	let isScimModalVisible = false;
	let isReportTemplateModalVisible = false;
	let isReportDeliveryModalVisible = false;
	let isReportInboxModalVisible = false;
	let isExportAlertVisible = false;
	let isDeleteAlertVisible = false;

	// tabs
	const tabs = [
		{ id: 'general', label: 'General' },
		{ id: 'stats', label: 'Custom Stats' },
		{ id: 'integrations', label: 'Integrations' },
		{ id: 'reporting', label: 'Reporting' },
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
		await Promise.all([
			loadCompany(),
			loadAutoPrune(),
			loadScimStatus(),
			loadReportInbox(),
			loadReportPDFEnabled()
		]);
	};

	const loadReportPDFEnabled = async () => {
		try {
			const res = await api.option.get('report_pdf_enabled');
			reportPDFEnabled = res.success && res.data?.value === 'true';
		} catch (_) {
			reportPDFEnabled = false;
		}
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
			formValues.color = company.color || '';
			savedColor = formValues.color;
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

	const loadReportInbox = async () => {
		try {
			const res = await api.company.reportInbox.getByCompanyID(companyId);
			if (res && res.success && res.data) {
				reportInbox = res.data;
			} else if (res && res.statusCode === 404) {
				reportInbox = null;
			}
		} catch (_) {
			// keep the last known status
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
			const res = await api.company.update(
				companyId,
				formValues.name,
				formValues.comment,
				formValues.color
			);
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
		<Breadcrumb crumbs={[{ label: 'Companies', href: '/company' }, { label: company?.name ?? '' }]} />
		<Headline docSlug="companies" preserveCase>{company?.name ?? 'Company'}</Headline>

		<TabBar items={tabs} {active} label="Company sections" on:select={(e) => selectTab(e.detail)} />

		<div class="pb-8">
			{#if active === 'general'}
				<div class="flex flex-wrap items-start gap-6">
					<SettingsCard title="Company Details" widthClass="w-full lg:w-[34rem]">
						<form on:submit|preventDefault={onSaveGeneral} class="flex flex-col flex-1">
							<TextField
								id="company-name"
								required
								width="full"
								minLength={1}
								maxLength={64}
								bind:value={formValues.name}>Company name</TextField
							>
							<TextareaField
								id="company-comment"
								optional
								fullWidth
								height="large"
								maxLength={1000000}
								bind:value={formValues.comment}
								placeholder="Notes about this company">Comment</TextareaField
							>
							<div class="flex flex-col py-2">
								<label for="company-color" class="flex">
									<FieldLabel optional>Banner color</FieldLabel>
								</label>
								<p class="text-gray-600 dark:text-gray-300 text-sm mb-3">
									Tints the banner and frame shown while viewing as this company, making it easier to
									recognize which company you are working in.
								</p>
								<div class="flex flex-wrap items-center gap-3">
									<input
										type="color"
										aria-label="Pick banner color"
										value={formValues.color || DEFAULT_COMPANY_COLOR}
										on:input={(e) => (formValues.color = e.currentTarget.value)}
										class="pc-focus h-9 w-12 rounded-md border border-gray-300 dark:border-gray-700 bg-transparent cursor-pointer"
									/>
									<input
										id="company-color"
										type="text"
										bind:value={formValues.color}
										placeholder={DEFAULT_COMPANY_COLOR}
										maxlength="7"
										aria-describedby={formValues.color ? undefined : 'company-color-state'}
										class="w-32 py-2 px-3 rounded-md font-mono text-sm text-slate-800 dark:text-gray-300 border border-transparent dark:border-gray-700/60 bg-grayblue-light dark:bg-gray-900/60 focus:outline-none focus:ring-2 focus:border-cta-blue focus:ring-cta-blue/30 dark:focus:border-highlight-blue dark:focus:ring-highlight-blue/30 transition-colors duration-200"
									/>
									{#if formValues.color}
										<button
											type="button"
											on:click={() => (formValues.color = '')}
											class="pc-focus rounded text-sm text-gray-500 dark:text-gray-400 hover:text-cta-blue dark:hover:text-highlight-blue transition-colors"
										>
											Reset to default
										</button>
									{:else}
										<span
											id="company-color-state"
											class="inline-flex items-center h-6 px-2 rounded text-xs bg-gray-100 text-slate-600 dark:bg-gray-700 dark:text-gray-300"
										>
											Default color
										</span>
									{/if}
								</div>
							</div>
							<FormError message={generalError} />
							<div class="mt-6 flex justify-end">
								<FormButton size="medium" isSubmitting={isSaving}>Save changes</FormButton>
							</div>
						</form>
					</SettingsCard>

					<SettingsCard title="Automatically Prune Orphaned Recipients" description="Choose whether orphaned recipients are removed automatically. Changes are saved right away.">
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
					<SettingsCard title="SCIM Provisioning" description="Automatically provision recipients from your identity provider.">
						<div class="flex items-center gap-2 mt-2">
							<span class="text-sm text-gray-500 dark:text-gray-400">Status</span>
							<StatusBadge
								tone={scimStatus === 'enabled'
									? 'success'
									: scimStatus === 'disabled'
										? 'warning'
										: 'neutral'}
							>
								{scimStatus === 'enabled'
									? 'Enabled'
									: scimStatus === 'disabled'
										? 'Disabled'
										: 'Not configured'}
							</StatusBadge>
						</div>
						<svelte:fragment slot="footer">
							<FormButton size="medium" on:click={() => (isScimModalVisible = true)}
								>Configure SCIM</FormButton
							>
						</svelte:fragment>
					</SettingsCard>
				</div>
			{:else if active === 'reporting'}
				<div class="flex flex-wrap gap-6">
					<SettingsCard title="Reporting Inbox" description="Set up automated report tracking with Microsoft 365.">
						<div class="mt-2">
							<ReportInboxStatus inbox={reportInbox} />
						</div>
						<svelte:fragment slot="footer">
							<FormButton size="medium" on:click={() => (isReportInboxModalVisible = true)}
								>{reportInbox ? 'Manage' : 'Configure'}</FormButton
							>
						</svelte:fragment>
					</SettingsCard>
				</div>
			{:else if active === 'reports'}
				{#if !reportPDFEnabled}
					<div
						class="rounded-md border border-amber-400 dark:border-amber-500/60 bg-amber-50 dark:bg-amber-900/20 p-4 max-w-xl"
					>
						<p class="text-sm text-amber-700 dark:text-amber-400">
							PDF report generation is disabled. Enable it under Settings → PDF Reports to configure
							report templates and delivery.
						</p>
					</div>
				{:else}
					<div class="flex flex-wrap gap-6">
						<SettingsCard title="Report Template" description="Override the global report template for this company.">
							<svelte:fragment slot="footer">
								<FormButton size="medium" on:click={() => (isReportTemplateModalVisible = true)}
									>Edit template</FormButton
								>
							</svelte:fragment>
						</SettingsCard>

						<SettingsCard title="Report Delivery" description="Email the campaign report PDF to a recipient group, on demand or when a campaign is closed.">
							<svelte:fragment slot="footer">
								<FormButton size="medium" on:click={() => (isReportDeliveryModalVisible = true)}
									>Configure</FormButton
								>
							</svelte:fragment>
						</SettingsCard>
					</div>

					<div class="mt-8">
						<SubHeadline>Delivery log</SubHeadline>
						<p class="text-gray-600 dark:text-gray-300 text-sm mt-1 mb-4">
							Reports sent for this company, including automatic and on demand deliveries.
						</p>
						<CompanyReportDeliveryLog companyId={companyId} />
					</div>
				{/if}
			{:else if active === 'data'}
				<div class="flex flex-wrap gap-6">
					<SettingsCard title="Export" description="Download a ZIP with all company data, recipients, and campaign events.">
						<svelte:fragment slot="footer">
							<FormButton size="medium" on:click={() => (isExportAlertVisible = true)}
								>Export data</FormButton
							>
						</svelte:fragment>
					</SettingsCard>
				</div>
			{:else if active === 'danger'}
				<div class="flex flex-wrap gap-6">
					<SettingsCard title="Delete Company" description="Permanently removes this company and all of its domains, campaigns, and recipients. This cannot be undone.">
						<svelte:fragment slot="footer">
							<Button
								variant="danger-outline"
								type="button"
								on:click={() => (isDeleteAlertVisible = true)}
								>Delete</Button
							>
						</svelte:fragment>
					</SettingsCard>
				</div>
			{/if}
		</div>
	{/if}

	<ScimModal bind:visible={isScimModalVisible} {company} />
	<CompanyReportTemplateModal bind:visible={isReportTemplateModalVisible} {company} />
	<CompanyReportDeliveryModal bind:visible={isReportDeliveryModalVisible} {company} />
	<CompanyReportInboxModal
		bind:visible={isReportInboxModalVisible}
		{company}
		on:change={(e) => (reportInbox = e.detail)}
	/>

	<Alert
		headline="Export company data"
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
