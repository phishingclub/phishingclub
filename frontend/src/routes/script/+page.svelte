<script>
	import { page } from '$app/stores';
	import { api } from '$lib/api/apiProxy.js';
	import { onMount } from 'svelte';
	import { newTableURLParams } from '$lib/service/tableURLParams.js';
	import { globalButtonDisabledAttributes } from '$lib/utils/form.js';
	import Headline from '$lib/components/Headline.svelte';
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
	import FormFooter from '$lib/components/FormFooter.svelte';
	import Table from '$lib/components/table/Table.svelte';
	import HeadTitle from '$lib/components/HeadTitle.svelte';
	import { getModalText } from '$lib/utils/common';
	import { showIsLoading, hideIsLoading } from '$lib/store/loading.js';
	import TableCopyButton from '$lib/components/table/TableCopyButton.svelte';
	import TableDropDownEllipsis from '$lib/components/table/TableDropDownEllipsis.svelte';
	import DeleteAlert from '$lib/components/modal/DeleteAlert.svelte';
	import TableCellScope from '$lib/components/table/TableCellScope.svelte';
	import ScriptEditor from '$lib/components/script/ScriptEditor.svelte';

	// services
	const appStateService = AppStateService.instance;

	// data
	let contextCompanyID = null;
	let formValues = {
		id: '',
		name: '',
		companyID: '',
		script: ''
	};
	let scripts = [];
	let scriptsHasNextPage = true;
	// the backend returns 404 on every script endpoint when the feature is off
	let featureDisabled = false;

	let modalError = '';
	const tableURLParams = newTableURLParams();
	let isModalVisible = false;
	let isSubmitting = false;
	let isTableLoading = false;
	let modalMode = null;
	let modalText = '';
	// bumped each time the modal opens so the editor remounts with fresh content
	let editorKey = 0;

	let isDeleteAlertVisible = false;
	let deleteValues = {
		id: null,
		name: null
	};

	$: {
		modalText = getModalText('script', modalMode);
	}

	onMount(() => {
		if (appStateService.getContext()) {
			contextCompanyID = appStateService.getContext().companyID;
			formValues.companyID = contextCompanyID;
		}
		refreshScripts();
		tableURLParams.onChange(refreshScripts);

		(async () => {
			const editID = $page.url.searchParams.get('edit');
			if (editID) {
				await openEditModal(editID);
			}
		})();

		return () => {
			tableURLParams.unsubscribe();
		};
	});

	const refreshScripts = async () => {
		try {
			isTableLoading = true;
			const result = await getScripts();
			scripts = result.rows;
			scriptsHasNextPage = result.hasNextPage;
		} catch (e) {
			console.error(e);
		} finally {
			isTableLoading = false;
		}
	};

	const getScripts = async () => {
		const res = await api.script.getAll(tableURLParams, contextCompanyID);
		if (res.success) {
			featureDisabled = false;
			return res.data;
		}
		// feature turned off at the server level: show the how-to-enable banner
		// instead of an error toast
		if (res.statusCode === 404) {
			featureDisabled = true;
		} else {
			addToast('Failed to get scripts', 'Error');
			console.error('failed to get scripts', res.error);
		}
		return { rows: [], hasNextPage: false };
	};

	const onSubmit = async (event) => {
		// FormGrid dispatches saveOnly:true on ctrl/cmd+s so the editor saves
		// without closing the modal, matching the remote browser editor
		const saveOnly = event?.detail?.saveOnly || false;
		try {
			isSubmitting = true;
			if (modalMode === 'create' || modalMode === 'copy') {
				await onClickCreate(saveOnly);
			} else {
				await onClickUpdate(saveOnly);
			}
		} finally {
			isSubmitting = false;
		}
	};

	const onClickCreate = async (saveOnly = false) => {
		try {
			const res = await api.script.create(formValues);
			if (!res.success) {
				modalError = res.error;
				return;
			}
			modalError = '';
			refreshScripts();
			addToast(saveOnly ? 'Saved script' : 'Created script', 'Success');
			if (saveOnly) {
				// keep the modal open and switch to update mode so a further save
				// edits the same script instead of creating a duplicate
				formValues = { ...formValues, id: res.data.id };
				modalMode = 'update';
			} else {
				closeModal();
			}
		} catch (err) {
			addToast('Failed to create script', 'Error');
			console.error('failed to create script:', err);
		}
	};

	const onClickUpdate = async (saveOnly = false) => {
		try {
			const res = await api.script.update(formValues);
			if (!res.success) {
				modalError = res.error;
				return;
			}
			modalError = '';
			refreshScripts();
			addToast(saveOnly ? 'Saved script' : 'Updated script', 'Success');
			if (!saveOnly) {
				closeModal();
			}
		} catch (err) {
			console.error('failed to update script:', err);
		}
	};

	const openDeleteAlert = async (script) => {
		isDeleteAlertVisible = true;
		deleteValues.id = script.id;
		deleteValues.name = script.name;
	};

	/** @param {string} id */
	const onClickDelete = async (id) => {
		const action = api.script.delete(id);
		action
			.then((res) => {
				if (res.success) {
					refreshScripts();
					return;
				}
				throw res.error;
			})
			.catch((e) => {
				console.error('failed to delete script:', e);
			});
		return action;
	};

	const resetForm = () => {
		formValues = {
			id: '',
			name: '',
			companyID: contextCompanyID ?? '',
			script: ''
		};
	};

	const openCreateModal = () => {
		modalMode = 'create';
		resetForm();
		modalError = '';
		editorKey += 1;
		isModalVisible = true;
	};

	/** @param {string} id */
	const openEditModal = async (id) => {
		modalMode = 'update';
		try {
			showIsLoading();
			const script = await api.script.getByID(id);
			if (!script.success) {
				throw script.error;
			}
			const r = globalButtonDisabledAttributes(script, contextCompanyID);
			if (r.disabled) {
				hideIsLoading();
				return;
			}
			formValues = script.data;
			modalError = '';
			editorKey += 1;
			isModalVisible = true;
		} catch (e) {
			addToast('Failed to get script', 'Error');
			console.error('failed to get script:', e);
		} finally {
			hideIsLoading();
		}
	};

	const openCopyModal = async (id) => {
		modalMode = 'copy';
		try {
			showIsLoading();
			const script = await api.script.getByID(id);
			if (!script.success) {
				throw script.error;
			}
			formValues = script.data;
			formValues.id = null;
			modalError = '';
			editorKey += 1;
			isModalVisible = true;
		} catch (e) {
			addToast('Failed to get script', 'Error');
			console.error('failed to get script:', e);
		} finally {
			hideIsLoading();
		}
	};

	const closeModal = () => {
		isModalVisible = false;
		modalError = '';
	};
