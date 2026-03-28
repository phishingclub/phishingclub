<script>
	import { api } from '$lib/api/apiProxy.js';
	import { onMount } from 'svelte';
	import { goto } from '$app/navigation';
	import Headline from '$lib/components/Headline.svelte';
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
	import BigButton from '$lib/components/BigButton.svelte';
	import FormFooter from '$lib/components/FormFooter.svelte';
	import Table from '$lib/components/table/Table.svelte';
	import HeadTitle from '$lib/components/HeadTitle.svelte';
	import { showIsLoading, hideIsLoading } from '$lib/store/loading.js';
	import TableDropDownEllipsis from '$lib/components/table/TableDropDownEllipsis.svelte';
	import DeleteAlert from '$lib/components/modal/DeleteAlert.svelte';
	import TableDropDownButton from '$lib/components/table/TableDropDownButton.svelte';
	import Alert from '$lib/components/Alert.svelte';

	// bindings
	let form = null;
	let companyAutoPruneEnabled = false;
	let companyAutoPruneEnabledOriginal = false;
	const formValues = {
		name: null,
		comment: null
	};
	// data
	let modalError = '';
	let companies = [];
	let companiesHasNextPage = true;
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

	let isViewCommentModalVisible = false;
	let viewCommentCompany = null;

	let isExportCompanyModalVisible = false;
	let isExportSharedModalVisible = false;
	let exportCompany = null;

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
			const data = await getCompanies();
			companies = data.rows;
			companiesHasNextPage = data.hasNextPage;
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
				return res.data;
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
			const res = await api.company.create(formValues.name, formValues.comment);
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
			const res = await api.company.update(formValues.id, formValues.name, formValues.comment);
			if (!res.success) {
				modalError = res.error;
				return;
			}
			if (companyAutoPruneEnabled !== companyAutoPruneEnabledOriginal) {
				await saveCompanyAutoPrune(formValues.id, companyAutoPruneEnabled);
			}
			addToast('Company updated', 'Success');
			closeUpdateModal();
		} catch (e) {
			addToast('Failed to update company', 'Error');
			console.error('failed to update company', e);
		}
		refreshCompanies();
	};

	const saveCompanyAutoPrune = async (id, enabled) => {
		try {
			const res = await api.company.setAutoPrune(id, enabled);
			if (!res.success) {
				addToast('Failed to save auto-prune setting', 'Error');
			}
		} catch (e) {
			addToast('Failed to save auto-prune setting', 'Error');
			console.error('failed to save company auto-prune', e);
		}
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
		// reset form values for create mode
		formValues.id = null;
		formValues.name = null;
		formValues.comment = null;
		isModalVisible = true;
	};

	const closeModal = () => {
		modalError = '';
		isModalVisible = false;
		// reset form values
		formValues.id = null;
		formValues.name = null;
		formValues.comment = null;
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
			formValues.comment = company.comment || null;
			try {
				const optRes = await api.company.getAutoPrune(id);
				companyAutoPruneEnabled = optRes.success && optRes.data?.enabled === true;
				companyAutoPruneEnabledOriginal = companyAutoPruneEnabled;
			} catch (_) {
				companyAutoPruneEnabled = false;
			}
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
		// reset form values
		formValues.id = null;
		formValues.name = null;
		formValues.comment = null;
		companyAutoPruneEnabled = false;
		companyAutoPruneEnabledOriginal = false;
		form.reset();
	};

	const openViewCommentModal = (company) => {
		viewCommentCompany = company;
		isViewCommentModalVisible = true;
	};

	const closeViewCommentModal = () => {
		isViewCommentModalVisible = false;
		viewCommentCompany = null;
	};

	const openExportCompanyModal = (company) => {
		exportCompany = company;
		isExportCompanyModalVisible = true;
	};

	const closeExportCompanyModal = () => {
		isExportCompanyModalVisible = false;
		exportCompany = null;
	};

	const openExportSharedModal = () => {
		isExportSharedModalVisible = true;
	};

	const closeExportSharedModal = () => {
		isExportSharedModalVisible = false;
	};

	const onConfirmExportCompany = async () => {
		try {
			showIsLoading();
			api.company.export(exportCompany.id);
			closeExportCompanyModal();
			return { success: true };
		} catch (e) {
			addToast('Failed to export company events', 'Error');
			console.error('failed to export company events', e);
			return { success: false, error: e };
		} finally {
			hideIsLoading();
		}
	};

	const onConfirmExportShared = async () => {
		try {
			showIsLoading();
			api.company.export();
			closeExportSharedModal();
			return { success: true };
		} catch (e) {
			addToast('Failed to export shared events', 'Error');
			console.error('failed to export shared events', e);
			return { success: false, error: e };
		} finally {
			hideIsLoading();
		}
	};
