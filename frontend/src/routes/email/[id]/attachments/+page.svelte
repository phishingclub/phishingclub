<script>
	import { page } from '$app/stores';
	import { onMount } from 'svelte';
	import { api } from '$lib/api/apiProxy.js';
	import Headline from '$lib/components/Headline.svelte';
	import { addToast } from '$lib/store/toast';
	import FormError from '$lib/components/FormError.svelte';
	import { AppStateService } from '$lib/service/appState';
	import { fetchAllRows } from '$lib/utils/api-utils';
	import TableRow from '$lib/components/table/TableRow.svelte';
	import TableCell from '$lib/components/table/TableCell.svelte';
	import TableDeleteButton from '$lib/components/table/TableDeleteButton2.svelte';
	import TableCellEmpty from '$lib/components/table/TableCellEmpty.svelte';
	import TextFieldMultiSelect from '$lib/components/TextFieldMultiSelect.svelte';
	import CheckboxField from '$lib/components/CheckboxField.svelte';
	import TableCellCheck from '$lib/components/table/TableCellCheck.svelte';
	import Modal from '$lib/components/Modal.svelte';
	import FormGrid from '$lib/components/FormGrid.svelte';
	import { BiMap } from '$lib/utils/maps';
	import TableCellAction from '$lib/components/table/TableCellAction.svelte';
	import Table from '$lib/components/table/Table.svelte';
	import BigButton from '$lib/components/BigButton.svelte';
	import FormColumns from '$lib/components/FormColumns.svelte';
	import FormColumn from '$lib/components/FormColumn.svelte';
	import FormFooter from '$lib/components/FormFooter.svelte';
	import HeadTitle from '$lib/components/HeadTitle.svelte';
	import { newTableParams } from '$lib/service/tableParams';
	import { getPaginatedChunkWithParams } from '$lib/service/paginationChunk';
	import TableDropDownEllipsis from '$lib/components/table/TableDropDownEllipsis.svelte';
	import DeleteAlert from '$lib/components/modal/DeleteAlert.svelte';
	import { globalButtonDisabledAttributes } from '$lib/utils/form';

	// services
	const appStateService = AppStateService.instance;

	// bindings
	let formValues = {
		emailID: '',
		attachmentIDs: [],
		isInline: false
	};

	// local state
	let availableAttachmentMap = new BiMap({});
	let contextCompanyID = null;
	let showAddAttachmentModal = false;
	let addError = '';
	let allSelectedAttachments = [];
	let allSelectedAttachmentsChunk = [];
	let hasNextPage = true;
	let emailName = '';
	let isSubmitting = false;
	let isTableLoading = false;
	const tableParams = newTableParams();

	let isDeleteAlertVisible = false;
	let deleteValues = {
		id: null,
		name: null
	};

	const refreshTablePaginated = () => {
		allSelectedAttachmentsChunk = getPaginatedChunkWithParams(allSelectedAttachments, {
			page: tableParams.currentPage,
			perPage: tableParams.perPage,
			search: tableParams.search,
			sortBy: tableParams.sortBy,
			sortOrder: tableParams.sortOrder
		});
		const offset = (tableParams.currentPage - 1) * tableParams.perPage;
		hasNextPage = allSelectedAttachments.length > offset + tableParams.perPage;
	};

	//hooks
	onMount(async () => {
		const context = appStateService.getContext();
		if (context) {
			contextCompanyID = context.companyID;
		}
		refreshData();
		tableParams.onChange(() => {
			refreshTablePaginated();
		});
	});

	// component logic
	const refreshData = async () => {
		try {
			isTableLoading = true;
			const email = await getEmail();
			emailName = email.name;
			allSelectedAttachments = [...email.attachments];
			refreshTablePaginated();
			const allAttachments = await getAllAttachments();
			availableAttachmentMap = BiMap.FromArrayOfObjects(
				allAttachments
					// map to filenames instead of optional name field with could be dublicates which is not supported
					.map((attachment) => {
						return {
							id: attachment.id,
							name: attachment.fileName
						};
					})
					// remove attachments from allAttachments that are already attached
					.filter((attachment) => {
						return !allSelectedAttachments.some((selectedAttachment) => {
							return selectedAttachment.id === attachment.id;
						});
					})
			);
		} catch (e) {
			addToast('Failed to load data', 'Error');
			console.error('failed to load data', e);
		} finally {
			isTableLoading = false;
		}
	};

	const getEmail = async () => {
		try {
			const res = await api.email.getByID($page.params.id);
			if (!res.success) {
				throw res.error;
			}
			return res.data;
		} catch (e) {
			addToast('Failed to load email', 'Error');
			console.error('failed to get email', e);
		}
	};

	const getAllAttachments = async () => {
		try {
			return await fetchAllRows((options) => {
				return api.attachment.getByContext(contextCompanyID, options);
			});
		} catch (e) {
			addToast('Failed to load attachments', 'Error');
			console.error('failed to get attachments', e);
		}
	};

	const onClickAddAttachment = async () => {
		try {
			isSubmitting = true;
			const attachmentsToAdd = formValues.attachmentIDs.map((id) => {
				return {
					id: availableAttachmentMap.byValue(id),
					isInline: formValues.isInline
				};
			});
			const res = await api.email.addAttachments($page.params.id, attachmentsToAdd);
			if (!res.success) {
				addError = res.error;
				return;
			}
			addError = '';
			const msg = attachmentsToAdd.length > 1 ? 'attachments' : 'attachment';
			addToast(`Added ${msg}`, 'Success');
			refreshData();
			showAddAttachmentModal = false;
		} catch (e) {
			addToast('Failed to add attachment', 'Error');
			console.error('failed to add attchment', e);
		} finally {
			isSubmitting = false;
		}
	};

	/** @param {string} id */
	const onClickRemoveAttachment = async (id) => {
		const action = api.email.removeAttachment($page.params.id, id);
		action
			.then((res) => {
				if (!res.success) {
					throw res.error;
				}
				refreshData();
			})
			.catch((e) => {
				console.error('failed to remove attchment', e);
			});
		return action;
	};

	const openDeleteAlert = async (attachment) => {
		isDeleteAlertVisible = true;
		deleteValues.id = attachment.id;
		deleteValues.name = attachment.name;
	};

	const openModal = () => {
		showAddAttachmentModal = true;
		formValues.attachmentIDs = [];
		formValues.isInline = false;
		addError = '';
	};

	const closeModal = () => {
		showAddAttachmentModal = false;
		formValues.attachmentIDs = [];
		formValues.isInline = false;
		addError = '';
	};