</script>

<HeadTitle title="Scripts" />
<main>
	<Headline>Scripts</Headline>

	{#if featureDisabled}
		<div class="mt-6 max-w-xl rounded-lg border border-slate-700 bg-slate-800/50 px-6 py-8">
			<p class="mb-1 text-sm font-semibold text-white">Scripts are not enabled</p>
			<p class="mb-3 text-sm text-slate-400">
				This feature is disabled by default for security reasons. When enabled, any operator with
				access can write scripts that run on the server when campaign events fire — including
				making outbound HTTP requests. Only enable it on instances where every operator is trusted
				as a server admin.
			</p>
			<p class="mb-4 text-sm text-slate-400">
				To enable it, set <code class="rounded bg-slate-700 px-1 text-slate-200">enabled: true</code>
				in the <code class="rounded bg-slate-700 px-1 text-slate-200">script</code> block of
				<code class="rounded bg-slate-700 px-1 text-slate-200">config.json</code> and restart the service.
			</p>
		</div>
	{:else}
		<BigButton on:click={openCreateModal}>New script</BigButton>
		<Table
		columns={[
			{ column: 'Name', size: 'large' },
			...(contextCompanyID ? [{ column: 'Scope', size: 'small' }] : [])
		]}
		sortable={['name', ...(contextCompanyID ? ['scope'] : [])]}
		hasData={!!scripts.length}
		hasNextPage={scriptsHasNextPage}
		plural="Scripts"
		pagination={tableURLParams}
		isGhost={isTableLoading}
	>
		{#each scripts as script}
			<TableRow>
				<TableCell>
					<button
						on:click={() => {
							openEditModal(script.id);
						}}
						{...globalButtonDisabledAttributes(script, contextCompanyID)}
						title={script.name}
					>
						{script.name}
					</button>
				</TableCell>
				{#if contextCompanyID}
					<TableCellScope companyID={script.companyID} />
				{/if}
				<TableCellEmpty />
				<TableCellAction>
					<TableDropDownEllipsis>
						<TableUpdateButton
							on:click={() => openEditModal(script.id)}
							{...globalButtonDisabledAttributes(script, contextCompanyID)}
						/>
						<TableCopyButton
							title={'Copy'}
							on:click={() => openCopyModal(script.id)}
							{...globalButtonDisabledAttributes(script, contextCompanyID)}
						/>
						<TableDeleteButton
							on:click={() => openDeleteAlert(script)}
							{...globalButtonDisabledAttributes(script, contextCompanyID)}
						></TableDeleteButton>
					</TableDropDownEllipsis>
				</TableCellAction>
			</TableRow>
		{/each}
	</Table>
	{/if}

	<Modal
		headerText={modalText}
		visible={isModalVisible}
		fullscreen={true}
		onClose={closeModal}
		{isSubmitting}
	>
		<FormGrid on:submit={onSubmit} {isSubmitting} {modalMode}>
			<div class="col-span-3 flex flex-col min-h-0 overflow-hidden px-4 py-4">
				{#key editorKey}
					<ScriptEditor bind:name={formValues.name} bind:script={formValues.script} />
				{/key}
			</div>

			<FormError message={modalError} />

			<FormFooter {closeModal} {isSubmitting} okText={modalMode === 'create' ? 'Create' : 'Update'} />
		</FormGrid>
	</Modal>

	<DeleteAlert
		name={deleteValues.name}
		onClick={() => onClickDelete(deleteValues.id)}
		bind:isVisible={isDeleteAlertVisible}
	></DeleteAlert>
</main>
