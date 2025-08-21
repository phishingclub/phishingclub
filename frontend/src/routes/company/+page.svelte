<script>
	import { api } from '$lib/api/apiProxy.js';
	import { onMount } from 'svelte';
	import Headline from '$lib/components/Headline.svelte';
	import TextField from '$lib/components/TextField.svelte';
	import TableRow from '$lib/components/table/TableRow.svelte';
	import TableCell from '$lib/components/table/TableCell.svelte';
	import TableUpdateButton from '$lib/components/table/TableUpdateButton.svelte';
	import TableDeleteButton from '$lib/components/table/TableDeleteButton2.svelte';
	import FormError from '$lib/components/FormError.svelte';
	import { addToast } from '$lib/store/toast';
	import TableCellAction from '$lib/components/table/TableCellAction.svelte';
	import TableCellEmpty from '$lib/components/table/TableCellEmpty.svelte';
	import { newTableURLParams } from '$lib/service/tableURLParams.js';
	import Modal from '$lib/components/Modal.svelte';
	import FormGrid from '$lib/components/FormGrid.svelte';
	import BigButton from '$lib/components/BigButton.svelte';
	import FormColumns from '$lib/components/FormColumns.svelte';
	import FormColumn from '$lib/components/FormColumn.svelte';
	import FormFooter from '$lib/components/FormFooter.svelte';
	import Table from '$lib/components/table/Table.svelte';
	import HeadTitle from '$lib/components/HeadTitle.svelte';
	import { showIsLoading, hideIsLoading } from '$lib/store/loading.js';
	import TableDropDownEllipsis from '$lib/components/table/TableDropDownEllipsis.svelte';
	import DeleteAlert from '$lib/components/modal/DeleteAlert.svelte';
	import TableDropDownButton from '$lib/components/table/TableDropDownButton.svelte';

	// bindings
	let form = null;
	const formValues = {
		name: null
	};
	// data
	let modalError = '';
	let companies = [];
	const tableURLParams = newTableURLParams();
	let isModalVisible = false;
	let isSubmitting = false;
	let isTableLoading = true;
	let modalMode = null;
	let modalText = '';

	let isDeleteAlertVisible = false;
	let deleteValues = {
		id: null,
		name: null
	};

	$: {
		modalText = modalMode === 'create' ? 'New company' : 'Update company';
	}

	// hooks
	onMount(() => {
		refreshCompanies();
		tableURLParams.onChange(refreshCompanies);
		return () => {
			tableURLParams.unsubscribe();
		};
	});

	// component logic
	const refreshCompanies = async () => {
		try {
			isTableLoading = true;
			companies = await getCompanies();
		} catch (e) {
			addToast('Failed to get companies', 'Error');
			console.error('failed to get companies', e);
		} finally {
			isTableLoading = false;
		}
	};

	/**
	 * Gets a company by ID
	 * @param {string} id
	 */
	const getCompany = async (id) => {
		try {
			const res = await api.company.getByID(id);
			if (res.success) {
				return res.data;
			} else {
				throw res.error;
			}
		} catch (e) {
			addToast('Failed to get company', 'Error');
			console.error('failed to get company', e);
		}
	};

	const getCompanies = async () => {
		try {
			const res = await api.company.getAll(tableURLParams);
			if (res.success) {
				return res.data.rows;
			}
			throw new res.error();
		} catch (e) {
			addToast('Failed to getcompanies', 'Error');
			console.error('failed to get companies', e);
		}
		return [];
	};

	const onSubmit = async () => {
		try {
			isSubmitting = true;
			if (modalMode === 'create') {
				await create();
			} else {
				await update();
			}
		} finally {
			isSubmitting = false;
		}
	};

	const create = async () => {
		modalError = '';
		try {
			const res = await api.company.create(formValues.name);
			if (!res.success) {
				modalError = res.error;
				return;
			}
			addToast('Company created', 'Success');
			closeModal();
		} catch (e) {
			addToast('Failed to create company', 'Error');
			console.error('failed to create company:', e);
		}
		refreshCompanies();
	};

	const update = async () => {
		modalError = '';
		try {
			const res = await api.company.update(formValues.id, formValues.name);
			if (!res.success) {
				modalError = res.error;
				return;
			}
			addToast('Company updated', 'Success');
			closeUpdateModal();
		} catch (e) {
			addToast('Failed to update company', 'Error');
			console.error('failed to update company', e);
		}
		refreshCompanies();
	};

	const openDeleteAlert = async (company) => {
		isDeleteAlertVisible = true;
		deleteValues.id = company.id;
		deleteValues.name = company.name;
	};

	/**
	 * Deletes a company
	 * @param {number} id
	 */
	const onClickDelete = async (id) => {
		const action = api.company.delete(id);
		action
			.then((res) => {
				if (!res.success) {
					throw res.error;
				}
				refreshCompanies();
			})
			.catch((e) => {
				console.error('failed to delete company:', e);
			});
		return action;
	};

	const openCreateModal = () => {
		modalMode = 'create';
		modalError = '';
		isModalVisible = true;
	};

	const closeModal = () => {
		modalError = '';
		isModalVisible = false;
		form.reset();
	};

	/**
	 * @param {string} id
	 */
	const openUpdateModal = async (id) => {
		modalMode = 'update';
		try {
			showIsLoading();
			const company = await getCompany(id);
			formValues.id = company.id;
			formValues.name = company.name;
			isModalVisible = true;
		} catch (e) {
			addToast('Failed to get company', 'Error');
			console.error('failed to get company', e);
		} finally {
			hideIsLoading();
		}
	};

	const closeUpdateModal = () => {
		isModalVisible = false;
		modalError = '';
		form.reset();
	};

	const onClickExport = async (id) => {
		try {
			showIsLoading();
			api.company.export(id);
		} catch (e) {
			addToast('Failed to export company events', 'Error');
			console.error('failed to export company events', e);
		} finally {
			hideIsLoading();
		}
	};
</script>

<HeadTitle title="companies" />
<main>
	<Headline>Companies</Headline>
	<BigButton on:click={openCreateModal}>New company</BigButton>
	<BigButton on:click={() => onClickExport()}>Export shared</BigButton>
	<Table
		columns={[{ column: 'Name', size: 'large' }]}
		sortable={['name']}
		hasData={!!companies.length}
		plural="companies"
		pagination={tableURLParams}
		isGhost={isTableLoading}
	>
		{#each companies as company}
			<TableRow>
				<TableCell>
					<button
						on:click={() => {
							openUpdateModal(company.id);
						}}
					>
						{company.name}
					</button>
				</TableCell>
				<TableCellEmpty />
				<TableCellAction>
					<TableDropDownEllipsis>
						<TableUpdateButton on:click={() => openUpdateModal(company.id)} />
						<TableDropDownButton name="Export" on:click={() => onClickExport(company.id)} />
						<TableDeleteButton on:click={() => openDeleteAlert(company)} />
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
						required
						minLength={1}
						maxLength={64}
						placeholder="Alices Enterprise Solutions"
						bind:value={formValues.name}>Name</TextField
					>
				</FormColumn>
			</FormColumns>
			<FormError message={modalError} />
			<FormFooter {closeModal} {isSubmitting} />
		</FormGrid>
	</Modal>

	<DeleteAlert
		list={['All data related to the company such as domains, campaign, recipients will be lost']}
		name={deleteValues.name}
		onClick={() => onClickDelete(deleteValues.id)}
		bind:isVisible={isDeleteAlertVisible}
	></DeleteAlert>
</main>
