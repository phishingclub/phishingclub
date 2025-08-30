<script>
	import { onMount } from 'svelte';
	import { api } from '$lib/api/apiProxy.js';
	import { newTableURLParams } from '$lib/service/tableURLParams.js';
	import { page } from '$app/stores';
	import { globalButtonDisabledAttributes } from '$lib/utils/form.js';
	import { BiMap } from '$lib/utils/maps';
	import Headline from '$lib/components/Headline.svelte';
	import SubHeadline from '$lib/components/SubHeadline.svelte';
	import TableRow from '$lib/components/table/TableRow.svelte';
	import TableCell from '$lib/components/table/TableCell.svelte';
	import { addToast } from '$lib/store/toast';
	import FormError from '$lib/components/FormError.svelte';
	import TableCellEmpty from '$lib/components/table/TableCellEmpty.svelte';
	import TableCellAction from '$lib/components/table/TableCellAction.svelte';
	import TableDeleteButton from '$lib/components/table/TableDeleteButton2.svelte';
	import { AppStateService } from '$lib/service/appState';
	import Modal from '$lib/components/Modal.svelte';
	import FormGrid from '$lib/components/FormGrid.svelte';
	import TextFieldSearchSelect from '$lib/components/TextFieldSearchSelect.svelte';
	import { newTableParams } from '$lib/service/tableParams';
	import { getPaginatedChunkWithParams } from '$lib/service/paginationChunk';
	import { parseCSVToRecipients } from '$lib/utils/csv';
	import CheckboxField from '$lib/components/CheckboxField.svelte';
	import BigButton from '$lib/components/BigButton.svelte';
	import FormColumns from '$lib/components/FormColumns.svelte';
	import FormColumn from '$lib/components/FormColumn.svelte';
	import Table from '$lib/components/table/Table.svelte';
	import FormFooter from '$lib/components/FormFooter.svelte';
	import HeadTitle from '$lib/components/HeadTitle.svelte';
	import { showIsLoading, hideIsLoading } from '$lib/store/loading.js';
	import FileField from '$lib/components/FileField.svelte';
	import TableDropDownEllipsis from '$lib/components/table/TableDropDownEllipsis.svelte';
	import DeleteAlert from '$lib/components/modal/DeleteAlert.svelte';

	// services
	const appStateService = AppStateService.instance;

	// local state
	let contextCompanyID = '';

	// bindings
	let groupValues = {
		id: null,
		name: null,
		companyID: null
	};
	let recipientSearch = '';
	let searchOptions = [];
	let searchMap = new BiMap({});
	let addRecipientForm = null;
	let mapToSelectableRecipients = new BiMap({});
	let selectedAddRecipients = [];
	let selectedAddRecipientsPaginatedChunk = [];
	let allAddRecipientsInGroup = [];

	let importForm = null;
	let importError = '';
	let importFormValues = {
		recipients: [],
		ignoreOverwriteEmptyFields: true
	};
	const tableImportParams = newTableParams({ sortBy: 'email' });
	let selectedRecipientsImportPaginatedChunk = [];
	let isImportModalVisible = false;

	// local state
	let isAddRecipientModalVisible = false;
	let isSubmitting = false;
	let isTableLoading = false;
	let addRecipientError = '';
	let recipients = [];
	const tableParams = newTableParams({ sortBy: 'email' });
	const tableURLParams = newTableURLParams();

	let isDeleteAlertVisible = false;
	let deleteValues = {
		id: null,
		email: null
	};

	// hooks
	onMount(() => {
		const context = appStateService.getContext();
		if (context) {
			contextCompanyID = context.companyID;
		}
		loadGroup();
		refreshRecipients();
		tableURLParams.onChange(refreshRecipients);
		tableImportParams.onChange(refreshImportsPaginated);
		tableParams.onChange(refreshAddRecipientsPaginated);
		return () => {
			tableURLParams.unsubscribe();
			tableImportParams.unsubscribe();
			tableParams.unsubscribe();
		};
	});

	// component logic
	const loadGroup = async () => {
		try {
			const res = await api.recipient.getGroupByID($page.params.id);
			if (!res.success) {
				throw res.error;
			}
			groupValues.id = res.data.id;
			groupValues.name = res.data.name;
			groupValues.companyID = res.data.companyID;
		} catch (err) {
			addToast('Failed to load group', 'Error');
			console.error('failed to load group', err);
		}
	};

	const refreshRecipients = async () => {
		try {
			isTableLoading = true;
			const res = await api.recipient.getAllByGroupID($page.params.id, tableURLParams);
			if (!res.success) {
				throw res.error;
			}
			recipients = res.data.rows;
		} catch (err) {
			addToast('Failed to load recipients', 'Error');
			console.error('failed to load recipients', err);
		} finally {
			isTableLoading = false;
		}
	};

	const searchRecipients = async () => {
		try {
			const res = await api.recipient.getAll(
				{
					search: recipientSearch
				},
				contextCompanyID
			);
			if (!res.success) {
				throw res.error;
			}
			// filter out recipients that are already selected
			res.data.rows = res.data.rows.filter(
				(r) => !selectedAddRecipients.find((sr) => sr.id === r.id)
			);
			// TODO ! filter out recipients that are already in the group
			res.data.rows = res.data.rows.filter(
				(r) => !allAddRecipientsInGroup.find((sr) => sr.id === r.id)
			);

			/** @type {Record<string, string>} */
			const mapToOptionName = {};
			/** @type {Record<string, *>} */
			const mapToFullRecipient = {};
			searchOptions = res.data.rows.map((r) => {
				const key = `${r.firstName} (${r.email})`;
				mapToOptionName[r.id] = key;
				mapToFullRecipient[r.id] = r;
				return key;
			});
			if (searchOptions.length > 0) {
				searchMap = new BiMap(mapToOptionName);
				mapToSelectableRecipients = new BiMap(mapToFullRecipient);
			}
			return searchOptions;
		} catch (err) {
			addToast('Failed to load recipients', 'Error');
			console.error('failed to load recipients', err);
		}
	};

	/** @param {string} option */
	const onSelectAddRecipient = (option) => {
		const recipientID = searchMap.byValue(option);
		const recipient = mapToSelectableRecipients.byKey(recipientID);
		// if this recipient is already selected, do nothing
		if (
			selectedAddRecipients.find((r) => {
				return r.id === recipientID;
			})
		) {
			return;
		}
		selectedAddRecipients = [...selectedAddRecipients, recipient];
		refreshAddRecipientsPaginated();
	};

	const openDeleteAlert = async (recipient) => {
		isDeleteAlertVisible = true;
		deleteValues.id = recipient.id;
		deleteValues.email = recipient.email;
	};

	/** @param {string} id */
	const onClickDeselectAddRecipient = (id) => {
		selectedAddRecipients = selectedAddRecipients.filter((r) => r.id !== id);
		refreshAddRecipientsPaginated();
	};

	const onClickAddRecipients = async () => {
		try {
			isSubmitting = true;
			const res = await api.recipient.addToGroup(
				$page.params.id,
				selectedAddRecipients.map((r) => r.id)
			);
			if (!res.success) {
				addRecipientError = res.error;
				return;
			}
			addToast('Recipient added to group', 'Success');
			refreshRecipients();
			selectedAddRecipients = [];
			mapToSelectableRecipients = new BiMap({});
			closeAddRecipientModal();
		} catch (err) {
			addToast('Failed to /dd recipient to group', 'Error');
			console.error('failed to add recipient to group', err);
		} finally {
			isSubmitting = false;
		}
	};

	/** @param {string} id */
	const onClickRemoveRecipient = async (id) => {
		const action = api.recipient.removeFromGroup($page.params.id, [id]);
		action
			.then((res) => {
				if (res.success) {
					refreshRecipients();
					return;
				}
				throw res.error;
			})
			.catch((e) => {
				console.error('failed to remove recipient from group', 'Error');
			});
		return action;
	};

	const refreshAddRecipientsPaginated = async () => {
		selectedAddRecipientsPaginatedChunk = getPaginatedChunkWithParams(selectedAddRecipients, {
			page: tableParams.currentPage,
			perPage: tableParams.perPage,
			search: tableParams.search,
			sortBy: tableParams.sortBy,
			sortOrder: tableParams.sortOrder
		});
	};

	const onClickImport = async () => {
		try {
			isSubmitting = true;
			const res = await api.recipient.importToGroup({
				recipients: importFormValues.recipients,
				groupID: $page.params.id,
				ignoreOverwriteEmptyFields: importFormValues.ignoreOverwriteEmptyFields,
				companyID: contextCompanyID
			});
			if (!res.success) {
				importError = res.error;
				return;
			}
			addToast('Recipients imported to group', 'Success');
			closeImportModal();
			refreshRecipients();
		} catch (err) {
			addToast('Failed to import recipients to group', 'Error');
			console.error('failed to import recipients to group', err);
		} finally {
			isSubmitting = false;
		}
	};

	/** @param {Event} event */
	const onHandleCSVFile = async (event) => {
		const target = /** @type {HTMLInputElement} */ (event.target);
		const files = target.files;
		try {
			for (let i = 0; i < files.length; i++) {
				const file = files[i];
				const recipientsForImport = await parseCSVToRecipients(file);
				importFormValues.recipients = importFormValues.recipients.concat(
					recipientsForImport.filter(
						(recipient) =>
							!importFormValues.recipients.some(
								(existingRecipient) => existingRecipient.email === recipient.email
							)
					)
				);
				refreshImportsPaginated();
			}
		} catch (e) {
			importError = e;
			console.error('failed to import CSV file', e);
		}
		let ele = /** @type {HTMLInputElement} */ (document.querySelector('input[type=file]'));
		ele.value = null; // clear files from input field
	};

	const refreshImportsPaginated = async () => {
		selectedRecipientsImportPaginatedChunk = getPaginatedChunkWithParams(
			importFormValues.recipients,
			{
				page: tableImportParams.currentPage,
				perPage: tableImportParams.perPage,
				search: tableImportParams.search,
				sortBy: tableImportParams.sortBy,
				sortOrder: tableImportParams.sortOrder
			}
		);
	};

	/** @param {string} email */
	const onClickRemoveImportRecipient = (email) => {
		importFormValues.recipients = importFormValues.recipients.filter((r) => r.email !== email);
		refreshImportsPaginated();
	};

	const openAddRecipientsModal = () => {
		isAddRecipientModalVisible = true;
	};

	const closeAddRecipientModal = () => {
		isAddRecipientModalVisible = false;
		allAddRecipientsInGroup = [];
		selectedAddRecipients = [];
		selectedAddRecipientsPaginatedChunk = [];
		addRecipientForm.reset();
		addRecipientError = '';
	};

	const openImportModal = () => {
		isImportModalVisible = true;
	};

	const closeImportModal = () => {
		isImportModalVisible = false;
		importForm.reset();
		importError = '';
		importFormValues.recipients = [];
		selectedRecipientsImportPaginatedChunk = [];
		tableImportParams.reset();
	};
