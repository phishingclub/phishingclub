<script>
	import { api } from '$lib/api/apiProxy.js';
	import { onMount } from 'svelte';
	import Headline from '$lib/components/Headline.svelte';
	import TableRow from '$lib/components/table/TableRow.svelte';
	import FormError from '$lib/components/FormError.svelte';
	import { addToast } from '$lib/store/toast';
	import { newTableURLParams } from '$lib/service/tableURLParams.js';
	import Modal from '$lib/components/Modal.svelte';
	import BigButton from '$lib/components/BigButton.svelte';
	import FormFooter from '$lib/components/FormFooter.svelte';
	import Table from '$lib/components/table/Table.svelte';
	import HeadTitle from '$lib/components/HeadTitle.svelte';
	import { showIsLoading, hideIsLoading } from '$lib/store/loading.js';
	import Alert from '$lib/components/Alert.svelte';
	import FormGrid from '$lib/components/FormGrid.svelte';
	import FormColumns from '$lib/components/FormColumns.svelte';
	import FormColumn from '$lib/components/FormColumn.svelte';
	import TextField from '$lib/components/TextField.svelte';
	import TextareaField from '$lib/components/TextareaField.svelte';
	import TableCell from '$lib/components/table/TableCell.svelte';

	// bindings
	let form = null;
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

	let isExportSharedModalVisible = false;

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

	const getCompanies = async () => {
		try {
			const res = await api.company.getAll(tableURLParams);
			if (res.success) {
				return res.data;
			}
			throw new res.error();
		} catch (e) {
			addToast('Failed to get companies', 'Error');
			console.error('failed to get companies', e);
		}
		return [];
	};

	const onSubmit = async () => {
		try {
			isSubmitting = true;
			await create();
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

	const openCreateModal = () => {
		modalError = '';
		formValues.name = null;
		formValues.comment = null;
		isModalVisible = true;
	};

	const closeModal = () => {
		modalError = '';
		isModalVisible = false;
		formValues.name = null;
		formValues.comment = null;
		form.reset();
	};

	const openExportSharedModal = () => {
		isExportSharedModalVisible = true;
	};

	const onConfirmExportShared = async () => {
		try {
			showIsLoading();
			api.company.export();
			isExportSharedModalVisible = false;
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

<HeadTitle title="Companies" />
<main>
	<Headline docSlug="companies">Companies</Headline>
	<div class="flex flex-wrap gap-3 mt-6 mb-10 [&>button]:m-0">
		<BigButton on:click={openCreateModal}>New company</BigButton>
		<BigButton variant="secondary" on:click={openExportSharedModal}>Export shared</BigButton>
	</div>
	<Table
		reserveSelect
		columns={[{ column: 'Name', size: 'large' }]}
		sortable={['name']}
		hasData={!!companies.length}
		hasNextPage={companiesHasNextPage}
		plural="companies"
		pagination={tableURLParams}
		hasActions={false}
		isGhost={isTableLoading}
	>
		{#each companies as company}
			<TableRow>
				<TableCell wrap>
					<a
						class="pc-cell-link group w-full !flex items-center justify-between gap-2 min-h-10"
						href={`/company/${company.id}`}
						title="Open {company.name}"
					>
						<span class="truncate">{company.name}</span>
						<svg
							class="h-4 w-4 shrink-0 text-gray-400 dark:text-gray-500 group-hover:text-slate-600 dark:group-hover:text-gray-200"
							viewBox="0 0 20 20"
							fill="currentColor"
							aria-hidden="true"
						>
							<path
								fill-rule="evenodd"
								d="M7.21 14.77a.75.75 0 0 1 .02-1.06L11.17 10 7.23 6.29a.75.75 0 1 1 1.04-1.08l4.5 4.25a.75.75 0 0 1 0 1.08l-4.5 4.25a.75.75 0 0 1-1.06-.02Z"
								clip-rule="evenodd"
							/>
						</svg>
					</a>
				</TableCell>
			</TableRow>
		{/each}
	</Table>

	<Modal headerText="New company" visible={isModalVisible} onClose={closeModal} {isSubmitting}>
		<FormGrid on:submit={onSubmit} bind:bindTo={form} {isSubmitting}>
			<FormColumns>
				<FormColumn>
					<div class="w-full sm:w-[28rem]">
						<TextField
							id="company-name"
							required
							minLength={1}
							maxLength={64}
							width="full"
							bind:value={formValues.name}
							placeholder="e.g. Alice's Enterprise Solutions">Name</TextField
						>
						<TextareaField
							id="company-comment"
							optional
							fullWidth
							height="large"
							maxLength={1000000}
							bind:value={formValues.comment}
							placeholder="Notes about this company">Comment</TextareaField
						>
					</div>
				</FormColumn>
			</FormColumns>
			<FormError message={modalError} />
			<FormFooter {closeModal} {isSubmitting} />
		</FormGrid>
	</Modal>

	<Alert
		headline="Export shared data"
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
