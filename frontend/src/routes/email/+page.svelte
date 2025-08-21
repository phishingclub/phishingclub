<script>
	import { page } from '$app/stores';
	import { api } from '$lib/api/apiProxy.js';
	import { onMount } from 'svelte';
	import { goto } from '$app/navigation';
	import { newTableURLParams } from '$lib/service/tableURLParams.js';
	import { globalButtonDisabledAttributes } from '$lib/utils/form.js';
	import Headline from '$lib/components/Headline.svelte';
	import TextField from '$lib/components/TextField.svelte';
	import TableCell from '$lib/components/table/TableCell.svelte';
	import TableRow from '$lib/components/table/TableRow.svelte';
	import TableUpdateButton from '$lib/components/table/TableUpdateButton.svelte';
	import TableDeleteButton from '$lib/components/table/TableDeleteButton2.svelte';
	import { addToast } from '$lib/store/toast';
	import FormError from '$lib/components/FormError.svelte';
	import { AppStateService } from '$lib/service/appState';
	import TableCellEmpty from '$lib/components/table/TableCellEmpty.svelte';
	import TableCellAction from '$lib/components/table/TableCellAction.svelte';
	import CheckboxField from '$lib/components/CheckboxField.svelte';
	import Modal from '$lib/components/Modal.svelte';
	import FormGrid from '$lib/components/FormGrid.svelte';
	import TableCellCheck from '$lib/components/table/TableCellCheck.svelte';
	import Table from '$lib/components/table/Table.svelte';
	import BigButton from '$lib/components/BigButton.svelte';
	import FormFooter from '$lib/components/FormFooter.svelte';
	import HeadTitle from '$lib/components/HeadTitle.svelte';
	import { getModalText } from '$lib/utils/common';
	import TableCopyButton from '$lib/components/table/TableCopyButton.svelte';
	import TableViewButton from '$lib/components/table/TableViewButton.svelte';
	import { showIsLoading, hideIsLoading } from '$lib/store/loading.js';
	import Editor from '$lib/components/editor/Editor.svelte';
	import { defaultOptions, fetchAllRows } from '$lib/utils/api-utils';
	import FormColumns from '$lib/components/FormColumns.svelte';
	import FormColumn from '$lib/components/FormColumn.svelte';
	import TextFieldSelect from '$lib/components/TextFieldSelect.svelte';
	import { BiMap } from '$lib/utils/maps';
	import TableDropDownEllipsis from '$lib/components/table/TableDropDownEllipsis.svelte';
	import DeleteAlert from '$lib/components/modal/DeleteAlert.svelte';
	import TableDropDownButton from '$lib/components/table/TableDropDownButton.svelte';

	// services
	const appStateService = AppStateService.instance;

	// data
	let contextCompanyID = null;
	let form = null;
	let sendTestForm = null;
	let formValues = {
		id: null,
		name: null,
		content: null,
		mailEnvelopeFrom: null,
		mailHeaderFrom: null,
		mailHeaderSubject: null,
		addTrackingPixel: false
	};
	let isDeleteAlertVisible = false;
	let deleteValues = {
		id: null,
		name: null
	};
	let smtpMap = new BiMap({});
	let recipientMap = new BiMap({});
	let domainMap = new BiMap({});
	let selectedTestSMTPValue = null;
	let selectedTestEmailID = null;
	let selectedTestEmailRecipientID = null;
	let selectedTestDomainID = null;
	let modalError = '';
	let sendTestModalError = '';
	let emails = [];
	const tableURLParams = newTableURLParams();
	let isModalVisible = false;
	let isSendTestModalVisible = false;
	let isTableLoading = false;
	// @type {null|'create'|'update'}
	let modalMode = null;
	let modalText = '';
	let isSubmitting = false;

	$: {
		modalText = getModalText('email', modalMode);
	}

	// hooks
	onMount(() => {
		const context = appStateService.getContext();
		if (context) {
			contextCompanyID = context.companyID;
		}
		refreshEmails();
		tableURLParams.onChange(refreshEmails);

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
	const refreshEmails = async () => {
		try {
			isTableLoading = true;
			const res = await getEmails();
			emails = res.rows;
		} catch (e) {
			addToast('Failed to load emails', 'Error');
			console.error('Failed to load emails', e);
		} finally {
			isTableLoading = false;
		}
	};

	const refreshDomains = async () => {
		const domains = await fetchAllRows((options) => {
			return api.domain.getAllSubset(options, contextCompanyID);
		});
		domainMap = BiMap.FromArrayOfObjects(domains);
	};

	const getEmails = async () => {
		try {
			const res = await api.email.getOverviews(tableURLParams, contextCompanyID);
			if (!res.success) {
				throw res.error;
			}
			return res.data;
		} catch (e) {
			addToast('Failed to load emails', 'Error');
			console.error('failed to get emails', e);
		}
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
			const res = await api.email.create({
				name: formValues.name,
				content: formValues.content,
				mailEnvelopeFrom: formValues.mailEnvelopeFrom,
				mailHeaderFrom: formValues.mailHeaderFrom,
				mailHeaderSubject: formValues.mailHeaderSubject,
				addTrackingPixel: formValues.addTrackingPixel,
				companyID: contextCompanyID
			});
			if (!res.success) {
				modalError = res.error;
				return;
			}
			closeModal();
			addToast('Email created', 'Success');
			refreshEmails();
		} catch (err) {
			addToast('Failed to create email', 'Error');
			console.error('failed to create email:', err);
		}
	};

	const onClickUpdate = async () => {
		try {
			const res = await api.email.update({
				id: formValues.id,
				name: formValues.name,
				content: formValues.content,
				mailEnvelopeFrom: formValues.mailEnvelopeFrom,
				mailHeaderFrom: formValues.mailHeaderFrom,
				mailHeaderSubject: formValues.mailHeaderSubject,
				addTrackingPixel: formValues.addTrackingPixel
			});
			if (!res.success) {
				modalError = res.error;
				return;
			}
			modalError = '';
			closeModal();
			addToast('Email updated', 'Success');
			refreshEmails();
		} catch (e) {
			addToast('Failed to update email', 'Error');
			console.error('failed to update email', e);
		}
	};

	/** @param {string} id */
	const getEmail = async (id) => {
		try {
			const res = await api.email.getByID(id);
			if (!res.success) {
				throw res.error;
			}
			return res.data;
		} catch (e) {
			addToast('Failed to load email', 'Error');
			console.error('failed to get email', e);
		}
	};

	/** @param {string} id */
	const gotoAttachments = (id) => {
		goto(`/email/${id}/attachments/`);
	};

	/** @param {string} id */
	const onClickDelete = async (id) => {
		const action = api.email.delete(id);
		action
			.then((res) => {
				if (res.success) {
					refreshEmails();
					return;
				}
				throw res.error;
			})
			.catch((e) => {
				console.error('failed to delete email', e);
			});
		return action;
	};

	const openCreateModal = () => {
		modalMode = 'create';
		isModalVisible = true;
	};

	const closeModal = () => {
		modalError = '';
		formValues = {
			id: null,
			name: null,
			content: null,
			mailEnvelopeFrom: null,
			mailHeaderFrom: null,
			mailHeaderSubject: null,
			addTrackingPixel: false
		};
		form.reset();
		isModalVisible = false;
	};

	/** @param {string} id */
	const openUpdateModal = async (id) => {
		modalMode = 'update';
		try {
			showIsLoading();
			await refreshDomains();
			const page = await getEmail(id);
			const r = globalButtonDisabledAttributes(page, contextCompanyID);
			if (r.disabled) {
				hideIsLoading();
				return;
			}
			isModalVisible = true;
			assignEmail(page);
		} catch (e) {
			addToast('Failed to load email', 'Error');
			console.error('failed to get email', e);
		} finally {
			hideIsLoading();
		}
	};

	const openCopyModal = async (id) => {
		modalMode = 'copy';
		try {
			showIsLoading();
			const email = await getEmail(id);
			isModalVisible = true;
			assignEmail(email);
		} catch (e) {
			addToast('Failed to load email', 'Error');
			console.error('failed to get email', e);
		} finally {
			hideIsLoading();
		}
	};

	const assignEmail = (email) => {
		formValues.id = email.id;
		formValues.name = email.name;
		formValues.content = email.content;
		formValues.mailEnvelopeFrom = email.mailEnvelopeFrom;
		formValues.mailHeaderFrom = email.mailHeaderFrom;
		formValues.mailHeaderSubject = email.mailHeaderSubject;
		formValues.addTrackingPixel = email.addTrackingPixel;
	};

	const openSendTestModal = async (id) => {
		try {
			showIsLoading();
			const smtps = await fetchAllRows(async (options) => {
				return api.smtpConfiguration.getAll(options, contextCompanyID);
			});
			const recipients = await fetchAllRows(
				async (options) => {
					return api.recipient.getAll(options, contextCompanyID);
				},
				{
					...defaultOptions,
					sortBy: 'first_name'
				}
			);
			await refreshDomains();
			smtpMap = BiMap.FromArrayOfObjects(smtps);
			recipientMap = BiMap.FromArrayOfObjects(recipients, 'id', 'email');
			selectedTestEmailID = id;
			isSendTestModalVisible = true;
		} catch (e) {
			addToast('Failed to get data', 'Error');
		} finally {
			hideIsLoading();
		}
	};

	const closeSendTestModal = () => {
		isSendTestModalVisible = false;
		selectedTestSMTPValue = null;
		selectedTestEmailID = null;
	};

	const openDeleteAlert = async (email) => {
		isDeleteAlertVisible = true;
		deleteValues.id = email.id;
		deleteValues.name = email.name;
	};

	const sendPreview = async () => {
		try {
			const smtpID = smtpMap.byValue(selectedTestSMTPValue);
			const recpID = recipientMap.byValue(selectedTestEmailRecipientID);
			const domainID = domainMap.byValue(selectedTestDomainID);

			const res = await api.email.sendTest({
				id: selectedTestEmailID,
				smtpID: smtpID,
				recipientID: recpID,
				domainID: domainID
			});
			if (!res.success) {
				sendTestModalError = res.error;
				return;
			}
			closeSendTestModal();
			addToast('Email sent', 'Success');
			refreshEmails();
		} catch (err) {
			addToast('Failed to sent test email', 'Error');
			console.error('failed to send test email:', err);
		}
	};
</script>

<HeadTitle title="Emails" />
<main>
	<Headline>Emails</Headline>
	<BigButton on:click={openCreateModal}>New Email</BigButton>
	<Table
		columns={[
			{ column: 'Name', size: 'large' },
			{ column: 'From', size: 'medium' },
			{ column: 'Subject', size: 'medium' },
			{ column: 'Tracking Pixel', size: 'small', alignText: 'center' }
		]}
		sortable={['Name', 'From', 'Subject', 'Tracking Pixel']}
		hasData={!!emails.length}
		plural="emails"
		pagination={tableURLParams}
		isGhost={isTableLoading}
	>
		{#each emails as email}
			<TableRow>
				<TableCell>
					<button
						on:click={() => {
							openUpdateModal(email.id);
						}}
						{...globalButtonDisabledAttributes(email, contextCompanyID)}
						title={email.name}
					>
						{email.name}
					</button>
				</TableCell>
				<TableCell value={email.mailHeaderFrom} />
				<TableCell value={email.mailHeaderSubject} />
				<TableCellCheck>
					{#if email.addTrackingPixel}
						<img class="w-6" src="/icon-true.svg" alt="true" />
					{:else}
						<img class="w-6" src="/icon-false.svg" alt="false" />
					{/if}
				</TableCellCheck>
				<TableCellEmpty />
				<TableCellAction>
					<TableDropDownEllipsis>
						<TableDropDownButton name="Attachments" on:click={() => gotoAttachments(email.id)} />
						<TableUpdateButton
							on:click={() => openUpdateModal(email.id)}
							{...globalButtonDisabledAttributes(email, contextCompanyID)}
						/>
						<TableCopyButton title={'Copy'} on:click={() => openCopyModal(email.id)} />
						<TableDeleteButton
							on:click={() => openDeleteAlert(email)}
							{...globalButtonDisabledAttributes(email, contextCompanyID)}
						/>
						<TableViewButton name="Send test" on:click={() => openSendTestModal(email.id)} />
					</TableDropDownEllipsis>
				</TableCellAction>
			</TableRow>
		{/each}
	</Table>
	<Modal headerText={modalText} visible={isModalVisible} onClose={closeModal} {isSubmitting}>
		<FormGrid on:submit={onSubmit} bind:bindTo={form} {isSubmitting}>
			<Editor contentType="email" {domainMap} bind:value={formValues.content}>
				<div class="flex flex-col lg:flex-row w-full pl-4">
					<div class="flex flex-col lg:flex-row justify-between w-1/3">
						<TextField
							minLength={1}
							maxLength={64}
							required
							bind:value={formValues.name}
							placeholder="Verify login information"
						>
							Name
						</TextField>
						<TextField
							minLength={5}
							maxLength={254}
							type="email"
							required
							bind:value={formValues.mailEnvelopeFrom}
							placeholder="alice@example.test"
							toolTipText="Envelope From. ex. 'a@example.test"
						>
							Envelope From
						</TextField>
					</div>
					<div class="flex flex-col lg:flex-row justify-between w-1/3 lg:ml-8">
						<TextField
							minLength={5}
							maxLength={254}
							required
							pattern="([a-zA-Z0-9 ]+<)?[a-zA-Z0-9\._\-]+@[a-zA-Z0-9\._\-]+>?"
							bind:value={formValues.mailHeaderFrom}
							placeholder="Alice <a@example.test>"
							toolTipText="Header From. ex. '<a@example.test>'"
						>
							From
						</TextField>
						<TextField
							required
							minLength={1}
							maxLength={255}
							bind:value={formValues.mailHeaderSubject}
							placeholder="Important: Verification required"
						>
							Subject
						</TextField>
					</div>
					<div class="lg:px-8">
						<CheckboxField
							bind:value={formValues.addTrackingPixel}
							defaultValue={true}
							toolTipText="Adds a tracking pixel to the email">Add tracking pixel</CheckboxField
						>
					</div>
				</div>
			</Editor>
			<FormError message={modalError} />
			<FormFooter {closeModal} {isSubmitting} />
		</FormGrid>
	</Modal>
	<Modal
		headerText={'Send test'}
		visible={isSendTestModalVisible}
		onClose={closeSendTestModal}
		{isSubmitting}
	>
		<FormGrid on:submit={sendPreview} bind:bindTo={sendTestForm} {isSubmitting}>
			<FormColumns>
				<FormColumn>
					<TextFieldSelect
						required
						id="smtp"
						options={smtpMap.values()}
						bind:value={selectedTestSMTPValue}>Sender SMTP</TextFieldSelect
					>
				</FormColumn>
				<FormColumn>
					<TextFieldSelect
						required
						id="domainID"
						options={domainMap.values()}
						bind:value={selectedTestDomainID}>Domain</TextFieldSelect
					>
				</FormColumn>
				<FormColumn>
					<TextFieldSelect
						required
						id="recp"
						options={recipientMap.values()}
						bind:value={selectedTestEmailRecipientID}>Reciever</TextFieldSelect
					>
				</FormColumn>
			</FormColumns>
			<FormError message={sendTestModalError} />
			<FormFooter okText={'Send test e-mail'} closeModal={closeSendTestModal} {isSubmitting} />
		</FormGrid>
	</Modal>
	<DeleteAlert
		name={deleteValues.name}
		onClick={() => onClickDelete(deleteValues.id)}
		bind:isVisible={isDeleteAlertVisible}
	></DeleteAlert>
</main>
