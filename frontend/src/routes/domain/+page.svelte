<script>
	import { page } from '$app/stores';
	import { api } from '$lib/api/apiProxy.js';
	import { onMount } from 'svelte';
	import { goto } from '$app/navigation';
	import { newTableURLParams } from '$lib/service/tableURLParams.js';
	import { globalButtonDisabledAttributes } from '$lib/utils/form.js';
	import Headline from '$lib/components/Headline.svelte';
	import TextField from '$lib/components/TextField.svelte';
	import CheckboxField from '$lib/components/CheckboxField.svelte';
	import TableRow from '$lib/components/table/TableRow.svelte';
	import TableCell from '$lib/components/table/TableCell.svelte';
	import TableUpdateButton from '$lib/components/table/TableUpdateButton.svelte';
	import TableDeleteButton from '$lib/components/table/TableDeleteButton2.svelte';
	import { addToast } from '$lib/store/toast';
	import FormError from '$lib/components/FormError.svelte';
	import { AppStateService } from '$lib/service/appState';
	import TableCellAction from '$lib/components/table/TableCellAction.svelte';
	import TableCellEmpty from '$lib/components/table/TableCellEmpty.svelte';
	import FormGrid from '$lib/components/FormGrid.svelte';
	import Modal from '$lib/components/Modal.svelte';
	import TableCellCheck from '$lib/components/table/TableCellCheck.svelte';
	import BigButton from '$lib/components/BigButton.svelte';
	import FormColumns from '$lib/components/FormColumns.svelte';
	import FormColumn from '$lib/components/FormColumn.svelte';
	import FormFooter from '$lib/components/FormFooter.svelte';
	import Table from '$lib/components/table/Table.svelte';
	import HeadTitle from '$lib/components/HeadTitle.svelte';
	import { getModalText } from '$lib/utils/common';
	import TableCopyButton from '$lib/components/table/TableCopyButton.svelte';
	import TableViewButton from '$lib/components/table/TableViewButton.svelte';
	import { showIsLoading, hideIsLoading } from '$lib/store/loading.js';
	import FileField from '$lib/components/FileField.svelte';
	import Editor from '$lib/components/editor/Editor.svelte';
	import TableDropDownEllipsis from '$lib/components/table/TableDropDownEllipsis.svelte';
	import DeleteAlert from '$lib/components/modal/DeleteAlert.svelte';
	import TableDropDownButton from '$lib/components/table/TableDropDownButton.svelte';
	import SelectSquare from '$lib/components/SelectSquare.svelte';

	// services
	const appStateService = AppStateService.instance;

	// data
	let form = null;
	let contentForm = null;
	let contentNotFoundForm = null;
	let formValues = {
		id: null,
		name: null,
		managedTLS: true, // managed TLS
		ownManagedTLS: false, // custom certificates
		ownManagedTLSKey: null,
		ownManagedTLSPem: null,
		hostWebsite: true,
		pageContent: '', // default value
		pageNotFoundContent: '404 page not found', // default value
		redirectURL: ''
	};

	let isDeleteAlertVisible = false;
	let deleteValues = {
		id: null,
		name: null
	};

	let defaultValues = {
		...formValues
	};
	let contextCompanyID = null;
	let domains = [];
	let modalError = '';
	let isSubmitting = false;
	let updateContentError = '';
	let tableURLParams = newTableURLParams();
	let isModalVisible = false;
	let isUpdateContentModalVisible = false;
	let isUpdateNotFoundModalVisible = false;
	let isCopyContentModalVisible = false;
	let isDomainTableLoading = false;
	// @type {null|'create'|'update'}
	let modalMode = null;
	let modalText = '';

	/** @type {HTMLInputElement|null} */
	let managedTLSInputElement = null;
	/** @type {HTMLInputElement|null} */
	let ownManagedTLSInputElement = null;
	/** @type {HTMLInputElement|null} */
	let ownManagedTLSKeyElement = null;
	/** @type {HTMLInputElement|null} */
	let ownManagedPemKeyElement = null;

	$: {
		modalText = '';
		modalText = getModalText('domain', modalMode);
	}
	// hooks
	onMount(() => {
		const context = appStateService.getContext();
		if (context) {
			contextCompanyID = context.companyID;
		}
		refreshDomains();
		tableURLParams.onChange(refreshDomains);

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

	const refreshDomains = async (showLoading = true) => {
		try {
			if (showLoading) {
				isDomainTableLoading = true;
			}
			const result = await getDomains();
			domains = result.rows;
		} catch (e) {
			addToast('failed to load domains', 'Error');
			console.error('failed to load domains', e);
		} finally {
			if (showLoading) {
				isDomainTableLoading = false;
			}
		}
	};

	const getDomains = async () => {
		try {
			const res = await api.domain.getAllSubset(tableURLParams, contextCompanyID);
			if (!res.success) {
				throw res.error;
			}
			return res.data;
		} catch (e) {
			addToast('Failed to load domains', 'Error');
			console.error('Failed to load domains', e);
		}
		return [];
	};

	/**
	 * Get a domain by id
	 * @param {string} id - The domain id
	 */
	const getDomain = async (id) => {
		try {
			const res = await api.domain.getByID(id);
			if (!res.success) {
				throw res.error;
			}
			return res.data;
		} catch (e) {
			addToast('Failed to load domain', 'Error');
			console.error('failed to load domain', e);
		}
		return null;
	};

	const onSubmitPageUpdate = async () => {
		try {
			await onClickUpdate();
		} finally {
			isSubmitting = false;
		}
	};

	const onSubmit = async () => {
		try {
			// reset validate
			managedTLSInputElement?.setCustomValidity('');
			ownManagedTLSInputElement?.setCustomValidity('');
			ownManagedTLSKeyElement?.setCustomValidity('');
			ownManagedPemKeyElement?.setCustomValidity('');
			// validate custom - allow both to be disabled for external TLS termination
			if (formValues.ownManagedTLS && formValues.managedTLS) {
				modalError = 'Managed TLS and Custom Certificates can not both be enabled';
				return;
			}
			if (formValues.ownManagedTLS) {
				if (!formValues.ownManagedTLSKey) {
					modalError = 'Certificate .key and .pem is required';
					return;
				}

				if (!formValues.ownManagedTLSPem) {
					modalError = 'Certificate .key and .pem is required';
					return;
				}
			}
			// check / report validation
			if (!form.checkValidity()) {
				form.reportValidity();
				return;
			}
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
		modalError = '';
		try {
			// clear site contents if not hosting a website
			if (!formValues.hostWebsite) {
				formValues.pageContent = '';
				formValues.pageNotFoundContent = '';
			}
			const res = await api.domain.create({
				name: formValues.name,
				managedTLS: formValues.managedTLS,
				ownManagedTLS: formValues.ownManagedTLS,
				ownManagedTLSKey: formValues.ownManagedTLSKey,
				ownManagedTLSPem: formValues.ownManagedTLSPem,
				hostWebsite: formValues.hostWebsite,
				pageContent: formValues.pageContent,
				pageNotFoundContent: formValues.pageNotFoundContent,
				redirectURL: formValues.redirectURL,
				companyID: contextCompanyID
			});
			if (res.success) {
				addToast('Domain created', 'Success');
				closeAllModals();
				refreshDomains();
				return;
			}
			modalError = res.error;
		} catch (err) {
			addToast('Failed to create domain', 'Error');
			console.error('failed to create domain:', err);
		}
	};

	const onClickUpdate = async () => {
		modalError = '';
		// clear site contents if not hosting a website
		if (!formValues.hostWebsite) {
			formValues.pageContent = '';
			formValues.pageNotFoundContent = '';
		}
		try {
			const res = await api.domain.update({
				id: formValues.id,
				managedTLS: formValues.managedTLS,
				ownManagedTLS: formValues.ownManagedTLS,
				ownManagedTLSKey: formValues.ownManagedTLSKey,
				ownManagedTLSPem: formValues.ownManagedTLSPem,
				hostWebsite: formValues.hostWebsite,
				pageContent: formValues.pageContent,
				pageNotFoundContent: formValues.pageNotFoundContent,
				redirectURL: formValues.redirectURL,
				companyID: contextCompanyID
			});
			if (!res.success) {
				modalError = res.error;
				return;
			}
			addToast('Domain updated', 'Success');
			closeAllModals();
			refreshDomains();
		} catch (e) {
			addToast('Failed to update domain', 'Error');
			console.error('failed to update domain', e);
		}
	};

	/**
	 * Goto the asset view of the domain
	 * @param {string} domain - The domain
	 */
	const gotoDomainAssets = (domain) => {
		goto(`/asset/${domain}/`);
	};

	/**
	 * Delete a domain
	 * @param {string} id - The domain id
	 */
	const onClickDelete = async (id) => {
		const action = api.domain.delete(id);
		action
			.then((res) => {
				if (!res.success) {
					throw res.error;
				}
				refreshDomains();
			})
			.catch((e) => {
				console.error('failed to delete domain:', e);
			});
		return action;
	};

	const openCreateModal = () => {
		modalMode = 'create';
		isModalVisible = true;
	};

	/**
	 * Open the update content modal
	 * @param {string} id - The domain id
	 */
	const openUpdateModal = async (id) => {
		modalMode = 'update';
		showIsLoading();
		try {
			const domain = await getDomain(id);
			formValues = {
				id: domain.id,
				name: domain.name,
				managedTLS: domain.managedTLS,
				ownManagedTLS: domain.ownManagedTLS,
				ownManagedTLSKey: null,
				ownManagedTLSPem: null,
				hostWebsite: domain.hostWebsite,
				pageContent: domain.pageContent,
				pageNotFoundContent: domain.pageNotFoundContent,
				redirectURL: domain.redirectURL,
				staticContent: domain.staticContent
			};
			const r = globalButtonDisabledAttributes(domain, contextCompanyID);
			if (r.disabled) {
				hideIsLoading();
				return;
			}
			isModalVisible = true;
		} catch (e) {
			addToast('Failed to load domain', 'Error');
			console.error('failed to load domain', e);
		} finally {
			hideIsLoading();
		}
	};

	/**
	 * Open the update content modal
	 * @param {string} id - The domain id
	 */
	const openUpdateContentModal = async (id) => {
		modalMode = 'update';
		showIsLoading();
		try {
			const domain = await getDomain(id);
			assignDomainValues(domain);
			isUpdateContentModalVisible = true;
		} catch (e) {
			addToast('Failed to load domain', 'Error');
			console.error('failed to load domain', e);
		} finally {
			hideIsLoading();
		}
	};

	const openCopyModal = async (id) => {
		modalMode = 'update';
		showIsLoading();
		try {
			const domain = await getDomain(id);
			domain.id = null;
			assignDomainValues(domain);
			modalMode = 'copy';
			isCopyContentModalVisible = true;
		} catch (e) {
			addToast('Failed to load domain', 'Error');
			console.error('failed to load domain', e);
		} finally {
			hideIsLoading();
		}
	};

	const assignDomainValues = async (domain) => {
		formValues = {
			id: domain.id,
			name: domain.name,
			managedTLS: domain.managedTLS,
			ownManagedTLS: domain.ownManagedTLS,
			ownManagedTLSKey: null,
			ownManagedTLSPem: null,
			hostWebsite: domain.hostWebsite,
			pageContent: domain.pageContent,
			pageNotFoundContent: domain.pageNotFoundContent,
			redirectURL: domain.redirectURL,
			staticContent: domain.staticContent
		};
	};

	const closeAllModals = () => {
		modalError = '';
		formValues.id = null;
		if (form) {
			form.reset();
		}
		if (contentForm) {
			contentForm.reset();
		}
		if (contentNotFoundForm) {
			contentNotFoundForm.reset();
		}
		isModalVisible = false;
		formValues = {
			id: null,
			name: null,
			managedTLS: true, // managed TLS
			ownManagedTLS: false, // custom certificates
			ownManagedTLSKey: null,
			ownManagedTLSPem: null,
			hostWebsite: true,
			pageContent: '', // default value
			pageNotFoundContent: '404 page not found', // default value
			redirectURL: ''
		};
		isModalVisible = false;
		isUpdateNotFoundModalVisible = false;
		isUpdateContentModalVisible = false;
		isCopyContentModalVisible = false;
	};

	/**
	 * Open the update not found content modal
	 * @param {string} id - The domain id
	 */
	const openUpdateNotFoundContentModal = async (id) => {
		showIsLoading();
		try {
			const domain = await getDomain(id);
			formValues = {
				id: domain.id,
				name: domain.name,
				managedTLS: domain.managedTLS,
				ownManagedTLS: domain.ownManagedTLS,
				ownManagedTLSKey: null,
				ownManagedTLSPem: null,
				hostWebsite: domain.hostWebsite,
				pageContent: domain.pageContent,
				pageNotFoundContent: domain.pageNotFoundContent,
				redirectURL: domain.redirectURL
			};
			isUpdateNotFoundModalVisible = true;
		} catch (e) {
			addToast('Failed to load domain', 'Error');
			console.error('failed to load domain', e);
		} finally {
			hideIsLoading();
		}
	};

	const openDeleteAlert = async (domain) => {
		isDeleteAlertVisible = true;
		deleteValues.id = domain.id;
		deleteValues.name = domain.name;
	};

	/**
	 * @param {*} event
	 * @param {string} formValuesTarget
	 */
	const onSetFile = (event, formValuesTarget) => {
		// read file from event
		const file = event.target.files[0];
		const reader = new FileReader();
		reader.onload = (e) => {
			formValues[formValuesTarget] = e.target.result.toString();
		};
		reader.readAsText(file);
	};
</script>

<HeadTitle title="Domains" />
<main>
	<div class="flex justify-between">
		<Headline>Domains</Headline>
	</div>
	<BigButton on:click={openCreateModal}>New domain</BigButton>
	<Table
		columns={[
			{ column: 'Name', size: 'large' },
			{ column: 'Hosting website', size: 'small', alignText: 'center' },
			{ column: 'Redirects', size: 'small', alignText: 'center' },
			{ column: 'Managed TLS', size: 'small', alignText: 'center' },
			{ column: 'Custom Certificates', size: 'small', alignText: 'center' }
		]}
		sortable={['Name', 'Hosting website', 'Redirects']}
		hasData={!!domains.length}
		plural="domains"
		pagination={tableURLParams}
		isGhost={isDomainTableLoading}
	>
		{#each domains as domain}
			<TableRow>
				<TableCell>
					<button
						on:click={() => {
							openUpdateModal(domain.id);
						}}
						{...globalButtonDisabledAttributes(domain, contextCompanyID)}
						title={domain.name}
					>
						{domain.name}
					</button>
				</TableCell>
				<TableCellCheck>
					{#if domain.hostWebsite}
						<img class="w-6" src="/icon-true.svg" alt="true" />
					{:else}
						<img class="w-6" src="/icon-false.svg" alt="false" />
					{/if}
				</TableCellCheck>
				<TableCellCheck>
					{#if domain.redirectURL}
						<img class="w-6" src="/icon-true.svg" alt="true" />
					{:else}
						<img class="w-6" src="/icon-false.svg" alt="false" />
					{/if}
				</TableCellCheck>
				<TableCellCheck>
					{#if domain.managedTLS}
						<img class="w-6" src="/icon-true.svg" alt="true" />
					{:else}
						<img class="w-6" src="/icon-false.svg" alt="false" />
					{/if}
				</TableCellCheck>
				<TableCellCheck>
					{#if domain.ownManagedTLS}
						<img class="w-6" src="/icon-true.svg" alt="true" />
					{:else}
						<img class="w-6" src="/icon-false.svg" alt="false" />
					{/if}
				</TableCellCheck>
				<TableCellEmpty />
				<TableCellAction>
					<TableDropDownEllipsis>
						<TableViewButton
							title={'View'}
							on:click={() => {
								window.open(`https://${domain.name}`, '_blank');
							}}
						/>
						<TableUpdateButton
							name={'Settings'}
							on:click={() => openUpdateModal(domain.id)}
							{...globalButtonDisabledAttributes(domain, contextCompanyID)}
						/>
						<TableUpdateButton
							name={'Update page'}
							on:click={() => openUpdateContentModal(domain.id)}
							{...globalButtonDisabledAttributes(domain, contextCompanyID)}
						/>
						<TableUpdateButton
							name={'Update 404 page'}
							on:click={() => openUpdateNotFoundContentModal(domain.id)}
							{...globalButtonDisabledAttributes(domain, contextCompanyID)}
						/>
						<TableCopyButton title={'Copy'} on:click={() => openCopyModal(domain.id)} />
						<TableDeleteButton
							on:click={() => openDeleteAlert(domain)}
							{...globalButtonDisabledAttributes(domain, contextCompanyID)}
						></TableDeleteButton>
						<TableDropDownButton name={'Assets'} on:click={() => gotoDomainAssets(domain.name)} />
					</TableDropDownEllipsis>
				</TableCellAction>
			</TableRow>
		{/each}
	</Table>

	<Modal
		headerText={modalText}
		visible={isModalVisible || isCopyContentModalVisible}
		onClose={closeAllModals}
		{isSubmitting}
	>
		<FormGrid novalidate on:submit={onSubmit} bind:bindTo={form} {isSubmitting}>
			<FormColumns>
				<FormColumn>
					<!-- Domain Information Section -->
					<div class="mb-6 pt-4 pb-2 border-b border-gray-200 w-full">
						<h3 class="text-base font-medium text-pc-darkblue mb-3">Domain Information</h3>
						<div class="space-y-6">
							<TextField
								minLength={3}
								maxLength={255}
								required
								readonly={modalMode === 'update'}
								bind:value={formValues.name}
								placeholder="example.com">Domain</TextField
							>

							<SelectSquare
								label="Website Hosting"
								options={[
									{ value: true, label: 'Host Website' },
									{ value: false, label: 'Redirect Only' }
								]}
								bind:value={formValues.hostWebsite}
							/>

							{#if !formValues.hostWebsite}
								<TextField
									bind:value={formValues.redirectURL}
									optional
									type="url"
									minLength={8}
									maxLength={1024}
									placeholder="https://example.com"
									toolTipText="Redirect to another website when visiting domain &#13 (except for landing page or asset)"
									>Redirect URL</TextField
								>
							{/if}
						</div>
					</div>

					<!-- TLS Configuration Section -->
					<div class="mb-6 pt-4 pb-2 w-full">
						<h3 class="text-base font-medium text-pc-darkblue mb-3">TLS Configuration</h3>
						<div class="space-y-6">
							<SelectSquare
								label="Managed TLS"
								options={[
									{ value: true, label: 'Enable' },
									{ value: false, label: 'Disable' }
								]}
								bind:value={formValues.managedTLS}
								toolTipText="Managed TLS via. public certificate authority"
							/>

							<SelectSquare
								label="Custom Certificates"
								options={[
									{ value: true, label: 'Enable' },
									{ value: false, label: 'Disable' }
								]}
								bind:value={formValues.ownManagedTLS}
								toolTipText="Upload own certificates for TLS"
							/>

							{#if formValues.ownManagedTLS}
								<div class="space-y-4">
									<FileField
										bind:bindTo={ownManagedTLSKeyElement}
										name="certKey"
										accept=".key"
										on:change={(e) => onSetFile(e, 'ownManagedTLSKey')}
										>Private key (.key)</FileField
									>
									<FileField
										bind:bindTo={ownManagedPemKeyElement}
										name="certPem"
										accept=".pem"
										on:change={(e) => onSetFile(e, 'ownManagedTLSPem')}
										>Certificate (.pem)</FileField
									>
								</div>
							{/if}
						</div>
					</div>
					<FormError message={modalError} />
				</FormColumn>
			</FormColumns>
			<FormFooter {isSubmitting} closeModal={closeAllModals} />
		</FormGrid>
	</Modal>
	<!-- Domain Content Editor -->
	{#if isUpdateContentModalVisible}
		<Modal
			headerText={'Domain content'}
			bind:visible={isUpdateContentModalVisible}
			onClose={closeAllModals}
			{isSubmitting}
		>
			<FormGrid on:submit={onSubmitPageUpdate} bind:bindTo={contentForm} {isSubmitting}>
				<Editor
					contentType="domain"
					baseURL={formValues.name}
					bind:value={formValues.pageContent}
				/>
				<FormError message={updateContentError} />
				<FormFooter closeModal={closeAllModals} {isSubmitting} />
			</FormGrid>
		</Modal>
	{/if}
	<!-- Domain Not Found Editor -->
	<Modal
		headerText={'Domain not found content'}
		bind:visible={isUpdateNotFoundModalVisible}
		onClose={closeAllModals}
		{isSubmitting}
	>
		<FormGrid on:submit={onSubmitPageUpdate} bind:bindTo={contentNotFoundForm} {isSubmitting}>
			<Editor
				contentType="domain"
				baseURL={formValues.pageNotFoundContent}
				bind:value={formValues.pageNotFoundContent}
			/>
			<FormError message={updateContentError} />
			<FormFooter closeModal={closeAllModals} {isSubmitting} />
		</FormGrid>
	</Modal>
	<!-- /Domain Not Found Editor -->
	<DeleteAlert
		list={[
			'All assets will be deleted',
			'Templates using this domain will become unusable',
			'Scheduled or active campaigns using this domain will be closed'
		]}
		name={deleteValues.name}
		onClick={() => onClickDelete(deleteValues.id)}
		bind:isVisible={isDeleteAlertVisible}
	></DeleteAlert>
</main>
