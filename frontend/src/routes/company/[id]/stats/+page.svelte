<script>
	import { page } from '$app/stores';
	import { goto } from '$app/navigation';
	import { onMount } from 'svelte';
	import { api } from '$lib/api/apiProxy.js';
	import Headline from '$lib/components/Headline.svelte';
	import TableRow from '$lib/components/table/TableRow.svelte';
	import TableCell from '$lib/components/table/TableCell.svelte';
	import TableUpdateButton from '$lib/components/table/TableUpdateButton.svelte';
	import TableDeleteButton from '$lib/components/table/TableDeleteButton2.svelte';
	import FormError from '$lib/components/FormError.svelte';
	import { addToast } from '$lib/store/toast';
	import TableCellAction from '$lib/components/table/TableCellAction.svelte';
	import Modal from '$lib/components/Modal.svelte';
	import BigButton from '$lib/components/BigButton.svelte';
	import FormFooter from '$lib/components/FormFooter.svelte';
	import Table from '$lib/components/table/Table.svelte';
	import HeadTitle from '$lib/components/HeadTitle.svelte';
	import { showIsLoading, hideIsLoading } from '$lib/store/loading.js';
	import TableDropDownEllipsis from '$lib/components/table/TableDropDownEllipsis.svelte';
	import DeleteAlert from '$lib/components/modal/DeleteAlert.svelte';
	import TableCellEmpty from '$lib/components/table/TableCellEmpty.svelte';
	import TextField from '$lib/components/TextField.svelte';
	import FormGrid from '$lib/components/FormGrid.svelte';
	import FormColumns from '$lib/components/FormColumns.svelte';
	import FormColumn from '$lib/components/FormColumn.svelte';

	function getStatPercentages(stats) {
		const totalRecipients = stats.totalRecipients || 0;
		const emailsSent = stats.emailsSent || 0;
		const read = stats.trackingPixelLoaded || 0;
		const clicked = stats.websiteVisits || 0;
		const reported = stats.reported || 0;

		function pct(n, d) {
			return d > 0 ? Math.round((n / d) * 100) : 0;
		}

		return {
			sent: {
				count: emailsSent,
				absolute: pct(emailsSent, totalRecipients),
				relative: pct(emailsSent, totalRecipients)
			},
			read: {
				count: read,
				absolute: pct(read, totalRecipients),
				relative: pct(read, emailsSent)
			},
			clicked: {
				count: clicked,
				absolute: pct(clicked, totalRecipients),
				relative: pct(clicked, read)
			},
			reported: {
				count: reported,
				absolute: pct(reported, totalRecipients),
				relative: pct(reported, emailsSent)
			}
		};
	}

	// Get company ID from URL params
	$: companyId = $page.params.id;

	// bindings
	let form = null;
	const formValues = {
		id: null,
		campaignName: '',
		totalRecipients: '',
		emailsSent: '',
		trackingPixelLoaded: '',
		websiteVisits: '',
		dataSubmissions: '',
		reported: '',
		date: ''
	};

	// data
	let modalError = '';
	let customStats = [];
	let company = {
		name: ''
	};

	let isModalVisible = false;
	let isSubmitting = false;
	let isTableLoading = true;
	let modalMode = null;
	let modalText = '';

	let isDeleteAlertVisible = false;
	let deleteValues = {
		id: null,
		name: null
	};

	$: {
		modalText = modalMode === 'create' ? 'New Custom Stats' : 'Update Custom Stats';
	}

	// hooks
	onMount(() => {
		refreshData();
	});

	// component logic
	const refreshData = async () => {
		await Promise.all([getCompany(), refreshCustomStats()]);
	};

	const getCompany = async () => {
		try {
			const res = await api.company.getByID(companyId);
			if (res.success) {
				company = res.data;
			} else {
				throw res.error;
			}
		} catch (e) {
			addToast('Failed to get company', 'Error');
			console.error('failed to get company', e);
		}
	};

	const refreshCustomStats = async () => {
		try {
			isTableLoading = true;
			customStats = await getCustomStats();
		} catch (e) {
			addToast('Failed to get custom stats', 'Error');
			console.error('failed to get custom stats', e);
		} finally {
			isTableLoading = false;
		}
	};

	const getCustomStats = async () => {
		try {
			const res = await api.campaign.getManualCampaignStats(companyId);
			if (res.success) {
				return res.data.rows;
			}
			throw new res.error();
		} catch (e) {
			addToast('Failed to get custom stats', 'Error');
			console.error('failed to get custom stats', e);
		}
		return [];
	};

	const getStatsById = async (id) => {
		try {
			// We'll need to implement this endpoint or get it from the list
			const stat = customStats.find((s) => s.id === id);
			return stat;
		} catch (e) {
			addToast('Failed to get stats', 'Error');
			console.error('failed to get stats', e);
		}
	};

	const onSubmit = async () => {
		try {
			isSubmitting = true;
			if (modalMode === 'create') {
				await create();
			} else {
				await update();
			}
		} finally {
			isSubmitting = false;
		}
	};

	const create = async () => {
		modalError = '';
		try {
			const campaignDate = formValues.date ? new Date(formValues.date).toISOString() : null;

			const payload = {
				campaignName: formValues.campaignName,
				totalRecipients: parseInt(formValues.totalRecipients) || 0,
				emailsSent: parseInt(formValues.emailsSent) || 0,
				trackingPixelLoaded: parseInt(formValues.trackingPixelLoaded) || 0,
				websiteVisits: parseInt(formValues.websiteVisits) || 0,
				dataSubmissions: parseInt(formValues.dataSubmissions) || 0,
				reported: parseInt(formValues.reported) || 0,
				campaignType: 'Scheduled',
				templateName: '',
				companyId: companyId,
				campaignStartDate: campaignDate,
				campaignEndDate: campaignDate,
				campaignClosedAt: campaignDate
			};

			const res = await api.campaign.createStats(payload);
			if (!res.success) {
				modalError = res.error;
				return;
			}
			addToast('Custom stats created', 'Success');
			closeModal();
		} catch (e) {
			addToast('Failed to create custom stats', 'Error');
			console.error('failed to create custom stats:', e);
		}
		refreshCustomStats();
	};

	const update = async () => {
		modalError = '';
		try {
			const campaignDate = formValues.date ? new Date(formValues.date).toISOString() : null;

			const payload = {
				campaignName: formValues.campaignName,
				totalRecipients: parseInt(formValues.totalRecipients) || 0,
				emailsSent: parseInt(formValues.emailsSent) || 0,
				trackingPixelLoaded: parseInt(formValues.trackingPixelLoaded) || 0,
				websiteVisits: parseInt(formValues.websiteVisits) || 0,
				dataSubmissions: parseInt(formValues.dataSubmissions) || 0,
				reported: parseInt(formValues.reported) || 0,
				campaignType: 'Scheduled',
				templateName: '',
				companyId: companyId,
				campaignStartDate: campaignDate,
				campaignEndDate: campaignDate,
				campaignClosedAt: campaignDate
			};

			const res = await api.campaign.updateStats(formValues.id, payload);
			if (!res.success) {
				modalError = res.error;
				return;
			}
			addToast('Custom stats updated', 'Success');
			closeUpdateModal();
		} catch (e) {
			addToast('Failed to update custom stats', 'Error');
			console.error('failed to update custom stats', e);
		}
		refreshCustomStats();
	};

	const openDeleteAlert = async (stats) => {
		isDeleteAlertVisible = true;
		deleteValues.id = stats.id;
		deleteValues.name = stats.campaignName;
	};

	const onClickDelete = async (id) => {
		const action = api.campaign.deleteStats(id);
		action
			.then((res) => {
				if (!res.success) {
					throw res.error;
				}
				refreshCustomStats();
			})
			.catch((e) => {
				console.error('failed to delete custom stats:', e);
			});
		return action;
	};

	const openCreateModal = () => {
		modalMode = 'create';
		modalError = '';
		resetFormValues();
		isModalVisible = true;
	};

	const closeModal = () => {
		modalError = '';
		isModalVisible = false;
		resetFormValues();
		if (form) form.reset();
	};

	const openUpdateModal = async (id) => {
		modalMode = 'update';
		try {
			showIsLoading();
			const stats = await getStatsById(id);
			if (stats) {
				formValues.id = stats.id;
				formValues.campaignName = stats.campaignName;
				formValues.totalRecipients = stats.totalRecipients.toString();
				formValues.emailsSent = stats.emailsSent.toString();
				formValues.trackingPixelLoaded = stats.trackingPixelLoaded.toString();
				formValues.websiteVisits = stats.websiteVisits.toString();
				formValues.dataSubmissions = stats.dataSubmissions.toString();
				formValues.reported = stats.reported.toString();
				formValues.date = stats.campaignStartDate
					? new Date(stats.campaignStartDate).toISOString().slice(0, 16)
					: '';
				isModalVisible = true;
			}
		} catch (e) {
			addToast('Failed to get stats', 'Error');
			console.error('failed to get stats', e);
		} finally {
			hideIsLoading();
		}
	};

	const closeUpdateModal = () => {
		isModalVisible = false;
		modalError = '';
		resetFormValues();
		if (form) form.reset();
	};

	const resetFormValues = () => {
		formValues.id = null;
		formValues.campaignName = '';
		formValues.totalRecipients = '';
		formValues.emailsSent = '';
		formValues.trackingPixelLoaded = '';
		formValues.websiteVisits = '';
		formValues.dataSubmissions = '';
		formValues.reported = '';
		formValues.date = '';
	};
