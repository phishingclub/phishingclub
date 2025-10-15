<script>
	import { api } from '$lib/api/apiProxy.js';
	import { onMount } from 'svelte';
	import { newTableURLParams } from '$lib/service/tableURLParams.js';
	import { globalButtonDisabledAttributes } from '$lib/utils/form.js';
	import Headline from '$lib/components/Headline.svelte';
	import TextField from '$lib/components/TextField.svelte';
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

		return () => {
			tableURLParams.unsubscribe();
		};
	});

	// component logic
	const refreshWebhooks = async () => {
		try {
			isTableLoading = true;
			const result = await getWebhooks();
			webhooks = result.rows;
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
			testResponse.status = `${res.data.status}`;
			testResponse.body = res.data.body;
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
	<Headline>Webhooks</Headline>
	<BigButton on:click={openCreateModal}>New webhook</BigButton>
	<Table
		columns={[
			{ column: 'Name', size: 'large' },
			...(contextCompanyID ? [{ column: 'Scope', size: 'small' }] : [])
		]}
		sortable={['name', ...(contextCompanyID ? ['scope'] : [])]}
		hasData={!!webhooks.length}
		plural="Webhooks"
		pagination={tableURLParams}
		isGhost={isTableLoading}
	>
		{#each webhooks as webhook}
			<TableRow>
				<TableCell>
					<button
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
						<TableViewButton name="Perform test" on:click={() => openTestModal(webhook.id)} />
						<TableUpdateButton
							on:click={() => openEditModal(webhook.id)}
							{...globalButtonDisabledAttributes(webhook, contextCompanyID)}
						/>
						<TableCopyButton
							title={'Copy'}
							on:click={() => openCopyModal(webhook.id)}
							{...globalButtonDisabledAttributes(webhook, contextCompanyID)}
						/>
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
		<FormGrid on:submit={onSubmit} bind:bindTo={form} {isSubmitting}>
			<FormColumns>
				<FormColumn>
					<TextField
						required
						minLength={1}
						maxLength={127}
						bind:value={formValues.name}
						placeholder="My webhook">Name</TextField
					>
					<TextField
						bind:value={formValues.url}
						type="url"
						required
						minLength={1}
						maxLength={1024}
						toolTipText="The URL to send the webhook to, including the protocol (http/https)"
						placeholder="https://notify-me.test/api/webhook">URL</TextField
					>
					<TextField
						bind:value={formValues.secret}
						optional={true}
						minLength={1}
						maxLength={1024}
						toolTipText="Secret used to sign the webhook payload"
						placeholder="9fYKWxLMPwIJjM0foQRAQOH0DO3FbPR4">Secret</TextField
					>
				</FormColumn>
			</FormColumns>
			<FormError message={modalError} />
			<FormFooter closeModal={closeCreateModal} {isSubmitting} />
		</FormGrid>
	</Modal>
	<Modal headerText="Webhook test" visible={isTestModalVisible} onClose={closeTestModal}>
		<FormColumns>
			<FormColumn>
				<Table
					columns={[
						{ column: 'Key', size: 'small' },
						{ column: 'Value', size: 'large' }
					]}
					hasData={true}
					plural="Webhook test"
					hasActions={false}
				>
					<TableRow>
						<TableCell value="URL" />
						<TableCell value={`POST ${testResponse.url}`} />
					</TableRow>
					<TableRow>
						<TableCell value="Status" />
						<TableCell value={testResponse.status} />
					</TableRow>
					<TableRow>
						<TableCell value="Body" />
						<TableCell>
							{testResponse.body}
						</TableCell>
					</TableRow>
				</Table>
			</FormColumn>
		</FormColumns>
	</Modal>
	<DeleteAlert
		name={deleteValues.name}
		onClick={() => onClickDelete(deleteValues.id)}
		bind:isVisible={isDeleteAlertVisible}
	></DeleteAlert>
</main>
