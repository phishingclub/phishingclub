<script>
	import { page } from '$app/stores';
	import { api } from '$lib/api/apiProxy.js';
	import { onMount } from 'svelte';
	import { newTableURLParams } from '$lib/service/tableURLParams.js';
	import { globalButtonDisabledAttributes } from '$lib/utils/form.js';
	import Headline from '$lib/components/Headline.svelte';
	import TextField from '$lib/components/TextField.svelte';
	import PasswordField from '$lib/components/PasswordField.svelte';
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
	import InstalledResourceBadge from '$lib/components/InstalledResourceBadge.svelte';
	import BigButton from '$lib/components/BigButton.svelte';
	import Button from '$lib/components/Button.svelte';
	import StatusBadge from '$lib/components/StatusBadge.svelte';
	import FormColumn from '$lib/components/FormColumn.svelte';
	import FormColumns from '$lib/components/FormColumns.svelte';
	import FormFooter from '$lib/components/FormFooter.svelte';
	import Table from '$lib/components/table/Table.svelte';
	import HeadTitle from '$lib/components/HeadTitle.svelte';
	import TableViewButton from '$lib/components/table/TableViewButton.svelte';
	import { getModalText } from '$lib/utils/common';
	import { showIsLoading, hideIsLoading } from '$lib/store/loading.js';
	import TableCopyButton from '$lib/components/table/TableCopyButton.svelte';
	import TableDropDownEllipsis from '$lib/components/table/TableDropDownEllipsis.svelte';
	import DeleteAlert from '$lib/components/modal/DeleteAlert.svelte';
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

	// data
	let form = null;
	let contextCompanyID = null;
	let formValues = {
		id: '',
		name: '',
		companyID: '',
		url: '',
		secret: ''
	};
	let webhooks = [];
	let webhooksHasNextPage = true;

	// multi select
	const selection = createTableSelection();
	let isBulkDeleteAlertVisible = false;
	const isRowDisabled = (w) => globalButtonDisabledAttributes(w, contextCompanyID).disabled;
	$: selectablePageIds = webhooks
		.filter((w) => !globalButtonDisabledAttributes(w, contextCompanyID).disabled)
		.map((w) => w.id);
	$: headerState = headerSelectionState($selection, selectablePageIds);
	$: showMultiSelect = webhooks.length > 0;
	const onClickBulkDelete = async () => {
		await runBulkDelete({ ids: [...$selection], deleteFn: api.webhook.delete, noun: 'webhook' });
		await refreshWebhooks();
		return { success: true };
	};
	let modalError = '';
	const tableURLParams = newTableURLParams();
	let isModalVisible = false;
	let isSubmitting = false;
	let isTestModalVisible = false;
	let isTableLoading = false;
	let modalMode = null;
	let modalText = '';

	let testResponse = {
		url: null,
		status: null,
		body: null
	};

	let isDeleteAlertVisible = false;
	let deleteValues = {
		id: null,
		name: null
	};

	$: {
		modalText = getModalText('webhook', modalMode);
	}

	// hook
	onMount(() => {
		if (appStateService.getContext()) {
			contextCompanyID = appStateService.getContext().companyID;
			formValues.companyID = contextCompanyID;
		}
		refreshWebhooks();
		tableURLParams.onChange(refreshWebhooks);

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

	// component logic
	const refreshWebhooks = async () => {
		try {
			isTableLoading = true;
			selection.clear();
			const result = await getWebhooks();
			webhooks = result.rows;
			webhooksHasNextPage = result.hasNextPage;
		} catch (e) {
			addToast('Failed to get webhooks', 'Error');
			console.error(e);
		} finally {
			isTableLoading = false;
		}
	};

	const getWebhooks = async () => {
		try {
			const res = await api.webhook.getAll(tableURLParams, contextCompanyID);
			if (!res.success) {
				throw res.error;
			}
			return res.data;
		} catch (e) {
			addToast('Failed to get webhooks', 'Error');
			console.error('failed to get webhooks', e);
		}
		return [];
	};

	const onSubmit = async () => {
		try {
			isSubmitting = true;
			if (modalMode === 'create' || modalMode === 'copy') {
				await onClickCreate();
				return;
			} else {
				await onClickUpdate();
				return;
			}
		} finally {
			isSubmitting = false;
		}
	};

	const onClickCreate = async () => {
		try {
			const res = await api.webhook.create(formValues);
			if (!res.success) {
				modalError = res.error;
				return;
			}
			addToast('Created webhook', 'Success');
			closeCreateModal();
			refreshWebhooks();
		} catch (err) {
			addToast('Failed to create webhook', 'Error');
			console.error('failed to create webhook:', err);
		}
	};

	const onClickUpdate = async () => {
		try {
			const res = await api.webhook.update(formValues);
			if (!res.success) {
				modalError = res.error;
				throw res.error;
			}
			addToast('Updated webhook', 'Success');
			closeEditModal();
			refreshWebhooks();
		} catch (err) {
			console.error('failed to update webhook:', err);
		}
	};

	const openDeleteAlert = async (domain) => {
		isDeleteAlertVisible = true;
		deleteValues.id = domain.id;
		deleteValues.name = domain.name;
	};

	/** @param {string} id */
	const onClickDelete = async (id) => {
		const action = api.webhook.delete(id);
		action
			.then((res) => {
				if (res.success) {
					refreshWebhooks();
					return;
				}
				throw res.error;
			})
			.catch((e) => {
				console.error('failed to delete webhook:', e);
			});
		return action;
	};

	/** @param {string} id */
	const openTestModal = async (id) => {
		try {
			showIsLoading();
			const webhook = await api.webhook.getByID(id);
			if (!webhook.success) {
				throw webhook.error;
			}
			const res = await api.webhook.test(id);
			testResponse.url = webhook.data.url;
			testResponse.status = res.data?.status != null ? `${res.data.status}` : '';
			testResponse.body = res.data?.body;
			isTestModalVisible = true;
		} catch (e) {
			addToast('Failed to test web hook', 'Error');
			console.error('failed to test web hook:', e);
		} finally {
			hideIsLoading();
		}
	};

	const openCreateModal = () => {
		modalMode = 'create';
		isModalVisible = true;
	};

	const closeCreateModal = () => {
		isModalVisible = false;
		form.reset();
		modalError = '';
	};

	/** @param {string} id */
	const openEditModal = async (id) => {
		modalMode = 'update';
		try {
			showIsLoading();
			const webhook = await api.webhook.getByID(id);
			if (!webhook.success) {
				throw webhook.error;
			}
			const r = globalButtonDisabledAttributes(webhook, contextCompanyID);
			if (r.disabled) {
				hideIsLoading();
				return;
			}
			assignWebhook(webhook.data);
			isModalVisible = true;
		} catch (e) {
			addToast('Failed to get web hook', 'Error');
			console.error('failed to get web hook:', e);
		} finally {
			hideIsLoading();
		}
	};

	const openCopyModal = async (id) => {
		modalMode = 'copy';
		try {
			showIsLoading();
			const webhook = await api.webhook.getByID(id);
			if (!webhook.success) {
				throw webhook.error;
			}
			assignWebhook(webhook.data);
			formValues.id = null;
			isModalVisible = true;
		} catch (e) {
			hideIsLoading();
			addToast('Failed to get web hook', 'Error');
			console.error('failed to get web hook:', e);
		} finally {
			hideIsLoading();
		}
	};

	const assignWebhook = (webhook) => {
		formValues = webhook;
	};

	const closeTestModal = () => {
		isTestModalVisible = false;
	};

	const closeEditModal = () => {
		isModalVisible = false;
		form.reset();
		modalError = '';
	};
</script>

<HeadTitle title="Webhooks" />
<main>
	<Headline docSlug="webhooks">Webhooks</Headline>
	<BigButton on:click={openCreateModal}>New webhook</BigButton>
	<BulkActionBar
		count={$selection.size}
		noun="webhook"
		on:delete={() => (isBulkDeleteAlertVisible = true)}
		on:clear={() => selection.clear()}
	/>
	<Table
		selectable
		{headerState}
		on:toggleAll={(e) => selection.setPageSelection(selectablePageIds, e.detail)}
		columns={[
			{ column: 'Name', size: 'large' },
			...(contextCompanyID ? [{ column: 'Scope', size: 'small' }] : [])
		]}
		sortable={['name', ...(contextCompanyID ? ['scope'] : [])]}
		hasData={!!webhooks.length}
		hasNextPage={webhooksHasNextPage}
		plural="webhooks"
		pagination={tableURLParams}
		isGhost={isTableLoading}
	>
		{#each webhooks as webhook}
			<TableRow>
				{#if showMultiSelect}
					<TableCellCheckbox
						checked={$selection.has(webhook.id)}
						disabled={isRowDisabled(webhook)}
						on:change={() => selection.toggle(webhook.id)}
					/>
				{/if}
				<TableCell wrap>
					<button
						class="block w-full py-1 text-left"
						on:click={() => {
							openEditModal(webhook.id);
						}}
						{...globalButtonDisabledAttributes(webhook, contextCompanyID)}
						title={webhook.name}
					>
						{webhook.name}
					</button>
				</TableCell>
				{#if contextCompanyID}
					<TableCellScope companyID={webhook.companyID} />
				{/if}
				<TableCellEmpty />
				<TableCellAction>
					<TableDropDownEllipsis>
						<TableUpdateButton
							on:click={() => openEditModal(webhook.id)}
							{...globalButtonDisabledAttributes(webhook, contextCompanyID)}
						/>
						<TableCopyButton
							title={'Copy'}
							on:click={() => openCopyModal(webhook.id)}
							{...globalButtonDisabledAttributes(webhook, contextCompanyID)}
						/>
						<TableViewButton name="Send test" on:click={() => openTestModal(webhook.id)} />
						<TableDeleteButton
							on:click={() => openDeleteAlert(webhook)}
							{...globalButtonDisabledAttributes(webhook, contextCompanyID)}
						></TableDeleteButton>
					</TableDropDownEllipsis>
				</TableCellAction>
			</TableRow>
		{/each}
	</Table>

	<Modal headerText={modalText} visible={isModalVisible} onClose={closeCreateModal} {isSubmitting}>
		<div class="max-w-full sm:max-w-[36rem]">
			<FormGrid on:submit={onSubmit} bind:bindTo={form} {isSubmitting}>
				<FormColumns>
					<FormColumn>
						<div class="flex flex-col w-full sm:w-[26rem]">
							{#if modalMode === 'update'}
								<InstalledResourceBadge resourceType="webhook" resourceId={formValues.id} variant="notice" />
							{/if}
							<TextField
								width="full"
								required
								minLength={1}
								maxLength={127}
								bind:value={formValues.name}
								placeholder="My webhook">Name</TextField
							>
							<TextField
								bind:value={formValues.url}
								type="url"
								width="full"
								required
								minLength={1}
								maxLength={1024}
								toolTipText="The URL to send the webhook to, including the protocol (http/https)"
								placeholder="https://notify-me.test/api/webhook">URL</TextField
							>
							<PasswordField
								width="full"
								bind:value={formValues.secret}
								optional={true}
								minLength={1}
								maxLength={1024}
								toolTipText="Secret used to sign the webhook payload"
								placeholder="Shared secret for the signature">Secret</PasswordField
							>
						</div>
					</FormColumn>
				</FormColumns>
				<FormError message={modalError} />
				<FormFooter closeModal={closeCreateModal} {isSubmitting} />
			</FormGrid>
		</div>
	</Modal>
	<Modal headerText="Webhook test" visible={isTestModalVisible} onClose={closeTestModal}>
		<div class="w-[640px] max-w-full pt-4 space-y-6 select-text overflow-x-hidden">
			<section class="pb-4 border-b border-gray-200 dark:border-gray-600 w-full">
				<h3 class="text-lg font-semibold text-pc-darkblue dark:text-white mb-3">Request</h3>
				<div
					class="p-3 bg-gray-50 dark:bg-gray-800 rounded-md border border-gray-200 dark:border-gray-600 overflow-hidden"
				>
					<div class="font-medium break-all">POST {testResponse.url ?? ''}</div>
				</div>
			</section>
			<section class="pb-4 border-b border-gray-200 dark:border-gray-600 w-full">
				<h3 class="text-lg font-semibold text-pc-darkblue dark:text-white mb-3">Response status</h3>
				{#if testResponse.status}
					<StatusBadge tone={testResponse.status.startsWith('2') ? 'success' : 'danger'}
						>{testResponse.status}</StatusBadge
					>
				{:else}
					<StatusBadge tone="neutral">No response</StatusBadge>
				{/if}
			</section>
			<section class="pb-4 w-full">
				<h3 class="text-lg font-semibold text-pc-darkblue dark:text-white mb-3">Response body</h3>
				<div
					class="p-3 bg-gray-50 dark:bg-gray-800 rounded-md border border-gray-200 dark:border-gray-600 overflow-hidden"
				>
					<pre
						class="text-xs whitespace-pre-wrap break-all max-h-80 overflow-y-auto font-mono"
						style="overflow-wrap: anywhere;">{testResponse.body || 'Empty response'}</pre>
				</div>
			</section>
		</div>
		<!-- same sticky footer row as FormFooter, without a submit button -->
		<div
			class="sticky bottom-0 z-10 -mx-4 sm:-mx-8 px-4 sm:px-8 py-4 border-t-2 border-gray-200 dark:border-gray-700 bg-white dark:bg-gray-800 flex justify-end transition-colors duration-200"
		>
			<Button variant="secondary" type="button" on:click={closeTestModal}>Close</Button>
		</div>
	</Modal>
	<DeleteAlert
		name={deleteValues.name}
		onClick={() => onClickDelete(deleteValues.id)}
		bind:isVisible={isDeleteAlertVisible}
	></DeleteAlert>
	<DeleteAlert
		title="Delete webhooks"
		name={`${$selection.size} webhook${$selection.size === 1 ? '' : 's'}`}
		onClick={onClickBulkDelete}
		confirm
		bind:isVisible={isBulkDeleteAlertVisible}
	></DeleteAlert>
</main>
