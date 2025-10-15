<script>
	import { page } from '$app/stores';
	import { api } from '$lib/api/apiProxy.js';
	import { onMount } from 'svelte';
	import { newTableURLParams } from '$lib/service/tableURLParams.js';
	import { globalButtonDisabledAttributes } from '$lib/utils/form.js';
	import Headline from '$lib/components/Headline.svelte';
	import TextField from '$lib/components/TextField.svelte';
	import CheckboxField from '$lib/components/CheckboxField.svelte';
	import TableRow from '$lib/components/table/TableRow.svelte';
	import TableCell from '$lib/components/table/TableCell.svelte';
	import TableUpdateButton from '$lib/components/table/TableUpdateButton.svelte';
	import TableDeleteButton from '$lib/components/table/TableDeleteButton2.svelte';
	import FormError from '$lib/components/FormError.svelte';
	import { addToast } from '$lib/store/toast';
	import { AppStateService } from '$lib/service/appState';
	import TableCellAction from '$lib/components/table/TableCellAction.svelte';
	import TableCellEmpty from '$lib/components/table/TableCellEmpty.svelte';
	import FormGrid from '$lib/components/FormGrid.svelte';
	import Modal from '$lib/components/Modal.svelte';
	import BigButton from '$lib/components/BigButton.svelte';
	import FormColumns from '$lib/components/FormColumns.svelte';
	import FormColumn from '$lib/components/FormColumn.svelte';
	import FormFooter from '$lib/components/FormFooter.svelte';
	import Table from '$lib/components/table/Table.svelte';
	import HeadTitle from '$lib/components/HeadTitle.svelte';
	import TableCellCheck from '$lib/components/table/TableCellCheck.svelte';
	import { debounceTyping, getModalText } from '$lib/utils/common';
	import TableCopyButton from '$lib/components/table/TableCopyButton.svelte';
	import { showIsLoading, hideIsLoading } from '$lib/store/loading.js';
	import TextareaField from '$lib/components/TextareaField.svelte';
	import FileField from '$lib/components/FileField.svelte';
	import TableDropDownEllipsis from '$lib/components/table/TableDropDownEllipsis.svelte';
	import DeleteAlert from '$lib/components/modal/DeleteAlert.svelte';
	import SelectSquare from '$lib/components/SelectSquare.svelte';
	import TableCellScope from '$lib/components/table/TableCellScope.svelte';

	// services
	const appStateService = AppStateService.instance;

	// data
	let form = null;
	let formValues = {
		id: null,
		name: null,
		cidrs: null,
		allowed: null
	};
	let allowDenyList = [];
	let formError = '';
	let contextCompanyID = null;
	const tableURLParams = newTableURLParams();
	let isModalVisible = false;
	let isSubmitting = false;
	let isTableLoading = false;
	let modalMode = null;
	let modalText = '';

	let isDeleteAlertVisible = false;
	let deleteValues = {
		id: null,
		name: null
	};

	$: {
		modalText = getModalText('IP filter', modalMode);
	}

	// hooks
	onMount(() => {
		if (appStateService.getContext()) {
			contextCompanyID = appStateService.getContext().companyID;
		}
		refreshAllowDenies();
		tableURLParams.onChange(refreshAllowDenies);

		(async () => {
			const editID = $page.url.searchParams.get('edit');
			if (editID) {
				await openUpdateModal(editID);
			}
		})();

		return () => {
			tableURLParams.unsubscribe();
		};
	});

	// component logic
	const refreshAllowDenies = async () => {
		try {
			isTableLoading = true;
			allowDenyList = await getAllAllowDenyEntries();
		} catch (e) {
			addToast('Failed to get IP filters', 'Error');
			console.error(e);
		} finally {
			isTableLoading = false;
		}
	};

	/**
	 * @param {string} id
	 */
	const getAllowDenyListEntry = async (id) => {
		try {
			const res = await api.allowDeny.getByID(id);
			if (res.success) {
				return res.data;
			} else {
				throw res.error;
			}
		} catch (e) {
			addToast('Failed to get IP filter', 'Error');
			console.error('failed to get IP filter', e);
		}
	};

	const getAllAllowDenyEntries = async () => {
		try {
			const res = await api.allowDeny.getAllOverview(tableURLParams, contextCompanyID);
			if (!res.success) {
				throw res.error;
			}
			return res.data.rows;
		} catch (e) {
			addToast('Failed to get IP filters', 'Error');
			console.error('failed to get IP filters', e);
		}
		return [];
	};

	const onClickSubmit = async () => {
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
		formError = '';
		formValues.cidrs = formValues.cidrs
			.split('\n')
			.map((line) => singleIPToCIDR(line))
			.filter((line) => line.length > 0)
			.join('\n');

		try {
			const res = await api.allowDeny.create({
				name: formValues.name,
				cidrs: formValues.cidrs,
				allowed: formValues.allowed,
				companyID: contextCompanyID
			});
			if (!res.success) {
				formError = res.error;
				return;
			}
			addToast('Created IP filter', 'Success');
			closeModal();
		} catch (err) {
			addToast('Failed to create IP filter', 'Error');
			console.error('failed to create IP filter:', err);
		}
		refreshAllowDenies();
	};

	const update = async () => {
		formError = '';
		formValues.cidrs = formValues.cidrs
			.split('\n')
			.map((line) => singleIPToCIDR(line))
			.filter((line) => line.length > 0)
			.join('\n');

		try {
			const res = await api.allowDeny.update({
				id: formValues.id,
				name: formValues.name,
				cidrs: formValues.cidrs,
				companyID: formValues.companyID
			});
			if (res.success) {
				addToast('Updated IP filter', 'Success');
				closeModal();
			} else {
				formError = res.error;
			}
		} catch (e) {
			addToast('Failed to update IP filter', 'Error');
			console.error('failed to update IP filter', e);
		}
		refreshAllowDenies();
	};

	const openDeleteAlert = async (domain) => {
		isDeleteAlertVisible = true;
		deleteValues.id = domain.id;
		deleteValues.name = domain.name;
	};

	/**
	 * @param {string} id
	 */
	const onClickDelete = async (id) => {
		const action = api.allowDeny.delete(id);
		action
			.then((res) => {
				if (res.success) {
					refreshAllowDenies();
					return;
				}
				throw res.error;
			})
			.catch((e) => {
				console.error('failed to delete IP filter:', e);
			});
		return action;
	};

	const openCreateModal = () => {
		modalMode = 'create';
		isModalVisible = true;
	};

	const closeModal = () => {
		formError = '';
		isModalVisible = false;
		form.reset();
	};

	/**
	 * Opens the update modal
	 * @param {string} id
	 */
	const openUpdateModal = async (id) => {
		modalMode = 'update';
		try {
			showIsLoading();
			const allowDeny = await getAllowDenyListEntry(id);
			const r = globalButtonDisabledAttributes(allowDeny, contextCompanyID);
			if (r.disabled) {
				hideIsLoading();
				console.log(r.title);
				return;
			}
			assignAllowDeny(allowDeny);
			isModalVisible = true;
		} catch (e) {
			addToast('Failed to get IP filter', 'Error');
			console.error('failed to get IP filter', e);
		} finally {
			hideIsLoading();
		}
	};

	const openCopyModal = async (id) => {
		modalMode = 'copy';

		try {
			showIsLoading();
			const allowDeny = await getAllowDenyListEntry(id);
			assignAllowDeny(allowDeny);
			allowDeny.id = null;
			isModalVisible = true;
		} catch (e) {
			addToast('Failed to get IP filter', 'Error');
			console.error('failed to get IP filter', e);
		} finally {
			hideIsLoading();
		}
	};

	const assignAllowDeny = (allowDeny) => {
		formValues = {
			id: allowDeny.id,
			name: allowDeny.name,
			cidrs: allowDeny.cidrs,
			allowed: allowDeny.allowed,
			companyID: allowDeny.companyID
		};
	};

	/** @param {string} ip */
	const singleIPToCIDR = (ip) => {
		if (ip.trim() == '') {
			return '';
		}
		if (ip.includes('/')) {
			return ip;
		}
		if (ip.includes(':')) {
			return ip + '/128';
		}
		return ip + '/32';
	};

	/** @param {*} event */
	const onSetFile = (event) => {
		// read file from event
		const file = event.target.files[0];
		const reader = new FileReader();
		reader.onload = (e) => {
			formValues.cidrs = e.target.result;
		};
		reader.readAsText(file);
		formValues.cidrs = file;
		// reset field
		event.target.value = '';
	};
