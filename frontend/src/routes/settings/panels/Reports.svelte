<script>
	import { onMount } from 'svelte';
	import { api } from '$lib/api/apiProxy.js';
	import { addToast } from '$lib/store/toast';
	import { hideIsLoading, showIsLoading } from '$lib/store/loading';
	import SettingsCard from '$lib/components/SettingsCard.svelte';
	import StatusBadge from '$lib/components/StatusBadge.svelte';
	import Button from '$lib/components/Button.svelte';
	import ActionButton from '$lib/components/ActionButton.svelte';
	import Modal from '$lib/components/Modal.svelte';
	import InstalledResourceBadge from '$lib/components/InstalledResourceBadge.svelte';
	import Alert from '$lib/components/Alert.svelte';
	import FormGrid from '$lib/components/FormGrid.svelte';
	import FormError from '$lib/components/FormError.svelte';
	import FormFooter from '$lib/components/FormFooter.svelte';
	import Editor from '$lib/components/editor/Editor.svelte';
	import CompanyReportDeliveryModal from '$lib/components/modal/CompanyReportDeliveryModal.svelte';
	import ReportPreviewPDF from '$lib/components/ReportPreviewPDF.svelte';

	let loaded = false;

	// PDF reports
	let isReportPDFEnabled = false;
	let isReportPDFEnableModalVisible = false;
	let isTogglingReportPDF = false;

	// global default report delivery
	let isReportDeliveryModalVisible = false;

	// report template
	let isReportTemplateModalVisible = false;
	let reportTemplateContent = '';
	let reportTemplateID = null;
	let reportTemplateError = '';
	let isReportTemplateSubmitting = false;
	// the phishing and training reports are separate templates; edit one at a time
	let reportRows = [];
	let reportKind = 'phishing'; // 'phishing' | 'training'
	let reportEditor;
	let isUseDefaultAlertVisible = false;

	onMount(async () => {
		showIsLoading();
		try {
			await refreshReportPDFEnabled();
		} finally {
			loaded = true;
			hideIsLoading();
		}
	});

	const refreshReportPDFEnabled = async () => {
		const response = await api.option.get('report_pdf_enabled');
		isReportPDFEnabled = response.success && response.data?.value === 'true';
	};

	const onClickReportPDFToggle = () => {
		if (isReportPDFEnabled) {
			onDisableReportPDF();
		} else {
			isReportPDFEnableModalVisible = true;
		}
	};

	const onDisableReportPDF = async () => {
		isTogglingReportPDF = true;
		try {
			const response = await api.option.set('report_pdf_enabled', 'false');
			if (response.success) {
				isReportPDFEnabled = false;
				addToast('PDF reports disabled', 'Success');
			} else {
				addToast(response.error || 'Failed to update setting', 'Error');
			}
		} catch (e) {
			addToast('Failed to update setting', 'Error');
		} finally {
			isTogglingReportPDF = false;
		}
	};

	const onConfirmEnableReportPDF = async () => {
		isTogglingReportPDF = true;
		try {
			const response = await api.option.set('report_pdf_enabled', 'true');
			if (response.success) {
				isReportPDFEnabled = true;
				isReportPDFEnableModalVisible = false;
				addToast('PDF reports enabled', 'Success');
			} else {
				addToast(response.error || 'Failed to update setting', 'Error');
			}
		} catch (e) {
			addToast('Failed to update setting', 'Error');
		} finally {
			isTogglingReportPDF = false;
		}
	};

	const openReportTemplateModal = async () => {
		try {
			showIsLoading();
			reportTemplateError = '';
			reportKind = 'phishing';
			reportDrafts = {};
			const response = await api.reportTemplate.getAll(null);
			reportRows = response.success ? response.data?.rows || [] : [];
			loadReportKind('phishing');
		} catch (error) {
			console.error('Failed to load report template:', error);
			reportTemplateError = 'Failed to load template';
		} finally {
			hideIsLoading();
			isReportTemplateModalVisible = true;
		}
	};

	// unsaved editor content per kind, kept while the modal is open so
	// switching kind does not lose edits
	let reportDrafts = {};

	// switches the editor to another kind, keeping the edits of the current one
	const selectReportKind = (kind) => {
		if (kind === reportKind) return;
		reportDrafts[reportKind] = reportTemplateContent;
		loadReportKind(kind);
	};

	// loads the kind's unsaved edits, or its stored template, into the editor
	const loadReportKind = (kind) => {
		reportKind = kind;
		reportTemplateError = '';
		const row = reportRows.find((r) => !!r.isTraining === (kind === 'training'));
		reportTemplateContent = reportDrafts[kind] ?? (row?.content || '');
		reportTemplateID = row?.id || null;
		reportEditor?.replaceContent(reportTemplateContent);
	};

	// replaces the editor content with the built in default for the selected
	// kind. nothing is stored until the user saves
	const onConfirmUseDefault = async () => {
		const response = await api.reportTemplate.getDefault(reportKind === 'training');
		if (response.success) {
			reportTemplateContent = response.data?.content || '';
			reportEditor?.replaceContent(reportTemplateContent, true);
			addToast('Default loaded, click Save to keep it', 'Info');
		}
		return response;
	};

	// re-fetch the templates after a save so the active kind keeps its id
	const refreshReportRows = async () => {
		const response = await api.reportTemplate.getAll(null);
		reportRows = response.success ? response.data?.rows || [] : [];
		const row = reportRows.find((r) => !!r.isTraining === (reportKind === 'training'));
		if (row?.id) {
			reportTemplateID = row.id;
		}
	};

	const closeReportTemplateModal = () => {
		isReportTemplateModalVisible = false;
		reportTemplateError = '';
	};

	const onSubmitReportTemplate = async (event) => {
		const saveOnly = event?.detail?.saveOnly || false;
		isReportTemplateSubmitting = true;
		reportTemplateError = '';
		try {
			let response;
			if (reportTemplateID) {
				response = await api.reportTemplate.update(reportTemplateID, {
					content: reportTemplateContent
				});
			} else {
				response = await api.reportTemplate.create({
					content: reportTemplateContent,
					isTraining: reportKind === 'training'
				});
				if (response.success && response.data?.id) {
					reportTemplateID = response.data.id;
				}
			}
			if (response.success) {
				await refreshReportRows();
				addToast('Report template saved', 'Success');
				if (!saveOnly) {
					isReportTemplateModalVisible = false;
				}
			} else {
				reportTemplateError = response.error || 'Failed to save template';
			}
		} catch (error) {
			console.error('Failed to save report template:', error);
			reportTemplateError = 'Failed to save template';
		} finally {
			isReportTemplateSubmitting = false;
		}
	};
