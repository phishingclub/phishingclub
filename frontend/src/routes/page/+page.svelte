<script>
	import { page } from '$app/stores';
	import { api } from '$lib/api/apiProxy.js';
	import { onMount } from 'svelte';
	import { newTableURLParams } from '$lib/service/tableURLParams.js';
	import { globalButtonDisabledAttributes } from '$lib/utils/form.js';
	import Headline from '$lib/components/Headline.svelte';
	import TextField from '$lib/components/TextField.svelte';
	import TableRow from '$lib/components/table/TableRow.svelte';
	import TableCell from '$lib/components/table/TableCell.svelte';
	import TableCellLink from '$lib/components/table/TableCellLink.svelte';
	import TableUpdateButton from '$lib/components/table/TableUpdateButton.svelte';
	import TableDeleteButton from '$lib/components/table/TableDeleteButton2.svelte';
	import FormError from '$lib/components/FormError.svelte';
	import { addToast } from '$lib/store/toast';
	import { AppStateService } from '$lib/service/appState';
	import TableCellEmpty from '$lib/components/table/TableCellEmpty.svelte';
	import TableCellAction from '$lib/components/table/TableCellAction.svelte';
	import Modal from '$lib/components/Modal.svelte';
	import FormGrid from '$lib/components/FormGrid.svelte';

	import BigButton from '$lib/components/BigButton.svelte';
	import FormColumns from '$lib/components/FormColumns.svelte';
	import FormColumn from '$lib/components/FormColumn.svelte';
	import FormFooter from '$lib/components/FormFooter.svelte';
	import Table from '$lib/components/table/Table.svelte';
	import HeadTitle from '$lib/components/HeadTitle.svelte';
	import { getModalText } from '$lib/utils/common';
	import TableCopyButton from '$lib/components/table/TableCopyButton.svelte';
	import { showIsLoading, hideIsLoading } from '$lib/store/loading.js';
	import TableDropDownEllipsis from '$lib/components/table/TableDropDownEllipsis.svelte';
	import TableCellScope from '$lib/components/table/TableCellScope.svelte';
	import DeleteAlert from '$lib/components/modal/DeleteAlert.svelte';
	import Editor from '$lib/components/editor/Editor.svelte';
	import { fetchAllRows } from '$lib/utils/api-utils';
	import { BiMap } from '$lib/utils/maps';
	import AutoRefresh from '$lib/components/AutoRefresh.svelte';

	// services
	const appStateService = AppStateService.instance;

	// bindings
	let form = null;
	let formValues = {
		id: null,
		name: null,
		content: null
	};
	let isSubmitting = false;

	// data
	const tableURLParams = newTableURLParams();
	let contextCompanyID = null;
	let pages = [];
	let domainMap = new BiMap({});
	let formError = '';
	let isModalVisible = false;
	let isPageTableLoading = false;
	let modalMode = null;
	let modalText = '';

	let isDeleteAlertVisible = false;
	let deleteValues = {
		id: null,
		name: null
	};

	$: {
		modalText = getModalText('page', modalMode);
	}

	// hooks
	onMount(() => {
		const context = appStateService.getContext();
		if (context) {
			contextCompanyID = context.companyID;
		}
		refreshPages();
		tableURLParams.onChange(refreshPages);
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
	const refreshPages = async (showLoading = true) => {
		try {
			if (showLoading) {
				isPageTableLoading = true;
			}
			const res = await getPages();
			pages = res.rows;
		} catch (e) {
			addToast('Failed to load pages', 'Error');
			console.error('Failed to load pages', e);
		} finally {
			if (showLoading) {
				isPageTableLoading = false;
			}
		}
	};

	const refreshAllDomains = async () => {
		const domains = await fetchAllRows((options) => {
			return api.domain.getAllSubsetWithoutProxies(options, contextCompanyID);
		});
		domainMap = BiMap.FromArrayOfObjects(domains);
	};

	const getPages = async () => {
		try {
			const res = await api.page.getOverviews(tableURLParams, contextCompanyID);
			if (res.success) {
				return res.data;
			}
			throw res.error;
		} catch (e) {
			addToast('Failed to load pages', 'Error');
			console.error('failed to get pages', e);
		}
		return [];
	};

	/** @param {string} id */
	const getPage = async (id) => {
		try {
			const res = await api.page.getByID(id);
			if (!res.success) {
				throw res.error;
			}
			return res.data;
		} catch (e) {
			addToast('Failed to load page', 'Error');
			console.error('failed to get page', e);
		}
	};

	const onSubmit = async (event) => {
		try {
			isSubmitting = true;
			const saveOnly = event?.detail?.saveOnly || false;
			if (modalMode === 'create' || modalMode === 'copy') {
				await create();
				return;
			} else {
				await update(saveOnly);
				return;
			}
		} finally {
			isSubmitting = false;
		}
	};

	const create = async () => {
		try {
			const res = await api.page.create(formValues.name, formValues.content, contextCompanyID);
			if (!res.success) {
				formError = res.error;
				return;
			}
			formError = '';
			addToast('Page created', 'Success');
			closeModal();
			refreshPages();
		} catch (err) {
			addToast('Failed to create page', 'Error');
			console.error('failed to create page:', err);
		}
	};

	const update = async (saveOnly = false) => {
		try {
			const updateData = {
				name: formValues.name,
				content: formValues.content
			};

			const res = await api.page.update(formValues.id, updateData);
			if (!res.success) {
				formError = res.error;
				return;
			}
			formError = '';
			addToast(saveOnly ? 'Page saved' : 'Page updated', 'Success');
			if (!saveOnly) {
				closeModal();
			}
			refreshPages();
		} catch (e) {
			addToast(saveOnly ? 'Failed to save page' : 'Failed to update page', 'Error');
			console.error('failed to update page', e);
		}
	};

	/** @param {string} id */
	const onClickDelete = async (id) => {
		const action = api.page.delete(id);
		action
			.then((res) => {
				if (res.success) {
					refreshPages();
					return;
				}
				throw res.error;
			})
			.catch((e) => {
				console.error('failed to delete page:', e);
			});
		return action;
	};

	const openCreateModal = () => {
		modalMode = 'create';
		refreshAllDomains();
		isModalVisible = true;
	};

	const closeModal = () => {
		isModalVisible = false;
		formValues.content = '';
		formValues.name = '';
		formValues.id = '';
		form.reset();
		formError = '';
	};

	/** @param {string} id */
	const openUpdateModal = async (id) => {
		modalMode = 'update';
		refreshAllDomains();
		showIsLoading();

		// Reset form values first
		formValues = {
			id: null,
			name: null,
			content: null
		};

		try {
			const page = await getPage(id);
			const r = globalButtonDisabledAttributes(page, contextCompanyID);
			if (r.disabled) {
				hideIsLoading();
				return;
			}

			assignPage(page);
			isModalVisible = true;
		} catch (e) {
			addToast('Failed to load page', 'Error');
			console.error('failed to get page', e);
		} finally {
			hideIsLoading();
		}
	};

	const openCopyModal = async (id) => {
		modalMode = 'copy';
		showIsLoading();

		// Reset form values first
		formValues = {
			id: null,
			name: null,
			content: null
		};

		try {
			const page = await getPage(id);
			assignPage(page);
			formValues.id = null; // Clear ID for copy
			isModalVisible = true;
		} catch (e) {
			addToast('Failed to load page', 'Error');
			console.error('failed to get page', e);
		} finally {
			hideIsLoading();
		}
	};

	const openDeleteAlert = async (page) => {
		isDeleteAlertVisible = true;
		deleteValues.id = page.id;
		deleteValues.name = page.name;
	};

	const assignPage = (page) => {
		formValues.id = page.id;
		formValues.name = page.name;
		formValues.content = page.content || '';
	};

	/** @param {*} event */
	const onSetFile = (event) => {
		// read file from event
		const file = event.target.files[0];
		const reader = new FileReader();
		reader.onload = (e) => {
			formValues.content = e.target.result;
		};
		reader.readAsText(file);
		formValues.content = file;
		// reset field
		event.target.value = '';
	};
</script>

<HeadTitle title="Pages" />
<main>
	<div class="flex justify-between">
		<Headline>Pages</Headline>
		<AutoRefresh
			isLoading={false}
			onRefresh={() => {
				refreshPages(false);
			}}
		/>
	</div>
	<BigButton on:click={openCreateModal}>New Page</BigButton>
	<Table
		columns={[
			{ column: 'Name', size: 'large' },
			...(contextCompanyID ? [{ column: 'Scope', size: 'small' }] : [])
		]}
		sortable={['Name', ...(contextCompanyID ? ['scope'] : [])]}
		hasData={!!pages.length}
		plural="pages"
		pagination={tableURLParams}
		isGhost={isPageTableLoading}
	>
		{#each pages as page}
			<TableRow>
				<TableCell>
					<button
						on:click={() => {
							openUpdateModal(page.id);
						}}
						{...globalButtonDisabledAttributes(page, contextCompanyID)}
						title={page.name}
						class="block w-full py-1 text-left"
					>
						{page.name}
					</button>
				</TableCell>
				{#if contextCompanyID}
					<TableCellScope companyID={page.companyID} />
				{/if}
				<TableCellEmpty />
				<TableCellAction>
					<TableDropDownEllipsis>
						<TableUpdateButton
							on:click={() => openUpdateModal(page.id)}
							{...globalButtonDisabledAttributes(page, contextCompanyID)}
						/>
						<TableCopyButton title={'Copy'} on:click={() => openCopyModal(page.id)} />
						<TableDeleteButton
							on:click={() => openDeleteAlert(page)}
							{...globalButtonDisabledAttributes(page, contextCompanyID)}
						></TableDeleteButton>
					</TableDropDownEllipsis>
				</TableCellAction>
			</TableRow>
		{/each}
	</Table>
	<Modal headerText={modalText} visible={isModalVisible} onClose={closeModal} {isSubmitting}>
		<FormGrid on:submit={onSubmit} bind:bindTo={form} {isSubmitting} {modalMode}>
			<Editor contentType="page" {domainMap} bind:value={formValues.content}>
				<div class="pl-4">
					<TextField
						minLength={1}
						maxLength={64}
						required
						bind:value={formValues.name}
						placeholder="Intranet login">Name</TextField
					>
				</div>
			</Editor>
			<FormError message={formError} />
			<FormFooter {closeModal} {isSubmitting} />
		</FormGrid>
	</Modal>
	<DeleteAlert
		list={[
			'Templates using this page will become unusable',
			'Scheduled or active campaigns using this domain will be cancelled'
		]}
		name={deleteValues.name}
		onClick={() => onClickDelete(deleteValues.id)}
		bind:isVisible={isDeleteAlertVisible}
	></DeleteAlert>
</main>