</script>

<HeadTitle title="companies" />
<main>
	<Headline>Companies</Headline>
	<BigButton on:click={openCreateModal}>New company</BigButton>
	<BigButton on:click={openExportSharedModal}>Export shared</BigButton>
	<Table
		columns={[{ column: 'Name', size: 'large' }]}
		sortable={['name']}
		hasData={!!companies.length}
		hasNextPage={companiesHasNextPage}
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
						class="block w-full py-1 text-left"
					>
						{company.name}
					</button>
				</TableCell>
				<TableCellEmpty />
				<TableCellAction>
					<TableDropDownEllipsis>
						<TableUpdateButton on:click={() => openUpdateModal(company.id)} />
						<TableDropDownButton
							name="View Comment"
							on:click={() => openViewCommentModal(company)}
						/>
						<TableDropDownButton name="Export" on:click={() => openExportCompanyModal(company)} />
						<TableDropDownButton
							name="Custom Stats"
							on:click={() => goto(`/company/${company.id}/stats`)}
						/>
						<TableDeleteButton on:click={() => openDeleteAlert(company)} />
					</TableDropDownEllipsis>
				</TableCellAction>
			</TableRow>
		{/each}
	</Table>

	<Modal headerText={modalText} visible={isModalVisible} onClose={closeModal} {isSubmitting}>
		<div class="w-[1000px] p-6">
			<form on:submit|preventDefault={onSubmit} bind:this={form}>
				<div class="space-y-6">
					<div>
						<label
							for="company-name"
							class="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-2"
						>
							Company Name
						</label>
						<input
							id="company-name"
							type="text"
							required
							minlength="1"
							maxlength="64"
							placeholder="Alices Enterprise Solutions"
							bind:value={formValues.name}
							class="w-96 px-3 py-2 border border-gray-300 rounded-md focus:outline-none focus:ring-2 focus:ring-blue-500 dark:bg-gray-700 dark:border-gray-600 dark:text-white"
						/>
					</div>

					<div>
						<div class="flex items-center mb-2">
							<label
								for="company-comment"
								class="block text-sm font-medium text-gray-700 dark:text-gray-300"
							>
								Comment
							</label>
							<div
								class="bg-gray-100 dark:bg-gray-800/60 ml-2 px-2 rounded-md transition-colors duration-200 h-6 flex items-center"
							>
								<p class="text-slate-600 dark:text-gray-400 text-xs transition-colors duration-200">
									optional
								</p>
							</div>
						</div>
						<textarea
							id="company-comment"
							bind:value={formValues.comment}
							maxlength={1000000}
							rows="8"
							placeholder="Add notes about this company..."
							class="w-full p-4 border border-gray-300 rounded-md focus:outline-none focus:ring-2 focus:ring-blue-500 resize-y dark:bg-gray-700 dark:border-gray-600 dark:text-white"
						/>
					</div>

					<div>
						<div class="flex items-center mb-2">
							<p class="block text-sm font-medium text-gray-700 dark:text-gray-300">
								Auto-Prune Orphaned Recipients
							</p>
						</div>
						<div class="inline-flex flex-col space-y-2 min-w-64 mb-4">
							<label
								class="flex items-start gap-3 p-3 border rounded-lg cursor-pointer transition-colors {companyAutoPruneEnabled
									? 'bg-blue-50 dark:bg-blue-900/20 border-blue-500 dark:border-blue-600'
									: 'border-gray-300 dark:border-gray-600'}"
							>
								<input
									type="radio"
									checked={companyAutoPruneEnabled}
									on:change={() => (companyAutoPruneEnabled = true)}
									class="mt-0.5 w-4 h-4 text-blue-600 bg-gray-100 dark:bg-gray-700 border-gray-300 dark:border-gray-600 focus:ring-blue-500 focus:ring-2"
								/>
								<div class="text-left flex-1">
									<span class="text-sm font-medium text-gray-900 dark:text-gray-100 block"
										>Enabled</span
									>
									<span class="text-xs text-gray-500 dark:text-gray-400 block mt-0.5"
										>Orphaned recipients are deleted automatically each hour</span
									>
								</div>
							</label>
							<label
								class="flex items-start gap-3 p-3 border rounded-lg cursor-pointer transition-colors {!companyAutoPruneEnabled
									? 'bg-blue-50 dark:bg-blue-900/20 border-blue-500 dark:border-blue-600'
									: 'border-gray-300 dark:border-gray-600'}"
							>
								<input
									type="radio"
									checked={!companyAutoPruneEnabled}
									on:change={() => (companyAutoPruneEnabled = false)}
									class="mt-0.5 w-4 h-4 text-blue-600 bg-gray-100 dark:bg-gray-700 border-gray-300 dark:border-gray-600 focus:ring-blue-500 focus:ring-2"
								/>
								<div class="text-left flex-1">
									<span class="text-sm font-medium text-gray-900 dark:text-gray-100 block"
										>Disabled</span
									>
									<span class="text-xs text-gray-500 dark:text-gray-400 block mt-0.5"
										>Orphaned recipients are kept until manually deleted</span
									>
								</div>
							</label>
						</div>
					</div>
				</div>

				<FormError message={modalError} />
				<FormFooter {closeModal} {isSubmitting} />
			</form>
		</div>
	</Modal>

	<Modal
		headerText="Company Comment"
		visible={isViewCommentModalVisible}
		onClose={closeViewCommentModal}
	>
		<div class="p-8 w-full min-w-[800px] max-w-6xl">
			<div class="mb-4">
				<h3 class="text-lg font-semibold text-gray-800 dark:text-gray-200 mb-2">
					{viewCommentCompany?.name || 'Company'}
				</h3>
			</div>
			{#if viewCommentCompany?.comment && viewCommentCompany.comment.trim()}
				<div
					class="bg-gray-50 dark:bg-gray-800 p-8 rounded-lg border min-h-[400px] max-h-[600px] overflow-y-auto"
				>
					<pre
						class="whitespace-pre-wrap text-base text-gray-700 dark:text-gray-300 font-normal leading-relaxed">{viewCommentCompany.comment}</pre>
				</div>
			{:else}
				<div class="bg-gray-50 dark:bg-gray-800 p-8 rounded-lg border text-center">
					<p class="text-sm text-gray-500 dark:text-gray-400 italic">No comment available.</p>
				</div>
			{/if}
			<div class="mt-6 flex justify-end">
				<button
					type="button"
					on:click={closeViewCommentModal}
					class="px-4 py-2 bg-gray-600 hover:bg-gray-700 text-white rounded-md transition-colors duration-200"
				>
					Close
				</button>
			</div>
		</div>
	</Modal>

	<DeleteAlert
		list={['All data related to the company such as domains, campaign, recipients will be lost']}
		name={deleteValues.name}
		onClick={() => onClickDelete(deleteValues.id)}
		bind:isVisible={isDeleteAlertVisible}
	></DeleteAlert>

	<Alert
		headline="Export Company Data"
		bind:visible={isExportCompanyModalVisible}
		onConfirm={onConfirmExportCompany}
	>
		<div>
			{#if exportCompany}
				<p class="mb-4">Are you sure you want to export all data for:</p>
				<div class="bg-gray-50 dark:bg-gray-700 p-3 rounded mb-4">
					<p class="font-medium">{exportCompany.name}</p>
				</div>
				<p class="text-sm text-gray-500">
					This will download a ZIP file containing all company data, recipients, and campaign
					events.
				</p>
			{/if}
		</div>
	</Alert>

	<Alert
		headline="Export Shared Data"
		bind:visible={isExportSharedModalVisible}
		onConfirm={onConfirmExportShared}
	>
		<div>
			<p class="mb-4">Are you sure you want to export all shared data?</p>
			<p class="text-sm text-gray-500">
				This will download a ZIP file containing all recipients and campaign events that are not
				assigned to any specific company.
			</p>
		</div>
	</Alert>
</main>