</script>

{#if loaded}
<div class="flex flex-wrap gap-6">
	<SettingsCard title="PDF Reports" description="Generate PDF reports for campaigns. Requires Chromium and system dependencies.">
		<div class="mt-2 space-y-4">
			<div class="flex items-center gap-2">
				<span class="text-sm text-gray-500 dark:text-gray-400">Status</span>
				<StatusBadge tone={isReportPDFEnabled ? 'success' : 'neutral'}>
					{isReportPDFEnabled ? 'Enabled' : 'Disabled'}
				</StatusBadge>
			</div>
		</div>
		<svelte:fragment slot="footer">
			<Button
				size={'large'}
				variant={isReportPDFEnabled ? 'danger-outline' : 'primary'}
				disabled={isTogglingReportPDF}
				on:click={onClickReportPDFToggle}
			>
				{isReportPDFEnabled ? 'Disable' : 'Enable'}
			</Button>
		</svelte:fragment>
	</SettingsCard>

	{#if isReportPDFEnabled}
		<SettingsCard title="Report Template" description="Default HTML template used when generating campaign PDF reports. Companies without their own template fall back to this.">
			<svelte:fragment slot="footer">
				<Button size={'large'} on:click={openReportTemplateModal}>Edit Template</Button>
			</svelte:fragment>
		</SettingsCard>

		<SettingsCard title="Report Delivery" description="Default delivery settings used to email campaign reports. Companies without their own configuration fall back to this.">
			<svelte:fragment slot="footer">
				<Button size={'large'} on:click={() => (isReportDeliveryModalVisible = true)}
					>Configure</Button
				>
			</svelte:fragment>
		</SettingsCard>
	{/if}
</div>
{/if}

<CompanyReportDeliveryModal bind:visible={isReportDeliveryModalVisible} isGlobal={true} />

{#if isReportTemplateModalVisible}
	<Modal
		bind:visible={isReportTemplateModalVisible}
		headerText="Edit Report Template"
		onClose={closeReportTemplateModal}
	>
		{#if reportTemplateID}
			<InstalledResourceBadge
				resourceType="reporttemplate"
				resourceId={reportTemplateID}
				variant="notice"
			/>
		{/if}
		<FormGrid
			on:submit={onSubmitReportTemplate}
			isSubmitting={isReportTemplateSubmitting}
			modalMode="update"
		>
			<div
				class="w-80vw max-w-full col-start-1 col-end-4 row-start-1 py-8 px-6 flex flex-col bg-white dark:bg-gray-800 text-gray-900 dark:text-gray-100 transition-colors duration-200"
			>
				<div class="flex flex-wrap items-center gap-2 mb-4">
					<span class="text-sm font-semibold text-gray-600 dark:text-gray-300">Report for</span>
					<div
						class="flex items-center rounded overflow-hidden border border-gray-300 dark:border-gray-600 text-sm"
					>
						<button
							type="button"
							class="px-3 py-1 transition-colors duration-200 {reportKind === 'phishing'
								? 'bg-cta-blue text-white'
								: 'text-gray-700 dark:text-gray-300 hover:bg-gray-100 dark:hover:bg-gray-700'}"
							on:click={() => selectReportKind('phishing')}
						>
							Phishing
						</button>
						<button
							type="button"
							class="px-3 py-1 transition-colors duration-200 {reportKind === 'training'
								? 'bg-training-completed text-white'
								: 'text-gray-700 dark:text-gray-300 hover:bg-gray-100 dark:hover:bg-gray-700'}"
							on:click={() => selectReportKind('training')}
						>
							Training
						</button>
					</div>
					<div class="ml-auto flex flex-wrap items-center justify-end gap-2">
						<ReportPreviewPDF
							content={reportTemplateContent}
							isTraining={reportKind === 'training'}
						/>
						<ActionButton
							variant="secondary"
							icon="reset"
							on:click={() => (isUseDefaultAlertVisible = true)}
						>
							Use Latest Default
						</ActionButton>
					</div>
				</div>
				<Editor
					bind:this={reportEditor}
					contentType="report"
					bind:value={reportTemplateContent}
				/>
				<FormError message={reportTemplateError} />
			</div>
			<FormFooter isSubmitting={isReportTemplateSubmitting} closeModal={closeReportTemplateModal} />
		</FormGrid>
	</Modal>
{/if}

{#if isUseDefaultAlertVisible}
	<Alert
		headline="Use Latest Default"
		bind:visible={isUseDefaultAlertVisible}
		onConfirm={onConfirmUseDefault}
		ok="Replace"
	>
		<div class="mt-4 text-gray-700 dark:text-gray-200 space-y-3">
			<p>
				Replace the {reportKind} report template in the editor with the latest built in default?
			</p>
			<p>Nothing is saved until you click Save. Cancel keeps the stored template.</p>
		</div>
	</Alert>
{/if}

{#if isReportPDFEnableModalVisible}
	<Alert
		headline="Enable PDF Reports"
		bind:visible={isReportPDFEnableModalVisible}
		onConfirm={onConfirmEnableReportPDF}
		ok="Enable"
	>
		<div class="mt-4 text-gray-700 dark:text-gray-200 space-y-3">
			<p>
				PDF report generation requires Chromium and additional system dependencies that are not part
				of the standard installation.
			</p>
			<p>
				Before enabling, ensure the host has the required libraries and any AppArmor restrictions on
				unprivileged user namespaces have been addressed.
			</p>
			<p>
				See <a
					href="https://phishing.club/guide/settings/#pdf-reports"
					target="_blank"
					rel="noopener noreferrer"
					class="underline">the setup guide</a
				> for dependency installation and AppArmor configuration.
			</p>
		</div>
	</Alert>
{/if}