</script>

<HeadTitle title="Group ({groupValues.name}" />
<main>
	<Headline>Group Recipients</Headline>
	<SubHeadline>{groupValues.name}</SubHeadline>
	<BigButton
		on:click={openAddRecipientsModal}
		{...globalButtonDisabledAttributes(groupValues, contextCompanyID)}>Add Recipients</BigButton
	>
	<BigButton
		on:click={openImportModal}
		{...globalButtonDisabledAttributes(groupValues, contextCompanyID)}>Import from CSV</BigButton
	>
	<SubHeadline>Recipients</SubHeadline>
	<Table
		columns={[
			{ column: 'Email', size: 'small' },
			{ column: 'First name', size: 'small' },
			{ column: 'Last name', size: 'small' },
			{ column: 'Phone', size: 'small' },
			{ column: 'Extra identifier', size: 'small' },
			{ column: 'Position', size: 'small' },
			{ column: 'Department', size: 'small' },
			{ column: 'City', size: 'small' },
			{ column: 'Country', size: 'small' },
			{ column: 'Misc', size: 'small' },
			{ column: '', size: 'small' },
			{ column: 'Actions', size: 'small' }
		]}
		sortable={[
			'email',
			'first name',
			'last name',
			'phone',
			'extra identifier',
			'position',
			'department',
			'city',
			'country',
			'misc'
		]}
		hasData={!!recipients.length}
		plural="recipients"
		pagination={tableURLParams}
		isGhost={isTableLoading}
	>
		{#each recipients as recipient}
			<TableRow>
				<TableCell value={recipient.email} />
				<TableCell>
					<a href="/recipient/{recipient.id}">{recipient.firstName}</a>
				</TableCell>
				<TableCell>
					<a href="/recipient/{recipient.id}">{recipient.lastName}</a>
				</TableCell>
				<TableCell value={recipient.phone} />
				<TableCell>
					<a href="/recipient/{recipient.id}">{recipient.extraIdentifier}</a>
				</TableCell>
				<TableCell value={recipient.position} />
				<TableCell value={recipient.department} />
				<TableCell value={recipient.city} />
				<TableCell value={recipient.country} />
				<TableCell value={recipient.misc} />
				<TableCellEmpty />
				<TableCellAction>
					<TableDropDownEllipsis>
						<TableDeleteButton
							on:click={() => openDeleteAlert(recipient)}
							{...globalButtonDisabledAttributes(recipient, contextCompanyID)}
						></TableDeleteButton>
					</TableDropDownEllipsis>
				</TableCellAction>
			</TableRow>
		{/each}
	</Table>

	<Modal
		headerText={'Add recipients'}
		visible={isAddRecipientModalVisible}
		onClose={closeAddRecipientModal}
		{isSubmitting}
	>
		<FormGrid on:submit={onClickAddRecipients} bind:bindTo={addRecipientForm} {isSubmitting}>
			<FormColumns>
				<FormColumn>
					<TextFieldSearchSelect
						id="recipientSearch"
						placeholder={'Type to search'}
						onKeyUp={searchRecipients}
						onSelect={onSelectAddRecipient}
						options={searchOptions}
						bind:value={recipientSearch}>Recipients</TextFieldSearchSelect
					>
				</FormColumn>
				<FormColumn>
					<Table
						columns={['Email', 'First name', 'Last name', 'Department', '', 'Actions']}
						sortable={['Email', 'First name', 'Last name', 'Department']}
						hasData={!!selectedAddRecipients.length}
						plural="recipients"
						pagination={tableParams}
					>
						{#each selectedAddRecipientsPaginatedChunk as recipient}
							<TableRow>
								<TableCell value={recipient.email} />
								<TableCell value={recipient.firstName} />
								<TableCell value={recipient.lastName} />
								<TableCell value={recipient.department} />
								<TableCellEmpty />
								<TableCellAction>
									<TableDropDownEllipsis>
										<TableDeleteButton
											on:click={() => onClickDeselectAddRecipient(recipient.id)}
											{...globalButtonDisabledAttributes(recipient, contextCompanyID)}
										></TableDeleteButton>
									</TableDropDownEllipsis>
								</TableCellAction>
							</TableRow>
						{/each}
					</Table>
				</FormColumn>
			</FormColumns>
			<FormError message={addRecipientError} />
			<FormFooter closeModal={closeAddRecipientModal} {isSubmitting} />
		</FormGrid>
	</Modal>
	<Modal
		headerText={'Import from CSV'}
		bind:visible={isImportModalVisible}
		onClose={closeImportModal}
		{isSubmitting}
	>
		<FormGrid on:submit={onClickImport} bind:bindTo={importForm} {isSubmitting}>
			<FormColumns>
				<FormColumn>
					<label for="file" class="flex flex-col py-2 w-60">
						<FileField
							accept=".csv"
							toolTipText="Select a CSV file to import recipients"
							multiple={true}
							on:change={onHandleCSVFile}>Files</FileField
						>
						<CheckboxField
							toolTipText="Ignores empty fields in the CSV file on existing recipients"
							defaultValue={true}
							bind:value={importFormValues.ignoreOverwriteEmptyFields}
						>
							Append data
						</CheckboxField>
					</label>
				</FormColumn>
				<FormColumn overflowX={true}>
					<Table
						columns={[
							{ column: 'Email', size: 'small' },
							{ column: 'First name', size: 'small' },
							{ column: 'Last name', size: 'small' },
							{ column: 'Phone', size: 'small' },
							{ column: 'Extra identifier', size: 'small' },
							{ column: 'Position', size: 'small' },
							{ column: 'Department', size: 'small' },
							{ column: 'City', size: 'small' },
							{ column: 'Country', size: 'small' },
							{ column: 'Misc', size: 'small' },
							{ column: '', size: 'small' },
							{ column: 'Actions', size: 'small' }
						]}
						sortable={[
							'email',
							'first name',
							'last name',
							'phone',
							'extra identifier',
							'position',
							'department',
							'city',
							'country',
							'misc'
						]}
						hasData={!!importFormValues.recipients.length}
						plural="recipients"
						pagination={tableImportParams}
					>
						{#each selectedRecipientsImportPaginatedChunk as recipient}
							<TableRow>
								<TableCell value={recipient.email} />
								<TableCell value={recipient.firstName} />
								<TableCell value={recipient.lastName} />
								<TableCell value={recipient.phone} />
								<TableCell value={recipient.extraIdentifier} />
								<TableCell value={recipient.position} />
								<TableCell value={recipient.department} />
								<TableCell value={recipient.city} />
								<TableCell value={recipient.country} />
								<TableCell value={recipient.misc} />
								<TableCellEmpty />
								<TableCellAction>
									<TableDropDownEllipsis>
										<TableDeleteButton
											on:click={() => onClickRemoveImportRecipient(recipient.email)}
											{...globalButtonDisabledAttributes(recipient, contextCompanyID)}
										></TableDeleteButton>
									</TableDropDownEllipsis>
								</TableCellAction>
							</TableRow>
						{/each}
					</Table>
				</FormColumn>
			</FormColumns>
			<FormError message={importError} />
			<FormFooter closeModal={closeImportModal} {isSubmitting} />
		</FormGrid>
	</Modal>
	<DeleteAlert
		list={['The recipient will only be removed from the group and not deleted']}
		name={deleteValues.email}
		onClick={() => onClickRemoveRecipient(deleteValues.id)}
		bind:isVisible={isDeleteAlertVisible}
	></DeleteAlert>
</main>
