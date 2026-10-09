<script>
	import { api } from '$lib/api/apiProxy.js';
	import { page } from '$app/stores';
	import { onMount } from 'svelte';
	import { globalButtonDisabledAttributes } from '$lib/utils/form.js';
	import { openPreviewBytes } from '$lib/utils/safePreview.js';
	import Headline from '$lib/components/Headline.svelte';
	import TextField from '$lib/components/TextField.svelte';
	import TableRow from '$lib/components/table/TableRow.svelte';
	import TableDeleteButton from '$lib/components/table/TableDeleteButton2.svelte';
	import TableCell from '$lib/components/table/TableCell.svelte';
	import { addToast } from '$lib/store/toast';
	import FormError from '$lib/components/FormError.svelte';
	import { AppStateService } from '$lib/service/appState';
	import TableCellEmpty from '$lib/components/table/TableCellEmpty.svelte';
	import TableCellAction from '$lib/components/table/TableCellAction.svelte';
	import TableUpdateButton from '$lib/components/table/TableUpdateButton.svelte';
	import { newTableURLParams } from '$lib/service/tableURLParams.js';
	import { fetchAllRows } from '$lib/utils/api-utils';
	import Modal from '$lib/components/Modal.svelte';
	import FormGrid from '$lib/components/FormGrid.svelte';
	import { goto } from '$app/navigation';
	import BigButton from '$lib/components/BigButton.svelte';
	import FormColumns from '$lib/components/FormColumns.svelte';
	import FormColumn from '$lib/components/FormColumn.svelte';
	import FormFooter from '$lib/components/FormFooter.svelte';
	import Table from '$lib/components/table/Table.svelte';
	import HeadTitle from '$lib/components/HeadTitle.svelte';
	import TableViewButton from '$lib/components/table/TableViewButton.svelte';
	import { showIsLoading, hideIsLoading } from '$lib/store/loading.js';
	import TableDropDownEllipsis from '$lib/components/table/TableDropDownEllipsis.svelte';
	import TableDropDownButton from '$lib/components/table/TableDropDownButton.svelte';
	import DeleteAlert from '$lib/components/modal/DeleteAlert.svelte';
	import FileField from '$lib/components/FileField.svelte';
	import SimpleCodeEditor from '$lib/components/editor/SimpleCodeEditor.svelte';
	import Editor from '$lib/components/editor/Editor.svelte';
	import { BiMap } from '$lib/utils/maps.js';
	import TableCellCheckbox from '$lib/components/table/TableCellCheckbox.svelte';
	import BulkActionBar from '$lib/components/table/BulkActionBar.svelte';
	import {
		createTableSelection,
		headerSelectionState,
		runBulkDelete
	} from '$lib/service/tableSelection.js';
	import { onClickCopy } from '$lib/utils/common.js';

	// services
	const appStateService = AppStateService.instance;

	// data
	// the 'company' route shows the company asset folder, which is stored under
	// the shared directory keyed by the company assets slug
	const isCompanyFolder = $page.params.domain === 'company';
	let domainContext =
		$page.params.domain === 'shared' || isCompanyFolder ? '' : $page.params.domain;
	// the company assets slug, fetched on mount for company folder view
	let companyAssetsKey = '';
	let pathTooltip = 'Web root relative path to the file(s).';
	if (isCompanyFolder) {
		pathTooltip = 'Reference as {{.BaseURL}}/<key>/<path> in templates and emails.';
	} else if (!domainContext) {
		pathTooltip = 'Web root relative path to the file(s) on any domain.';
	}
	let contextCompanyID = '';
	let assets = [];
	let assetsHasNextPage = true;

	// multi select
	const selection = createTableSelection();
	let isBulkDeleteAlertVisible = false;
	const isRowDisabled = (a) => globalButtonDisabledAttributes(a, contextCompanyID).disabled;
	$: selectablePageIds = assets
		.filter((a) => !globalButtonDisabledAttributes(a, contextCompanyID).disabled)
		.map((a) => a.id);
	$: headerState = headerSelectionState($selection, selectablePageIds);
	$: showMultiSelect = assets.length > 0;
	const onClickBulkDelete = async () => {
		await runBulkDelete({ ids: [...$selection], deleteFn: api.asset.delete, noun: 'asset' });
		await refreshAssets();
		return { success: true };
	};

	const tableURLParams = newTableURLParams();
	let isModalVisible = false;
	let modalError = '';
	let form = null;
	let formValues = {
		id: '',
		name: '',
		description: '',
		path: ''
	};

	let isDeleteAlertVisible = false;
	let deleteValues = {
		id: null,
		name: null,
		path: null
	};
	let modalMode = null;
	let modalText = '';
	let isSubmitting = false;
	let isTableLoading = false;
	let hoveredImageUrl = null;
	let hoveredImageName = '';
	let popoverPosition = { x: 0, y: 0 };
	let hideTimeout = null;

	$: {
		modalText = modalMode === 'create' ? 'New asset' : 'Update asset';
	}

	// hooks
	onMount(() => {
		const context = appStateService.getContext();
		if (context) {
			contextCompanyID = context.companyID ?? '';
		}
		// the company folder only exists within a company context
		if (isCompanyFolder && !contextCompanyID) {
			goto('/asset');
			return;
		}
		// if were have a domain context but are in
		refreshAssets();
		redirectIfWrongContext();
		loadDomainMap();
		tableURLParams.onChange(refreshAssets);
		return () => {
			tableURLParams.unsubscribe();
		};
	});

	const loadCompanyAssetsKey = async () => {
		try {
			const res = await api.company.getByID(contextCompanyID);
			if (!res.success) {
				throw res.error;
			}
			companyAssetsKey = res.data.assetsKey ?? '';
		} catch (e) {
			addToast('Failed to load company asset folder', 'Error');
			console.error('failed to load company assets key', e);
		}
	};

	/**
	 * Copy the template reference for a company folder asset
	 * @param {string} path
	 */
	const onClickCopyPath = (path) => {
		onClickCopy(`{{.BaseURL}}/${companyAssetsKey}/${path}`);
	};

	const redirectIfWrongContext = async () => {
		if (!domainContext || domainContext === 'shared') {
			return;
		}
		try {
			const res = await api.domain.getByName(domainContext);
			if (!res.success) {
				console.error('domain not found - unexpected error');
				return;
			}
			if (res.data.companyID && !contextCompanyID) {
				console.log(
					'redirecting to assets overview as the context does not match the current view'
				);
				addToast('Company domain assets can not be viewed in shared view', 'Error');
				goto('/asset');
			}
		} catch (e) {
			console.error('failed to get domain', e);
		}
	};

	// component logic
	const refreshAssets = async () => {
		try {
			isTableLoading = true;
			selection.clear();
			// load the company asset slug before assets render so the company
			// folder previews and copy paths resolve correctly
			if (isCompanyFolder && !companyAssetsKey) {
				await loadCompanyAssetsKey();
			}
			const res = await api.asset.getByDomain(domainContext, contextCompanyID, tableURLParams);
			if (!res.success) {
				throw res.error;
			}
			assets = res.data.rows ?? [];
			assetsHasNextPage = res.data.hasNextPage;
			// if global context but domain has a company relation, then we should redirect
		} catch (e) {
			addToast('Failed to get assets', 'Error');
			console.error('failed to get assets', e);
		} finally {
			isTableLoading = false;
		}
	};

	const onSubmit = async () => {
		try {
			isSubmitting = true;
			if (modalMode === 'create') {
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

	const update = async () => {
		try {
			// 1. name and description
			const res = await api.asset.update(formValues.id, formValues.name, formValues.description);
			if (!res.success) {
				modalError = res.error;
				throw res.error;
			}
			// 2. content, for a text file edited in the same modal. runs before the move
			// so the text check uses the current extension.
			if (editContentEditable) {
				const cr = await api.asset.saveContent(formValues.id, editContent.value);
				if (!cr.success) {
					modalError = cr.error;
					throw cr.error;
				}
			}
			// 3. rename / move, only when the path changed
			if (formValues.path && formValues.path !== editOriginalPath) {
				const mr = await api.asset.move(formValues.id, formValues.path);
				if (!mr.success) {
					modalError = mr.error;
					throw mr.error;
				}
			}
			addToast('Updated asset', 'Success');
			refreshAssets();
			closeModal();
		} catch (e) {
			addToast('Failed to update asset', 'Error');
			console.error('failed to update asset', e);
		}
	};

	const create = async () => {
		/** @type {HTMLInputElement} */
		let fileInput = document.querySelector('#files');
		let formData = new FormData();
		for (let file of fileInput.files) {
			formData.append('files', file);
		}
		formData.append('name', formValues.name);
		formData.append('description', formValues.description);
		formData.append('path', formValues.path);
		if (domainContext) {
			formData.append('domain', domainContext);
		}
		if (contextCompanyID) {
			formData.append('companyID', contextCompanyID);
		}

		// Send the form data using fetch
		try {
			const res = await api.asset.upload(formData);
			if (!res.success) {
				modalError = res.error;
				return;
			}
			addToast('Uploaded asset', 'Success');
			refreshAssets();
			closeModal();
		} catch (e) {
			addToast('Failed to upload asset', 'Error');
			console.error('failed to upload asset', e);
		}
	};

	const closeModal = () => {
		modalError = '';
		isModalVisible = false;
		editContentEditable = false;
		editContent = { id: '', path: '', value: '', language: 'plaintext' };
		editIsHtml = false;
		form.reset();
	};

	const openCreateModal = async () => {
		modalMode = 'create';
		// start clean so a previously edited asset's path does not leak in
		formValues = { id: '', name: '', description: '', path: '' };
		editContentEditable = false;
		editIsHtml = false;
		editOriginalPath = '';
		isModalVisible = true;
	};

	/**
	 * Open the edit modal: name and description, plus the content editor for a text file.
	 * @param {*} asset
	 */
	const onClickEdit = async (asset) => {
		modalMode = 'update';
		editContentEditable = false;
		editContent = { id: asset.id, path: asset.path, value: '', language: 'plaintext' };
		editIsHtml = /\.(html?|xhtml)$/i.test(asset.path);
		try {
			showIsLoading();
			const res = await api.asset.getByID(asset.id);
			if (!res.success) {
				addToast('Failed to get asset', 'Error');
				console.error('failed to get asset', res.error);
				return;
			}
			formValues.id = res.data.id;
			formValues.name = res.data.name;
			formValues.description = res.data.description;
			formValues.path = res.data.path;
			editOriginalPath = res.data.path;
			// for a text file, load its content so the same modal can edit it
			if (isTextEditable(asset.path)) {
				const cr = await api.asset.getContent(asset.id);
				if (cr.success && cr.data.editable) {
					editContentEditable = true;
					editContent.value = decodeBase64Utf8(cr.data.content);
					editContent.language = languageForPath(asset.path);
				}
			}
			isModalVisible = true;
		} catch (e) {
			addToast('Failed to get asset', 'Error');
			console.error('failed to get asset', e);
		} finally {
			hideIsLoading();
		}
	};

	// text content editing, folded into the edit modal for text files
	let editContent = { id: '', path: '', value: '', language: 'plaintext' };
	let editContentEditable = false;
	// an HTML asset uses the same editor as a landing page, with its live domain
	// preview; other text files use a plain code editor
	let editIsHtml = false;
	// domains for the editor's preview dropdown, same source as the page editor
	let domainMap = new BiMap({});

	const loadDomainMap = async () => {
		try {
			const domains = await fetchAllRows((options) =>
				api.domain.getAllSubsetWithoutProxies(options, contextCompanyID)
			);
			domainMap = BiMap.FromArrayOfObjects(domains);
		} catch (e) {
			console.error('failed to load domains for preview', e);
		}
	};

	// the asset's path when the edit modal opened, to detect a rename / move on save
	let editOriginalPath = '';

	// replace file modal
	let isReplaceVisible = false;
	let replaceValues = { id: '', path: '' };
	let replaceError = '';
	let isReplacing = false;
	let replaceForm = null;

	// extensions whose content can be edited as text, mirrors the backend
	const textEditableExtensions = [
		'.html',
		'.htm',
		'.xhtml',
		'.txt',
		'.css',
		'.js',
		'.mjs',
		'.json',
		'.xml',
		'.svg',
		'.md',
		'.csv',
		'.yml',
		'.yaml'
	];

	/**
	 * Check if a file can be edited as text based on its extension
	 * @param {string} path
	 */
	const isTextEditable = (path) => {
		if (!path) {
			return false;
		}
		const extension = path.toLowerCase().substring(path.lastIndexOf('.'));
		return textEditableExtensions.includes(extension);
	};

	/**
	 * Map a file extension to a Monaco language for syntax highlighting
	 * @param {string} path
	 */
	const languageForPath = (path) => {
		const extension = path.toLowerCase().substring(path.lastIndexOf('.'));
		switch (extension) {
			case '.html':
			case '.htm':
			case '.xhtml':
				return 'html';
			case '.css':
				return 'css';
			case '.js':
			case '.mjs':
				return 'javascript';
			case '.json':
				return 'json';
			case '.xml':
			case '.svg':
				return 'xml';
			case '.md':
				return 'markdown';
			case '.yml':
			case '.yaml':
				return 'yaml';
			default:
				return 'plaintext';
		}
	};

	/**
	 * Decode a base64 string of UTF-8 bytes into text
	 * @param {string} b64
	 */
	const decodeBase64Utf8 = (b64) => {
		const binary = atob(b64);
		const bytes = new Uint8Array(binary.length);
		for (let i = 0; i < binary.length; i++) {
			bytes[i] = binary.charCodeAt(i);
		}
		return new TextDecoder('utf-8').decode(bytes);
	};

	/**
	 * Open the replace file modal for an asset
	 * @param {*} asset
	 */
	const openReplaceModal = (asset) => {
		replaceValues = { id: asset.id, path: asset.path };
		replaceError = '';
		isReplaceVisible = true;
	};

	const submitReplace = async () => {
		try {
			isReplacing = true;
			/** @type {HTMLInputElement} */
			const fileInput = document.querySelector('#replaceFile');
			if (!fileInput || !fileInput.files || fileInput.files.length === 0) {
				replaceError = 'Select a file';
				return;
			}
			const formData = new FormData();
			formData.append('file', fileInput.files[0]);
			const res = await api.asset.replaceFile(replaceValues.id, formData);
			if (!res.success) {
				replaceError = res.error;
				return;
			}
			addToast('Replaced asset file', 'Success');
			isReplaceVisible = false;
			if (replaceForm) {
				replaceForm.reset();
			}
			refreshAssets();
		} catch (e) {
			addToast('Failed to replace asset file', 'Error');
			console.error('failed to replace asset file', e);
		} finally {
			isReplacing = false;
		}
	};

	/**
	 * Delete an asset
	 * @param {string} id
	 */
	const onClickDelete = async (id) => {
		const action = api.asset.delete(id);
		action
			.then((res) => {
				if (!res.success) {
					throw res.error;
				}
				addToast('Deleted asset', 'Success');
				refreshAssets();
			})
			.catch((e) => {
				addToast('Failed to delete asset', 'Error');
				console.error('failed to delete asset', e);
			});
		return action;
	};

	const openDeleteAlert = async (asset) => {
		isDeleteAlertVisible = true;
		deleteValues.id = asset.id;
		deleteValues.name = asset.name;
		deleteValues.path = asset.path;
	};

	const onClickPreview = async (path) => {
		if ($page.params.domain === 'shared' || isCompanyFolder) {
			const viewPath = isCompanyFolder ? `${companyAssetsKey}/${path}` : path;
			const res = await api.asset.getRaw('shared', viewPath);
			if (!res.success) {
				addToast('Failed to get asset', 'Error');
				console.error('failed to get asset', res.error);
				return;
			}
			const binaryData = atob(res.data.file);
			const byteArray = new Uint8Array(binaryData.length);
			for (let i = 0; i < binaryData.length; i++) {
				byteArray[i] = binaryData.charCodeAt(i);
			}
			// isolate html and other active content in a sandbox so a preview
			// can not run script on the admin origin
			openPreviewBytes(byteArray, res.data.mimeType);
		} else {
			// a real domain asset opens on the phishing origin, separate from admin
			window.open(`https://${$page.params.domain}/${path}`, '_blank', 'noopener,noreferrer');
		}
	};

	/**
	 * Check if a file path represents an image
	 * @param {string} path
	 */
	const isImageFile = (path) => {
		const imageExtensions = ['.jpg', '.jpeg', '.png', '.gif', '.bmp', '.webp', '.svg', '.ico'];
		const extension = path.toLowerCase().substring(path.lastIndexOf('.'));
		return imageExtensions.includes(extension);
	};

	/**
	 * Get image URL for preview
	 * @param {string} path
	 */
	const getImagePreviewUrl = async (path) => {
		if ($page.params.domain === 'shared' || isCompanyFolder) {
			try {
				const viewPath = isCompanyFolder ? `${companyAssetsKey}/${path}` : path;
				const res = await api.asset.getRaw('shared', viewPath);
				if (!res.success) {
					return null;
				}

				// Handle SVG files differently - they're text-based
				if (path.toLowerCase().endsWith('.svg')) {
					const svgContent = atob(res.data.file);
					const blob = new Blob([svgContent], { type: 'image/svg+xml' });
					return URL.createObjectURL(blob);
				} else {
					// Handle binary image files
					const binaryData = atob(res.data.file);
					const byteArray = new Uint8Array(binaryData.length);
					for (let i = 0; i < binaryData.length; i++) {
						byteArray[i] = binaryData.charCodeAt(i);
					}
					const blob = new Blob([byteArray], { type: res.data.mimeType });
					return URL.createObjectURL(blob);
				}
			} catch (e) {
				console.error('failed to get image preview', e);
				return null;
			}
		} else {
			return `https://${$page.params.domain}/${path}`;
		}
	};

	/**
	 * Handle mouse enter for image preview popover
	 * @param {MouseEvent} event
	 * @param {string} path
	 * @param {string} name
	 */
	const handleImageMouseEnter = async (event, path, name) => {
		// Clear any pending hide timeout
		if (hideTimeout) {
			clearTimeout(hideTimeout);
			hideTimeout = null;
		}

		const rect = /** @type {HTMLElement} */ (event.target).getBoundingClientRect();
		popoverPosition = {
			x: rect.right + 10,
			y: rect.top
		};
		hoveredImageName = name;
		hoveredImageUrl = await getImagePreviewUrl(path);
	};

	/**
	 * Handle mouse leave for image preview popover
	 */
	const handleImageMouseLeave = () => {
		hideTimeout = setTimeout(() => {
			hoveredImageUrl = null;
			hoveredImageName = '';
			hideTimeout = null;
		}, 100);
	};

	/**
	 * Handle mouse enter on popover to keep it visible
	 */
	const handlePopoverMouseEnter = () => {
		if (hideTimeout) {
			clearTimeout(hideTimeout);
			hideTimeout = null;
		}
	};

	/**
	 * Handle mouse leave on popover to hide it
	 */
	const handlePopoverMouseLeave = () => {
		hoveredImageUrl = null;
		hoveredImageName = '';
	};
</script>

<HeadTitle title="Assets ({isCompanyFolder ? 'company' : $page.params.domain})" />
<main>
	<Headline docSlug="assets">
		{#if isCompanyFolder}
			Company assets
		{:else}
			Assets: <span class="select-all">{$page.params.domain}</span>
		{/if}
	</Headline>
	<BigButton on:click={openCreateModal}>New asset</BigButton>
	<BulkActionBar
		count={$selection.size}
		noun="asset"
		on:delete={() => (isBulkDeleteAlertVisible = true)}
		on:clear={() => selection.clear()}
	/>
	<Table
		selectable
		{headerState}
		on:toggleAll={(e) => selection.setPageSelection(selectablePageIds, e.detail)}
		columns={[
			{ column: 'Preview', size: 'small' },
			{ column: 'Name', size: 'large' },
			{ column: 'Description', size: 'medium' },
			{ column: 'Path', size: 'medium' }
		]}
		sortable={['Name', 'Description', 'Path']}
		hasData={!!assets.length}
		hasNextPage={assetsHasNextPage}
		plural="assets"
		pagination={tableURLParams}
		isGhost={isTableLoading}
	>
		{#each assets as asset}
			<TableRow>
				{#if showMultiSelect}
					<TableCellCheckbox
						checked={$selection.has(asset.id)}
						disabled={isRowDisabled(asset)}
						on:change={() => selection.toggle(asset.id)}
					/>
				{/if}
				<TableCell>
					{#if isImageFile(asset.path)}
						{#await getImagePreviewUrl(asset.path)}
							<div class="w-12 h-12 animate-pulse rounded"></div>
						{:then imageUrl}
							{#if imageUrl}
								<button
									type="button"
									class="w-12 h-12 rounded cursor-pointer hover:opacity-80 focus:outline-none focus:ring-2 focus:ring-blue-500"
									on:click={() => onClickPreview(asset.path)}
									on:mouseenter={(e) => handleImageMouseEnter(e, asset.path, asset.name)}
									on:mouseleave={handleImageMouseLeave}
								>
									<img
										src={imageUrl}
										alt={asset.name}
										class="w-12 h-12 object-cover rounded"
										on:error={() => console.error('Failed to load image preview')}
									/>
								</button>
							{:else}
								<div
									class="w-12 h-12 rounded !flex items-center justify-center text-xs text-gray-600"
								>
									No preview
								</div>
							{/if}
						{:catch}
							<div class="w-12 h-12 rounded !flex items-center justify-center text-xs text-red-600">
								Error
							</div>
						{/await}
					{:else}
						<div
							class="w-12 h-12 rounded !flex items-center justify-center text-2xl leading-none text-gray-500"
						>
							📄
						</div>
					{/if}
				</TableCell>
				<TableCell>
					<button
						on:click={() => {
							onClickPreview(asset.path);
						}}
					>
						{asset.name}
					</button>
				</TableCell>
				<TableCell value={asset.description} />
				<TableCell>
					{asset.path}
				</TableCell>
				<TableCellEmpty />
				<TableCellAction>
					<TableDropDownEllipsis>
						<TableViewButton on:click={() => onClickPreview(asset.path)} />
						{#if isCompanyFolder}
							<TableDropDownButton
								name="Copy path"
								title="Copy template reference"
								on:click={() => onClickCopyPath(asset.path)}
							/>
						{/if}
						<TableUpdateButton
							on:click={() => onClickEdit(asset)}
							{...globalButtonDisabledAttributes(asset, contextCompanyID)}
						/>
						<TableDropDownButton
							name="Replace file"
							title="Replace the file content with an upload"
							on:click={() => openReplaceModal(asset)}
							{...globalButtonDisabledAttributes(asset, contextCompanyID)}
						/>
						<TableDeleteButton
							on:click={() => openDeleteAlert(asset)}
							{...globalButtonDisabledAttributes(asset, contextCompanyID)}
						></TableDeleteButton>
					</TableDropDownEllipsis>
				</TableCellAction>
			</TableRow>
		{/each}
	</Table>
	<Modal headerText={modalText} visible={isModalVisible} onClose={closeModal} {isSubmitting}>
		<FormGrid on:submit={onSubmit} bind:bindTo={form} {isSubmitting} {modalMode}>
			{#if modalMode === 'update' && editContentEditable && editIsHtml}
				<!-- HTML asset: the same editor as a landing page, with live domain preview -->
				<Editor
					contentType="domain"
					{domainMap}
					baseURL={domainContext || 'example.test'}
					bind:value={editContent.value}
				>
					<div class="flex w-full flex-row flex-wrap items-start gap-x-6 gap-y-2 pl-4">
						<TextField
							minLength={1}
							maxLength={127}
							bind:value={formValues.name}
							optional={true}
							placeholder={'Candidate CV'}>Name</TextField
						>
						<TextField
							bind:value={formValues.description}
							optional={true}
							minLength={1}
							maxLength={255}
							placeholder="Fake CV with embedded link">Description</TextField
						>
						<TextField
							bind:value={formValues.path}
							minLength={2}
							maxLength={512}
							pattern="[a-zA-Z0-9\._\/\-]+"
							placeholder={'profile/alice/cv.html'}
							toolTipText="Full path including filename. Change it to rename or move the file on save."
							>Path</TextField
						>
					</div>
				</Editor>
			{:else if modalMode === 'update' && editContentEditable}
				<!-- non-HTML text asset: plain code editor, no preview -->
				<div class="col-start-1 col-end-4 flex w-[70vw] flex-col gap-4">
					<div class="flex w-full flex-row flex-wrap items-start gap-x-6 gap-y-2">
						<TextField
							minLength={1}
							maxLength={127}
							bind:value={formValues.name}
							optional={true}
							placeholder={'Candidate CV'}>Name</TextField
						>
						<TextField
							bind:value={formValues.description}
							optional={true}
							minLength={1}
							maxLength={255}
							placeholder="Fake CV with embedded link">Description</TextField
						>
						<TextField
							bind:value={formValues.path}
							minLength={2}
							maxLength={512}
							pattern="[a-zA-Z0-9\._\/\-]+"
							placeholder={'profile/alice/app.css'}
							toolTipText="Full path including filename. Change it to rename or move the file on save."
							>Path</TextField
						>
					</div>
					<SimpleCodeEditor
						bind:value={editContent.value}
						language={editContent.language}
						height="large"
					/>
				</div>
			{:else}
				<FormColumns>
					<FormColumn>
						<TextField
							minLength={1}
							maxLength={127}
							bind:value={formValues.name}
							optional={true}
							placeholder={'Candidate CV'}>Name</TextField
						>
						<TextField
							bind:value={formValues.description}
							optional={true}
							minLength={1}
							maxLength={255}
							placeholder="Fake CV with embedded link">Description</TextField
						>
						{#if modalMode === 'create'}
							<TextField
								bind:value={formValues.path}
								minLength={2}
								maxLength={512}
								pattern="[a-zA-Z0-9\._\/\-]+"
								optional={true}
								placeholder={'profile/alice'}
								toolTipText={pathTooltip}>Path</TextField
							>

							<FileField name="files" multiple={true}>Files</FileField>
						{:else}
							<TextField
								bind:value={formValues.path}
								minLength={2}
								maxLength={512}
								pattern="[a-zA-Z0-9\._\/\-]+"
								placeholder={'profile/alice/cv.pdf'}
								toolTipText="Full path including filename. Change it to rename or move the file on save."
								>Path</TextField
							>
						{/if}
					</FormColumn>
				</FormColumns>
			{/if}
			<FormError message={modalError} />
			<FormFooter {closeModal} {isSubmitting} />
		</FormGrid>
	</Modal>
	<Modal
		headerText={`Replace: ${replaceValues.path}`}
		visible={isReplaceVisible}
		onClose={() => (isReplaceVisible = false)}
		isSubmitting={isReplacing}
	>
		<FormGrid on:submit={submitReplace} bind:bindTo={replaceForm} isSubmitting={isReplacing}>
			<FormColumns>
				<FormColumn>
					<FileField name="replaceFile" multiple={false}>File</FileField>
				</FormColumn>
			</FormColumns>
			<FormError message={replaceError} />
			<FormFooter closeModal={() => (isReplaceVisible = false)} isSubmitting={isReplacing} />
		</FormGrid>
	</Modal>
	<DeleteAlert
		name={deleteValues.name || deleteValues.path || 'Unnamed asset'}
		onClick={() => onClickDelete(deleteValues.id)}
		bind:isVisible={isDeleteAlertVisible}
	></DeleteAlert>
	<DeleteAlert
		title="Delete assets"
		name={`${$selection.size} asset${$selection.size === 1 ? '' : 's'}`}
		onClick={onClickBulkDelete}
		confirm
		bind:isVisible={isBulkDeleteAlertVisible}
	></DeleteAlert>

	<!-- Image Preview Popover -->
	{#if hoveredImageUrl}
		<div
			class="fixed z-50 bg-white border border-gray-300 rounded-lg shadow-lg p-2 max-w-xs"
			style="left: {popoverPosition.x}px; top: {popoverPosition.y}px;"
			on:mouseenter={handlePopoverMouseEnter}
			on:mouseleave={handlePopoverMouseLeave}
			role="tooltip"
			aria-label="Image preview"
		>
			<img
				src={hoveredImageUrl}
				alt={hoveredImageName}
				class="max-w-full max-h-64 object-contain rounded"
				on:error={() => console.error('Failed to load popover image')}
			/>
			<div class="text-xs text-gray-600 mt-1 truncate">{hoveredImageName}</div>
		</div>
	{/if}
</main>
