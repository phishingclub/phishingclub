<script>
	import { api } from '$lib/api/apiProxy.js';
	import { addToast } from '$lib/store/toast';
	import { hideIsLoading, showIsLoading } from '$lib/store/loading';
	import SettingsCard from '$lib/components/SettingsCard.svelte';
	import Button from '$lib/components/Button.svelte';
	import Modal from '$lib/components/Modal.svelte';
	import FormGrid from '$lib/components/FormGrid.svelte';
	import FormError from '$lib/components/FormError.svelte';
	import FormFooter from '$lib/components/FormFooter.svelte';
	import SimpleCodeEditor from '$lib/components/editor/SimpleCodeEditor.svelte';

	let isObfuscationTemplateModalVisible = false;
	let obfuscationTemplate = '';
	let obfuscationTemplateError = '';
	let isObfuscationTemplateSubmitting = false;

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

	const closeObfuscationTemplateModal = () => {
		isObfuscationTemplateModalVisible = false;
		obfuscationTemplateError = '';
	};

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

<div class="flex flex-wrap gap-6">
	<SettingsCard title="Obfuscation Template">
		<div class="space-y-4">
			<p class="text-gray-600 dark:text-gray-300 text-sm transition-colors duration-200">
				Customize the template used when obfuscation is enabled to.
			</p>
			<div class="bg-gray-50 dark:bg-gray-700 p-3 rounded-md transition-colors duration-200">
				<p class="text-sm text-gray-700 dark:text-gray-300 mb-2">
					<strong>Internal obfuscation variable:</strong>
				</p>
				<p class="text-xs text-gray-600 dark:text-gray-400 font-mono">
					{'{{.Script}}'}
				</p>
			</div>
		</div>
		<svelte:fragment slot="footer">
			<Button size={'large'} on:click={openObfuscationTemplateModal}>Edit Template</Button>
		</svelte:fragment>
	</SettingsCard>
</div>

{#if isObfuscationTemplateModalVisible}
	<Modal
		bind:visible={isObfuscationTemplateModalVisible}
		headerText="Edit Obfuscation Template"
		onClose={closeObfuscationTemplateModal}
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
