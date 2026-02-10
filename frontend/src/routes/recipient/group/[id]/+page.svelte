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
	import TableCellLink from '$lib/components/table/TableCellLink.svelte';
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
	import Button from '$lib/components/Button.svelte';
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
	let csvSkippedRows = [];
	let importResult = null;
	let isImportResultModalVisible = false;
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

			// store import result for display
			importResult = res.data;

			// build summary message
			const summary = res.data.summary;
			let message = `Import complete: ${summary.success} succeeded (${summary.created} created, ${summary.updated} updated)`;
			if (summary.failed > 0) {
				message += `, ${summary.failed} failed`;
			}
			if (csvSkippedRows.length > 0) {
				message += `, ${csvSkippedRows.length} skipped in CSV`;
			}

			console.log(summary);
			addToast(
				'Import finished',
				summary.failed > 0 || csvSkippedRows.length > 0 ? 'Warning' : 'Success'
			);

			// show result modal
			closeImportModal();
			isImportResultModalVisible = true;
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
					importError = `CSV rows skipped:\n${skippedMsg}${remaining}\n\nReview the data below before importing.`;
				}
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
		csvSkippedRows = [];
		importError = '';
		importResult = null;
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
			{ column: 'Email', size: 'large' },
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
				<TableCellLink href="/recipient/{recipient.id}" title={recipient.firstName}>
					{recipient.firstName}
				</TableCellLink>
				<TableCellLink href="/recipient/{recipient.id}" title={recipient.lastName}>
					{recipient.lastName}
				</TableCellLink>
				<TableCell value={recipient.phone} />
				<TableCellLink href="/recipient/{recipient.id}" title={recipient.extraIdentifier}>
					{recipient.extraIdentifier}
				</TableCellLink>
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
						columns={['Email', 'First name', 'Last name', 'Department']}
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
							{ column: 'Email', size: 'large' },
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

	{#if isImportResultModalVisible && importResult}
		<Modal headerText="Recipient Import Summary" bind:visible={isImportResultModalVisible}>
			<div class="p-6 max-h-[80vh] overflow-y-auto">
				<div class="space-y-6">
					<!-- Statistics Section -->
					<div class="grid grid-cols-1 gap-6">
						<div>
							<h3 class="font-semibold text-gray-900 dark:text-gray-100 mb-2">Recipients</h3>
							<ul class="space-y-1">
								<li>Total: {importResult.summary.total}</li>
								<li>Created: {importResult.summary.created}</li>
								<li>Updated: {importResult.summary.updated}</li>
								<li>Failed: {importResult.summary.failed}</li>
								{#if csvSkippedRows.length > 0}
									<li>Skipped in CSV: {csvSkippedRows.length}</li>
								{/if}
							</ul>
						</div>
					</div>

					<!-- Details Section -->
					<div class="border-t pt-6">
						<div class="space-y-4">
							{#if importResult.createdRecipients?.length > 0}
								<div
									class="bg-gray-50 dark:bg-gray-800 rounded-lg border border-gray-200 dark:border-gray-700"
								>
									<details class="group">
										<summary
											class="cursor-pointer p-4 font-semibold text-base text-pc-darkblue dark:text-white hover:bg-gray-100 dark:hover:bg-gray-700/50 rounded-lg transition-colors list-none flex items-center gap-2"
										>
											<svg
												class="w-4 h-4 transition-transform group-open:rotate-90"
												fill="none"
												stroke="currentColor"
												viewBox="0 0 24 24"
											>
												<path
													stroke-linecap="round"
													stroke-linejoin="round"
													stroke-width="2"
													d="M9 5l7 7-7 7"
												/>
											</svg>
											<span>Created ({importResult.createdRecipients.length})</span>
										</summary>
										<div class="px-4 pb-4">
											<div class="space-y-1">
												{#each importResult.createdRecipients as recipient}
													<div
														class="flex items-center justify-between py-2 px-3 rounded hover:bg-white dark:hover:bg-gray-700/50 transition-colors"
													>
														<span
															class="text-sm text-gray-900 dark:text-gray-100 font-medium truncate flex-1"
														>
															{recipient.email}
														</span>
														{#if recipient.firstName || recipient.lastName}
															<span
																class="text-sm text-gray-500 dark:text-gray-400 ml-4 whitespace-nowrap"
															>
																{recipient.firstName || ''}
																{recipient.lastName || ''}
															</span>
														{/if}
													</div>
												{/each}
											</div>
										</div>
									</details>
								</div>
							{/if}

							{#if importResult.updatedRecipients?.length > 0}
								<div
									class="bg-gray-50 dark:bg-gray-800 rounded-lg border border-gray-200 dark:border-gray-700"
								>
									<details class="group">
										<summary
											class="cursor-pointer p-4 font-semibold text-base text-pc-darkblue dark:text-white hover:bg-gray-100 dark:hover:bg-gray-700/50 rounded-lg transition-colors list-none flex items-center gap-2"
										>
											<svg
												class="w-4 h-4 transition-transform group-open:rotate-90"
												fill="none"
												stroke="currentColor"
												viewBox="0 0 24 24"
											>
												<path
													stroke-linecap="round"
													stroke-linejoin="round"
													stroke-width="2"
													d="M9 5l7 7-7 7"
												/>
											</svg>
											<span>Updated ({importResult.updatedRecipients.length})</span>
										</summary>
										<div class="px-4 pb-4">
											<div class="space-y-1">
												{#each importResult.updatedRecipients as recipient}
													<div
														class="flex items-center justify-between py-2 px-3 rounded hover:bg-white dark:hover:bg-gray-700/50 transition-colors"
													>
														<span
															class="text-sm text-gray-900 dark:text-gray-100 font-medium truncate flex-1"
														>
															{recipient.email}
														</span>
														{#if recipient.firstName || recipient.lastName}
															<span
																class="text-sm text-gray-500 dark:text-gray-400 ml-4 whitespace-nowrap"
															>
																{recipient.firstName || ''}
																{recipient.lastName || ''}
															</span>
														{/if}
													</div>
												{/each}
											</div>
										</div>
									</details>
								</div>
							{/if}

							{#if csvSkippedRows.length > 0}
								<div
									class="bg-gray-50 dark:bg-gray-800 rounded-lg border border-gray-200 dark:border-gray-700"
								>
									<details class="group">
										<summary
											class="cursor-pointer p-4 font-semibold text-base text-pc-darkblue dark:text-white hover:bg-gray-100 dark:hover:bg-gray-700/50 rounded-lg transition-colors list-none flex items-center gap-2"
										>
											<svg
												class="w-4 h-4 transition-transform group-open:rotate-90"
												fill="none"
												stroke="currentColor"
												viewBox="0 0 24 24"
											>
												<path
													stroke-linecap="round"
													stroke-linejoin="round"
													stroke-width="2"
													d="M9 5l7 7-7 7"
												/>
											</svg>
											<span>Skipped in CSV ({csvSkippedRows.length})</span>
										</summary>
										<div class="px-4 pb-4">
											<p class="text-sm text-gray-500 dark:text-gray-400 mb-3">
												These rows were skipped during CSV parsing (before import)
											</p>
											<div class="space-y-1">
												{#each csvSkippedRows as skip}
													<div
														class="flex items-center justify-between py-2 px-3 rounded hover:bg-white dark:hover:bg-gray-700/50 transition-colors"
													>
														<span
															class="text-sm text-gray-900 dark:text-gray-100 font-medium truncate flex-1"
														>
															Line {skip.line}: {skip.reason}
															{#if skip.row?.email}
																<span class="text-gray-500 dark:text-gray-400 ml-2"
																	>({skip.row.email})</span
																>
															{/if}
														</span>
													</div>
												{/each}
											</div>
										</div>
									</details>
								</div>
							{/if}

							{#if importResult.failures?.length > 0}
								<div
									class="bg-gray-50 dark:bg-gray-800 rounded-lg border border-gray-200 dark:border-gray-700"
								>
									<details class="group">
										<summary
											class="cursor-pointer p-4 font-semibold text-base text-pc-darkblue dark:text-white hover:bg-gray-100 dark:hover:bg-gray-700/50 rounded-lg transition-colors list-none flex items-center gap-2"
										>
											<svg
												class="w-4 h-4 transition-transform group-open:rotate-90"
												fill="none"
												stroke="currentColor"
												viewBox="0 0 24 24"
											>
												<path
													stroke-linecap="round"
													stroke-linejoin="round"
													stroke-width="2"
													d="M9 5l7 7-7 7"
												/>
											</svg>
											<span>Import Errors ({importResult.failures.length})</span>
										</summary>
										<div class="px-4 pb-4">
											<p class="text-sm text-gray-500 dark:text-gray-400 mb-3">
												These recipients failed to import (backend errors)
											</p>
											<div class="space-y-1">
												{#each importResult.failures as err}
													<div
														class="flex items-center justify-between py-2 px-3 rounded hover:bg-white dark:hover:bg-gray-700/50 transition-colors"
													>
														<span
															class="text-sm text-gray-900 dark:text-gray-100 font-medium truncate flex-1"
														>
															{err.email}: {err.reason}
														</span>
													</div>
												{/each}
											</div>
										</div>
									</details>
								</div>
							{/if}
						</div>
					</div>

					<div class="mt-4 flex justify-end">
						<Button on:click={() => (isImportResultModalVisible = false)}>Close</Button>
					</div>
				</div>
			</div>
		</Modal>
	{/if}
</main>
