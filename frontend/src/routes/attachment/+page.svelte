<script>
	import { api } from '$lib/api/apiProxy.js';
	import { onMount } from 'svelte';
	import { globalButtonDisabledAttributes } from '$lib/utils/form.js';
	import Headline from '$lib/components/Headline.svelte';
	import TextField from '$lib/components/TextField.svelte';
	import TableRow from '$lib/components/table/TableRow.svelte';
	import TableDeleteButton from '$lib/components/table/TableDeleteButton2.svelte';
	import TableCell from '$lib/components/table/TableCell.svelte';
	import { addToast } from '$lib/store/toast';
	import FormError from '$lib/components/FormError.svelte';
	import { AppStateService } from '$lib/service/appState';
	import { newTableURLParams } from '$lib/service/tableURLParams.js';
	import Modal from '$lib/components/Modal.svelte';
	import FormGrid from '$lib/components/FormGrid.svelte';
	import TableCellEmpty from '$lib/components/table/TableCellEmpty.svelte';
	import TableCellAction from '$lib/components/table/TableCellAction.svelte';
	import TableUpdateButton from '$lib/components/table/TableUpdateButton.svelte';
	import BigButton from '$lib/components/BigButton.svelte';
	import FormColumns from '$lib/components/FormColumns.svelte';
	import TableCellScope from '$lib/components/table/TableCellScope.svelte';
	import FormColumn from '$lib/components/FormColumn.svelte';
	import FormFooter from '$lib/components/FormFooter.svelte';
	import Table from '$lib/components/table/Table.svelte';
	import TableViewButton from '$lib/components/table/TableViewButton.svelte';
	import HeadTitle from '$lib/components/HeadTitle.svelte';
	import { showIsLoading, hideIsLoading } from '$lib/store/loading.js';
	import TableCellCheck from '$lib/components/table/TableCellCheck.svelte';
	import CheckboxField from '$lib/components/CheckboxField.svelte';
	import TableDropDownEllipsis from '$lib/components/table/TableDropDownEllipsis.svelte';
	import DeleteAlert from '$lib/components/modal/DeleteAlert.svelte';
	import FileField from '$lib/components/FileField.svelte';

	// services
	const appStateService = AppStateService.instance;

	let contextCompanyID = null; // companyID or if empty the context is the global context
	let form = null;
	let formValues = {
		name: '',
		description: '',
		embeddedContent: false,
		sendAsCalendar: false,
		fileName: ''
	};

	// the send as calendar option only applies to ics calendar files
	let showCalendarOption = false;
	const isCalendarFileName = (name) => {
		if (!name) {
			return false;
		}
		const lower = name.toLowerCase();
		return lower.endsWith('.ics') || lower.endsWith('.ical');
	};
	const onFilesSelected = (e) => {
		const files = e.target.files;
		showCalendarOption = Array.from(files).some((f) => isCalendarFileName(f.name));
		if (!showCalendarOption) {
			formValues.sendAsCalendar = false;
		}
	};

	let attachments = [];
	let attachmentsHasNextPage = false;
	let isModalVisible = false;
	let isSubmitting = false;
	const tableURLParams = newTableURLParams();
	// @type {null|'create'|'update'}
	let modalMode = null;
	let modalError = '';

	let isDeleteAlertVisible = false;
	let deleteValues = {
		id: null,
		name: null
	};

	let modalText = '';
	$: {
		modalText = modalMode === 'create' ? 'New attachment' : 'Update attachment';
	}

	// hooks
	onMount(() => {
		const context = appStateService.getContext();
		if (context) {
			contextCompanyID = context.companyID ?? '';
		}
		refreshAttachments();
		tableURLParams.onChange(refreshAttachments);

		return () => {
			tableURLParams.unsubscribe();
		};
	});

	// component logic
	const refreshAttachments = async () => {
		try {
			showIsLoading();
			const res = await api.attachment.getByContext(contextCompanyID, tableURLParams);
			attachments = res.data.rows ?? [];
			attachmentsHasNextPage = res.data.hasNextPage;
		} catch (e) {
			addToast('Failed to get attachments', 'Error');
			console.error('failed to get attachments', e);
		} finally {
			hideIsLoading();
		}
	};

	const onClickUpdate = async () => {
		try {
			const res = await api.attachment.update(formValues);
			if (!res.success) {
				modalError = res.error;
				return;
			}
			addToast('Updated attachment', 'Success');
			refreshAttachments();
			closeModal();
		} catch (e) {
			addToast('Failed to update attachment', 'Error');
			console.error('failed to update attachment', e);
		}
	};

	/** @param {string} id */
	const onClickDelete = async (id) => {
		const action = api.attachment.delete(id);
		action
			.then((res) => {
				if (!res.success) {
					throw res.error;
				}
				refreshAttachments();
				return;
			})
			.catch((e) => {
				console.error('failed to delete attachment email', e);
			});
		return action;
	};

	const openDeleteAlert = async (domain) => {
		isDeleteAlertVisible = true;
		deleteValues.id = domain.id;
		deleteValues.name = domain.name;
	};

	const onSubmit = async () => {
		try {
			isSubmitting = true;
			if (modalMode === 'create') {
				await onClickCreate();
				return;
			} else {
				await onClickUpdate();
				return;
			}
		} finally {
			isSubmitting = false;
		}
	};

	const onClickCreate = async () => {
		try {
			/** @type {HTMLInputElement} */
			let fileInput = document.querySelector('#files');
			let formData = new FormData();
			for (let file of fileInput.files) {
				formData.append('files', file);
			}
			formData.append('name', formValues.name);
			formData.append('description', formValues.description);
			formData.append('embeddedContent', formValues.embeddedContent ? 'true' : 'false');
			formData.append('sendAsCalendar', formValues.sendAsCalendar ? 'true' : 'false');
			if (contextCompanyID) {
				formData.append('companyID', contextCompanyID);
			}
			// Send the data to the server
			const res = await api.attachment.upload(formData);
			if (!res.success) {
				modalError = res.error;
				return;
			}
			closeModal();
			addToast('Created attachment', 'Success');
			refreshAttachments();
		} catch (e) {
			addToast('Failed to create attachment', 'Error');
			console.error('failed to create attachment', e);
		}
	};

	const openCreateModal = () => {
		modalMode = 'create';
		isModalVisible = true;
	};

	/**
	 * Show the update modal
	 * @param {string} id
	 */
	const openUpdateModal = async (id) => {
		modalMode = 'update';
		// get the attachment
		try {
			showIsLoading();
			const res = await api.attachment.getByID(id);
			if (!res.success) {
				addToast('Failed to get attachment', 'Error');
				console.error('failed to get attachment', res.error);
			}
			formValues.id = res.data.id;
			formValues.name = res.data.name;
			formValues.description = res.data.description;
			formValues.embeddedContent = res.data.embeddedContent;
			formValues.sendAsCalendar = res.data.sendAsCalendar;
			formValues.fileName = res.data.fileName;
			showCalendarOption = isCalendarFileName(res.data.fileName);
			isModalVisible = true;
		} catch (e) {
			addToast('Failed to get attachment', 'Error');
			console.error('failed to get attachment', e);
		} finally {
			hideIsLoading();
		}
	};

	const closeModal = () => {
		modalError = '';
		form.reset();
		formValues.name = '';
		formValues.description = '';
		formValues.embeddedContent = false;
		formValues.sendAsCalendar = false;
		formValues.fileName = '';
		showCalendarOption = false;
		isModalVisible = false;
	};

	const onClickPreview = async (id) => {
		try {
			showIsLoading();
			const res = await api.attachment.getContentByID(id);
			if (!res.success) {
				throw res.error;
			}
			const binaryData = atob(res.data.file);
			const byteArray = new Uint8Array(binaryData.length);
			for (let i = 0; i < binaryData.length; i++) {
				byteArray[i] = binaryData.charCodeAt(i);
			}
			const blob = new Blob([byteArray], { type: res.data.mimeType });
			const url = URL.createObjectURL(blob);
			window.open(url, '_blank');
		} catch (e) {
			addToast('Failed to get attachment content', 'Error');
			console.error('failed to get attachment content', e);
		} finally {
			hideIsLoading();
		}
	};