</script>

<HeadTitle title="Custom Campaign Stats" />
<main>
	<Headline>
		Custom Stats - {company.name}
	</Headline>

	<BigButton on:click={openCreateModal}>Add</BigButton>

	<script>
		function getStatPercentages(stats) {
			const totalRecipients = stats.totalRecipients || 0;
			const emailsSent = stats.emailsSent || 0;
			const read = stats.trackingPixelLoaded || 0;
			const clicked = stats.websiteVisits || 0;
			const reported = stats.reported || 0;

			function pct(n, d) {
				return d > 0 ? Math.round((n / d) * 100) : 0;
			}

			return {
				sent: {
					count: emailsSent,
					absolute: pct(emailsSent, totalRecipients),
					relative: pct(emailsSent, totalRecipients)
				},
				read: {
					count: read,
					absolute: pct(read, totalRecipients),
					relative: pct(read, emailsSent)
				},
				clicked: {
					count: clicked,
					absolute: pct(clicked, totalRecipients),
					relative: pct(clicked, read)
				},
				reported: {
					count: reported,
					absolute: pct(reported, totalRecipients),
					relative: pct(reported, emailsSent)
				}
			};
		}
	</script>

	<Table
		columns={[
			{ column: 'Campaign Name', size: 'large' },
			{ column: 'Recipients', size: 'small', alignText: 'center' },
			{ column: 'Sent', size: 'small', alignText: 'center' },
			{ column: 'Read', size: 'small', alignText: 'center' },
			{ column: 'Clicked', size: 'small', alignText: 'center' },
			{ column: 'Reported', size: 'small', alignText: 'center' },
			{ column: 'Time ago', size: 'small', alignText: 'center' }
		]}
		sortable={[]}
		hasData={!!customStats.length}
		plural="custom statistics"
		isGhost={isTableLoading}
	>
		{#each customStats as stats}
			{@const pct = getStatPercentages(stats)}
			<TableRow>
				<TableCell>
					<button
						on:click={() => openUpdateModal(stats.id)}
						class="block w-full py-1 text-left font-medium text-gray-900 dark:text-gray-100"
					>
						{stats.campaignName}
					</button>
				</TableCell>
				<TableCell alignText="center" value={stats.totalRecipients} />
				<TableCell alignText="center" value={pct.sent.count} />
				<TableCell alignText="center" value={`${pct.read.count} (${pct.read.absolute}%)`} />
				<TableCell
					alignText="center"
					value={`${pct.clicked.count} (${pct.clicked.absolute}%, rel: ${pct.clicked.relative}%)`}
				/>
				<TableCell
					alignText="center"
					value={`${pct.reported.count} (${pct.reported.absolute}%, rel: ${pct.reported.relative}%)`}
				/>
				<TableCell alignText="center" value={stats.createdAt} isDate isRelative />
				<TableCellEmpty />
				<TableCellAction>
					<TableDropDownEllipsis>
						<TableUpdateButton on:click={() => openUpdateModal(stats.id)} />
						<TableDeleteButton on:click={() => openDeleteAlert(stats)} />
					</TableDropDownEllipsis>
				</TableCellAction>
			</TableRow>
		{/each}
	</Table>

	<Modal headerText={modalText} visible={isModalVisible} onClose={closeModal} {isSubmitting}>
		<FormGrid on:submit={onSubmit} bind:bindTo={form} {isSubmitting} {modalMode}>
			<FormColumns>
				<FormColumn>
					<TextField
						minLength={1}
						maxLength={64}
						required
						bind:value={formValues.campaignName}
						placeholder="Campaign Name">Campaign Name</TextField
					>
					<TextField
						type="number"
						min="0"
						required
						bind:value={formValues.totalRecipients}
						placeholder="0">Total Recipients</TextField
					>

					<TextField type="number" min="0" bind:value={formValues.emailsSent} placeholder="0"
						>Emails Sent</TextField
					>
					<TextField
						type="number"
						min="0"
						bind:value={formValues.trackingPixelLoaded}
						placeholder="0">Email Opens (Read)</TextField
					>
				</FormColumn>
				<FormColumn>
					<TextField type="number" min="0" bind:value={formValues.websiteVisits} placeholder="0"
						>Links Clicked</TextField
					>
					<TextField type="number" min="0" bind:value={formValues.dataSubmissions} placeholder="0"
						>Data Submissions</TextField
					>

					<TextField type="number" min="0" bind:value={formValues.reported} placeholder="0"
						>Reported as Phishing</TextField
					>
					<TextField type="datetime-local" required bind:value={formValues.date}>Date</TextField>
				</FormColumn>
			</FormColumns>

			<FormError message={modalError} />
			<FormFooter {closeModal} {isSubmitting} />
		</FormGrid>
	</Modal>

	<DeleteAlert
		list={['All statistics data will be permanently removed']}
		name={deleteValues.name}
		onClick={() => onClickDelete(deleteValues.id)}
		bind:isVisible={isDeleteAlertVisible}
	/>
</main>
