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
	import ProxySvgIcon from '$lib/components/ProxySvgIcon.svelte';
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
	import Editor from '$lib/components/editor/Editor.svelte';
	import { fetchAllRows } from '$lib/utils/api-utils';
	import { BiMap } from '$lib/utils/maps';
	import AutoRefresh from '$lib/components/AutoRefresh.svelte';
	import SimpleCodeEditor from '$lib/components/editor/SimpleCodeEditor.svelte';

	// services
	const appStateService = AppStateService.instance;

	// bindings
	let form = null;
	let formValues = {
		id: null,
		name: null,
		content: null,
		type: 'regular',
		targetURL: null,
		proxyConfig: null
	};
	let isSubmitting = false;

	// data
	const tableURLParams = newTableURLParams();
	let contextCompanyID = null;
	let pages = [];
	let domainMap = new BiMap({});
	let formError = '';
	let isModalVisible = false;
	let isPageTableLoading = false;
	let modalMode = null;
	let modalText = '';

	let isDeleteAlertVisible = false;
	let deleteValues = {
		id: null,
		name: null
	};

	// proxy example configuration - simplified to only capture and replacement rules
	const proxyExample = `capture:
  - name: 'login credentials'
    method: 'POST'  # optional, default GET
    path: '/login'  # regex path pattern - matches /login exactly
    find: 'username=([^&]+)&password=([^&]+)'  # REQUIRED - regex pattern to capture data
    from: 'request_body'  # where to capture from: request_body, request_header, response_body, response_header, any
    # required: true  # default - all captures are required unless explicitly set to false

  - name: 'has completed login'
    method: 'GET'
    path: '/secure'  # navigation tracking - just checks if user visited this path
    # no find pattern needed for path-based navigation tracking
    # required: true  # default - user must visit /secure before campaign progresses

  - name: 'form submission'
    method: 'POST'
    path: '/submit-data'  # tracks POST requests to this endpoint
    # no find pattern needed - just tracking that the form was submitted

  - name: 'profile update'
    method: 'PUT'
    path: '/api/profile'  # tracks PUT requests for profile updates
    # navigation tracking works with any HTTP method

  - name: 'api tokens'
    path: '/api/v\\d+/auth.*'  # regex - matches /api/v1/auth, /api/v2/auth/token, etc.
    find: 'token=([a-zA-Z0-9]+)'  # REQUIRED - all captures must have a find pattern
    from: 'response_body'

  - name: 'optional tracking data'
    path: '^/dashboard'  # regex - matches paths starting with /dashboard
    find: 'session_id=([a-f0-9]+)'  # REQUIRED - find pattern is mandatory
    from: 'response_header'
    required: false  # explicitly mark as optional - campaign will progress without this

replace:
  - name: 'replace logo'
    find: 'https://target\\.example\\.com/logo\\.png'
    replace: 'https://evil.domain.com/assets/logo.png'

  - name: 'replace links'
    find: 'href="([^"]*target\\.example\\.com[^"]*)"'
    replace: 'href="https://evil.domain.com$1"'`;

	$: isRegularPage = formValues.type === 'regular';
	$: isProxyPage = formValues.type === 'proxy';

	$: {
		modalText = getModalText('page', modalMode);
	}

	// hooks
	onMount(() => {
		const context = appStateService.getContext();
		if (context) {
			contextCompanyID = context.companyID;
		}
		refreshPages();
		tableURLParams.onChange(refreshPages);
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
	const refreshPages = async (showLoading = true) => {
		try {
			if (showLoading) {
				isPageTableLoading = true;
			}
			const res = await getPages();
			pages = res.rows;
		} catch (e) {
			addToast('Failed to load pages', 'Error');
			console.error('Failed to load pages', e);
		} finally {
			if (showLoading) {
				isPageTableLoading = false;
			}
		}
	};

	const refreshAllDomains = async () => {
		const domains = await fetchAllRows((options) => {
			return api.domain.getAllSubset(options, contextCompanyID);
		});
		domainMap = BiMap.FromArrayOfObjects(domains);
	};

	const getPages = async () => {
		try {
			const res = await api.page.getOverviews(tableURLParams, contextCompanyID);
			if (res.success) {
				return res.data;
			}
			throw res.error;
		} catch (e) {
			addToast('Failed to load pages', 'Error');
			console.error('failed to get pages', e);
		}
		return [];
	};

	/** @param {string} id */
	const getPage = async (id) => {
		try {
			const res = await api.page.getByID(id);
			if (!res.success) {
				throw res.error;
			}
			return res.data;
		} catch (e) {
			addToast('Failed to load page', 'Error');
			console.error('failed to get page', e);
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
			const pageData = {
				name: formValues.name,
				type: formValues.type,
				content: isRegularPage ? formValues.content : null,
				targetURL: isProxyPage ? formValues.targetURL : null,
				proxyConfig: isProxyPage ? formValues.proxyConfig : null
			};

			const res = await api.page.create(
				pageData.name,
				pageData.content,
				contextCompanyID,
				pageData
			);
			if (!res.success) {
				formError = res.error;
				return;
			}
			addToast('Page created', 'Success');
			closeModal();
			refreshPages();
		} catch (err) {
			addToast('Failed to create page', 'Error');
			console.error('failed to create page:', err);
		}
	};

	const update = async () => {
		try {
			const updateData = {
				name: formValues.name,
				type: formValues.type,
				content: isRegularPage ? formValues.content : null,
				targetURL: isProxyPage ? formValues.targetURL : null,
				proxyConfig: isProxyPage ? formValues.proxyConfig : null
			};

			const res = await api.page.update(formValues.id, updateData);
			if (!res.success) {
				formError = res.error;
				return;
			}
			addToast('Page updated', 'Success');
			closeModal();
			refreshPages();
		} catch (e) {
			addToast('Failed to update page', 'Error');
			console.error('failed to update page', e);
		}
	};

	/** @param {string} id */
	const onClickDelete = async (id) => {
		const action = api.page.delete(id);
		action
			.then((res) => {
				if (res.success) {
					refreshPages();
					return;
				}
				throw res.error;
			})
			.catch((e) => {
				console.error('failed to delete page:', e);
			});
		return action;
	};

	const openCreateModal = () => {
		modalMode = 'create';
		refreshAllDomains();
		isModalVisible = true;
	};

	const closeModal = () => {
		isModalVisible = false;
		formValues.content = '';
		formValues.name = '';
		formValues.id = '';
		formValues.type = 'regular';
		formValues.targetURL = '';
		formValues.proxyConfig = '';
		form.reset();
		formError = '';
	};

	/** @param {string} id */
	const openUpdateModal = async (id) => {
		modalMode = 'update';
		refreshAllDomains();
		showIsLoading();

		// Reset form values first
		formValues = {
			id: null,
			name: null,
			content: null,
			type: 'regular',
			targetURL: null,
			proxyConfig: null
		};

		try {
			const page = await getPage(id);
			const r = globalButtonDisabledAttributes(page, contextCompanyID);
			if (r.disabled) {
				hideIsLoading();
				return;
			}

			assignPage(page);
			isModalVisible = true;
		} catch (e) {
			addToast('Failed to load page', 'Error');
			console.error('failed to get page', e);
		} finally {
			hideIsLoading();
		}
	};

	const openCopyModal = async (id) => {
		modalMode = 'copy';
		showIsLoading();

		// Reset form values first
		formValues = {
			id: null,
			name: null,
			content: null,
			type: 'regular',
			targetURL: null,
			proxyConfig: null
		};

		try {
			const page = await getPage(id);
			assignPage(page);
			formValues.id = null; // Clear ID for copy
			isModalVisible = true;
		} catch (e) {
			addToast('Failed to load page', 'Error');
			console.error('failed to get page', e);
		} finally {
			hideIsLoading();
		}
	};

	const openDeleteAlert = async (page) => {
		isDeleteAlertVisible = true;
		deleteValues.id = page.id;
		deleteValues.name = page.name;
	};

	const assignPage = (page) => {
		formValues.id = page.id;
		formValues.name = page.name;
		formValues.content = page.content || '';
		formValues.type = page.type && page.type.trim() !== '' ? page.type : 'regular';
		formValues.targetURL = page.targetURL || '';
		formValues.proxyConfig = page.proxyConfig || '';
	};

	/** @param {*} event */
	const onSetFile = (event) => {
		// read file from event
		const file = event.target.files[0];
		const reader = new FileReader();
		reader.onload = (e) => {
			formValues.content = e.target.result;
		};
		reader.readAsText(file);
		formValues.content = file;
		// reset field
		event.target.value = '';
	};
</script>

<HeadTitle title="Pages" />
<main>
	<div class="flex justify-between">
		<Headline>Pages</Headline>
		<AutoRefresh
			isLoading={false}
			onRefresh={() => {
				refreshPages(false);
			}}
		/>
	</div>
	<BigButton on:click={openCreateModal}>New Page</BigButton>
	<Table
		columns={[{ column: 'Name', size: 'large' }]}
		sortable={['Name']}
		hasData={!!pages.length}
		plural="pages"
		pagination={tableURLParams}
		isGhost={isPageTableLoading}
	>
		{#each pages as page}
			<TableRow>
				<TableCell>
					<button
						on:click={() => {
							openUpdateModal(page.id);
						}}
						{...globalButtonDisabledAttributes(page, contextCompanyID)}
						title={page.name}
						class="block w-full py-1 text-left"
					>
						{page.name}
					</button>
				</TableCell>

				<TableCellEmpty />
				<TableCellAction>
					<TableDropDownEllipsis>
						<TableUpdateButton
							on:click={() => openUpdateModal(page.id)}
							{...globalButtonDisabledAttributes(page, contextCompanyID)}
						/>
						<TableCopyButton title={'Copy'} on:click={() => openCopyModal(page.id)} />
						<TableDeleteButton
							on:click={() => openDeleteAlert(page)}
							{...globalButtonDisabledAttributes(page, contextCompanyID)}
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
								placeholder="Intranet login">Name</TextField
							>
						</div>
						<div>
							<div class="w-full">
								<div class="flex flex-col py-2">
									<div class="flex items-center">
										<p
											class="font-semibold text-slate-600 dark:text-gray-300 py-2 transition-colors duration-200"
										>
											Type
										</p>
									</div>
									<div class="flex space-x-4">
										<label
											class="flex items-center space-x-2 px-3 py-2 border rounded-lg cursor-pointer hover:bg-gray-50 dark:hover:bg-gray-700 transition-colors duration-200"
											class:bg-blue-50={formValues.type === 'regular'}
											class:border-blue-300={formValues.type === 'regular'}
											class:dark:bg-blue-900={formValues.type === 'regular'}
										>
											<input
												type="radio"
												bind:group={formValues.type}
												value="regular"
												class="text-blue-600"
											/>
											<span class="text-sm text-slate-600 dark:text-gray-300">📄 Regular</span>
										</label>
										<label
											class="flex items-center space-x-2 px-3 py-2 border rounded-lg cursor-pointer hover:bg-gray-50 dark:hover:bg-gray-700 transition-colors duration-200"
											class:bg-blue-50={formValues.type === 'proxy'}
											class:border-blue-300={formValues.type === 'proxy'}
											class:dark:bg-blue-900={formValues.type === 'proxy'}
										>
											<input
												type="radio"
												bind:group={formValues.type}
												value="proxy"
												class="text-blue-600"
											/>
											<span
												class="text-sm text-slate-600 dark:text-gray-300 flex items-center gap-1"
											>
												<ProxySvgIcon size="w-4 h-4" />
												Proxy
											</span>
										</label>
									</div>
								</div>
							</div>
						</div>
					</div>
				</div>

				<!-- Content Configuration Section -->
				<div class="w-full">
					<h3 class="text-base font-medium text-pc-darkblue dark:text-white mb-3">
						{#if isProxyPage}
							Proxy Configuration
						{:else}
							Page Content
						{/if}
					</h3>

					{#if isRegularPage}
						<Editor contentType="page" {domainMap} bind:value={formValues.content} />
					{/if}

					{#if isProxyPage}
						<div class="space-y-6">
							<div class="flex flex-col py-2 w-full">
								<div class="flex items-center">
									<p class="font-bold text-slate-600 dark:text-gray-300 py-2">
										Proxy Capture & Replacement Rules (YAML)
									</p>
									<div class="ml-2 text-xs text-gray-500">
										Data captures require a 'find' pattern. Path-based navigation tracking (any
										method) doesn't need 'find'. All captures are required by default.
									</div>
								</div>
								<div class="w-80vw">
									<SimpleCodeEditor
										bind:value={formValues.proxyConfig}
										height="medium"
										language="yaml"
										placeholder={proxyExample}
									/>
								</div>
							</div>
						</div>
					{/if}
				</div>

				<FormError message={formError} />
			</div>

			<FormFooter {closeModal} {isSubmitting} />
		</FormGrid>
	</Modal>
	<DeleteAlert
		list={[
			'Templates using this page will become unusable',
			'Scheduled or active campaigns using this domain will be cancelled'
		]}
		name={deleteValues.name}
		onClick={() => onClickDelete(deleteValues.id)}
		bind:isVisible={isDeleteAlertVisible}
	></DeleteAlert>
</main>
