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
	import ProxyConfigBuilder from '$lib/components/proxy/ProxyConfigBuilder.svelte';

	// services
	const appStateService = AppStateService.instance;

	// bindings
	let form = null;
	let proxyConfigBuilder = null;
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

	// counter to force ProxyConfigBuilder recreation when modal opens
	let modalOpenCounter = 0;

	// file input reference for YAML mode import
	let yamlFileInput = null;

	// import configuration from YAML string with metadata (for YAML mode)
	function importYamlConfig(yamlStr) {
		if (!yamlStr || yamlStr.trim() === '') {
			return;
		}

		try {
			// dynamically import js-yaml
			import('$lib/components/yaml/index.js').then((jsyaml) => {
				const parsed = jsyaml.default.load(yamlStr);
				if (!parsed || typeof parsed !== 'object') {
					console.warn('Invalid YAML: not an object');
					return;
				}

				// extract and apply general section
				if (parsed._general) {
					if (parsed._general.name) {
						formValues.name = parsed._general.name;
					}
					if (parsed._general.description) {
						formValues.description = parsed._general.description;
					}
					if (parsed._general.start_url) {
						formValues.startURL = parsed._general.start_url;
					}
					// remove _general from parsed object before serializing back
					delete parsed._general;
				}

				// serialize back to YAML without _meta for the config
				const cleanYaml = jsyaml.default.dump(parsed, {
					indent: 2,
					lineWidth: -1,
					quotingType: "'",
					forceQuotes: false,
					noRefs: true
				});
				formValues.proxyConfig = cleanYaml;
			});
		} catch (e) {
			console.warn('Failed to parse imported YAML config:', e);
		}
	}

	// export configuration to YAML file with metadata (for YAML mode)
	function exportYamlConfig() {
		import('$lib/components/yaml/index.js').then((jsyaml) => {
			try {
				// parse current config
				const parsed = formValues.proxyConfig
					? jsyaml.default.load(formValues.proxyConfig) || {}
					: {};

				// build output with _general first
				const output = {};

				// add general section with proxy metadata
				output._general = {};
				if (formValues.name) {
					output._general.name = formValues.name;
				}
				if (formValues.description) {
					output._general.description = formValues.description;
				}
				if (formValues.startURL) {
					output._general.start_url = formValues.startURL;
				}

				// merge rest of config
				Object.assign(output, parsed);

				// serialize to YAML
				const yamlContent = jsyaml.default.dump(output, {
					indent: 2,
					lineWidth: -1,
					quotingType: "'",
					forceQuotes: false,
					noRefs: true
				});

				// create blob and download
				const blob = new Blob([yamlContent], { type: 'application/x-yaml' });
				const url = URL.createObjectURL(blob);
				const a = document.createElement('a');
				a.href = url;
				const safeName = (formValues.name || 'proxy-config').replace(/[^a-zA-Z0-9-_]/g, '_');
				a.download = `${safeName}.yaml`;
				document.body.appendChild(a);
				a.click();
				document.body.removeChild(a);
				URL.revokeObjectURL(url);
			} catch (e) {
				console.warn('Failed to export YAML config:', e);
			}
		});
	}

	// trigger file input for YAML mode import
	function triggerYamlImport() {
		yamlFileInput?.click();
	}

	// handle file selection for YAML mode import
	function handleYamlImportFile(event) {
		const file = event.target.files?.[0];
		if (!file) return;

		const reader = new FileReader();
		reader.onload = (e) => {
			const content = e.target?.result;
			if (typeof content === 'string') {
				importYamlConfig(content);
			}
		};
		reader.readAsText(file);

		// reset file input so same file can be imported again
		event.target.value = '';
	}

	// editor mode: 'yaml' or 'gui' - restore from localStorage, default to yaml for safety
	const EDITOR_MODE_STORAGE_KEY = 'proxy-editor-mode';
	let editorMode =
		(typeof localStorage !== 'undefined' && localStorage.getItem(EDITOR_MODE_STORAGE_KEY)) ||
		'yaml';

	// save editor mode to localStorage when it changes
	$: if (typeof localStorage !== 'undefined' && editorMode) {
		localStorage.setItem(EDITOR_MODE_STORAGE_KEY, editorMode);
	}

	// fullscreen mode for modal - automatically true when in GUI mode
	$: isModalFullscreen = editorMode === 'gui';

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
  # optional: specify scheme for proxying to target (defaults to https)
  # scheme: "http"   # use http:// when connecting to target
  # scheme: "https"  # use https:// when connecting to target
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

			// validate config when in GUI mode
			if (editorMode === 'gui' && proxyConfigBuilder) {
				const validation = proxyConfigBuilder.validate();
				if (!validation.valid) {
					isSubmitting = false;
					return;
				}
			}

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
				// only refresh the table when actually closing the modal
				refreshProxies();
			}
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
		modalOpenCounter++;
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
		modalOpenCounter++;
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
		modalOpenCounter++;
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
	<Modal
		headerText={modalText}
		visible={isModalVisible}
		onClose={closeModal}
		{isSubmitting}
		fullscreen={isModalFullscreen}
	>
		<FormGrid on:submit={onSubmit} bind:bindTo={form} {isSubmitting} {modalMode}>
			<div
				class="col-span-3 w-full px-6 py-4 {isModalFullscreen
					? 'flex flex-col min-h-0 overflow-hidden'
					: 'overflow-y-auto space-y-8'}"
			>
				{#if editorMode === 'yaml'}
					<!-- Basic Information Section - only shown in YAML mode -->
					<div class="w-full mb-6 pt-4 pb-2 border-b border-gray-200 dark:border-gray-600">
						<div class="flex justify-between items-center mb-3">
							<h3 class="text-base font-medium text-pc-darkblue dark:text-white">
								Basic Information
							</h3>
							<div class="flex gap-2">
								<input
									type="file"
									accept=".yaml,.yml"
									bind:this={yamlFileInput}
									on:change={handleYamlImportFile}
									class="hidden"
								/>
								<button
									type="button"
									class="flex items-center gap-1.5 px-3 py-1.5 text-xs font-medium text-slate-600 dark:text-gray-400 bg-slate-100 dark:bg-gray-800 border border-slate-200 dark:border-gray-700 rounded-md hover:bg-slate-200 dark:hover:bg-gray-700 transition-colors"
									on:click={triggerYamlImport}
									title="Import configuration from YAML file"
								>
									<svg
										class="w-4 h-4"
										viewBox="0 0 24 24"
										fill="none"
										stroke="currentColor"
										stroke-width="2"
									>
										<path d="M21 15v4a2 2 0 01-2 2H5a2 2 0 01-2-2v-4" />
										<polyline points="7 10 12 15 17 10" />
										<line x1="12" y1="15" x2="12" y2="3" />
									</svg>
									Import
								</button>
								<button
									type="button"
									class="flex items-center gap-1.5 px-3 py-1.5 text-xs font-medium text-slate-600 dark:text-gray-400 bg-slate-100 dark:bg-gray-800 border border-slate-200 dark:border-gray-700 rounded-md hover:bg-slate-200 dark:hover:bg-gray-700 transition-colors"
									on:click={exportYamlConfig}
									title="Export configuration to YAML file"
								>
									<svg
										class="w-4 h-4"
										viewBox="0 0 24 24"
										fill="none"
										stroke="currentColor"
										stroke-width="2"
									>
										<path d="M21 15v4a2 2 0 01-2 2H5a2 2 0 01-2-2v-4" />
										<polyline points="17 8 12 3 7 8" />
										<line x1="12" y1="3" x2="12" y2="15" />
									</svg>
									Export
								</button>
							</div>
						</div>
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
									bind:value={formValues.startURL}
									placeholder="https://login.example.com/auth"
									toolTipText="Domain must match a phishing domain in the hosts configuration"
									>Start URL</TextField
								>
							</div>
						</div>
					</div>
				{/if}

				<!-- Proxy Configuration Section -->
				<div
					class="w-full {isModalFullscreen ? 'flex-1 flex flex-col min-h-0 overflow-hidden' : ''}"
				>
					<div class={isModalFullscreen ? 'flex flex-col h-full min-h-0' : 'space-y-4'}>
						<div class="flex items-center justify-between mb-4">
							<h3 class="text-base font-medium text-pc-darkblue dark:text-white">
								Proxy Configuration
							</h3>
							<!-- Editor Mode Tabs -->
							<div
								class="flex border border-gray-300 dark:border-gray-600 rounded-lg overflow-hidden"
							>
								<button
									type="button"
									class="px-4 py-2 text-sm font-medium transition-colors duration-200 {editorMode ===
									'yaml'
										? 'bg-blue-600 text-white'
										: 'bg-gray-100 dark:bg-gray-700 text-gray-700 dark:text-gray-300 hover:bg-gray-200 dark:hover:bg-gray-600'}"
									on:click={() => (editorMode = 'yaml')}
								>
									YAML
								</button>
								<button
									type="button"
									class="px-4 py-2 text-sm font-medium transition-colors duration-200 {editorMode ===
									'gui'
										? 'bg-blue-600 text-white'
										: 'bg-gray-100 dark:bg-gray-700 text-gray-700 dark:text-gray-300 hover:bg-gray-200 dark:hover:bg-gray-600'}"
									on:click={() => (editorMode = 'gui')}
								>
									Visual
								</button>
							</div>
						</div>

						{#if editorMode === 'yaml'}
							<div class="w-80vw">
								<SimpleCodeEditor
									bind:value={formValues.proxyConfig}
									height="large"
									language="yaml"
									placeholder={currentExample}
									enableProxyCompletion={true}
								/>
							</div>
						{:else}
							<div class="flex-1 min-h-0 overflow-hidden">
								{#key modalOpenCounter}
									<ProxyConfigBuilder
										bind:this={proxyConfigBuilder}
										config={formValues.proxyConfig}
										name={formValues.name}
										description={formValues.description}
										startURL={formValues.startURL}
										on:change={(e) => (formValues.proxyConfig = e.detail)}
										on:nameChange={(e) => (formValues.name = e.detail)}
										on:descriptionChange={(e) => (formValues.description = e.detail)}
										on:startURLChange={(e) => (formValues.startURL = e.detail)}
									/>
								{/key}
							</div>
						{/if}
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
