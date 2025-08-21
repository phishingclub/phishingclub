<script>
	import { api } from '$lib/api/apiProxy.js';
	import { page } from '$app/stores';
	import { onMount } from 'svelte';
	import { globalButtonDisabledAttributes } from '$lib/utils/form.js';
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
	import DeleteAlert from '$lib/components/modal/DeleteAlert.svelte';

	// services
	const appStateService = AppStateService.instance;

	// data
	let domainContext = $page.params.domain === 'shared' ? '' : $page.params.domain;
	let pathTooltip = 'Web root relative path to the file(s).';
	if (!domainContext) {
		pathTooltip = 'Web root relative path to the file(s) on any domain.';
	}
	let contextCompanyID = '';
	let assets = [];
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
		name: null
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
		// if were have a domain context but are in
		refreshAssets();
		redirectIfWrongContext();
		tableURLParams.onChange(refreshAssets);
		return () => {
			tableURLParams.unsubscribe();
		};
	});

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
			const res = await api.asset.getByDomain(domainContext, contextCompanyID, tableURLParams);
			if (!res.success) {
				throw res.error;
			}
			assets = res.data.rows ?? [];
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
			const res = await api.asset.update(formValues.id, formValues.name, formValues.description);
			if (res.success) {
				addToast('Successfully updated asset', 'Success');
				refreshAssets();
				closeModal();
				return;
			}
			modalError = res.error;
			throw res.error;
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
			addToast('Successfully uploaded asset', 'Success');
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
		form.reset();
	};

	const openCreateModal = async () => {
		modalMode = 'create';
		isModalVisible = true;
	};

	/**
	 * @param {string} id
	 */
	const onClickEdit = async (id) => {
		modalMode = 'update';
		// get the asset
		try {
			showIsLoading();
			const res = await api.asset.getByID(id);
			if (!res.success) {
				addToast('Failed to get asset', 'Error');
				console.error('failed to get asset', res.error);
				return;
			}
			isModalVisible = true;
			formValues.id = res.data.id;
			formValues.name = res.data.name;
			formValues.description = res.data.description;
		} catch (e) {
			addToast('Failed to get asset', 'Error');
			console.error('failed to get asset', e);
		} finally {
			hideIsLoading();
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
				addToast('Successfully deleted asset', 'Success');
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
	};

	const onClickPreview = async (path) => {
		if ($page.params.domain === 'shared') {
			const res = await api.asset.getRaw('shared', path);
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
			const blob = new Blob([byteArray], { type: res.data.mimeType });
			const url = URL.createObjectURL(blob);
			window.open(url, '_blank');
		} else {
			window.open(`https://${$page.params.domain}/${path}`, '_blank');
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
		if ($page.params.domain === 'shared') {
			try {
				const res = await api.asset.getRaw('shared', path);
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

<HeadTitle title="Assets ({$page.params.domain})" />
<main>
	<Headline>
		Assets: <span class="select-all">{$page.params.domain}</span>
	</Headline>
	<BigButton on:click={openCreateModal}>New asset</BigButton>
	<Table
		columns={[
			{ column: 'Preview', size: 'small' },
			{ column: 'Name', size: 'large' },
			{ column: 'Description', size: 'medium' },
			{ column: 'Path', size: 'medium' }
		]}
		sortable={['Name', 'Description', 'Path']}
		hasData={!!assets.length}
		plural="assets"
		pagination={tableURLParams}
		isGhost={isTableLoading}
	>
		{#each assets as asset}
			<TableRow>
				<TableCell>
					{#if isImageFile(asset.path)}
						{#await getImagePreviewUrl(asset.path)}
							<div class="w-12 h-12 bg-gray-200 animate-pulse rounded"></div>
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
									class="w-12 h-12 bg-gray-300 rounded flex items-center justify-center text-xs text-gray-600"
								>
									No preview
								</div>
							{/if}
						{:catch}
							<div
								class="w-12 h-12 bg-red-100 rounded flex items-center justify-center text-xs text-red-600"
							>
								Error
							</div>
						{/await}
					{:else}
						<div
							class="w-12 h-12 bg-gray-100 rounded flex items-center justify-center text-xs text-gray-500"
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
						<TableUpdateButton
							on:click={() => onClickEdit(asset.id)}
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
		<FormGrid on:submit={onSubmit} bind:bindTo={form} {isSubmitting}>
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

						<label for="file" class="flex flex-col py-2 w-60">
							<p class="font-semibold text-slate-600 py-2">Files</p>

							<input
								id="files"
								type="file"
								name="files"
								class="border-solid border-2 py-2 px-2 rounded-md file:px-4 file:py-2 file:text-white file:cursor-pointer file:text-sm file:font-semibold file:bg-cta-green hover:cursor-pointer file:hover:bg-teal-300 file:border-hidden file:rounded-md"
								multiple
							/>
						</label>
					{/if}
				</FormColumn>
			</FormColumns>
			<FormError message={modalError} />
			<FormFooter {closeModal} {isSubmitting} />
		</FormGrid>
	</Modal>
	<DeleteAlert
		name={deleteValues.name}
		onClick={() => onClickDelete(deleteValues.id)}
		bind:isVisible={isDeleteAlertVisible}
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
