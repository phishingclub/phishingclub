<script>
	import { api } from '$lib/api/apiProxy.js';
	import { onMount } from 'svelte';
	import { UserService } from '$lib/service/user';
	import { addToast } from '$lib/store/toast';
	import HeadTitle from '$lib/components/HeadTitle.svelte';
	import Table from '$lib/components/table/Table.svelte';
	import TableRow from '$lib/components/table/TableRow.svelte';
	import TableCell from '$lib/components/table/TableCell.svelte';
	import TableCellEmpty from '$lib/components/table/TableCellEmpty.svelte';
	import TableCellAction from '$lib/components/table/TableCellAction.svelte';
	import TableDeleteButton from '$lib/components/table/TableDeleteButton2.svelte';
	import TableCellCheck from '$lib/components/table/TableCellCheck.svelte';
	import Headline from '$lib/components/Headline.svelte';
	import { newTableURLParams } from '$lib/service/tableURLParams';
	import TableDropDownEllipsis from '$lib/components/table/TableDropDownEllipsis.svelte';
	import DeleteAlert from '$lib/components/modal/DeleteAlert.svelte';
	import BigButton from '$lib/components/BigButton.svelte';

	// services
	const userService = UserService.instance;

	// local state
	let sessions = [];
	let sessionsHasNextPage = true;
	const tableURLParams = newTableURLParams();
	let isTableLoading = false;

	let isDeleteAlertVisible = false;
	let deleteValues = {
		id: null,
		ip: null,
		current: null
	};
	let isDeleteAllSessionsAlertVisible;

	const refreshSessions = async () => {
		try {
			isTableLoading = true;
			const params = {
				currentPage: tableURLParams.currentPage,
				perPage: tableURLParams.perPage,
				sortBy: tableURLParams.sortBy,
				sortOrder: tableURLParams.sortOrder,
				search: tableURLParams.search
			};
			const res = await api.user.getAllSessions(params);
			if (res.success) {
				sessions = res.data.sessions;
				sessionsHasNextPage = res.data.hasNextPage;
				return;
			}
			throw res.error;
		} catch (e) {
			addToast('Failed to load sessions', 'Error');
			console.error('failed to load sessions', e);
		} finally {
			isTableLoading = false;
		}
	};

	// hooks
	onMount(() => {
		refreshSessions();
		tableURLParams.onChange(refreshSessions);
		return () => {
			tableURLParams.unsubscribe();
		};
	});

	// component logic
	const openDeleteAlert = async (session) => {
		isDeleteAlertVisible = true;
		deleteValues.id = session.id;
		deleteValues.ip = session.ip;
		deleteValues.current = session.current;
	};

	const openDeleteAllSessionsAlert = async () => {
		isDeleteAllSessionsAlertVisible = true;
	};

	const revokeAllSession = async () => {
		const action = api.user.invalidateSessions();
		action
			.then((res) => {
				if (!res.success) {
					throw res.error;
				}
				userService.clear();
			})
			.catch((e) => {
				console.error('failed to revoke session', e);
			});
		return action;
	};

	/**
	 * @param {string} sessionID - The ID of the session to revoke.
	 * @param {boolean} isCurrent - Whether the session to revoke is the current session.
	 */
	const revokeSession = async (sessionID, isCurrent) => {
		const action = api.session.revoke(sessionID);
		action
			.then((res) => {
				if (res.success) {
					if (isCurrent) {
						userService.clear();
						return;
					}
					refreshSessions();
					return;
				}
				if (!res.success) {
					throw res.error;
				}
				userService.clear();
			})
			.catch((e) => {
				console.error('failed to revoke session', e);
			});
		return action;
	};
</script>

<HeadTitle title="Sessions" />
<main>
	<Headline>Sessions</Headline>
	<BigButton on:click={openDeleteAllSessionsAlert}>Delete all sessions</BigButton>
	<Table
		columns={[
			{ column: 'IP address', size: 'small' },
			{ column: 'Current session', size: 'small', alignText: 'center' }
		]}
		sortable={['IP address']}
		hasData={!!sessions.length}
		hasNextPage={sessionsHasNextPage}
		plural="Sessions"
		pagination={tableURLParams}
		isGhost={isTableLoading}
	>
		{#each sessions as session}
			<TableRow>
				<TableCell value={session.ip} />
				<TableCellCheck value={session.current} />
				<TableCellEmpty />
				<TableCellAction>
					<TableDropDownEllipsis>
						<TableDeleteButton on:click={() => openDeleteAlert(session)}></TableDeleteButton>
					</TableDropDownEllipsis>
				</TableCellAction>
			</TableRow>
		{/each}
	</Table>
	<DeleteAlert
		list={[]}
		name={deleteValues.ip}
		onClick={() => revokeSession(deleteValues.id, deleteValues.current)}
		bind:isVisible={isDeleteAlertVisible}
	></DeleteAlert>
	<DeleteAlert
		list={[]}
		name={'all sessions'}
		onClick={() => revokeAllSession()}
		bind:isVisible={isDeleteAllSessionsAlertVisible}
	></DeleteAlert>
</main>
