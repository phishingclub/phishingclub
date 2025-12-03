<script>
	import { api } from '$lib/api/apiProxy.js';
	import { newTableURLParams } from '$lib/service/tableURLParams.js';
	import { globalButtonDisabledAttributes } from '$lib/utils/form.js';
	import { onMount } from 'svelte';
	import Headline from '$lib/components/Headline.svelte';
	import TextField from '$lib/components/TextField.svelte';
	import TableRow from '$lib/components/table/TableRow.svelte';
	import TableCell from '$lib/components/table/TableCell.svelte';
	import TableCellLink from '$lib/components/table/TableCellLink.svelte';
	import TableUpdateButton from '$lib/components/table/TableUpdateButton.svelte';
	import TableDeleteButton from '$lib/components/table/TableDeleteButton2.svelte';
	import { addToast } from '$lib/store/toast';
	import FormError from '$lib/components/FormError.svelte';
	import { AppStateService } from '$lib/service/appState';
	import TableCellAction from '$lib/components/table/TableCellAction.svelte';
	import TableCellEmpty from '$lib/components/table/TableCellEmpty.svelte';
	import FormGrid from '$lib/components/FormGrid.svelte';
	import Modal from '$lib/components/Modal.svelte';
	import { newTableParams } from '$lib/service/tableParams';
	import { parseCSVToRecipients } from '$lib/utils/csv';
	import { getPaginatedChunkWithParams } from '$lib/service/paginationChunk';
	import CheckboxField from '$lib/components/CheckboxField.svelte';
	import BigButton from '$lib/components/BigButton.svelte';
	import FormColumns from '$lib/components/FormColumns.svelte';
	import FormColumn from '$lib/components/FormColumn.svelte';
	import FormFooter from '$lib/components/FormFooter.svelte';
	import Table from '$lib/components/table/Table.svelte';
	import HeadTitle from '$lib/components/HeadTitle.svelte';
	import FileField from '$lib/components/FileField.svelte';
	import TableCellScope from '$lib/components/table/TableCellScope.svelte';
	import { getModalText } from '$lib/utils/common';
	import TableCopyButton from '$lib/components/table/TableCopyButton.svelte';
	import { showIsLoading, hideIsLoading } from '$lib/store/loading.js';
	import TableViewButton from '$lib/components/table/TableViewButton.svelte';
	import { goto } from '$app/navigation';
	import TableCellCheck from '$lib/components/table/TableCellCheck.svelte';
	import TableDropDownEllipsis from '$lib/components/table/TableDropDownEllipsis.svelte';
	import DeleteAlert from '$lib/components/modal/DeleteAlert.svelte';

	// services
	const appStateService = AppStateService.instance;

	// data
	let form = null;
	let modalError = '';
	let formValues = {
		email: null,
		phone: null,
		extraIdentifier: null,
		firstName: null,
		lastName: null,
		position: null,
		department: null,
		city: null,
		country: null,
		misc: null
	};
	let importForm = null;
	let importModalError = '';
	let importFormValues = {
		recipients: [],
		ignoreOverwriteEmptyFields: true
	};
	let csvSkippedRows = [];
	const tableImportParams = newTableParams({ sortBy: 'email' });
	let selectedRecipientsImportPaginatedChunk = [];
	let isImportModalVisible = false;
	// @ts-ignore
	const tableURLParams = newTableURLParams({
		sortBy: 'email'
	});
	let contextCompanyID = null;
	let recipients = [];
	let recipientsHasNextPage = true;
	let isModalVisible = false;
	let isSubmitting = false;
	let modalMode = null;
	let modalText = '';

	let isDeleteAlertVisible = false;
	let deleteValues = {
		id: null,
		email: null
	};

	let isRecipientsTableLoading = false;

	$: {
		modalText = getModalText('recipient', modalMode);
	}

	// hooks
	onMount(() => {
		const context = appStateService.getContext();
		if (context) {
			contextCompanyID = context.companyID;
		}
		refreshRecipients();
		tableURLParams.onChange(refreshRecipients);
		tableImportParams.onChange(refreshImportsPaginated);

		return () => {
			tableURLParams.unsubscribe();
		};
	});

	// component logic
	const refreshRecipients = async () => {
		isRecipientsTableLoading = true;
		try {
			const res = await api.recipient.getAll(tableURLParams, contextCompanyID);
			if (res.success) {
				recipients = res.data.rows;
				recipientsHasNextPage = res.data.hasNextPage;
				return;
			}
			throw res.error;
		} catch (e) {
			addToast('Failed to load recipients', 'Error');
			console.error('failed to load recipients', e);
		} finally {
			isRecipientsTableLoading = false;
		}
	};

	const onSubmit = async () => {
		try {
			isSubmitting = true;
			if (modalMode === 'create' || modalMode === 'copy') {
				await create();
				return;
			} else {
				await update();
				return;
			}
		} finally {
			isSubmitting = false;
		}
	};

	const create = async () => {
		try {
			const res = await api.recipient.create({
				email: formValues.email,
				phone: formValues.phone,
				extraIdentifier: formValues.extraIdentifier,
				firstName: formValues.firstName,
				lastName: formValues.lastName,
				position: formValues.position,
				department: formValues.department,
				city: formValues.city,
				country: formValues.country,
				misc: formValues.misc,
				companyID: contextCompanyID
			});
			if (!res.success) {
				modalError = res.error;
				return;
			}
			addToast('Recipient created', 'Success');
			form.reset();
			refreshRecipients();
			isModalVisible = !isModalVisible;
		} catch (err) {
			console.error('failed to create recipient', err);
			addToast('Failed to create recipient', 'Error');
		}
	};

	const update = async () => {
		modalError = '';
		try {
			const res = await api.recipient.update({
				id: formValues.id,
				email: formValues.email,
				phone: formValues.phone,
				extraIdentifier: formValues.extraIdentifier,
				firstName: formValues.firstName,
				lastName: formValues.lastName,
				position: formValues.position,
				department: formValues.department,
				city: formValues.city,
				country: formValues.country,
				misc: formValues.misc,
				companyID: contextCompanyID
			});
			if (!res.success) {
				modalError = res.error;
				return;
			}
			addToast('Recipient updated', 'Success');
			refreshRecipients();
			isModalVisible = false;
		} catch (e) {
			addToast('Failed to update recipient', 'Error');
			console.error('failed to update recipient:', e);
		}
	};

	const onClickImport = async () => {
		try {
			isSubmitting = true;
			const res = await api.recipient.import({
				recipients: importFormValues.recipients,
				ignoreOverwriteEmptyFields: importFormValues.ignoreOverwriteEmptyFields,
				companyID: contextCompanyID
			});
			if (!res.success) {
				importModalError = res.error;
				return;
			}
			addToast('Recipients imported', 'Success');
			closeImportModal();
			refreshRecipients();
		} catch (err) {
			addToast('Failed to import recipients', 'Error');
			console.error('failed to import recipients', err);
		} finally {
			isSubmitting = false;
		}
	};

	/** @param {Event} event */
	const onHandleCSVFile = async (event) => {
		const target = /** @type {HTMLInputElement} */ (event.target);
		const files = target.files;
		try {
			showIsLoading();
			for (let i = 0; i < files.length; i++) {
				const file = files[i];
				const result = await parseCSVToRecipients(file);

				// track skipped rows
				if (result.skipped && result.skipped.length > 0) {
					csvSkippedRows = csvSkippedRows.concat(result.skipped);
					console.info(`CSV import: ${result.skipped.length} rows skipped`, result.skipped);
				}

				importFormValues.recipients = importFormValues.recipients.concat(
					result.recipients.filter(
						(recipient) =>
							!importFormValues.recipients.some(
								(existingRecipient) => existingRecipient.email === recipient.email
							)
					)
				);
				refreshImportsPaginated();

				// show info about skipped rows
				if (result.skipped && result.skipped.length > 0) {
					const skippedMsg = result.skipped
						.slice(0, 3)
						.map((s) => `Line ${s.line}: ${s.reason}`)
						.join('\n');
					const remaining =
						result.skipped.length > 3 ? `\n... and ${result.skipped.length - 3} more` : '';
					importModalError = `CSV rows skipped:\n${skippedMsg}${remaining}\n\nReview the data before importing.`;
				}
			}
		} catch (e) {
			importModalError = e;
			console.error('failed to import CSV file', e);
		} finally {
			hideIsLoading();
		}
		target.value = '';
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

	const openImportModal = () => {
		csvSkippedRows = [];
		importModalError = '';
		isImportModalVisible = true;
	};

	const closeImportModal = () => {
		isImportModalVisible = false;
		importForm.reset();
		importModalError = '';
		importFormValues.recipients = [];
		selectedRecipientsImportPaginatedChunk = [];
		tableImportParams.reset();
	};

	const openDeleteAlert = async (recipient) => {
		isDeleteAlertVisible = true;
		deleteValues.id = recipient.id;
		deleteValues.email = recipient.email;
	};

	/** @param {string} id */
	const onClickDelete = async (id) => {
		const action = api.recipient.delete(id);
		action
			.then((res) => {
				if (res.success) {
					refreshRecipients();
					return;
				}
				throw res.error;
			})
			.catch((e) => {
				console.error('failed to delete recipient:', e);
			});
		return action;
	};

	const openCreateModal = () => {
		modalMode = 'create';
		isModalVisible = true;
	};

	const closeModal = () => {
		isModalVisible = false;
		form.reset();
		modalError = '';
	};

	/** @param {string} id */
	const openUpdateModal = async (id) => {
		modalMode = 'update';
		try {
			showIsLoading();
			const recipient = await api.recipient.getByID(id);
			if (!recipient.success) {
				addToast('Failed to get recipient', 'Error');
				console.error('failed to get recipient', recipient.error);
				return;
			}
			assignRecipient(recipient);
			isModalVisible = true;
		} catch (e) {
			addToast('Failed to get recipient', 'Error');
			console.error('failed to get recipient', e);
		} finally {
			hideIsLoading();
		}
		isModalVisible = true;
	};

	const openCopyModal = async (id) => {
		modalMode = 'copy';
		try {
			const recipient = await api.recipient.getByID(id);
			if (!recipient.success) {
				addToast('Failed to get recipient', 'Error');
				console.error('failed to get recipient', recipient.error);
				return;
			}
			assignRecipient(recipient);
			isModalVisible = true;
		} catch (e) {
			addToast('Failed to get recipient', 'Error');
			console.error('failed to get recipient', e);
		}
		isModalVisible = true;
	};

	const assignRecipient = (recipient) => {
		formValues = {
			id: recipient.data.id,
			email: recipient.data.email,
			phone: recipient.data.phone,
			extraIdentifier: recipient.data.extraIdentifier,
			firstName: recipient.data.firstName,
			lastName: recipient.data.lastName,
			position: recipient.data.position,
			department: recipient.data.department,
			city: recipient.data.city,
			country: recipient.data.country,
			misc: recipient.data.misc
		};
	};
</script>

<HeadTitle title="Recipients" />
<section>
	<Headline>Recipients</Headline>

	<div class="flex gap-3">
		<BigButton on:click={openCreateModal}>New recipient</BigButton>
		<BigButton on:click={openImportModal}>Import from CSV</BigButton>
	</div>
	<Table
		isGhost={isRecipientsTableLoading}
		columns={[
			{ column: 'Email', size: 'small' },
			{ column: 'First name', size: 'small' },
			{ column: 'Last name', size: 'small' },
			{ column: 'Phone', size: 'small' },
			{ column: 'Extra identifier', size: 'small' },
			{ column: 'Position', size: 'small' },
			{ column: 'Repeat offender', size: 'small', alignText: 'center' },
			{ column: 'Department', size: 'small' },
			{ column: 'City', size: 'small' },
			{ column: 'Country', size: 'small' },
			{ column: 'Misc', size: 'small' },
			...(contextCompanyID ? [{ column: 'Scope', size: 'small' }] : [])
		]}
		sortable={[
			'first name',
			'last name',
			'extra identifier',
			'email',
			'phone',
			'repeat offender',
			'position',
			'department',
			'city',
			'country',
			'misc',
			...(contextCompanyID ? ['scope'] : [])
		]}
		hasData={!!recipients.length}
		hasNextPage={recipientsHasNextPage}
		plural="recipients"
		pagination={tableURLParams}
	>
		{#each recipients as recipient}
			<TableRow>
				<TableCellLink href={`/recipient/${recipient.id}`} title={recipient.email}>
					{#if recipient.email}
						{recipient.email}
					{/if}
				</TableCellLink>
				<TableCell>
					{#if recipient.firstName}
						<button
							on:click={() => {
								openUpdateModal(recipient.id);
							}}
							class="block w-full py-1 text-left"
						>
							{recipient.firstName}
						</button>
					{/if}
				</TableCell>
				<TableCell>
					{#if recipient.lastName}
						<button
							on:click={() => {
								openUpdateModal(recipient.id);
							}}
							class="block w-full py-1 text-left"
						>
							{recipient.lastName}
						</button>
					{/if}
				</TableCell>
				<TableCell value={recipient.phone} />
				<TableCell value={recipient.extraIdentifier} />
				<TableCell value={recipient.position} />
				<TableCellCheck value={recipient.isRepeatOffender} />
				<TableCell value={recipient.department} />
				<TableCell value={recipient.city} />
				<TableCell value={recipient.country} />
				<TableCell value={recipient.misc} />
				{#if contextCompanyID}
					<TableCellScope companyID={recipient.companyID} />
				{/if}
				<TableCellEmpty />
				<TableCellAction>
					<TableDropDownEllipsis>
						<TableViewButton
							on:click={() => {
								goto(`/recipient/${recipient.id}`);
							}}
						/>
						<TableUpdateButton
							on:click={() => openUpdateModal(recipient.id)}
							{...globalButtonDisabledAttributes(recipient, contextCompanyID)}
						/>
						<TableCopyButton
							title={'Copy'}
							on:click={() => openCopyModal(recipient.id)}
							{...globalButtonDisabledAttributes(recipient, contextCompanyID)}
						/>

						<TableDeleteButton
							on:click={() => openDeleteAlert(recipient)}
							{...globalButtonDisabledAttributes(recipient, contextCompanyID)}
						></TableDeleteButton>
					</TableDropDownEllipsis>
				</TableCellAction>
			</TableRow>
		{/each}
	</Table>
	<Modal headerText={modalText} bind:visible={isModalVisible} onClose={closeModal} {isSubmitting}>
		<FormGrid on:submit={onSubmit} bind:bindTo={form} {isSubmitting}>
			<FormColumns>
				<FormColumn>
					<TextField
						required
						minLength={1}
						maxLength={255}
						type="email"
						bind:value={formValues.email}
						placeholder="bob@example.test"
						readonly={modalMode === 'update'}>Email</TextField
					>
					<TextField
						minLength={1}
						maxLength={127}
						bind:value={formValues.firstName}
						placeholder="Bob"
						optional>First name</TextField
					>
					<TextField
						minLength={1}
						maxLength={127}
						bind:value={formValues.lastName}
						placeholder="Bob"
						optional>Last name</TextField
					>
					<TextField
						minLength={1}
						maxLength={127}
						bind:value={formValues.phone}
						placeholder="+45 555 555 5555"
						optional>Phone</TextField
					>
					<TextField
						minLength={1}
						maxLength={127}
						bind:value={formValues.extraIdentifier}
						placeholder="4982347283947"
						optional
						toolTipText="Optional extra identifier"
					>
						Extra identifier
					</TextField>
					<TextField
						minLength={1}
						maxLength={127}
						bind:value={formValues.position}
						placeholder="CEO"
						optional>Position</TextField
					>
				</FormColumn>
				<FormColumn>
					<TextField
						minLength={1}
						maxLength={127}
						bind:value={formValues.department}
						placeholder="Sales"
						optional>Department</TextField
					>
					<TextField
						minLength={1}
						maxLength={127}
						bind:value={formValues.city}
						optional
						placeholder="Copenhagen">City</TextField
					>
					<TextField
						minLength={1}
						maxLength={127}
						bind:value={formValues.country}
						optional
						placeholder="Denmark">Country</TextField
					>
					<TextField
						minLength={1}
						maxLength={127}
						bind:value={formValues.misc}
						optional
						placeholder="VIP"
						toolTipText="Any extra information">Miscallaneous</TextField
					>
				</FormColumn>
			</FormColumns>
			<FormError message={modalError} />
			<FormFooter {closeModal} {isSubmitting} />
		</FormGrid>
	</Modal>
	<!-- Import modal -->
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
							{ column: 'Misc', size: 'small' }
						]}
						sortable={[
							'Email',
							'Phone',
							'Extra identifier',
							'First name',
							'Last name',
							'Position',
							'Department',
							'City',
							'Country',
							'Misc',
							'country'
						]}
						hasData={!!importFormValues.recipients.length}
						plural="Recipients"
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
										/>
									</TableDropDownEllipsis>
								</TableCellAction>
							</TableRow>
						{/each}
					</Table>
				</FormColumn>
			</FormColumns>
			<FormError message={importModalError} />
			<FormFooter closeModal={closeImportModal} {isSubmitting} />
		</FormGrid>
	</Modal>
	<DeleteAlert
		list={['Any associated data will be anonymized']}
		name={deleteValues.email}
		onClick={() => onClickDelete(deleteValues.id)}
		bind:isVisible={isDeleteAlertVisible}
	></DeleteAlert>
</section>
