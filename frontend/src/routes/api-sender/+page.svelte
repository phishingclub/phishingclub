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
	import TableUpdateButton from '$lib/components/table/TableUpdateButton.svelte';
	import TableDeleteButton from '$lib/components/table/TableDeleteButton2.svelte';
	import FormError from '$lib/components/FormError.svelte';
	import { addToast } from '$lib/store/toast';
	import { AppStateService } from '$lib/service/appState';
	import TableCellAction from '$lib/components/table/TableCellAction.svelte';
	import TableCellEmpty from '$lib/components/table/TableCellEmpty.svelte';
	import FormGrid from '$lib/components/FormGrid.svelte';
	import Modal from '$lib/components/Modal.svelte';
	import TextareaField from '$lib/components/TextareaField.svelte';
	import BigButton from '$lib/components/BigButton.svelte';
	import FormColumn from '$lib/components/FormColumn.svelte';
	import FormColumns from '$lib/components/FormColumns.svelte';
	import FormFooter from '$lib/components/FormFooter.svelte';
	import Table from '$lib/components/table/Table.svelte';
	import HeadTitle from '$lib/components/HeadTitle.svelte';
	import TableViewButton from '$lib/components/table/TableViewButton.svelte';
	import { getModalText } from '$lib/utils/common';
	import TableCopyButton from '$lib/components/table/TableCopyButton.svelte';
	import { showIsLoading, hideIsLoading } from '$lib/store/loading.js';
	import TableDropDownEllipsis from '$lib/components/table/TableDropDownEllipsis.svelte';
	import DeleteAlert from '$lib/components/modal/DeleteAlert.svelte';
	import SimpleCodeEditor from '$lib/components/editor/SimpleCodeEditor.svelte';

	// services
	const appStateService = AppStateService.instance;

	// data
	let form = null;
	let contextCompanyID = null;
	let formValues = {
		name: '',
		companyID: '',
		apiKey: '',
		customField1: '',
		customField2: '',
		customField3: '',
		customField4: '',
		requestMethod: '',
		requestURL: '',
		requestHeaders: '',
		requestBody: '',
		messageID: '',
		expectedResponseStatusCode: null,
		expectedResponseHeaders: null,
		expectedResponseBody: null
	};
	let apiSenders = [];
	let modalError = '';
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

	// defined by response
	let testResponse = {};

	let isTestModalVisible = false;

	$: {
		modalText = getModalText('api sender', modalMode);
	}

	// hook
	onMount(() => {
		if (appStateService.getContext()) {
			contextCompanyID = appStateService.getContext().companyID;
			formValues.companyID = contextCompanyID;
		}
		refreshConfigurations();
		tableURLParams.onChange(refreshConfigurations);
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
	const refreshConfigurations = async () => {
		try {
			isTableLoading = true;
			apiSenders = await getAPISenders();
		} catch (e) {
			addToast('Failed to get API senders', 'Error');
			console.error(e);
		} finally {
			isTableLoading = false;
		}
	};

	const getAPISenders = async () => {
		try {
			const res = await api.apiSender.getAllOverview(tableURLParams, contextCompanyID);
			if (!res.success) {
				throw res.error;
			}
			return res.data.rows;
		} catch (e) {
			addToast('Failed to get API senders', 'Error');
			console.error('failed to get API senders', e);
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
			const res = await api.apiSender.create(formValues);
			if (!res.success) {
				modalError = res.error;
				return;
			}
			addToast('Created API sender', 'Success');
			closeCreateModal();
			refreshConfigurations();
		} catch (err) {
			addToast('Failed to create  API sender', 'Error');
			console.error('failed to create API sender:', err);
		}
	};

	const onClickUpdate = async () => {
		try {
			const res = await api.apiSender.update(formValues);
			if (!res.success) {
				modalError = res.error;
				throw res.error;
			}
			addToast('Updated API sender', 'Success');
			closeEditModal();
			refreshConfigurations();
		} catch (err) {
			console.error('failed to update API sender:', err);
		}
	};

	const openDeleteAlert = async (apiSender) => {
		isDeleteAlertVisible = true;
		deleteValues.id = apiSender.id;
		deleteValues.name = apiSender.name;
	};

	/** @param {string} id */
	const onClickDelete = async (id) => {
		const action = api.apiSender.delete(id);
		action
			.then((res) => {
				if (res.success) {
					refreshConfigurations();
					return;
				}
				throw res.error;
			})
			.catch((e) => {
				console.error('failed to delete API sender:', e);
			});
		return action;
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
			const apiSender = await api.apiSender.getByID(id);
			if (!apiSender.success) {
				throw apiSender.error;
			}
			const r = globalButtonDisabledAttributes(apiSender, contextCompanyID);
			if (r.disabled) {
				hideIsLoading();
				return;
			}
			assignAPISender(apiSender.data);
			isModalVisible = true;
		} catch (e) {
			addToast('Failed to get API sender', 'Error');
			console.error('failed to get API sender:', e);
		} finally {
			hideIsLoading();
		}
	};

	const openCopyModal = async (id) => {
		modalMode = 'copy';
		try {
			showIsLoading();
			const apiSender = await api.apiSender.getByID(id);
			if (!apiSender.success) {
				throw apiSender.error;
			}
			assignAPISender(apiSender.data);
			isModalVisible = true;
		} catch (e) {
			addToast('Failed to get API sender', 'Error');
			console.error('failed to get API sender:', e);
		} finally {
			hideIsLoading();
		}
	};

	const assignAPISender = (apiSender) => {
		formValues = apiSender;
	};

	const closeEditModal = () => {
		isModalVisible = false;
		form.reset();
		modalError = '';
	};

	/** @param {string} id */
	const openTestModal = async (id) => {
		try {
			showIsLoading();
			const res = await api.apiSender.test(id);
			if (!res.success) {
				const res2 = await api.apiSender.getByID(id);
				if (!res2.success) {
					throw res2.error;
				}
				testResponse.apiSender = res2.data;
				testResponse.error = res.error;
				isTestModalVisible = true;
				return;
			}
			testResponse = res.data;
			isTestModalVisible = true;
		} catch (e) {
			addToast('Failed to test API sender', 'Error');
			console.error('failed to test API sender:', e);
		} finally {
			hideIsLoading();
		}
	};

	const closeTestModal = () => {
		testResponse = {};
		isTestModalVisible = false;
	};

	const headersToString = (headers) => {
		if (!headers) {
			return '';
		}
		// if headers are a malformed array, we use a hack
		if (headers[0]) {
			headers = headers[0];
		}
		return Object.keys(headers)
			.map((key) => `${key}: ${headers[key]}`)
			.join('\n');
	};
</script>

<HeadTitle title="API Senders" />
<main>
	<Headline>API Senders</Headline>
	<BigButton on:click={openCreateModal}>New API sender</BigButton>
	<Table
		columns={[{ column: 'Name', size: 'large' }]}
		sortable={['name']}
		hasData={!!apiSenders.length}
		plural="API senders"
		pagination={tableURLParams}
		isGhost={isTableLoading}
	>
		{#each apiSenders as apiSender}
			<TableRow>
				<TableCell>
					<button
						on:click={() => {
							openEditModal(apiSender.id);
						}}
						{...globalButtonDisabledAttributes(apiSender, contextCompanyID)}
						title={apiSender.name}
					>
						{apiSender.name}
					</button>
				</TableCell>

				<TableCellEmpty />
				<TableCellAction>
					<TableDropDownEllipsis>
						<TableUpdateButton
							on:click={() => openEditModal(apiSender.id)}
							{...globalButtonDisabledAttributes(apiSender, contextCompanyID)}
						/>
						<TableCopyButton
							title={'Copy'}
							on:click={() => openCopyModal(apiSender.id)}
							{...globalButtonDisabledAttributes(apiSender, contextCompanyID)}
						/>
						<TableViewButton name="Perform test" on:click={() => openTestModal(apiSender.id)} />
						<TableDeleteButton
							on:click={() => openDeleteAlert(apiSender)}
							{...globalButtonDisabledAttributes(apiSender, contextCompanyID)}
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
					<!-- Basic Information Section -->
					<div class="mb-6 pt-4 pb-2 border-b border-gray-200 dark:border-gray-600 w-full">
						<h3 class="text-base font-medium text-pc-darkblue dark:text-white mb-3">
							Basic Information
						</h3>
						<TextField
							required
							minLength={1}
							maxLength={64}
							bind:value={formValues.name}
							placeholder="PhishingClub">Name</TextField
						>
					</div>

					<!-- API Configuration Section -->
					<div class="mb-6 pt-4 pb-2 border-b border-gray-200 dark:border-gray-600 w-full">
						<h3 class="text-base font-medium text-pc-darkblue dark:text-white mb-3">
							API Configuration
						</h3>
						<div class="grid grid-cols-1 md:grid-cols-2 gap-4">
							<div class="md:col-span-2">
								<TextField
									required
									minLength={1}
									maxLength={255}
									type="url"
									width="full"
									bind:value={formValues.requestURL}
									placeholder="https://api.example.com/v1/send">Request URL</TextField
								>
							</div>
							<div>
								<TextField
									required
									minLength={1}
									maxLength={64}
									bind:value={formValues.requestMethod}
									placeholder="POST">Request Method</TextField
								>
							</div>
							<div>
								<TextField
									bind:value={formValues.apiKey}
									minLength={1}
									maxLength={255}
									optional={true}
									toolTipText="Use as {'{{.APIKey}}'}"
									placeholder="S3C-R37-AP1-K3Y">API Key</TextField
								>
							</div>
						</div>
					</div>

					<!-- Headers and Body Section -->
					<div class="mb-6 pt-4 pb-2 w-full">
						<h3 class="text-base font-medium text-pc-darkblue dark:text-white mb-3">
							Request Details
						</h3>
						<div class="space-y-5">
							<TextareaField
								optional={true}
								height={'medium'}
								fullWidth
								bind:value={formValues.requestHeaders}
								placeholder={`Content-Type: application/json
Authorization: Bearer {{.APIKey}}
X-Custom-Header: Hello Friend"
								`}
								toolTipText="Each header should be on a new line in the format 'key: value'"
								>Request Headers</TextareaField
							>
							<div class="flex flex-col py-2 w-full">
								<div class="flex items-center">
									<p class="font-bold text-slate-600 dark:text-gray-300 py-2">Request Body</p>
									<div class="bg-gray-100 dark:bg-gray-700 ml-2 px-2 rounded-md">
										<p class="text-slate-600 dark:text-gray-300 text-xs">optional</p>
									</div>
								</div>
								<SimpleCodeEditor
									bind:value={formValues.requestBody}
									height="medium"
									language="json"
									placeholder={`{
  "to": "{{.Name}}",
  "from": "{{.From}}",
  "subject": "Important Security Alert",
  "body": "{{.Content}}"
}`}
								/>
							</div>
						</div>
					</div>

					<!-- Response Validation Section -->
					<div class="mb-6 pt-4 pb-2 border-b border-gray-200 dark:border-gray-600 w-full">
						<h3 class="text-base font-medium text-pc-darkblue dark:text-white mb-3">
							Response Validation
						</h3>
						<div class="space-y-5">
							<div>
								<TextField
									toolTipText="The HTTP status code that indicates a successful request"
									required
									type="number"
									width="small"
									bind:value={formValues.expectedResponseStatusCode}
									placeholder="200">Expected response code</TextField
								>
							</div>
							<TextareaField
								bind:value={formValues.expectedResponseHeaders}
								height={'medium'}
								fullWidth
								minLength={1}
								maxLength={4096}
								optional
								toolTipText="Headers must match, values must contain the expected value."
								placeholder="X-Operation-Status: OK">Header matches</TextareaField
							>
							<TextareaField
								minLength={1}
								maxLength={4096}
								height={'medium'}
								fullWidth
								optional
								bind:value={formValues.expectedResponseBody}
								defaultValue={null}
								placeholder="message sent"
								toolTipText="Text that should be present in the response body"
								>Response contains</TextareaField
							>
						</div>
					</div>

					<!-- Custom Fields Section -->
					<div class="pt-4 pb-2 w-full">
						<h3 class="text-base font-medium text-pc-darkblue dark:text-white mb-3">
							Custom Fields
						</h3>
						<div class="grid grid-cols-1 md:grid-cols-2 gap-4">
							<div>
								<TextField
									bind:value={formValues.customField1}
									minLength={1}
									maxLength={255}
									optional={true}
									placeholder="Value 1"
									toolTipText="Use as {'{{.CustomField1}}'}">Custom field 1</TextField
								>
							</div>
							<div>
								<TextField
									bind:value={formValues.customField2}
									minLength={1}
									maxLength={255}
									optional={true}
									placeholder="Value 2"
									toolTipText="Use as {'{{.CustomField2}}'}">Custom field 2</TextField
								>
							</div>
							<div>
								<TextField
									bind:value={formValues.customField3}
									minLength={1}
									maxLength={255}
									optional={true}
									placeholder="Value 3"
									toolTipText="Use as {'{{.CustomField3}}'}">Custom field 3</TextField
								>
							</div>
							<div>
								<TextField
									bind:value={formValues.customField4}
									minLength={1}
									maxLength={255}
									optional={true}
									placeholder="Value 4"
									toolTipText="Use as {'{{.CustomField4}}'}">Custom field 4</TextField
								>
							</div>
						</div>
					</div>
				</FormColumn>
			</FormColumns>
			<FormError message={modalError} />
			<FormFooter closeModal={closeCreateModal} {isSubmitting} />
		</FormGrid>
	</Modal>

	<Modal headerText="API Sender Test Results" visible={isTestModalVisible} onClose={closeTestModal}>
		<div class="col-span-3 w-full overflow-y-auto px-6 py-4 space-y-6 select-text">
			{#if !testResponse.error}
				<!-- Successful Test -->
				<div class="mb-6 pt-4 pb-2 border-b border-gray-200 dark:border-gray-600 w-full">
					<h3 class="text-base font-medium text-pc-darkblue dark:text-white mb-3">
						Request Details
					</h3>
					<div
						class="p-3 bg-gray-50 dark:bg-gray-800 rounded-md border border-gray-200 dark:border-gray-600"
					>
						<div class="font-medium">
							{testResponse.apiSender?.requestMethod}
							{testResponse?.request?.url}
						</div>
					</div>
				</div>

				<div class="mb-6 pt-4 pb-2 border-b border-gray-200 dark:border-gray-600 w-full">
					<h3 class="text-base font-medium text-pc-darkblue dark:text-white mb-3">
						Request Headers
					</h3>
					<div
						class="p-3 bg-gray-50 dark:bg-gray-800 rounded-md border border-gray-200 dark:border-gray-600"
					>
						<pre class="text-xs whitespace-pre-wrap overflow-x-auto max-h-60">{headersToString(
								testResponse.request?.headers
							) || 'No headers'}</pre>
					</div>
				</div>

				<div class="pt-4 pb-2 w-full">
					<h3 class="text-base font-medium text-pc-darkblue dark:text-white mb-3">Request Body</h3>
					<div
						class="p-3 bg-gray-50 dark:bg-gray-800 rounded-md border border-gray-200 dark:border-gray-600"
					>
						<pre class="text-xs whitespace-pre-wrap overflow-x-auto max-h-80">{testResponse.request
								?.body || 'Empty body'}</pre>
					</div>
				</div>
				<!-- response -->
				<div class="mb-6 pt-4 pb-2 border-b border-gray-200 dark:border-gray-600 w-full">
					<h3 class="text-base font-medium text-pc-darkblue dark:text-white mb-3">
						Response Status
					</h3>
					<div class="grid grid-cols-1 md:grid-cols-2 gap-4">
						<div
							class="p-3 bg-gray-50 dark:bg-gray-800 rounded-md border border-gray-200 dark:border-gray-600"
						>
							<div class="text-sm font-medium mb-1">Received:</div>
							<div
								class={testResponse.response?.code ==
								testResponse.apiSender?.expectedResponseStatusCode
									? 'text-green-600 font-medium'
									: 'text-red-600 font-medium'}
							>
								{testResponse.response?.code}
							</div>
						</div>
						<div
							class="p-3 bg-gray-50 dark:bg-gray-800 rounded-md border border-gray-200 dark:border-gray-600"
						>
							<div class="text-sm font-medium mb-1">Expected:</div>
							<div>{testResponse.apiSender?.expectedResponseStatusCode}</div>
						</div>
					</div>
				</div>

				<div class="mb-6 pt-4 pb-2 border-b border-gray-200 dark:border-gray-600 w-full">
					<h3 class="text-base font-medium text-pc-darkblue dark:text-white mb-3">
						Response Headers
					</h3>
					<div class="grid grid-cols-1 md:grid-cols-2 gap-4">
						<div
							class="p-3 bg-gray-50 dark:bg-gray-800 rounded-md border border-gray-200 dark:border-gray-600"
						>
							<div class="text-sm font-medium mb-1">Received:</div>
							<pre class="text-xs whitespace-pre-wrap overflow-x-auto max-h-60">{headersToString(
									testResponse.response?.headers
								) || 'No headers'}</pre>
						</div>
						<div
							class="p-3 bg-gray-50 dark:bg-gray-800 rounded-md border border-gray-200 dark:border-gray-600"
						>
							<div class="text-sm font-medium mb-1">Expected to contain:</div>
							<pre class="text-xs whitespace-pre-wrap overflow-x-auto max-h-60">{testResponse
									.apiSender?.expectedResponseHeaders || 'No validation specified'}</pre>
						</div>
					</div>
				</div>

				<div class="pt-4 pb-2 w-full">
					<h3 class="text-base font-medium text-pc-darkblue dark:text-white mb-3">Response Body</h3>
					<div class="grid grid-cols-1 md:grid-cols-2 gap-4">
						<div
							class="p-3 bg-gray-50 dark:bg-gray-800 rounded-md border border-gray-200 dark:border-gray-600"
						>
							<div class="text-sm font-medium mb-1">Received:</div>
							<pre class="text-xs whitespace-pre-wrap overflow-x-auto max-h-80">{testResponse
									.response?.body || 'Empty response'}</pre>
						</div>
						<div
							class="p-3 bg-gray-50 dark:bg-gray-800 rounded-md border border-gray-200 dark:border-gray-600"
						>
							<div class="text-sm font-medium mb-1">Expected to contain:</div>
							<pre class="text-xs whitespace-pre-wrap overflow-x-auto max-h-80">{testResponse
									.apiSender?.expectedResponseBody || 'No validation specified'}</pre>
						</div>
					</div>
				</div>
			{:else}
				<!-- Error Case -->
				<div class="mb-6 pt-4 pb-2 border-b border-gray-200 dark:border-gray-600 w-full">
					<h3 class="text-base font-medium text-pc-darkblue dark:text-white mb-3">
						Request Details
					</h3>
					<div
						class="p-3 bg-gray-50 dark:bg-gray-800 rounded-md border border-gray-200 dark:border-gray-600"
					>
						<div class="font-medium">
							{testResponse.apiSender?.requestMethod}
							{testResponse.apiSender?.requestURL}
						</div>
					</div>
				</div>
				<div class="pt-4 pb-2 w-full">
					<h3 class="text-base font-medium text-red-600 mb-3">Error</h3>
					<div class="p-4 bg-red-50 rounded-md border border-red-200">
						<div class="text-red-600 whitespace-pre-wrap">{testResponse.error}</div>
					</div>
				</div>
			{/if}

			<!-- Footer with Close Button -->
			<div class="pt-4 border-t border-gray-200 dark:border-gray-600">
				<div class="flex justify-end">
					<button
						type="button"
						class="px-4 py-2 bg-white dark:bg-gray-700 border border-gray-300 dark:border-gray-600 rounded-md shadow-sm text-sm font-medium text-gray-700 dark:text-gray-200 hover:bg-gray-50 dark:hover:bg-gray-600 focus:outline-none focus:ring-2 focus:ring-offset-2 focus:ring-blue-500"
						on:click={closeTestModal}
					>
						Close
					</button>
				</div>
			</div>
		</div>
	</Modal>
	<DeleteAlert
		list={[
			'Templates using this api sender will become unusable',
			'Scheduled or active campaigns using this api sender will be closed'
		]}
		name={deleteValues.name}
		onClick={() => onClickDelete(deleteValues.id)}
		bind:isVisible={isDeleteAlertVisible}
	></DeleteAlert>
</main>
