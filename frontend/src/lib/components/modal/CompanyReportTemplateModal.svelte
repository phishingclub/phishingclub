<script>
	import { api } from '$lib/api/apiProxy.js';
	import { addToast } from '$lib/store/toast';
	import { showIsLoading, hideIsLoading } from '$lib/store/loading.js';
	import Modal from '../Modal.svelte';
	import FormGrid from '../FormGrid.svelte';
	import FormFooter from '../FormFooter.svelte';
	import FormError from '../FormError.svelte';
	import Editor from '../editor/Editor.svelte';

	// external
	export let visible = false;
	/** @type {{ id: string, name: string } | null} */
	export let company = null;

	// local state
	let content = '';
	let templateID = null;
	let error = '';
	let isSubmitting = false;
	let loadedForCompanyID = null;
	// the phishing and training reports are separate templates; edit one at a time
	let rows = [];
	let reportKind = 'phishing'; // 'phishing' | 'training'

	// reactive: load the company template when the modal opens
	$: {
		if (visible && company && loadedForCompanyID !== company.id) {
			loadedForCompanyID = company.id;
			load();
		}
		if (!visible) {
			loadedForCompanyID = null;
		}
	}

	const load = async () => {
		content = '';
		templateID = null;
		error = '';
		rows = [];
		try {
			showIsLoading();
			const response = await api.reportTemplate.getAll(company.id);
			rows = response.success ? response.data?.rows || [] : [];
			selectReportKind('phishing');
		} catch (e) {
			console.error('Failed to load company report template:', e);
			error = 'Failed to load template';
		} finally {
			hideIsLoading();
		}
	};

	// loads the cached template row for the selected kind into the editor
	const selectReportKind = (kind) => {
		reportKind = kind;
		error = '';
		const row = rows.find((r) => !!r.isTraining === (kind === 'training'));
		content = row?.content || '';
		templateID = row?.id || null;
	};

	// re-fetch the templates after a save so the active kind keeps its id
	const refreshRows = async () => {
		const response = await api.reportTemplate.getAll(company.id);
		rows = response.success ? response.data?.rows || [] : [];
		const row = rows.find((r) => !!r.isTraining === (reportKind === 'training'));
		if (row?.id) {
			templateID = row.id;
		}
	};

	const close = () => {
		visible = false;
		error = '';
	};

	const onSubmit = async (event) => {
		const saveOnly = event?.detail?.saveOnly || false;
		isSubmitting = true;
		error = '';
		try {
			let response;
			if (templateID) {
				response = await api.reportTemplate.update(templateID, { content });
			} else {
				response = await api.reportTemplate.create({
					content,
					companyID: company.id,
					isTraining: reportKind === 'training'
				});
				if (response.success && response.data?.id) {
					templateID = response.data.id;
				}
			}
			if (response.success) {
				await refreshRows();
				addToast('Report template saved', 'Success');
				if (!saveOnly) {
					visible = false;
				}
			} else {
				error = response.error || 'Failed to save template';
			}
		} catch (e) {
			console.error('Failed to save company report template:', e);
			error = 'Failed to save template';
		} finally {
			isSubmitting = false;
		}
	};

	const onDelete = async () => {
		if (!templateID) return;
		isSubmitting = true;
		try {
			const response = await api.reportTemplate.delete(templateID);
			if (response.success) {
				addToast('Report template deleted', 'Success');
				rows = rows.filter((r) => r.id !== templateID);
				templateID = null;
				content = '';
				visible = false;
			} else {
				error = response.error || 'Failed to delete template';
			}
		} catch (e) {
			console.error('Failed to delete company report template:', e);
			error = 'Failed to delete template';
		} finally {
			isSubmitting = false;
		}
	};
</script>

{#if visible}
	<Modal bind:visible headerText="Report Template — {company?.name}" onClose={close}>
		<FormGrid on:submit={onSubmit} {isSubmitting} modalMode="update">
			<div
				class="w-80vw col-start-1 col-end-4 row-start-1 py-8 px-6 flex flex-col bg-white dark:bg-gray-800 text-gray-900 dark:text-gray-100 transition-colors duration-200"
			>
				<div class="flex items-center gap-2 mb-4">
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
				</div>
				<Editor contentType="report" bind:value={content} />
				<FormError message={error} />
				{#if templateID}
					<div class="mt-4">
						<button
							type="button"
							class="text-sm text-red-600 dark:text-red-400 hover:underline"
							on:click={onDelete}
							disabled={isSubmitting}
						>
							Delete company template (fall back to global)
						</button>
					</div>
				{/if}
			</div>
			<FormFooter {isSubmitting} closeModal={close} />
		</FormGrid>
	</Modal>
{/if}