</script>

<HeadTitle title="Email attachment ({emailName})" />
<main>
	<Headline>Attachments: {emailName}</Headline>
	<BigButton on:click={openModal}>Add attachement</BigButton>
	<Table
		columns={['Name', 'Description', 'Filename', { column: 'Inline', alignText: 'center' }]}
		sortable={['Name', 'Description', 'Filename', 'Inline']}
		pagination={tableParams}
		hasData={!!allSelectedAttachmentsChunk}
		{hasNextPage}
		plural="attachments"
		isGhost={isTableLoading}
	>
		{#each allSelectedAttachmentsChunk as attachment}
			<TableRow>
				<TableCell value={attachment.name} />
				<TableCell value={attachment.description} />
				<TableCell value={attachment.fileName} />
				<TableCellCheck value={attachment.isInline} />
				<TableCellEmpty />
				<TableCellAction>
					<TableDropDownEllipsis>
						<TableDeleteButton
							on:click={() => openDeleteAlert(attachment)}
							{...globalButtonDisabledAttributes(attachment, contextCompanyID)}
						></TableDeleteButton>
					</TableDropDownEllipsis>
				</TableCellAction>
			</TableRow>
		{/each}
	</Table>
	<Modal
		headerText={'Add attachment'}
		visible={showAddAttachmentModal}
		onClose={closeModal}
		{isSubmitting}
	>
		<FormGrid on:submit={onClickAddAttachment} {isSubmitting}>
			<FormColumns>
				<FormColumn>
					<TextFieldMultiSelect
						required
						id="attachmentIDs"
						bind:value={formValues.attachmentIDs}
						options={availableAttachmentMap.values()}
						>Attachment
					</TextFieldMultiSelect>
				</FormColumn>
				<FormColumn>
					<CheckboxField
						bind:value={formValues.isInline}
						toolTipText="Inline attachments can be referenced in email HTML using cid:filename.jpg"
						>Inline (for images)</CheckboxField
					>
				</FormColumn>
			</FormColumns>
			<FormError message={addError} />
			<FormFooter {closeModal} {isSubmitting} />
		</FormGrid>
	</Modal>
	<DeleteAlert
		list={['This will remove the attachmen from the email']}
		name={`${deleteValues.name}`}
		onClick={() => onClickRemoveAttachment(deleteValues.id)}
		permanent={false}
		bind:isVisible={isDeleteAlertVisible}
	></DeleteAlert>
</main>