</script>

<HeadTitle title="IP filter" />
<main>
	<Headline>IP filters</Headline>
	<BigButton on:click={openCreateModal}>New</BigButton>
	<Table
		columns={[
			{ column: 'Name', size: 'large' },
			{ column: 'Allowed', size: 'small', alignText: 'center' },
			...(contextCompanyID ? [{ column: 'Scope', size: 'small' }] : [])
		]}
		sortable={['Name', 'Allowed', ...(contextCompanyID ? ['scope'] : [])]}
		hasData={!!allowDenyList.length}
		plural="Allow deny entries"
		pagination={tableURLParams}
		isGhost={isTableLoading}
	>
		{#each allowDenyList as entry}
			<TableRow>
				<TableCell>
					<button
						on:click={() => {
							openUpdateModal(entry.id);
						}}
						{...globalButtonDisabledAttributes(entry, contextCompanyID)}
						title={entry.name}
					>
						{entry.name}
					</button>
				</TableCell>
				<TableCellCheck value={entry.allowed} />
				{#if contextCompanyID}
					<TableCellScope companyID={entry.companyID} />
				{/if}
				<TableCellEmpty />
				<TableCellAction>
					<TableDropDownEllipsis>
						<TableUpdateButton
							on:click={() => openUpdateModal(entry.id)}
							{...globalButtonDisabledAttributes(entry, contextCompanyID)}
						/>
						<TableCopyButton
							title={'Copy'}
							on:click={() => openCopyModal(entry.id)}
							{...globalButtonDisabledAttributes(entry, contextCompanyID)}
						/>

						<TableDeleteButton
							on:click={() => openDeleteAlert(entry)}
							{...globalButtonDisabledAttributes(entry, contextCompanyID)}
						></TableDeleteButton>
					</TableDropDownEllipsis>
				</TableCellAction>
			</TableRow>
		{/each}
	</Table>

	<Modal headerText={modalText} visible={isModalVisible} onClose={closeModal} {isSubmitting}>
		<FormGrid on:submit={onClickSubmit} bind:bindTo={form} {isSubmitting}>
			<FormColumns>
				<FormColumn>
					<TextField
						required
						minLength={1}
						maxLength={127}
						bind:value={formValues.name}
						placeholder="Company allow range">Name</TextField
					>
					<FileField on:change={(event) => onSetFile(event)} optional
						>Load content from file</FileField
					>
					{#if modalMode === 'create' || modalMode === 'copy'}
						<SelectSquare
							label="Filter Type"
							options={[
								{ value: true, label: 'Allow' },
								{ value: false, label: 'Deny' }
							]}
							bind:value={formValues.allowed}
						/>
					{/if}
					<TextareaField
						required
						minLength="1"
						bind:value={formValues.cidrs}
						placeholder="8.8.8.8/16"
						toolTipText="Newlines seperated CIDRs">CIDRs</TextareaField
					>
				</FormColumn>
			</FormColumns>
			<FormError message={formError} />
			<FormFooter {closeModal} {isSubmitting} />
		</FormGrid>
	</Modal>
	<DeleteAlert
		name={deleteValues.name}
		onClick={() => onClickDelete(deleteValues.id)}
		bind:isVisible={isDeleteAlertVisible}
	></DeleteAlert>
</main>
