<script>
	import { api } from '$lib/api/apiProxy.js';
	import { newTableURLParams } from '$lib/service/tableURLParams.js';
	import { onMount } from 'svelte';
	import { globalButtonDisabledAttributes } from '$lib/utils/form.js';
	import { goto } from '$app/navigation';
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

	// data
	let form = null;
	let modalError = '';
	let formValues = {
		name: null,
		companyID: null,
		recipients: []
	};
	let isModalVisible = false;
	let isSubmitting = false;
	let isTableLoading = false;
	const tableURLParams = newTableURLParams();
	let contextCompanyID = '';
	let groups = [];
	let modalMode = null;
	let modalText = '';

	let isDeleteAlertVisible = false;
	let deleteValues = {
		id: null,
		name: null
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

	const closeModal = () => {
		isModalVisible = false;
		form.reset();
		modalError = '';
	};
</script>

<HeadTitle title="Groups" />
<main>
	<Headline>Groups</Headline>
	<div class="flex gap-3">
		<BigButton on:click={openCreateModal}>New group</BigButton>
		<BigButton on:click={() => goto('/recipient/orphaned/')}>View Orphaned</BigButton>
	</div>
	<Table
		columns={[
			{ column: 'Name', size: 'large' },
			{ column: 'Count', size: 'small' },
			...(contextCompanyID ? [{ column: 'Scope', size: 'small' }] : [])
		]}
		sortable={['Name', ...(contextCompanyID ? ['scope'] : [])]}
		hasData={!!groups.length}
		plural="groups"
		pagination={tableURLParams}
		isGhost={isTableLoading}
	>
		{#each groups as group}
			<TableRow>
				<TableCellLink href={`/recipient/group/${group.id}`} title={group.name}>
					{group.name}
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
						<TableUpdateButton
							on:click={() => openUpdateModal(group.id)}
							{...globalButtonDisabledAttributes(group, contextCompanyID)}
						/>
						<TableDeleteButton
							on:click={() => openDeleteAlert(group)}
							{...globalButtonDisabledAttributes(group, contextCompanyID)}
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
						maxLength={127}
						bind:value={formValues.name}
						placeholder="Marketing">Name</TextField
					>
					<section>
						{#each formValues.recipients as recipient}
							<div class="row">
								<div class="col-12">
									{recipient.email}
								</div>
							</div>
						{/each}
					</section>
				</FormColumn>
			</FormColumns>
			<FormError message={modalError} />
			<FormFooter {closeModal} {isSubmitting} />
		</FormGrid>
	</Modal>
	<DeleteAlert
		list={[
			'All assets will be deleted',
			'Templates using this domain will become unusable',
			'Scheduled or active campaigns using this domain will be closed'
		]}
		name={deleteValues.name}
		onClick={() => onClickDelete(deleteValues.id)}
		bind:isVisible={isDeleteAlertVisible}
	></DeleteAlert>
</main>
