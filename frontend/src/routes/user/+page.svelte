<script>
	import { api } from '$lib/api/apiProxy.js';
	import { onMount } from 'svelte';
	import { newTableURLParams } from '$lib/service/tableURLParams.js';
	import Headline from '$lib/components/Headline.svelte';
	import TextField from '$lib/components/TextField.svelte';
	import TableCell from '$lib/components/table/TableCell.svelte';
	import TableRow from '$lib/components/table/TableRow.svelte';
	import TableUpdateButton from '$lib/components/table/TableUpdateButton.svelte';
	import TableDeleteButton from '$lib/components/table/TableDeleteButton2.svelte';
	import { addToast } from '$lib/store/toast';
	import FormError from '$lib/components/FormError.svelte';
	import TableCellEmpty from '$lib/components/table/TableCellEmpty.svelte';
	import TableCellAction from '$lib/components/table/TableCellAction.svelte';
	import Modal from '$lib/components/Modal.svelte';
	import FormGrid from '$lib/components/FormGrid.svelte';
	import PasswordField from '$lib/components/PasswordField.svelte';
	import BigButton from '$lib/components/BigButton.svelte';
	import FormColumns from '$lib/components/FormColumns.svelte';
	import FormColumn from '$lib/components/FormColumn.svelte';
	import FormFooter from '$lib/components/FormFooter.svelte';
	import Table from '$lib/components/table/Table.svelte';
	import HeadTitle from '$lib/components/HeadTitle.svelte';
	import { getModalText } from '$lib/utils/common';
	import DeleteAlert from '$lib/components/modal/DeleteAlert.svelte';
	import TableDropDownEllipsis from '$lib/components/table/TableDropDownEllipsis.svelte';
	import TableCellCheck from '$lib/components/table/TableCellCheck.svelte';

	// services

	// data
	let form = null;
	let formValues = {
		id: null,
		username: null,
		email: null,
		fullname: null,
		password: null
	};
	let users = [];
	let modalError = '';
	const tableURLParams = newTableURLParams();
	let isModalVisible = false;
	let isSubmitting = false;
	let isTableLoading = false;
	let modalMode = null;
	let modalText = '';

	let isDeleteAlertVisible = false;
	let deleteValues = {
		id: null,
		name: null
	};

	let isDeleteAllSessionsVisible = false;
	let deleteAllSessionsValues = {
		userID: null,
		username: null
	};

	$: {
		modalText = getModalText('user', modalMode);
	}

	// hooks
	onMount(() => {
		refreshUsers();
		tableURLParams.onChange(refreshUsers);
		return () => {
			tableURLParams.unsubscribe();
		};
	});

	// component logic
	const refreshUsers = async () => {
		try {
			isTableLoading = true;
			const result = await getUsers();
			users = result.rows;
		} catch (e) {
			addToast('Failed to load users', 'Error');
			console.error('Failed to load users', e);
		} finally {
			isTableLoading = false;
		}
	};

	const getUsers = async () => {
		try {
			const res = await api.user.getAll(tableURLParams);
			if (res.success) {
				return res.data;
			}
			throw res.error;
		} catch (e) {
			addToast('Failed to load users', 'Error');
			console.error('failed to get users', e);
		}
		return [];
	};

	/** @param {string} id */
	const refreshUser = async (id) => {
		try {
			const res = await api.user.getByID(id);
			if (!res.success) {
				throw res.error;
			}
			formValues.username = res.data.username;
			formValues.email = res.data.email;
			formValues.fullname = res.data.name;
			return;
		} catch (e) {
			addToast('Failed to load user', 'Error');
			console.error('failed to get user', e);
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

	const create = async () => {
		try {
			const res = await api.user.create({
				fullname: formValues.fullname,
				email: formValues.email,
				username: formValues.username,
				password: formValues.password
			});
			if (res.success) {
				form.reset();
				addToast('User created', 'Success');
				closeModal();
				refreshUsers();
				return;
			}
			modalError = res.error;
		} catch (err) {
			addToast('Failed to create user ', 'Error');
			console.error('failed to create user:', err);
		}
	};

	const update = async () => {
		try {
			const res = await api.user.updateByID({
				id: formValues.id,
				fullname: formValues.fullname,
				email: formValues.email,
				username: formValues.username
			});
			if (!res.success) {
				modalError = res.error;
				return;
			}
			addToast('User updated', 'Success');
			closeModal();
			refreshUsers();
		} catch (err) {
			addToast('Failed to update user ', 'Error');
			console.error('failed to update user:', err);
		}
	};

	const openDeleteAlert = async (user) => {
		isDeleteAlertVisible = true;
		deleteValues.id = user.id;
		deleteValues.username = user.username;
	};

	const openDeleteAllSessionsAlert = async (user) => {
		isDeleteAllSessionsVisible = true;
		deleteAllSessionsValues.userID = user.id;
		deleteAllSessionsValues.username = user.username;
	};

	const revokeAllSessions = async () => {
		const action = api.user.invalidateSessions(deleteAllSessionsValues.userID);
		action
			.then((res) => {
				if (!res.success) {
					throw res.error;
				}
			})
			.catch((e) => {
				console.error('failed to revoke session', e);
			});
		return action;
	};

	/** @param {string} id */
	const onClickDelete = async (id) => {
		const action = api.user.delete(id);
		action
			.then((res) => {
				if (res.success) {
					refreshUsers();
					return;
				}
				throw res.error;
			})
			.catch((e) => {
				console.error('failed to delete user:', e);
			});

		return action;
	};

	const openCreateModal = async () => {
		modalMode = 'create';
		isModalVisible = true;
	};

	const closeModal = () => {
		isModalVisible = false;
		modalError = '';
		form.reset();
	};

	/** @param {string} id */
	const showEditModal = async (id) => {
		modalMode = 'update';
		formValues.id = id;
		await refreshUser(id);
		isModalVisible = true;
	};
</script>

<HeadTitle title="Users" />
<main>
	<Headline>Users</Headline>
	<BigButton on:click={openCreateModal}>New user</BigButton>

	<Table
		columns={[
			{ column: 'Username', size: 'medium' },
			{ column: 'Email', size: 'large' },
			{ column: 'Name', size: 'medium' },
			{ column: 'SSO', size: 'small', alignText: 'center' }
		]}
		sortable={['Username', 'Email', 'Name']}
		hasData={!!users.length}
		plural="users"
		pagination={tableURLParams}
		isGhost={isTableLoading}
	>
		{#each users as user}
			<TableRow>
				<TableCell>
					<button
						on:click={() => {
							showEditModal(user.id);
						}}
						class="block w-full py-1 text-left"
					>
						{user.username}
					</button></TableCell
				>
				<TableCell value={user.email} />
				<TableCell value={user.name} />
				<TableCellCheck value={!!user.ssoID} />
				<TableCellEmpty />
				<TableCellAction>
					<TableDropDownEllipsis>
						<TableUpdateButton on:click={() => showEditModal(user.id)} />
						<TableDeleteButton
							name="Delete all sessions"
							on:click={() => openDeleteAllSessionsAlert(user)}
						/>
						<TableDeleteButton on:click={() => openDeleteAlert(user)} />
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
						required
						minLength={1}
						maxLength={64}
						bind:value={formValues.fullname}
						placeholder="Alice">Name</TextField
					>
					<TextField
						required
						minLength={1}
						maxLength={64}
						type="email"
						bind:value={formValues.email}
						placeholder="alice@example.com">Email</TextField
					>
					<TextField
						minLength={5}
						maxLength={255}
						bind:value={formValues.username}
						placeholder="alice">Username</TextField
					>
					{#if modalMode === 'create'}
						<PasswordField
							required
							minLength={16}
							maxLength={64}
							bind:value={formValues.password}
							placeholder="********">Password</PasswordField
						>
					{/if}
				</FormColumn>
			</FormColumns>
			<FormError message={modalError} />
			<FormFooter {closeModal} {isSubmitting} />
		</FormGrid>
	</Modal>
	<DeleteAlert
		confirm
		name={deleteValues.username}
		onClick={() => onClickDelete(deleteValues.id)}
		bind:isVisible={isDeleteAlertVisible}
	></DeleteAlert>
	<DeleteAlert
		list={[]}
		name={`all sessions for '${deleteAllSessionsValues.username}'`}
		onClick={() => revokeAllSessions()}
		bind:isVisible={isDeleteAllSessionsVisible}
		permanent={false}
	></DeleteAlert>
</main>
