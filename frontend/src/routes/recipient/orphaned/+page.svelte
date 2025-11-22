<script>
	import { onMount } from 'svelte';
	import HeadTitle from '$lib/components/HeadTitle.svelte';
	import Headline from '$lib/components/Headline.svelte';
	import BigButton from '$lib/components/BigButton.svelte';
	import Table from '$lib/components/table/Table.svelte';
	import TableRow from '$lib/components/table/TableRow.svelte';
	import TableCell from '$lib/components/table/TableCell.svelte';
	import TableCellLink from '$lib/components/table/TableCellLink.svelte';
	import TableCellCheck from '$lib/components/table/TableCellCheck.svelte';
	import TableCellEmpty from '$lib/components/table/TableCellEmpty.svelte';
	import TableCellAction from '$lib/components/table/TableCellAction.svelte';
	import TableDropDownEllipsis from '$lib/components/table/TableDropDownEllipsis.svelte';
	import TableViewButton from '$lib/components/table/TableViewButton.svelte';
	import TableDeleteButton from '$lib/components/table/TableDeleteButton2.svelte';
	import DeleteAlert from '$lib/components/modal/DeleteAlert.svelte';
	import { AppStateService } from '$lib/service/appState';
	import { addToast } from '$lib/store/toast';
	import { newTableURLParams } from '$lib/service/tableURLParams.js';
	import { api } from '$lib/api/apiProxy.js';
	import { goto } from '$app/navigation';

	// state
	const appStateService_ = AppStateService.instance;

	const tableURLParams = newTableURLParams();
	let contextCompanyID = null;
	let recipients = [];

	let isDeleteAlertVisible = false;
	let deleteValues = {
		title: 'Delete Recipient',
		id: null,
		email: null
	};

	let isRecipientsTableLoading = false;
	let isDeleteAllAlertVisible = false;

	// hooks
	onMount(() => {
		const context = appStateService_.getContext();
		if (context) {
			contextCompanyID = context.companyID;
		}
		refreshRecipients();
		tableURLParams.onChange(refreshRecipients);

		return () => {
			tableURLParams.unsubscribe();
		};
	});

	// component logic
	const refreshRecipients = async () => {
		isRecipientsTableLoading = true;
		try {
			const res = await api.recipient.getOrphaned(tableURLParams, contextCompanyID);
			if (res.success) {
				recipients = res.data.rows;
				return;
			}
			throw res.error;
		} catch (e) {
			addToast('Failed to load orphaned recipients', 'Error');
			console.error('failed to load orphaned recipients', e);
		} finally {
			isRecipientsTableLoading = false;
		}
	};

	const onDeleteAllOrphaned = async () => {
		try {
			const res = await api.recipient.deleteAllOrphaned(contextCompanyID);
			if (res.success) {
				addToast(`Deleted ${res.data.count} orphaned recipients`, 'Success');
				refreshRecipients();
				return res; // Return the success response
			}
			addToast('Failed to delete orphaned recipients', 'Error');
			throw res.error;
		} catch (e) {
			addToast('Failed to delete orphaned recipients', 'Error');
			console.error('failed to delete orphaned recipients:', e);
			throw e; // Re-throw the error for DeleteAlert to handle
		}
	};

	const openDeleteAlert = (recipient) => {
		deleteValues.id = recipient.id;
		deleteValues.email = recipient.email;
		isDeleteAlertVisible = true;
	};

	const onClickDelete = async (id) => {
		const action = api.recipient.delete(id);
		action
			.then((res) => {
				if (res.success) {
					addToast('Recipient deleted', 'Success');
					refreshRecipients();
					return;
				}
				throw res.error;
			})
			.catch((e) => {
				addToast('Failed to delete recipient', 'Error');
				console.error('failed to delete recipient:', e);
			});
		return action;
	};

	const openDeleteAllAlert = () => {
		isDeleteAllAlertVisible = true;
	};
</script>

<HeadTitle title="Orphaned Recipients" />

<section>
	<Headline>Orphaned Recipients</Headline>

	<div class="flex gap-4 mb-4">
		<BigButton on:click={() => goto('/recipient/group/')}>Back to Groups</BigButton>
		{#if recipients.length > 0}
			<button
				on:click={openDeleteAllAlert}
				class="self-start mt-6 bg-gradient-to-b from-red-500 to-red-600 dark:from-red-600 dark:to-red-700 px-4 w-64 py-2 hover:from-red-400 hover:to-red-500 dark:hover:from-red-500 dark:hover:to-red-600 text-white font-bold uppercase rounded-md mb-10 transition-all duration-200"
			>
				Delete All Orphans
			</button>
		{/if}
	</div>

	<Table
		isGhost={isRecipientsTableLoading}
		columns={[
			{ column: 'Email', size: 'small' },
			{ column: 'First name', size: 'small' },
			{ column: 'Last name', size: 'small' },
			{ column: 'Phone', size: 'small' },
			{ column: 'Extra identifier', size: 'small' },
			{ column: 'Position', size: 'small' },
			{ column: 'Repeat offender', size: 'small', alignText: 'center' },
			{ column: 'Department', size: 'small' },
			{ column: 'City', size: 'small' },
			{ column: 'Country', size: 'small' },
			{ column: 'Misc', size: 'small' }
		]}
		sortable={[
			'first name',
			'last name',
			'extra identifier',
			'email',
			'phone',
			'repeat offender',
			'position',
			'department',
			'city',
			'country',
			'misc'
		]}
		hasData={!!recipients.length}
		plural="recipients"
		pagination={tableURLParams}
	>
		{#each recipients as recipient}
			<TableRow>
				<TableCellLink href={`/recipient/${recipient.id}`} title={recipient.email}>
					{#if recipient.email}
						{recipient.email}
					{/if}
				</TableCellLink>
				<TableCell value={recipient.firstName} />
				<TableCell value={recipient.lastName} />
				<TableCell value={recipient.phone} />
				<TableCell value={recipient.extraIdentifier} />
				<TableCell value={recipient.position} />
				<TableCellCheck value={recipient.isRepeatOffender} />
				<TableCell value={recipient.department} />
				<TableCell value={recipient.city} />
				<TableCell value={recipient.country} />
				<TableCell value={recipient.misc} />
				<TableCellEmpty />
				<TableCellAction>
					<TableDropDownEllipsis>
						<TableViewButton
							on:click={() => {
								goto(`/recipient/${recipient.id}`);
							}}
						/>
						<TableDeleteButton on:click={() => openDeleteAlert(recipient)} />
					</TableDropDownEllipsis>
				</TableCellAction>
			</TableRow>
		{/each}
	</Table>

	<DeleteAlert
		bind:isVisible={isDeleteAlertVisible}
		name={deleteValues.email}
		onClick={() => onClickDelete(deleteValues.id)}
		title={deleteValues.title}
	/>

	<DeleteAlert
		bind:isVisible={isDeleteAllAlertVisible}
		name="all orphaned recipients"
		onClick={onDeleteAllOrphaned}
		title="Delete All Orphaned Recipients"
		list={[
			'This will permanently delete all recipients not assigned to any group',
			'This action cannot be undone',
			'All recipient data and statistics will be lost'
		]}
		confirm
	/>
</section>