</script>

<HeadTitle title="Attachments" />
<main>
	<Headline>Attachments</Headline>
	<BigButton on:click={openCreateModal}>New attachment</BigButton>
	<Table
		columns={[
			'Name',
			'Description',
			'Filename',
			{ column: 'Embedded Content', alignText: 'center' },
			{ column: 'Send As Calendar', alignText: 'center' },
			...(contextCompanyID ? [{ column: 'Scope', size: 'small' }] : [])
		]}
		sortable={[
			'Name',
			'Description',
			'Filename',
			'Embedded Content',
			'Send As Calendar',
			...(contextCompanyID ? ['scope'] : [])
		]}
		hasData={!!attachments.length}
		hasNextPage={attachmentsHasNextPage}
		plural="attachments"
		pagination={tableURLParams}
	>
		{#each attachments as attachment}
			<TableRow>
				<TableCell>
					{#if attachment.name}
						<button
							on:click={() => {
								openUpdateModal(attachment.id);
							}}
							{...globalButtonDisabledAttributes(attachment, contextCompanyID)}
							title={attachment.name}
							class="block w-full py-1 text-left"
						>
							{attachment.name}
						</button>
					{/if}
				</TableCell>
				<TableCell>
					{#if attachment.description}
						<button
							on:click={() => {
								openUpdateModal(attachment.id);
							}}
							{...globalButtonDisabledAttributes(attachment, contextCompanyID)}
							title={attachment.name}
							class="block w-full py-1 text-left"
						>
							{attachment.description}
						</button>
					{/if}
				</TableCell>
				<TableCell>
					{#if attachment.fileName}
						<button
							on:click={() => {
								openUpdateModal(attachment.id);
							}}
							{...globalButtonDisabledAttributes(attachment, contextCompanyID)}
							title={attachment.name}
							class="block w-full py-1 text-left"
						>
							{attachment.fileName}
						</button>
					{/if}
				</TableCell>
				<TableCellCheck value={attachment.embeddedContent} />
				<TableCellCheck value={attachment.sendAsCalendar} />
				{#if contextCompanyID}
					<TableCellScope companyID={attachment.companyID} />
				{/if}
				<TableCellEmpty />
				<TableCellAction>
					<TableDropDownEllipsis>
						<TableViewButton on:click={() => onClickPreview(attachment.id)} />
						<TableUpdateButton
							on:click={() => openUpdateModal(attachment.id)}
							{...globalButtonDisabledAttributes(attachment, contextCompanyID)}
						/>
						<TableDeleteButton
							on:click={() => openDeleteAlert(attachment)}
							{...globalButtonDisabledAttributes(attachment, contextCompanyID)}
						></TableDeleteButton>
					</TableDropDownEllipsis>
				</TableCellAction>
			</TableRow>
		{/each}
	</Table>
	<Modal headerText={modalText} visible={isModalVisible} onClose={closeModal} {isSubmitting}>
		<FormGrid on:submit={onSubmit} bind:bindTo={form} {isSubmitting}>
			<FormColumns>
				<FormColumn>
					<TextField
						minLength={1}
						maxLength={127}
						bind:value={formValues.name}
						optional={true}
						placeholder={'Candidate CV'}>Name</TextField
					>
					<TextField
						minLength={1}
						maxLength={255}
						bind:value={formValues.description}
						optional={true}
						placeholder="Fake CV with embedded link">Description</TextField
					>
					<CheckboxField
						bind:value={formValues.embeddedContent}
						defaultValue={false}
						optional
						toolTipText="File contains template variables">Embedded content</CheckboxField
					>
					{#if showCalendarOption}
						<CheckboxField
							bind:value={formValues.sendAsCalendar}
							defaultValue={false}
							optional
							toolTipText="Send ics files as a calendar invitation so Outlook shows Accept, Tentative and Decline">Send as calendar</CheckboxField
						>
					{/if}
					{#if modalMode === 'create'}
						<FileField name="files" multiple on:change={onFilesSelected}>Files</FileField>
					{/if}
				</FormColumn>
			</FormColumns>
			<FormError message={modalError} />
			<FormFooter {closeModal} {isSubmitting} />
		</FormGrid>
	</Modal>
	<DeleteAlert
		name={deleteValues.name}
		onClick={() => onClickDelete(deleteValues.id)}
		bind:isVisible={isDeleteAlertVisible}
	></DeleteAlert>
</main>
