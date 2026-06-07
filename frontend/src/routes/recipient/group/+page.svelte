<script>
	import { api } from '$lib/api/apiProxy.js';
	import { newTableURLParams } from '$lib/service/tableURLParams.js';
	import { onMount } from 'svelte';
	import { globalButtonDisabledAttributes } from '$lib/utils/form.js';
	import { goto } from '$app/navigation';
	import Headline from '$lib/components/Headline.svelte';
	import TextField from '$lib/components/TextField.svelte';
	import DynamicLabel from '$lib/components/DynamicLabel.svelte';
	import TextFieldSelect from '$lib/components/TextFieldSelect.svelte';
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
	import BigButton from '$lib/components/BigButton.svelte';
	import FormColumn from '$lib/components/FormColumn.svelte';
	import FormColumns from '$lib/components/FormColumns.svelte';
	import FormFooter from '$lib/components/FormFooter.svelte';
	import Table from '$lib/components/table/Table.svelte';
	import HeadTitle from '$lib/components/HeadTitle.svelte';
	import { showIsLoading, hideIsLoading } from '$lib/store/loading.js';
	import TableDropDownEllipsis from '$lib/components/table/TableDropDownEllipsis.svelte';
	import DeleteAlert from '$lib/components/modal/DeleteAlert.svelte';
	import TableDropDownButton from '$lib/components/table/TableDropDownButton.svelte';
	import TableCellScope from '$lib/components/table/TableCellScope.svelte';

	// services
	const appStateService = AppStateService.instance;

	// the recipient fields that can be used as a dynamic filter
	const dynamicFilterFields = ['position', 'department', 'city', 'country', 'misc'];

	// data
	let form = null;
	let dynamicForm = null;
	let modalError = '';
	let dynamicModalError = '';

	let formValues = {
		name: null,
		companyID: null,
		recipients: []
	};

	let dynamicFormValues = {
		name: null,
		filterField: dynamicFilterFields[0],
		filterValue: null
	};

	let isModalVisible = false;
	let isDynamicModalVisible = false;
	let isSubmitting = false;
	let isTableLoading = false;
	const tableURLParams = newTableURLParams();
	let contextCompanyID = '';
	let groups = [];
	let groupsHasNextPage = true;
	let modalMode = null;
	let modalText = '';

	let isDeleteAlertVisible = false;
	let deleteValues = {
		id: null,
		name: null
	};

	// dynamic update modal state
	let isDynamicUpdateModalVisible = false;
	let dynamicUpdateForm = null;
	let dynamicUpdateError = '';
	let dynamicUpdateValues = {
		id: null,
		name: null,
		filterField: dynamicFilterFields[0],
		filterValue: null
	};

	$: {
		modalText = modalMode === 'create' ? 'New group' : 'Update group';
	}

	// hooks
	onMount(() => {
		const context = appStateService.getContext();
		if (context) {
			contextCompanyID = context.companyID;
		}
		refreshGroups();
		tableURLParams.onChange(refreshGroups);

		return () => {
			tableURLParams.unsubscribe();
		};
	});

	// component logic
	/** @param {string} id */
	const loadGroup = async (id) => {
		try {
			const res = await api.recipient.getGroupByID(id);
			if (!res.success) {
				throw res.error;
			}
			return res.data;
		} catch (err) {
			addToast('Failed to load group', 'Error');
			console.error('failed to load group', err);
		}
	};

	const refreshGroups = async () => {
		try {
			isTableLoading = true;
			const res = await api.recipient.getAllGroups(tableURLParams, contextCompanyID);
			if (!res.success) {
				throw res.error;
			}
			groups = res.data.rows;
			groupsHasNextPage = res.data.hasNextPage;
		} catch (e) {
			addToast('Failed to load groups', 'Error');
			console.error('failed to load groups', e);
		} finally {
			isTableLoading = false;
		}
	};

	const onSubmit = async () => {
		try {
			isSubmitting = true;
			if (modalMode === 'create') {
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
			const res = await api.recipient.createGroup(
				formValues.name,
				contextCompanyID,
				formValues.recipients
			);
			if (!res.success) {
				modalError = res.error;
				return;
			}
			addToast('Group created', 'Success');
			formValues.recipients = [];
			refreshGroups();
			closeModal();
		} catch (e) {
			addToast('Failed to create recipient group', 'Error');
			console.error('failed to create recipient group', e);
		}
	};

	const update = async () => {
		try {
			const res = await api.recipient.updateGroup({
				id: formValues.id,
				name: formValues.name
			});
			if (!res.success) {
				modalError = res.error;
				return;
			}
			addToast('Group updated', 'Success');
			refreshGroups();
			closeModal();
		} catch (err) {
			addToast('Failed to update group', 'Error');
			console.error('failed to update group', err);
		}
	};

	const onSubmitDynamicCreate = async () => {
		try {
			isSubmitting = true;
			const res = await api.recipient.createDynamicGroup(
				dynamicFormValues.name,
				contextCompanyID,
				dynamicFormValues.filterField,
				dynamicFormValues.filterValue
			);
			if (!res.success) {
				dynamicModalError = res.error;
				return;
			}
			addToast('Dynamic group created', 'Success');
			refreshGroups();
			closeDynamicModal();
		} catch (e) {
			addToast('Failed to create dynamic group', 'Error');
			console.error('failed to create dynamic group', e);
		} finally {
			isSubmitting = false;
		}
	};

	const onSubmitDynamicUpdate = async () => {
		try {
			isSubmitting = true;
			const res = await api.recipient.updateDynamicGroup({
				id: dynamicUpdateValues.id,
				name: dynamicUpdateValues.name,
				filterField: dynamicUpdateValues.filterField,
				filterValue: dynamicUpdateValues.filterValue
			});
			if (!res.success) {
				dynamicUpdateError = res.error;
				return;
			}
			addToast('Dynamic group updated', 'Success');
			refreshGroups();
			closeDynamicUpdateModal();
		} catch (err) {
			addToast('Failed to update dynamic group', 'Error');
			console.error('failed to update dynamic group', err);
		} finally {
			isSubmitting = false;
		}
	};

	const openDeleteAlert = async (group) => {
		isDeleteAlertVisible = true;
		deleteValues.id = group.id;
		deleteValues.name = group.name;
	};

	/** @param {string} id */
	const onClickDelete = async (id) => {
		const action = api.recipient.deleteGroup(id);
		action
			.then((res) => {
				if (res.success) {
					refreshGroups();
					return;
				}
				throw res.error;
			})
			.catch((e) => {
				console.error('failed to delete recipient group', e);
			});
		return action;
	};

	/** @param {string} id */
	const gotoEditGroupRecipients = async (id) => {
		goto(`/recipient/group/${id}`, { invalidateAll: true });
	};

	const openCreateModal = () => {
		modalMode = 'create';
		isModalVisible = true;
	};

	const openDynamicCreateModal = () => {
		dynamicFormValues = {
			name: null,
			filterField: dynamicFilterFields[0],
			filterValue: null
		};
		dynamicModalError = '';
		isDynamicModalVisible = true;
	};

	/** @param {string} id */
	const openUpdateModal = async (id) => {
		modalMode = 'update';
		try {
			showIsLoading();
			const group = await loadGroup(id);
			formValues.id = id;
			formValues.name = group.name;
			isModalVisible = true;
		} catch (e) {
			addToast('Failed to load group', 'Error');
			console.error('failed to load group', e);
		} finally {
			hideIsLoading();
		}
	};

	/** @param {string} id */
	const openDynamicUpdateModal = async (id) => {
		try {
			showIsLoading();
			const group = await loadGroup(id);
			dynamicUpdateValues.id = id;
			dynamicUpdateValues.name = group.name;
			dynamicUpdateValues.filterField = group.filterField ?? dynamicFilterFields[0];
			dynamicUpdateValues.filterValue = group.filterValue ?? '';
			dynamicUpdateError = '';
			isDynamicUpdateModalVisible = true;
		} catch (e) {
			addToast('Failed to load group', 'Error');
			console.error('failed to load group', e);
		} finally {
			hideIsLoading();
		}
	};

	const closeModal = () => {
		isModalVisible = false;
		form.reset();
		modalError = '';
	};

	const closeDynamicModal = () => {
		isDynamicModalVisible = false;
		if (dynamicForm) dynamicForm.reset();
		dynamicModalError = '';
	};

	const closeDynamicUpdateModal = () => {
		isDynamicUpdateModalVisible = false;
		if (dynamicUpdateForm) dynamicUpdateForm.reset();
		dynamicUpdateError = '';
	};
</script>

<HeadTitle title="Groups" />
<main>
	<Headline>Groups</Headline>
	<div class="flex gap-3">
		<BigButton on:click={openCreateModal}>New group</BigButton>
		<BigButton on:click={openDynamicCreateModal}>New dynamic group</BigButton>
	</div>
	<Table
		columns={[
			{ column: 'Name', size: 'large' },
			{ column: 'Count', size: 'small' },
			...(contextCompanyID ? [{ column: 'Scope', size: 'small' }] : [])
		]}
		sortable={['Name', ...(contextCompanyID ? ['scope'] : [])]}
		hasData={!!groups.length}
		hasNextPage={groupsHasNextPage}
		plural="groups"
		pagination={tableURLParams}
		isGhost={isTableLoading}
	>
		{#each groups as group}
			<TableRow>
				<TableCellLink href={`/recipient/group/${group.id}`} title={group.name}>
					{#if group.isDynamic}
						<DynamicLabel />
					{/if}
					{group.name}
					{#if group.scimEnabled}
						<span
							class="ml-2 inline-flex items-center px-1.5 py-0.5 rounded text-xs font-medium bg-blue-100 text-blue-800 dark:bg-blue-900/40 dark:text-blue-300"
						>
							SCIM
						</span>
					{/if}
				</TableCellLink>

				<TableCellLink href={`/recipient/group/${group.id}`} title={group.recipientCount}>
					{group.recipientCount}
				</TableCellLink>

				{#if contextCompanyID}
					<TableCellScope companyID={group.companyID} />
				{/if}
				<TableCellEmpty />
				<TableCellAction>
					<TableDropDownEllipsis>
						<TableDropDownButton
							name="View"
							on:click={() => gotoEditGroupRecipients(group.id)}
							{...globalButtonDisabledAttributes(group, contextCompanyID)}
						/>
						{#if group.isDynamic}
							<TableUpdateButton
								on:click={() => openDynamicUpdateModal(group.id)}
								{...globalButtonDisabledAttributes(group, contextCompanyID)}
							/>
						{:else}
							<TableUpdateButton
								on:click={() => openUpdateModal(group.id)}
								{...globalButtonDisabledAttributes(group, contextCompanyID)}
							/>
						{/if}
						<TableDeleteButton
							on:click={() => openDeleteAlert(group)}
							{...globalButtonDisabledAttributes(group, contextCompanyID)}
						></TableDeleteButton>
					</TableDropDownEllipsis>
				</TableCellAction>
			</TableRow>
		{/each}
	</Table>

	<!-- static group modal -->
	<Modal headerText={modalText} bind:visible={isModalVisible} onClose={closeModal} {isSubmitting}>
		<FormGrid on:submit={onSubmit} bind:bindTo={form} {isSubmitting}>
			<FormColumns>
				<FormColumn>
					<TextField
						required
						minLength={1}
						maxLength={127}
						bind:value={formValues.name}
						placeholder="Marketing">Name</TextField
					>
				</FormColumn>
			</FormColumns>
			<FormError message={modalError} />
			<FormFooter {closeModal} {isSubmitting} />
		</FormGrid>
	</Modal>

	<!-- dynamic group create modal -->
	<Modal
		headerText="New dynamic group"
		bind:visible={isDynamicModalVisible}
		onClose={closeDynamicModal}
		{isSubmitting}
	>
		<FormGrid on:submit={onSubmitDynamicCreate} bind:bindTo={dynamicForm} {isSubmitting}>
			<FormColumns>
				<FormColumn>
					<TextField
						required
						minLength={1}
						maxLength={127}
						bind:value={dynamicFormValues.name}
						placeholder="Marketing">Name</TextField
					>
					<TextFieldSelect
						id="dynamic-group-filter-field"
						required
						bind:value={dynamicFormValues.filterField}
						options={dynamicFilterFields}
					>
						Filter field
					</TextFieldSelect>
					<TextField
						required
						minLength={1}
						maxLength={127}
						bind:value={dynamicFormValues.filterValue}
						placeholder="e.g. Engineering">Filter value</TextField
					>
				</FormColumn>
			</FormColumns>
			<FormError message={dynamicModalError} />
			<FormFooter closeModal={closeDynamicModal} {isSubmitting} />
		</FormGrid>
	</Modal>

	<!-- dynamic group update modal -->
	<Modal
		headerText="Update dynamic group"
		bind:visible={isDynamicUpdateModalVisible}
		onClose={closeDynamicUpdateModal}
		{isSubmitting}
	>
		<FormGrid on:submit={onSubmitDynamicUpdate} bind:bindTo={dynamicUpdateForm} {isSubmitting}>
			<FormColumns>
				<FormColumn>
					<TextField
						required
						minLength={1}
						maxLength={127}
						bind:value={dynamicUpdateValues.name}
						placeholder="Marketing">Name</TextField
					>
					<TextFieldSelect
						id="dynamic-group-filter-field-a"
						required
						bind:value={dynamicUpdateValues.filterField}
						options={dynamicFilterFields}
					>
						Filter field
					</TextFieldSelect>
					<TextField
						required
						minLength={1}
						maxLength={127}
						bind:value={dynamicUpdateValues.filterValue}
						placeholder="e.g. Engineering">Filter value</TextField
					>
				</FormColumn>
			</FormColumns>
			<FormError message={dynamicUpdateError} />
			<FormFooter closeModal={closeDynamicUpdateModal} {isSubmitting} />
		</FormGrid>
	</Modal>

	<DeleteAlert
		list={[
			'All recipients in this group will become orphaned',
			'Campaign data for recipients in this group will be anonymized',
			'Active campaign sends for recipients in this group will be cancelled',
			'This group will be removed from any campaigns it is assigned to'
		]}
		name={deleteValues.name}
		onClick={() => onClickDelete(deleteValues.id)}
		bind:isVisible={isDeleteAlertVisible}
	></DeleteAlert>
</main>
