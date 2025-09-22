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
	import DeleteAlert from '$lib/components/modal/DeleteAlert.svelte';
	import SimpleCodeEditor from '$lib/components/editor/SimpleCodeEditor.svelte';
	import AutoRefresh from '$lib/components/AutoRefresh.svelte';

	// services
	const appStateService = AppStateService.instance;

	// bindings
	let form = null;
	let formValues = {
		id: null,
		name: null,
		description: null,
		startURL: null,
		proxyConfig: null
	};
	let isSubmitting = false;

	// data
	const tableURLParams = newTableURLParams();
	let contextCompanyID = null;
	let proxies = [];
	let formError = '';
	let isModalVisible = false;
	let isProxyTableLoading = false;
	let modalMode = null;
	let modalText = '';

	let isDeleteAlertVisible = false;
	let deleteValues = {
		id: null,
		name: null
	};

	const currentExample = `version: "0.0" # config version
proxy: 172.20.0.138:8081 # proxy server address
global: # rules applied to all domains
    rewrite:
        - name: loose rename integrity # required identifier
          find: integrity=
          replace: data-no-integrity=
          from: response_body # where to apply
login.example.com: # original domain
    to: login.phishingclub.test # proxy domain
    capture:
        - name: username # required identifier
          method: POST # http method
          path: /auth # url path pattern
          find: username=([^&]+) # regex pattern to capture
          from: request_body # where to search: request_body|request_header|response_body|response_header|cookie|any
        - name: password # required identifier
          method: POST # http method
          path: /auth # url path pattern
          find: password=([^&]+) # regex pattern to capture
          from: request_body # where to search
        - name: session_token # required identifier
          method: GET # http method
          path: /dashboard # url path pattern
          find: SESSIONID # cookie name to capture
          from: cookie # captures full cookie data
    rewrite:
        - name: hide_warning # required identifier
          find: security-warning # text/pattern to find
          replace: hidden # replacement text
          from: response_body # where to apply: request_body|request_header|response_body|response_header|any
www.example.com: # original domain
    to: www.phishingclub.test # proxy domain`;

	$: {
		modalText = getModalText('Proxy', modalMode);
	}

	// hooks
	onMount(() => {
		const context = appStateService.getContext();
		if (context) {
			contextCompanyID = context.companyID;
		}
		refreshProxies();
		tableURLParams.onChange(refreshProxies);
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
	const refreshProxies = async (showLoading = true) => {
		try {
			if (showLoading) {
				isProxyTableLoading = true;
			}
			const res = await getProxies();
			proxies = res.rows;
		} catch (e) {
			addToast('Failed to load Proxies', 'Error');
			console.error('Failed to load Proxies', e);
		} finally {
			if (showLoading) {
				isProxyTableLoading = false;
			}
		}
	};

	const getProxies = async () => {
		try {
			const res = await api.proxy.getAllSubset(tableURLParams, contextCompanyID);
			if (res.success) {
				return res.data;
			}
			throw res.error;
		} catch (e) {
			addToast('Failed to load Proxies', 'Error');
			console.error('failed to get Proxies', e);
		}
		return [];
	};

	/** @param {string} id */
	const getProxy = async (id) => {
		try {
			const res = await api.proxy.getByID(id);
			if (!res.success) {
				throw res.error;
			}
			return res.data;
		} catch (e) {
			addToast('Failed to load Proxy', 'Error');
			console.error('failed to get Proxy', e);
		}
	};

	const onSubmit = async () => {
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
		try {
			const proxyData = {
				name: formValues.name,
				description: formValues.description,
				startURL: formValues.startURL,
				proxyConfig: formValues.proxyConfig
			};

			const res = await api.proxy.create({
				...proxyData,
				companyID: contextCompanyID
			});
			if (!res.success) {
				formError = res.error;
				return;
			}
			addToast('Proxy created', 'Success');
			closeModal();
			refreshProxies();
		} catch (err) {
			addToast('Failed to create Proxy', 'Error');
			console.error('failed to create Proxy:', err);
		}
	};

	const update = async () => {
		try {
			const updateData = {
				name: formValues.name,
				description: formValues.description,
				startURL: formValues.startURL,
				proxyConfig: formValues.proxyConfig
			};

			const res = await api.proxy.update(formValues.id, updateData);
			if (!res.success) {
				formError = res.error;
				return;
			}
			addToast('Proxy updated', 'Success');
			closeModal();
			refreshProxies();
		} catch (e) {
			addToast('Failed to update Proxy', 'Error');
			console.error('failed to update Proxy', e);
		}
	};

	/** @param {string} id */
	const onClickDelete = async (id) => {
		const action = api.proxy.delete(id);
		action
			.then((res) => {
				if (res.success) {
					refreshProxies();
					return;
				}
				throw res.error;
			})
			.catch((e) => {
				console.error('failed to delete Proxy:', e);
			});
		return action;
	};

	const openCreateModal = () => {
		modalMode = 'create';
		isModalVisible = true;
	};

	const closeModal = () => {
		isModalVisible = false;
		formValues.name = '';
		formValues.description = '';
		formValues.startURL = '';
		formValues.proxyConfig = '';
		formValues.id = '';
		form.reset();
		formError = '';
	};

	/** @param {string} id */
	const openUpdateModal = async (id) => {
		modalMode = 'update';
		showIsLoading();

		// reset form values first
		formValues = {
			id: null,
			name: null,
			description: null,
			startURL: null,
			proxyConfig: null
		};

		try {
			const proxy = await getProxy(id);
			const r = globalButtonDisabledAttributes(proxy, contextCompanyID);
			if (r.disabled) {
				hideIsLoading();
				return;
			}

			assignProxy(proxy);
			isModalVisible = true;
		} catch (e) {
			addToast('Failed to load Proxy', 'Error');
			console.error('failed to get Proxy', e);
		} finally {
			hideIsLoading();
		}
	};

	const openCopyModal = async (id) => {
		modalMode = 'copy';
		showIsLoading();

		// reset form values first
		formValues = {
			id: null,
			name: null,
			description: null,
			startURL: null,
			proxyConfig: null
		};

		try {
			const proxy = await getProxy(id);
			assignProxy(proxy);
			formValues.id = null; // clear ID for copy
			isModalVisible = true;
		} catch (e) {
			addToast('Failed to load Proxy', 'Error');
			console.error('failed to get Proxy', e);
		} finally {
			hideIsLoading();
		}
	};

	const openDeleteAlert = async (proxyItem) => {
		isDeleteAlertVisible = true;
		deleteValues.id = proxyItem.id;
		deleteValues.name = proxyItem.name;
	};

	const assignProxy = (proxyItem) => {
		formValues.id = proxyItem.id;
		formValues.name = proxyItem.name;
		formValues.description = proxyItem.description;
		formValues.startURL = proxyItem.startURL;
		formValues.proxyConfig = proxyItem.proxyConfig;
	};
</script>

<HeadTitle title="Proxies" />
<main>
	<div class="flex justify-between">
		<Headline>Proxies</Headline>
		<AutoRefresh
			isLoading={false}
			onRefresh={() => {
				refreshProxies(false);
			}}
		/>
	</div>
	<BigButton on:click={openCreateModal}>New Proxy</BigButton>
	<Table
		columns={[
			{ column: 'Name', size: 'large' },
			{ column: 'Start URL', size: 'medium' }
		]}
		sortable={['Name', 'Start URL']}
		hasData={!!proxies.length}
		plural="Proxies"
		pagination={tableURLParams}
		isGhost={isProxyTableLoading}
	>
		{#each proxies as proxy}
			<TableRow>
				<TableCell>
					<button
						on:click={() => {
							openUpdateModal(proxy.id);
						}}
						{...globalButtonDisabledAttributes(proxy, contextCompanyID)}
						title={proxy.name}
						class="block w-full py-1 text-left"
					>
						{proxy.name}
					</button>
				</TableCell>

				<TableCell>{proxy.startURL}</TableCell>

				<TableCellEmpty />
				<TableCellAction>
					<TableDropDownEllipsis>
						<TableUpdateButton
							on:click={() => openUpdateModal(proxy.id)}
							{...globalButtonDisabledAttributes(proxy, contextCompanyID)}
						/>
						<TableCopyButton title={'Copy'} on:click={() => openCopyModal(proxy.id)} />
						<TableDeleteButton
							on:click={() => openDeleteAlert(proxy)}
							{...globalButtonDisabledAttributes(proxy, contextCompanyID)}
						></TableDeleteButton>
					</TableDropDownEllipsis>
				</TableCellAction>
			</TableRow>
		{/each}
	</Table>
	<Modal headerText={modalText} visible={isModalVisible} onClose={closeModal} {isSubmitting}>
		<FormGrid on:submit={onSubmit} bind:bindTo={form} {isSubmitting}>
			<div class="col-span-3 w-full overflow-y-auto px-6 py-4 space-y-8">
				<!-- Basic Information Section -->
				<div class="w-full">
					<h3 class="text-base font-medium text-pc-darkblue dark:text-white mb-3">
						Basic Information
					</h3>
					<div class="grid grid-cols-1 md:grid-cols-2 gap-6">
						<div>
							<TextField
								required
								minLength={1}
								maxLength={64}
								bind:value={formValues.name}
								placeholder="Company Auth Proxy">Name</TextField
							>
						</div>
						<div>
							<TextField
								required
								minLength={3}
								maxLength={255}
								bind:value={formValues.startURL}
								placeholder="https://login.example.com/auth"
								toolTipText="The starting URL where the Proxy attack begins - domain must be in YAML mappings"
								>Start URL</TextField
							>
						</div>
					</div>
					<div class="mt-6">
						<TextField optional maxLength={255} bind:value={formValues.description}
							>Description</TextField
						>
					</div>
				</div>

				<!-- Proxy Configuration Section -->
				<div class="w-full">
					<div class="space-y-6">
						<div class="flex flex-col py-2 w-full">
							<h3 class="text-base font-medium text-pc-darkblue dark:text-white mb-3">
								Proxy Configuration
							</h3>
							<div class="w-80vw">
								<SimpleCodeEditor
									bind:value={formValues.proxyConfig}
									height="large"
									language="yaml"
									placeholder={currentExample}
								/>
							</div>
						</div>
					</div>
				</div>

				<FormError message={formError} />
			</div>

			<FormFooter {closeModal} {isSubmitting} />
		</FormGrid>
	</Modal>
	<DeleteAlert
		list={[
			'All associated domains will be deleted',
			'Templates using this Proxy will become unusable',
			'Scheduled or active campaigns using this Proxy will be cancelled'
		]}
		name={deleteValues.name}
		onClick={() => onClickDelete(deleteValues.id)}
		bind:isVisible={isDeleteAlertVisible}
	></DeleteAlert>
</main>
