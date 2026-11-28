<script>
	import { LANGUAGES } from '$lib/utils/languages.js';
	import { api } from '$lib/api/apiProxy.js';
	import { newTableURLParams } from '$lib/service/tableURLParams.js';
	import { onDestroy, onMount } from 'svelte';
	import StatusBadge from '$lib/components/StatusBadge.svelte';
	import { globalButtonDisabledAttributes } from '$lib/utils/form.js';
	import { goto } from '$app/navigation';
	import Headline from '$lib/components/Headline.svelte';
	import TextField from '$lib/components/TextField.svelte';
	import DynamicLabel from '$lib/components/DynamicLabel.svelte';
	import Tag from '$lib/components/Tag.svelte';
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
	import TableCellCheckbox from '$lib/components/table/TableCellCheckbox.svelte';
	import BulkActionBar from '$lib/components/table/BulkActionBar.svelte';
	import {
		createTableSelection,
		headerSelectionState,
		runBulkDelete
	} from '$lib/service/tableSelection.js';

	// services
	const appStateService = AppStateService.instance;

	// the recipient fields that can be used as a dynamic filter
	const dynamicFilterFields = ['position', 'department', 'city', 'country', 'misc', 'language'];
	const dynamicFilterFieldLabels = {
		position: 'Position',
		department: 'Department',
		city: 'City',
		country: 'Country',
		misc: 'Miscellaneous',
		language: 'Language'
	};
	// the language filter picks from the fixed list, the field holds codes
	const filterLanguageOptions = LANGUAGES.map((l) => ({ value: l.code, label: l.name }));
	// example value per filter field, shown as the Filter value placeholder
	const dynamicFilterValueExamples = {
		position: 'e.g. Engineer',
		department: 'e.g. Engineering',
		city: 'e.g. Copenhagen',
		country: 'e.g. Denmark',
		misc: 'e.g. VIP'
	};
	const filterValuePlaceholder = (field) =>
		dynamicFilterValueExamples[field] ??
		dynamicFilterValueExamples[
			Object.keys(dynamicFilterFieldLabels).find((k) => dynamicFilterFieldLabels[k] === field)
		] ??
		'e.g. Engineer';
	// labels for display only, the values sent to the API stay the raw field keys
	const dynamicFilterFieldOptions = dynamicFilterFields.map((value) => ({
		value,
		label: dynamicFilterFieldLabels[value]
	}));

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

	// multi select
	const selection = createTableSelection();
	let isBulkDeleteAlertVisible = false;
	// a row cannot be selected when its delete action is disabled in shared view
	const isRowDisabled = (group) => globalButtonDisabledAttributes(group, contextCompanyID).disabled;
	$: selectablePageIds = groups
		.filter((g) => !globalButtonDisabledAttributes(g, contextCompanyID).disabled)
		.map((g) => g.id);
	$: headerState = headerSelectionState($selection, selectablePageIds);
	$: showMultiSelect = groups.length > 0;

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

	// a filter value only fits the field it was entered for, so it is cleared
	// when the field changes
	let dynamicCreateField = dynamicFormValues.filterField;
	$: if (dynamicFormValues.filterField !== dynamicCreateField) {
		dynamicCreateField = dynamicFormValues.filterField;
		dynamicFormValues.filterValue = null;
	}
	let dynamicUpdateField = dynamicUpdateValues.filterField;
	$: if (dynamicUpdateValues.filterField !== dynamicUpdateField) {
		dynamicUpdateField = dynamicUpdateValues.filterField;
		dynamicUpdateValues.filterValue = '';
	}

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

	// while a listed group is being deleted in the background the list is
	// read again until it is gone
	const deletingRefreshEvery = 5000;
	let deletingTimer = null;
	let isDestroyed = false;
	const watchDeleting = (rows) => {
		if (isDestroyed || !rows.some((g) => g.deletingAt)) {
			clearTimeout(deletingTimer);
			deletingTimer = null;
			return;
		}
		if (deletingTimer) return;
		deletingTimer = setTimeout(async () => {
			await refreshGroups(false);
			deletingTimer = null;
			watchDeleting(groups);
		}, deletingRefreshEvery);
	};
	$: watchDeleting(groups);
	onDestroy(() => {
		isDestroyed = true;
		clearTimeout(deletingTimer);
		deletingTimer = null;
	});

	// a group being deleted can not be changed
	const deletingAttributes = (group) =>
		group.deletingAt ? { disabled: true, title: 'The group is being deleted' } : {};

	// a background refresh toasts only the first failure in a row, not one
	// every few seconds
	let refreshFailing = false;
	const refreshGroups = async (showLoading = true) => {
		const background = showLoading === false;
		try {
			isTableLoading = !background;
			// selection is current page only, drop it on a reload the user asked
			// for, a background refresh of the same page keeps it
			if (!background) selection.clear();
			const res = await api.recipient.getAllGroups(tableURLParams, contextCompanyID);
			if (!res.success) {
				throw res.error;
			}
			groups = res.data?.rows ?? [];
			groupsHasNextPage = !!res.data?.hasNextPage;
			refreshFailing = false;
		} catch (e) {
			if (!background || !refreshFailing) {
				addToast('Failed to load groups', 'Error');
			}
			refreshFailing = true;
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

	const onClickBulkDelete = async () => {
		await runBulkDelete({
			ids: [...$selection],
			deleteFn: api.recipient.deleteGroup,
			noun: 'group',
			doneVerb: 'Deleting'
		});
		await refreshGroups();
		return { success: true };
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
			dynamicUpdateField = dynamicUpdateValues.filterField;
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
	<Headline docSlug="groups">Groups</Headline>
	<div class="flex flex-wrap gap-3 mt-6 mb-10 [&>button]:m-0">
		<BigButton on:click={openCreateModal}>New group</BigButton>
		<BigButton variant="secondary" on:click={openDynamicCreateModal}>New dynamic group</BigButton>
	</div>
	<BulkActionBar
		count={$selection.size}
		noun="group"
		on:delete={() => (isBulkDeleteAlertVisible = true)}
		on:clear={() => selection.clear()}
	/>
	<Table
		selectable
		{headerState}
		on:toggleAll={(e) => selection.setPageSelection(selectablePageIds, e.detail)}
		columns={[
			{ column: 'Name', size: 'large' },
			{ column: 'Count', size: 'small', alignText: 'center' },
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
				{#if showMultiSelect}
					<TableCellCheckbox
						checked={$selection.has(group.id)}
						disabled={isRowDisabled(group)}
						on:change={() => selection.toggle(group.id)}
					/>
				{/if}
				<TableCellLink href={`/recipient/group/${group.id}`} title={group.name} wrap>
					{group.name}
					{#if group.isDynamic}
						<DynamicLabel class="ml-2" />
					{/if}
					{#if group.deletingAt}
						<span class="ml-2"
							><StatusBadge
								tone={group.deleteFailure ? 'danger' : 'neutral'}
								title={group.deleteFailure
									? `The delete is waiting: ${group.deleteFailure}. Remove it and the delete continues on its own.`
									: 'The group is deleted in the background'}>Deleting</StatusBadge
							></span
						>
					{/if}
					{#if group.scimEnabled}
						<Tag title="Managed through SCIM" class="ml-2">SCIM</Tag>
					{/if}
				</TableCellLink>

				<TableCellLink href={`/recipient/group/${group.id}`} title={group.recipientCount}>
					<span class="block text-center">{group.recipientCount}</span>
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
								{...deletingAttributes(group)}
							/>
						{:else}
							<TableUpdateButton
								on:click={() => openUpdateModal(group.id)}
								{...globalButtonDisabledAttributes(group, contextCompanyID)}
								{...deletingAttributes(group)}
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
						placeholder="e.g. Marketing">Name</TextField
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
						placeholder="e.g. Marketing">Name</TextField
					>
					<TextFieldSelect
						id="dynamic-group-filter-field"
						required
						bind:value={dynamicFormValues.filterField}
						options={dynamicFilterFieldOptions}
					>
						Filter field
					</TextFieldSelect>
					{#if dynamicFormValues.filterField === 'language'}
						<TextFieldSelect
							id="dynamic-group-filter-language"
							required
							bind:value={dynamicFormValues.filterValue}
							toolTipText="Recipients with this language are included automatically"
							options={filterLanguageOptions}>Filter value</TextFieldSelect
						>
					{:else}
						<TextField
							required
							minLength={1}
							maxLength={127}
							bind:value={dynamicFormValues.filterValue}
							toolTipText="Recipients whose field equals this value are included automatically"
							placeholder={filterValuePlaceholder(dynamicFormValues.filterField)}
							>Filter value</TextField
						>
					{/if}
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
						placeholder="e.g. Marketing">Name</TextField
					>
					<TextFieldSelect
						id="dynamic-group-filter-field-a"
						required
						bind:value={dynamicUpdateValues.filterField}
						options={dynamicFilterFieldOptions}
					>
						Filter field
					</TextFieldSelect>
					{#if dynamicUpdateValues.filterField === 'language'}
						<TextFieldSelect
							id="dynamic-group-filter-language-a"
							required
							bind:value={dynamicUpdateValues.filterValue}
							toolTipText="Recipients with this language are included automatically"
							options={filterLanguageOptions}>Filter value</TextFieldSelect
						>
					{:else}
						<TextField
							required
							minLength={1}
							maxLength={127}
							bind:value={dynamicUpdateValues.filterValue}
							toolTipText="Recipients whose field equals this value are included automatically"
							placeholder={filterValuePlaceholder(dynamicUpdateValues.filterField)}
							>Filter value</TextField
						>
					{/if}
				</FormColumn>
			</FormColumns>
			<FormError message={dynamicUpdateError} />
			<FormFooter closeModal={closeDynamicUpdateModal} {isSubmitting} />
		</FormGrid>
	</Modal>

	<DeleteAlert
		list={[
			'Recipients not in any other group will become orphaned',
			'Active campaign sends for recipients in this group will be cancelled',
			'This group will be removed from any campaigns it is assigned to',
			'Recipients are kept and their campaign history is left intact',
			'A group used by an open program cannot be deleted'
		]}
		name={deleteValues.name}
		onClick={() => onClickDelete(deleteValues.id)}
		bind:isVisible={isDeleteAlertVisible}
	></DeleteAlert>
	<DeleteAlert
		title="Delete groups"
		list={[
			'Recipients not in any other group will become orphaned',
			'Active campaign sends for recipients in these groups will be cancelled',
			'These groups will be removed from any campaigns they are assigned to',
			'Recipients are kept and their campaign history is left intact',
			'Groups used by an open program cannot be deleted'
		]}
		name={`${$selection.size} group${$selection.size === 1 ? '' : 's'}`}
		onClick={onClickBulkDelete}
		confirm
		bind:isVisible={isBulkDeleteAlertVisible}
	></DeleteAlert>
</main>
