<script>
	import { page } from '$app/stores';
	import { api } from '$lib/api/apiProxy.js';
	import { onMount } from 'svelte';
	import { newTableURLParams } from '$lib/service/tableURLParams.js';
	import { globalButtonDisabledAttributes } from '$lib/utils/form.js';
	import Headline from '$lib/components/Headline.svelte';
	import TextField from '$lib/components/TextField.svelte';
	import CheckboxField from '$lib/components/CheckboxField.svelte';
	import TableRow from '$lib/components/table/TableRow.svelte';
	import TableCell from '$lib/components/table/TableCell.svelte';
	import TableUpdateButton from '$lib/components/table/TableUpdateButton.svelte';
	import TableDeleteButton from '$lib/components/table/TableDeleteButton2.svelte';
	import FormError from '$lib/components/FormError.svelte';
	import { addToast } from '$lib/store/toast';
	import PasswordField from '$lib/components/PasswordField.svelte';
	import { AppStateService } from '$lib/service/appState';
	import TableCellAction from '$lib/components/table/TableCellAction.svelte';
	import TableCellEmpty from '$lib/components/table/TableCellEmpty.svelte';
	import FormGrid from '$lib/components/FormGrid.svelte';
	import Modal from '$lib/components/Modal.svelte';
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
	import TableDropDownEllipsis from '$lib/components/table/TableDropDownEllipsis.svelte';
	import DeleteAlert from '$lib/components/modal/DeleteAlert.svelte';
	import SelectSquare from '$lib/components/SelectSquare.svelte';
	import TableCellScope from '$lib/components/table/TableCellScope.svelte';

	// services
	const appStateService = AppStateService.instance;

	// data
	let form = null;
	let headerForm = null;
	let testForm = null;
	let formValues = {
		id: null,
		name: null,
		host: null,
		port: null,
		username: null,
		password: null,
		ignoreCertErrors: null
	};
	let headerFormValues = {
		id: null,
		key: null,
		value: null
	};
	let testFormValues = {
		mailFrom: null,
		email: null
	};
	let configurations = [];
	let headers = [];
	let formError = '';
	let headerError = '';
	let testError = '';
	let contextCompanyID = null;
	const tableURLParams = newTableURLParams();
	let isModalVisible = false;
	let isHeaderModalVisible = false;
	let isTestModalVisible = false;
	let isConfigTableLoading = false;
	let isSubmitting = false;
	let modalMode = null;
	let modalText = '';
	let isDeleteAlertVisible = false;
	let deleteValues = {
		id: null,
		name: null
	};

	$: {
		modalText = getModalText('configuration', modalMode);
	}

	// hooks
	onMount(() => {
		if (appStateService.getContext()) {
			contextCompanyID = appStateService.getContext().companyID;
		}
		refreshConfigurations();
		tableURLParams.onChange(refreshConfigurations);

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
	const refreshConfigurations = async () => {
		try {
			isConfigTableLoading = true;
			configurations = await getConfigurations();
		} catch (e) {
			addToast('Failed to get SMTP configurations', 'Error');
			console.error(e);
		} finally {
			isConfigTableLoading = false;
		}
	};

	/**
	 * Gets a company by ID
	 * @param {string} id
	 */
	const getConfiguration = async (id) => {
		try {
			showIsLoading();
			const res = await api.smtpConfiguration.getByID(id);
			if (res.success) {
				return res.data;
			} else {
				throw res.error;
			}
		} catch (e) {
			addToast('Failed to get SMTP configuration', 'Error');
			console.error('failed to get SMTP configuration', e);
		} finally {
			hideIsLoading();
		}
	};

	const getConfigurations = async () => {
		try {
			const res = await api.smtpConfiguration.getAll(tableURLParams, contextCompanyID);
			if (!res.success) {
				throw res.error;
			}
			return res.data.rows;
		} catch (e) {
			addToast('Failed to get SMTP configurations', 'Error');
			console.error('failed to get SMTP configurations', e);
		}
		return [];
	};

	const onClickSubmit = async () => {
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
		formError = '';
		try {
			const port = parseInt(formValues.port);
			const res = await api.smtpConfiguration.create({
				name: formValues.name,
				host: formValues.host,
				port: port,
				username: formValues.username,
				password: formValues.password,
				ignoreCertErrors: formValues.ignoreCertErrors,
				companyID: contextCompanyID
			});
			if (!res.success) {
				formError = res.error;
				return;
			}
			addToast('Created SMTP configuration', 'Success');
			closeModal();
		} catch (err) {
			addToast('Failed to create SMTP configuration', 'Error');
			console.error('failed to create SMTP configuration:', err);
		}
		refreshConfigurations();
	};

	const update = async () => {
		formError = '';
		try {
			const port = parseInt(formValues.port);
			const res = await api.smtpConfiguration.update({
				id: formValues.id,
				name: formValues.name,
				host: formValues.host,
				port: port,
				username: formValues.username,
				password: formValues.password,
				ignoreCertErrors: formValues.ignoreCertErrors,
				companyID: formValues.companyID
			});
			if (res.success) {
				addToast('Updated SMTP configuration', 'Success');
				closeModal();
			} else {
				formError = res.error;
			}
		} catch (e) {
			addToast('Failed to update SMTP configuration', 'Error');
			console.error('failed to update SMTP configuration', e);
		}
		refreshConfigurations();
	};

	const openDeleteAlert = async (conf) => {
		isDeleteAlertVisible = true;
		deleteValues.id = conf.id;
		deleteValues.name = conf.name;
	};

	/**
	 * Deletes a SMTP configuration
	 * @param {string} id
	 */
	const onClickDelete = async (id) => {
		const action = api.smtpConfiguration.delete(id);

		action
			.then((res) => {
				if (res.success) {
					refreshConfigurations();
					return;
				}
				throw res.error;
			})
			.catch((e) => {
				console.error('failed to delete smtp configuration', e);
			});
		return action;
	};

	const onClickCreateHeader = async () => {
		headerError = '';
		try {
			isSubmitting = true;
			const res = await api.smtpConfiguration.addHeader(
				headerFormValues.id,
				headerFormValues.key,
				headerFormValues.value
			);
			if (res.success) {
				addToast('Created SMTP header', 'Success');
				headerForm.reset();
				// quick hack to refresh the modal
				openHeaderModal(headerFormValues.id);
				refreshConfigurations();
				return;
			}
			headerError = res.error;
		} catch (e) {
			addToast('Failed to create SMTP header', 'Error');
			console.error('failed to create header for smtp configuration', e);
		} finally {
			isSubmitting = false;
		}
	};

	/**
	 * Deletes a header
	 * @param {string} id   smtp configuration id
	 * @param {string} headerID  header id
	 */
	const onClickDeleteHeader = async (id, headerID) => {
		const action = api.smtpConfiguration.deleteHeader(id, headerID);
		action
			.then((res) => {
				if (!res.success) {
					throw res.error;
				}
				refreshConfigurations();
				openHeaderModal(headerFormValues.id);
			})
			.catch((e) => {
				console.error('failed to delete header for smtp configuration', e);
			});

		return action;
	};

	const openCreateModal = () => {
		modalMode = 'create';
		isModalVisible = true;
	};

	const closeModal = () => {
		formError = '';
		form.reset();
		isModalVisible = false;
	};

	/**
	 * Opens the update modal
	 * @param {string} id
	 */
	const openUpdateModal = async (id) => {
		modalMode = 'update';
		try {
			showIsLoading();
			const conf = await getConfiguration(id);
			assignConfiguration(conf);
			const r = globalButtonDisabledAttributes(conf, contextCompanyID);
			if (r.disabled) {
				hideIsLoading();
				return;
			}
			isModalVisible = true;
		} catch (e) {
			addToast('Failed to get SMTP configuration', 'Error');
			console.error('failed to get SMTP configuration', e);
		} finally {
			hideIsLoading();
		}
	};

	const openCopyModal = async (id) => {
		modalMode = 'copy';
		try {
			showIsLoading();
			const conf = await getConfiguration(id);
			assignConfiguration(conf);
			isModalVisible = true;
		} catch (e) {
			addToast('Failed to get SMTP configuration', 'Error');
			console.error('failed to get SMTP configuration', e);
		} finally {
			hideIsLoading();
		}
	};

	const assignConfiguration = (configuration) => {
		formValues = {
			id: configuration.id,
			name: configuration.name,
			host: configuration.host,
			port: configuration.port,
			username: configuration.username,
			password: configuration.password,
			ignoreCertErrors: configuration.ignoreCertErrors
		};
	};

	const onClickSubmitTestModal = async () => {
		testError = '';
		try {
			isSubmitting = true;
			const res = await api.smtpConfiguration.sendTestEmail(testFormValues.id, {
				email: testFormValues.email,
				mailFrom: testFormValues.mailFrom
			});
			if (!res.success) {
				testError = res.error;
				return;
			}
			addToast('Test email sent', 'Success');
		} catch (e) {
			addToast('Failed to send test email', 'Error');
			console.error('failed to send test email', e);
		} finally {
			isSubmitting = false;
		}
	};

	const onClickShowTestModal = (id) => {
		isTestModalVisible = true;
		testFormValues.id = id;
	};

	const closeTestModal = () => {
		isTestModalVisible = false;
		testFormValues.id = null;
		testFormValues.email = '';
		testError = '';
	};

	/**
	 * Opens the header modal
	 * @param {string} id
	 */
	const openHeaderModal = async (id) => {
		try {
			showIsLoading();
			const conf = await getConfiguration(id);
			headers = conf.headers;
			isHeaderModalVisible = true;
			headerFormValues.id = id;
		} catch (e) {
			addToast('Failed to open header modal', 'Error');
			console.error('failed to open header modal', e);
		} finally {
			hideIsLoading();
		}
	};

	const closeHeaderModal = () => {
		headerForm.reset();
		headerError = '';
		headerFormValues.id = null;
		isHeaderModalVisible = false;
	};
</script>

<HeadTitle title="SMTP configurations" />
<main>
	<Headline>SMTP Configurations</Headline>
	<BigButton on:click={openCreateModal}>New configuration</BigButton>
	<Table
		columns={[
			{ column: 'Name', size: 'large' },
			{ column: 'Host', size: 'small' },
			{ column: 'Port', size: 'small' },
			{ column: 'Username', size: 'small' },
			...(contextCompanyID ? [{ column: 'Scope', size: 'small' }] : [])
		]}
		sortable={['Name', 'Host', 'Port', 'Username', ...(contextCompanyID ? ['scope'] : [])]}
		hasData={!!configurations.length}
		plural="SMTP configurations"
		pagination={tableURLParams}
		isGhost={isConfigTableLoading}
	>
		{#each configurations as conf}
			<TableRow>
				<TableCell>
					<button
						on:click={() => {
							openUpdateModal(conf.id);
						}}
						{...globalButtonDisabledAttributes(conf, contextCompanyID)}
						title={conf.name}
						class="block w-full py-1 text-left"
					>
						{conf.name}
					</button>
				</TableCell>
				<TableCell value={conf.host} />
				<TableCell value={conf.port} />
				<TableCell value={conf.username} />
				{#if contextCompanyID}
					<TableCellScope companyID={conf.companyID} />
				{/if}
				<TableCellEmpty />
				<TableCellAction>
					<TableDropDownEllipsis>
						<TableUpdateButton
							on:click={() => openUpdateModal(conf.id)}
							{...globalButtonDisabledAttributes(conf, contextCompanyID)}
						/>
						<TableUpdateButton
							name={'Update headers'}
							on:click={() => openHeaderModal(conf.id)}
							{...globalButtonDisabledAttributes(conf, contextCompanyID)}
						/>
						<TableCopyButton
							title={'Copy'}
							on:click={() => openCopyModal(conf.id)}
							{...globalButtonDisabledAttributes(conf, contextCompanyID)}
						/>
						<TableViewButton name="Perform test" on:click={() => onClickShowTestModal(conf.id)} />
						<TableDeleteButton
							on:click={() => openDeleteAlert(conf)}
							{...globalButtonDisabledAttributes(conf, contextCompanyID)}
						></TableDeleteButton>
					</TableDropDownEllipsis>
				</TableCellAction>
			</TableRow>
		{/each}
	</Table>

	<Modal headerText={modalText} visible={isModalVisible} onClose={closeModal} {isSubmitting}>
		<FormGrid on:submit={onClickSubmit} bind:bindTo={form} {isSubmitting}>
			<FormColumns>
				<FormColumn>
					<!-- Basic Configuration Section -->
					<div class="mb-6 pt-4 pb-2 border-b border-gray-200 dark:border-gray-600 w-full">
						<h3 class="text-base font-medium text-pc-darkblue dark:text-white mb-3">
							Basic Configuration
						</h3>
						<div class="space-y-6">
							<TextField
								required
								minLength={1}
								maxLength={127}
								bind:value={formValues.name}
								placeholder="Example Mailer">Name</TextField
							>
							<TextField
								required
								minLength={1}
								maxLength={255}
								bind:value={formValues.host}
								placeholder="smtp.example.test">Host</TextField
							>
							<TextField
								required
								type="number"
								min={1}
								max={65535}
								bind:value={formValues.port}
								placeholder="587">Port</TextField
							>
						</div>
					</div>

					<!-- Authentication Section -->
					<div class="mb-6 pt-4 pb-2 border-b border-gray-200 dark:border-gray-600 w-full">
						<h3 class="text-base font-medium text-pc-darkblue dark:text-white mb-3">
							Authentication
						</h3>
						<div class="space-y-6">
							<TextField
								minLength={1}
								maxLength={255}
								bind:value={formValues.username}
								placeholder="mail-user"
								optional={true}>Username</TextField
							>
							<PasswordField
								minLength={1}
								maxLength={255}
								bind:value={formValues.password}
								optional={true}>Password</PasswordField
							>
						</div>
					</div>

					<!-- Security Settings -->
					<div class="pt-4 pb-2 w-full">
						<h3 class="text-base font-medium text-pc-darkblue dark:text-white mb-3">
							Security Settings
						</h3>
						<div>
							<SelectSquare
								label="TLS Certificate Validation"
								options={[
									{ value: true, label: 'Ignore TLS Errors' },
									{ value: false, label: 'Validate Certificates' }
								]}
								bind:value={formValues.ignoreCertErrors}
							/>
						</div>
					</div>
				</FormColumn>
			</FormColumns>
			<FormError message={formError} />
			<FormFooter {closeModal} {isSubmitting} />
		</FormGrid>
	</Modal>
	<!-- TEST MODAL -->
	<Modal
		headerText={'Test Configuration'}
		visible={isTestModalVisible}
		onClose={closeTestModal}
		{isSubmitting}
	>
		<FormGrid on:submit={onClickSubmitTestModal} bind:bindTo={testForm} {isSubmitting}>
			<FormColumns>
				<FormColumn>
					<TextField bind:value={testFormValues.mailFrom} placeholder="from@domain.tld"
						>Mail From</TextField
					>
				</FormColumn>
				<FormColumn>
					<TextField bind:value={testFormValues.email} placeholder="reciever@domain.tld"
						>Email</TextField
					>
				</FormColumn>
			</FormColumns>
			<FormError message={testError} />
			<FormFooter closeModal={closeTestModal} {isSubmitting} okText="Send email" />
		</FormGrid>
	</Modal>
	<DeleteAlert
		name={deleteValues.name}
		list={[
			'Templates using this SMTP configuration will become unusable',
			'Scheduled or active campaigns using this SMTP configuration will be closed'
		]}
		onClick={() => onClickDelete(deleteValues.id)}
		bind:isVisible={isDeleteAlertVisible}
	></DeleteAlert>
	<!-- HEADER MODAL -->
	<Modal
		headerText={'SMPT headers'}
		visible={isHeaderModalVisible}
		onClose={closeHeaderModal}
		{isSubmitting}
	>
		<FormGrid on:submit={onClickCreateHeader} bind:bindTo={headerForm} {isSubmitting}>
			<FormColumns>
				<FormColumn>
					<TextField
						bind:value={headerFormValues.key}
						placeholder="X-Mailer"
						toolTipText="Mail header without trailing :">Key</TextField
					>
					<TextField bind:value={headerFormValues.value} placeholder="Phishing.Club"
						>Value</TextField
					>
				</FormColumn>
				<FormColumn>
					<Table
						columns={[
							{ column: 'Key', size: 'medium' },
							{ column: 'Value', size: 'medium' }
						]}
						hasData={!!headers.length}
						plural="headers"
					>
						{#each headers as header}
							<TableRow>
								<TableCell value={header.key} />
								<TableCell value={header.value} />
								<TableCellEmpty />
								<TableCellAction>
									<TableDropDownEllipsis>
										<TableDeleteButton
											on:click={() => onClickDeleteHeader(headerFormValues.id, header.id)}
										/>
									</TableDropDownEllipsis>
								</TableCellAction>
							</TableRow>
						{/each}
					</Table>
				</FormColumn>
			</FormColumns>
			<FormError message={headerError} />
			<FormFooter closeText={'Close'} closeModal={closeHeaderModal} {isSubmitting} />
		</FormGrid>
	</Modal>
	<!-- /HEADER MODAL -->
</main>
