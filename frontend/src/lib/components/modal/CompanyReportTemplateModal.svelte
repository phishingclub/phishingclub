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
		try {
			showIsLoading();
			const response = await api.reportTemplate.getAll(company.id);
			if (response.success && response.data?.rows?.length > 0) {
				const tmpl = response.data.rows[0];
				content = tmpl.content || '';
				templateID = tmpl.id || null;
			}
		} catch (e) {
			console.error('Failed to load company report template:', e);
			error = 'Failed to load template';
		} finally {
			hideIsLoading();
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
					companyID: company.id
				});
				if (response.success && response.data?.id) {
					templateID = response.data.id;
				}
			}
			if (response.success) {
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
