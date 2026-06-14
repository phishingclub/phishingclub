<script>
	import { onMount } from 'svelte';
	import { api } from '$lib/api/apiProxy.js';
	import { addToast } from '$lib/store/toast';
	import { hideIsLoading, showIsLoading } from '$lib/store/loading';
	import SettingsCard from '$lib/components/SettingsCard.svelte';
	import SettingsLoading from '$lib/components/SettingsLoading.svelte';
	import Button from '$lib/components/Button.svelte';
	import Modal from '$lib/components/Modal.svelte';
	import Alert from '$lib/components/Alert.svelte';
	import FormGrid from '$lib/components/FormGrid.svelte';
	import FormError from '$lib/components/FormError.svelte';
	import FormFooter from '$lib/components/FormFooter.svelte';
	import Editor from '$lib/components/editor/Editor.svelte';
	import CompanyReportDeliveryModal from '$lib/components/modal/CompanyReportDeliveryModal.svelte';

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

	onMount(async () => {
		try {
			await refreshReportPDFEnabled();
		} finally {
			loaded = true;
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
			reportTemplateContent = '';
			reportTemplateID = null;
			reportTemplateError = '';
			const response = await api.reportTemplate.getAll(null);
			if (response.success && response.data?.rows?.length > 0) {
				const tmpl = response.data.rows[0];
				reportTemplateContent = tmpl.content || '';
				reportTemplateID = tmpl.id || null;
			}
		} catch (error) {
			console.error('Failed to load report template:', error);
			reportTemplateError = 'Failed to load template';
		} finally {
			hideIsLoading();
			isReportTemplateModalVisible = true;
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
				response = await api.reportTemplate.create({ content: reportTemplateContent });
				if (response.success && response.data?.id) {
					reportTemplateID = response.data.id;
				}
			}
			if (response.success) {
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

{#if !loaded}
	<SettingsLoading />
{:else}
<div class="flex flex-wrap gap-6">
	<SettingsCard title="PDF Reports">
		<div class="space-y-4">
			<p class="text-gray-600 dark:text-gray-300 text-sm transition-colors duration-200">
				Generate PDF reports for campaigns. Requires Chromium and system dependencies.
			</p>
			<p
				class="text-sm font-medium transition-colors duration-200"
				class:text-green-600={isReportPDFEnabled}
				class:dark:text-green-400={isReportPDFEnabled}
				class:text-gray-500={!isReportPDFEnabled}
				class:dark:text-gray-400={!isReportPDFEnabled}
			>
				{isReportPDFEnabled ? 'Enabled' : 'Disabled'}
			</p>
		</div>
		<svelte:fragment slot="footer">
			<Button
				size={'large'}
				backgroundColor={isReportPDFEnabled ? 'bg-red-600' : 'bg-cta-blue'}
				disabled={isTogglingReportPDF}
				on:click={onClickReportPDFToggle}
			>
				{isReportPDFEnabled ? 'Disable' : 'Enable'}
			</Button>
		</svelte:fragment>
	</SettingsCard>

	{#if isReportPDFEnabled}
		<SettingsCard title="Report Template">
			<p class="text-gray-600 dark:text-gray-300 text-sm transition-colors duration-200">
				Default HTML template used when generating campaign PDF reports. Companies without their own
				template fall back to this.
			</p>
			<svelte:fragment slot="footer">
				<Button size={'large'} on:click={openReportTemplateModal}>Edit Template</Button>
			</svelte:fragment>
		</SettingsCard>

		<SettingsCard title="Report Delivery">
			<p class="text-gray-600 dark:text-gray-300 text-sm transition-colors duration-200">
				Default delivery settings used to email campaign reports. Companies without their own
				configuration fall back to this.
			</p>
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
		<FormGrid
			on:submit={onSubmitReportTemplate}
			isSubmitting={isReportTemplateSubmitting}
			modalMode="update"
		>
			<div
				class="w-80vw col-start-1 col-end-4 row-start-1 py-8 px-6 flex flex-col bg-white dark:bg-gray-800 text-gray-900 dark:text-gray-100 transition-colors duration-200"
			>
				<Editor contentType="report" bind:value={reportTemplateContent} />
				<FormError message={reportTemplateError} />
			</div>
			<FormFooter isSubmitting={isReportTemplateSubmitting} closeModal={closeReportTemplateModal} />
		</FormGrid>
	</Modal>
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
					class="underline">the setup guide</a
				> for dependency installation and AppArmor configuration.
			</p>
		</div>
	</Alert>
{/if}
