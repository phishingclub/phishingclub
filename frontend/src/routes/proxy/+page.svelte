<script>
	import FormSection from '$lib/components/form/FormSection.svelte';
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
	import InstalledResourceBadge from '$lib/components/InstalledResourceBadge.svelte';
	import FormGrid from '$lib/components/FormGrid.svelte';
	import BigButton from '$lib/components/BigButton.svelte';
	import FormFooter from '$lib/components/FormFooter.svelte';
	import Table from '$lib/components/table/Table.svelte';
	import HeadTitle from '$lib/components/HeadTitle.svelte';
	import { getModalText } from '$lib/utils/common';
	import TableCopyButton from '$lib/components/table/TableCopyButton.svelte';
	import TableViewButton from '$lib/components/table/TableViewButton.svelte';
	import { showIsLoading, hideIsLoading } from '$lib/store/loading.js';
	import TableDropDownEllipsis from '$lib/components/table/TableDropDownEllipsis.svelte';
	import DeleteAlert from '$lib/components/modal/DeleteAlert.svelte';
	import SimpleCodeEditor from '$lib/components/editor/SimpleCodeEditor.svelte';
	import TableCellCheckbox from '$lib/components/table/TableCellCheckbox.svelte';
	import BulkActionBar from '$lib/components/table/BulkActionBar.svelte';
	import {
		createTableSelection,
		headerSelectionState,
		runBulkDelete
	} from '$lib/service/tableSelection.js';
	import AutoRefresh from '$lib/components/AutoRefresh.svelte';
	import TableCellScope from '$lib/components/table/TableCellScope.svelte';
	import ProxyConfigBuilder from '$lib/components/proxy/ProxyConfigBuilder.svelte';
	import jsyaml, { dumpWithLiteralStrings } from '$lib/components/yaml/index.js';
	import FileField from '$lib/components/FileField.svelte';

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

	// cert state for custom TLS mode, populated by certChange event (visual mode) or yaml cert upload fields
	let globalTLSKey = '';
	let globalTLSPem = '';

	// detect if the current yaml config uses tls.mode: "custom" anywhere so we can show upload fields in yaml mode
	$: yamlHasCustomTLS = (() => {
		if (!formValues.proxyConfig || editorMode !== 'yaml') return false;
		try {
			// simple string check first to avoid parsing on every keystroke
			if (!formValues.proxyConfig.includes('custom')) return false;
			// dynamic import not available in reactive block, use a regex check on the yaml string
			// matches: mode: "custom" or mode: 'custom' or mode: custom
			return /mode\s*:\s*['"]?custom['"]?/.test(formValues.proxyConfig);
		} catch {
			return false;
		}
	})();

	// data
	const tableURLParams = newTableURLParams();
	let contextCompanyID = null;
	let proxies = [];
	let proxiesHasNextPage = true;

	// multi select
	const selection = createTableSelection();
	let isBulkDeleteAlertVisible = false;
	const isRowDisabled = (p) => globalButtonDisabledAttributes(p, contextCompanyID).disabled;
	$: selectablePageIds = proxies
		.filter((p) => !globalButtonDisabledAttributes(p, contextCompanyID).disabled)
		.map((p) => p.id);
	$: headerState = headerSelectionState($selection, selectablePageIds);
	$: showMultiSelect = proxies.length > 0;
	const onClickBulkDelete = async () => {
		await runBulkDelete({
			ids: [...$selection],
			deleteFn: api.proxy.delete,
			noun: 'proxy',
			nounPlural: 'proxies'
		});
		await refreshProxies();
		return { success: true };
	};
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
	let isClearIPAllowListAlertVisible = false;
	// phones show only IP and expiry so both fit without a sideways scroll
	let isPhone = false;

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
			const parsed = jsyaml.load(yamlStr);
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
			// use dumpWithLiteralStrings to preserve literal block style for replace/body fields
			formValues.proxyConfig = dumpWithLiteralStrings(parsed);
		} catch (e) {
			console.warn('Failed to parse imported YAML config:', e);
		}
	}

	// export configuration to YAML file with metadata (for YAML mode)
	function exportYamlConfig() {
		try {
			// parse current config
			const parsed = formValues.proxyConfig
				? jsyaml.load(formValues.proxyConfig) || {}
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

			// serialize to YAML with literal block style for replace/body fields
			const yamlContent = dumpWithLiteralStrings(output);

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

# global configuration (applies to all hosts unless overridden)
global:
  tls:
    mode: "managed"  # "managed" (Let's Encrypt), "self-signed", or "custom" (upload cert below)
  # template variables allow recipient data in rewrite rules
  # variables:
  #   enabled: true
  #   allowed: ["Email", "FirstName", "LastName"]  # optional: restrict to specific variables

portal.example.com:
  to: "evil.example.com"
  # optional: specify scheme for proxying to target (defaults to https)
  # scheme: "http"   # use http:// when connecting to target
  # scheme: "https"  # use https:// when connecting to target
  # optional: override global TLS config for this specific host
  # tls:
  #   mode: "self-signed"   # or "custom" for a manually supplied certificate
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
    # when variables are enabled, you can use recipient data:
    # - name: "personalize_greeting"
    #   find: "Welcome, User"
    #   replace: "Welcome, {{.FirstName}}"
    #   from: "response_body"
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
		const phoneQuery = window.matchMedia('(max-width: 639.98px)');
		const onPhoneChange = () => (isPhone = phoneQuery.matches);
		onPhoneChange();
		phoneQuery.addEventListener('change', onPhoneChange);
		refreshProxies();
		tableURLParams.onChange(refreshProxies);
		(async () => {
			const editID = $page.url.searchParams.get('edit');
			if (editID) {
				await openUpdateModal(editID);
			}
		})();
		return () => {
			phoneQuery.removeEventListener('change', onPhoneChange);
			tableURLParams.unsubscribe();
		};
	});

	// component logic
	const refreshProxies = async (showLoading = true) => {
		try {
			if (showLoading) {
				isProxyTableLoading = true;
			}
			selection.clear();
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
				name: proxyData.name,
				description: proxyData.description,
				startURL: proxyData.startURL,
				proxyConfig: proxyData.proxyConfig,
				companyID: contextCompanyID,
				globalTLSKey: globalTLSKey || undefined,
				globalTLSPem: globalTLSPem || undefined
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
				proxyConfig: formValues.proxyConfig,
				globalTLSKey: globalTLSKey || undefined,
				globalTLSPem: globalTLSPem || undefined
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
		globalTLSKey = '';
		globalTLSPem = '';
	};

	/**
	 * @param {Event} event
	 * @param {'key'|'pem'} target
	 */
	const onYamlCertFile = (event, target) => {
		const file = /** @type {HTMLInputElement} */ (event.target).files?.[0];
		if (!file) return;
		const reader = new FileReader();
		reader.onload = (e) => {
			if (target === 'key') {
				globalTLSKey = e.target?.result?.toString() ?? '';
			} else {
				globalTLSPem = e.target?.result?.toString() ?? '';
			}
		};
		reader.readAsText(file);
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

		try {
			const res = await api.ipAllowList.getForProxyConfig(proxy.id);
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
			return res;
		} catch (e) {
			addToast('Failed to clear IP allow list', 'Error');
			console.error('failed to clear IP allow list', e);
		}
	};
</script>

<HeadTitle title="Proxies" />
<main>
	<!-- fixed row height on wide screens so the taller refresh select does not push the title down -->
	<div class="flex flex-wrap items-center justify-between gap-3 md:h-[2.625rem] md:flex-nowrap">
		<div class="min-w-0 w-full sm:w-auto">
			<Headline docSlug="proxies">Proxies</Headline>
		</div>
		<AutoRefresh
			isLoading={false}
			onRefresh={() => {
				refreshProxies(false);
			}}
		/>
	</div>
	<BigButton on:click={openCreateModal}>New Proxy</BigButton>
	<BulkActionBar
		count={$selection.size}
		noun="proxy"
		nounPlural="proxies"
		on:delete={() => (isBulkDeleteAlertVisible = true)}
		on:clear={() => selection.clear()}
	/>
	<Table
		selectable
		{headerState}
		on:toggleAll={(e) => selection.setPageSelection(selectablePageIds, e.detail)}
		columns={[
			{ column: 'Name' },
			{ column: 'Start URL' },
			...(contextCompanyID ? [{ column: 'Scope', size: 'small' }] : [])
		]}
		sortable={['Name', 'Start URL', ...(contextCompanyID ? ['scope'] : [])]}
		hasData={!!proxies.length}
		hasNextPage={proxiesHasNextPage}
		plural="proxies"
		pagination={tableURLParams}
		isGhost={isProxyTableLoading}
	>
		{#each proxies as proxy}
			<TableRow>
				{#if showMultiSelect}
					<TableCellCheckbox
						checked={$selection.has(proxy.id)}
						disabled={isRowDisabled(proxy)}
						on:change={() => selection.toggle(proxy.id)}
					/>
				{/if}
				<TableCell wrap>
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
						<TableViewButton name="View IP allow list" on:click={() => openIPAllowListModal(proxy)} />
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
		width="lg"
	>
		<FormGrid on:submit={onSubmit} bind:bindTo={form} {isSubmitting} {modalMode}>
			<div
				class="w-full py-4 {isModalFullscreen
					? 'flex flex-col min-h-0 overflow-hidden'
					: 'overflow-y-auto space-y-8'}"
			>
				{#if modalMode === 'update'}
					<!-- zero width so the notice wraps to the form width instead of setting it -->
					<div class="w-0 min-w-full {isModalFullscreen ? '' : '-mb-6'}">
						<InstalledResourceBadge resourceType="proxy" resourceId={formValues.id} variant="notice" />
					</div>
				{/if}
				{#if editorMode === 'yaml'}
					<!-- only shown in YAML mode -->
					<FormSection title="Basic Information" css="pt-4">
						<input
							type="file"
							accept=".yaml,.yml"
							bind:this={yamlFileInput}
							on:change={handleYamlImportFile}
							class="hidden"
						/>
						<div class="grid grid-cols-1 md:grid-cols-2 md:gap-x-4">
							<div class="min-w-0">
								<TextField
									width="full"
									required
									minLength={1}
									maxLength={64}
									bind:value={formValues.name}
									placeholder="Company Auth Proxy">Name</TextField
								>
							</div>
							<div class="min-w-0">
								<TextField
									width="full"
									optional
									maxLength={255}
									bind:value={formValues.description}
									placeholder="What this proxy is for">Description</TextField
								>
							</div>
							<div class="min-w-0 md:col-span-2">
								<TextField
									width="full"
									required
									minLength={3}
									bind:value={formValues.startURL}
									placeholder="https://login.example.com/auth"
									toolTipText="Domain must match a phishing domain in the hosts configuration"
									>Start URL</TextField
								>
							</div>
						</div>
						{#if yamlHasCustomTLS}
							<div class="grid grid-cols-1 md:grid-cols-[1fr_1fr] gap-4 mt-2">
								<FileField
									name="yamlGlobalCertKey"
									accept=".key"
									optional
									on:change={(e) => onYamlCertFile(e, 'key')}>Private Key (.key)</FileField
								>
								<FileField
									name="yamlGlobalCertPem"
									accept=".pem,.crt"
									optional
									on:change={(e) => onYamlCertFile(e, 'pem')}>Certificate (.pem, .crt)</FileField
								>
							</div>
						{/if}
					</FormSection>
				{/if}

				<!-- Proxy Configuration Section -->
				<div
					class="w-full {isModalFullscreen ? 'flex-1 flex flex-col min-h-0 overflow-hidden' : ''}"
				>
					<div class={isModalFullscreen ? 'flex flex-col h-full min-h-0' : 'space-y-4'}>
						<div class="flex flex-wrap items-center justify-between gap-x-3 gap-y-2 mb-4">
							<h3 class="text-base font-semibold text-pc-darkblue dark:text-white">
								Proxy Configuration
							</h3>
							<div class="flex flex-wrap items-center gap-2 sm:gap-3">
								<!-- Import/Export Buttons -->
								<div class="flex gap-2">
									<button
										type="button"
										class="pc-focus flex items-center gap-1.5 h-9 px-3 text-sm font-medium text-slate-700 dark:text-gray-300 bg-white dark:bg-gray-700 border border-gray-300 dark:border-gray-600 rounded-md hover:bg-gray-50 dark:hover:bg-gray-600 transition-colors"
										on:click={() =>
											editorMode === 'yaml'
												? triggerYamlImport()
												: proxyConfigBuilder?.triggerImport()}
										title="Import configuration from YAML file"
									>
										<svg
											class="w-4 h-4"
											aria-hidden="true"
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
										class="pc-focus flex items-center gap-1.5 h-9 px-3 text-sm font-medium text-slate-700 dark:text-gray-300 bg-white dark:bg-gray-700 border border-gray-300 dark:border-gray-600 rounded-md hover:bg-gray-50 dark:hover:bg-gray-600 transition-colors"
										on:click={() =>
											editorMode === 'yaml'
												? exportYamlConfig()
												: proxyConfigBuilder?.exportConfig()}
										title="Export configuration to YAML file"
									>
										<svg
											class="w-4 h-4"
											aria-hidden="true"
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
								<!-- Editor Mode Tabs -->
								<div
									role="group"
									aria-label="Editor mode"
									class="flex border border-gray-300 dark:border-gray-600 rounded-md overflow-hidden"
								>
									{#each [{ value: 'yaml', label: 'YAML' }, { value: 'gui', label: 'Visual' }] as option}
										<button
											type="button"
											aria-pressed={editorMode === option.value}
											class="h-9 px-4 text-sm font-medium transition-colors duration-200 focus:outline-none focus-visible:ring-2 focus-visible:ring-inset focus-visible:ring-cta-blue dark:focus-visible:ring-highlight-blue {editorMode ===
											option.value
												? 'bg-cta-blue text-white'
												: 'bg-white dark:bg-gray-700 text-gray-700 dark:text-gray-300 hover:bg-gray-100 dark:hover:bg-gray-600'}"
											on:click={() => (editorMode = option.value)}
										>
											{option.label}
										</button>
									{/each}
								</div>
							</div>
						</div>

						{#if editorMode === 'yaml'}
							<div class="w-full min-w-0">
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
										on:certChange={(e) => {
											globalTLSKey = e.detail.globalTLSKey || '';
											globalTLSPem = e.detail.globalTLSPem || '';
										}}
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
	<DeleteAlert
		title="Delete proxies"
		list={[
			'All associated domains will be deleted',
			'Templates using these proxies will become unusable',
			'Scheduled or active campaigns using these proxies will be cancelled'
		]}
		name={`${$selection.size} ${$selection.size === 1 ? 'proxy' : 'proxies'}`}
		onClick={onClickBulkDelete}
		confirm
		bind:isVisible={isBulkDeleteAlertVisible}
	></DeleteAlert>

	<Modal
		headerText={`IP allow list: ${selectedProxyForIPList?.name || ''}`}
		visible={isIPAllowListModalVisible}
		onClose={closeIPAllowListModal}
		isSubmitting={false}
		width="lg"
	>
		<div class="w-full py-4 space-y-4">
			{#if !isLoadingIPAllowList && ipAllowListEntries?.length > 0}
				<div class="flex justify-end">
					<BigButton
						variant="danger-outline"
						on:click={() => (isClearIPAllowListAlertVisible = true)}>Clear all</BigButton
					>
				</div>
			{/if}
			{#if isLoadingIPAllowList}
				<div class="flex items-center justify-center py-8">
					<div
						class="animate-spin rounded-full h-8 w-8 border-b-2 border-cta-blue dark:border-highlight-blue"
					></div>
					<span class="ml-2 text-gray-600 dark:text-gray-400">Loading...</span>
				</div>
			{:else}
				<Table
					columns={isPhone
						? [
								{ column: 'IP Address', size: 'small' },
								{ column: 'Expires At', size: 'small' }
							]
						: [
								{ column: 'IP Address', size: 'medium' },
								{ column: 'Added At', size: 'medium' },
								{ column: 'Expires At', size: 'medium' }
							]}
					hasData={ipAllowListEntries?.length > 0}
					hasActions={false}
					plural="entries"
				>
					{#each ipAllowListEntries as entry}
						<TableRow>
							<TableCell value={entry.ip} />
							{#if !isPhone}
								<TableCell value={entry.createdAt} isDate />
							{/if}
							<TableCell value={entry.expiresAt} isDate />
						</TableRow>
					{/each}
				</Table>
			{/if}
		</div>
		<!-- same sticky footer row and close button as FormFooter, without a submit button -->
		<div
			class="sticky bottom-0 z-10 -mx-4 sm:-mx-8 px-4 sm:px-8 py-4 border-t-2 border-gray-200 dark:border-gray-700 bg-white dark:bg-gray-800 flex justify-end transition-colors duration-200"
		>
			<button
				type="button"
				on:click={closeIPAllowListModal}
				class="pc-focus min-w-[6rem] bg-transparent hover:bg-gray-100 border border-gray-300 text-slate-700 dark:bg-gray-700 dark:hover:bg-gray-600 dark:border-gray-500 dark:text-gray-100 text-sm uppercase font-bold px-4 py-2 rounded-md transition-colors duration-200"
				>Close</button
			>
		</div>
	</Modal>
	<DeleteAlert
		title="Clear IP allow list"
		name={selectedProxyForIPList?.name || ''}
		actionMessage="Clear every allow listed IP for"
		onClick={clearIPAllowList}
		bind:isVisible={isClearIPAllowListAlertVisible}
	></DeleteAlert>
</main>
