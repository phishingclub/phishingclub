<script>
	import { api } from '$lib/api/apiProxy.js';
	import { newTableURLParams } from '$lib/service/tableURLParams.js';
	import { onMount, tick } from 'svelte';
	import { page } from '$app/stores';
	import { goto } from '$app/navigation';
	import { fetchAllRows } from '$lib/utils/api-utils.js';
	import Headline from '$lib/components/Headline.svelte';
	import HeadTitle from '$lib/components/HeadTitle.svelte';
	import AutoRefresh from '$lib/components/AutoRefresh.svelte';
	import Table from '$lib/components/table/Table.svelte';
	import TableRow from '$lib/components/table/TableRow.svelte';
	import TableCell from '$lib/components/table/TableCell.svelte';
	import TableCellEmpty from '$lib/components/table/TableCellEmpty.svelte';
	import TableCellLink from '$lib/components/table/TableCellLink.svelte';
	import TableViewButton from '$lib/components/table/TableViewButton.svelte';
	import TableCellAction from '$lib/components/table/TableCellAction.svelte';
	import TableDropDownEllipsis from '$lib/components/table/TableDropDownEllipsis.svelte';
	import TableDropDownButton from '$lib/components/table/TableDropDownButton.svelte';
	import TableUpdateButton from '$lib/components/table/TableUpdateButton.svelte';
	import TableDeleteButton from '$lib/components/table/TableDeleteButton2.svelte';
	import BigButton from '$lib/components/BigButton.svelte';
	import DeleteAlert from '$lib/components/modal/DeleteAlert.svelte';
	import Alert from '$lib/components/Alert.svelte';
	import StatusBadge from '$lib/components/StatusBadge.svelte';
	import ProgramCloneModal from '$lib/components/program/ProgramCloneModal.svelte';
	import ProgramWizardModal from '$lib/components/program/ProgramWizardModal.svelte';
	import { addToast } from '$lib/store/toast';
	import { AppStateService } from '$lib/service/appState';
	import { formatDateTime, plural, programStatus } from '$lib/utils/programText.js';
	import { shortRelative } from '$lib/utils/shortRelative.js';

	const appStateService = AppStateService.instance;
	const tableURLParams = newTableURLParams();

	let contextCompanyID = '';
	let programs = [];
	let programsHasNextPage = false;
	let isTableLoading = false;
	// companies are only offered when cloning, a missing list is fine
	let companies = [];
	let wizard;
	// the time the short relative times count from, moved on every minute
	let relativeNow = Date.now();

	let isDeleteAlertVisible = false;
	let deleteValues = { id: null, name: null };

	const openCreateModal = () => wizard.openCreateModal();
	const openUpdateModal = (id) => wizard.openUpdateModal(id);

	onMount(() => {
		const context = appStateService.getContext();
		if (context) {
			contextCompanyID = context.companyID;
		}
		refreshPrograms();
		tableURLParams.onChange(refreshPrograms);
		const relativeTimer = setInterval(() => (relativeNow = Date.now()), 60000);
		(async () => {
			// let the wizard see the company context before it loads
			await tick();
			await wizard.loadDependencies();
			try {
				companies = await fetchAllRows((options) => api.company.getAll(options));
			} catch (e) {
				companies = [];
			}
			const updateID = $page.url.searchParams.get('update');
			if (updateID) {
				const url = new URL($page.url);
				url.searchParams.delete('update');
				goto(url.pathname + url.search, { replaceState: true, noScroll: true, keepFocus: true });
				await openUpdateModal(updateID);
			}
		})();
		return () => {
			clearInterval(relativeTimer);
			tableURLParams.unsubscribe();
		};
	});

	const refreshPrograms = async (useTableLoading = true) => {
		try {
			if (useTableLoading) {
				isTableLoading = true;
			}
			const res = await api.program.getAll(tableURLParams, contextCompanyID);
			if (!res.success) {
				throw res.error;
			}
			programs = res.data?.rows ?? [];
			programsHasNextPage = res.data?.hasNextPage ?? false;
		} catch (e) {
			addToast('Failed to load programs', 'Error');
			console.error('failed to load programs', e);
		} finally {
			if (useTableLoading) {
				isTableLoading = false;
			}
		}
	};

	// list actions

	const pauseResume = async (program) => {
		try {
			const res = program.pausedAt
				? await api.program.resume(program.id)
				: await api.program.pause(program.id);
			if (!res.success) {
				throw res.error;
			}
			addToast(program.pausedAt ? 'Program resumed' : 'Program paused', 'Success');
			refreshPrograms();
		} catch (e) {
			addToast(
				program.pausedAt ? 'Failed to resume the program' : 'Failed to pause the program',
				'Error'
			);
			console.error('failed to change program state', e);
		}
	};

	let isCloseAlertVisible = false;
	let closeValues = { id: null, name: null };

	const openCloseAlert = (program) => {
		closeValues = { id: program.id, name: program.name };
		isCloseAlertVisible = true;
	};

	const onClose = async () => {
		const res = await api.program.close(closeValues.id);
		if (res.success) {
			addToast('Program closed', 'Success');
			isCloseAlertVisible = false;
			refreshPrograms();
		}
		return res;
	};

	const openDeleteAlert = (program) => {
		deleteValues = { id: program.id, name: program.name };
		isDeleteAlertVisible = true;
	};

	const onDelete = async () => {
		const res = await api.program.delete(deleteValues.id);
		if (res.success) {
			addToast('Program deleted', 'Success');
			refreshPrograms();
		}
		return res;
	};

	// start now asks first, with the number of people enrolled
	let isStartAlertVisible = false;
	let startValues = { id: null, name: '', people: null };

	const openStartAlert = async (program) => {
		startValues = { id: program.id, name: program.name, people: null };
		isStartAlertVisible = true;
		try {
			const res = await api.program.getByID(program.id);
			const groups = res.success ? res.data?.recipientGroupIDs || [] : [];
			if (!groups.length) return;
			const count = await api.program.audience(groups, res.data?.companyID || contextCompanyID);
			if (count.success && startValues.id === program.id) {
				startValues = { ...startValues, people: count.data?.people ?? null };
			}
		} catch (e) {
			console.error('failed to count program audience', e);
		}
	};

	const onStart = async () => {
		const res = await api.program.start(startValues.id);
		if (res.success) {
			addToast('Program started', 'Success');
			refreshPrograms();
		}
		return res;
	};

	// clone, the dialog opens while cloneProgram is set
	let cloneProgram = null;
	const openCloneModal = (program) => {
		cloneProgram = program;
	};

	// details for the status tooltip
	const statusTip = (program) => {
		if (program.closedAt) return '';
		if (program.status === 'scheduled') {
			return program.startAt ? `Starts ${formatDateTime(program.startAt)}` : '';
		}
		if (program.needsAttention) {
			return [program.pausedAt ? 'Paused' : '', 'Needs attention', program.attentionReason || '']
				.filter(Boolean)
				.join('. ');
		}
		return '';
	};
</script>

<HeadTitle title="Programs" />
<main>
	<div class="flex flex-wrap items-center justify-between gap-x-6 gap-y-2">
		<Headline docSlug="programs">Programs</Headline>
		<AutoRefresh
			isLoading={false}
			onRefresh={() => {
				refreshPrograms(false);
			}}
		/>
	</div>
	<BigButton on:click={openCreateModal}>New Program</BigButton>
	<Table
		columns={[
			{ column: 'Name', size: 'large' },
			{ column: 'Status', size: 'small' },
			{ column: 'Templates', size: 'small' },
			{ column: 'Active', size: 'small' },
			{ column: 'Next Email', size: 'small' }
		]}
		sortable={['Name']}
		hasData={!!programs.length}
		hasNextPage={programsHasNextPage}
		plural="programs"
		pagination={tableURLParams}
		isGhost={isTableLoading}
	>
		{#each programs as program}
			{@const state = programStatus(program)}
			{@const tip = statusTip(program)}
			<TableRow>
				<TableCellLink href={`/program/${program.id}`} title={program.name} wrap>
					{program.name}
					<!-- the Status column is off screen on phones, so the status shows under the name -->
					<span class="block sm:hidden mt-1"
						><StatusBadge tone={state.tone} title={tip}>{state.label}</StatusBadge></span
					>
				</TableCellLink>
				<TableCell>
					<span><StatusBadge tone={state.tone} title={tip}>{state.label}</StatusBadge></span>
				</TableCell>
				<TableCell value={String(program.simulationCount ?? 0)} />
				<TableCell value={String(program.enrolled ?? 0)} />
				{#if program.status === 'scheduled' && program.startAt && !program.closedAt}
					<TableCell>
						<span title={new Date(program.startAt).toLocaleString()}
							>{shortRelative(program.startAt, relativeNow)}</span
						>
					</TableCell>
				{:else if program.status === 'draft' || program.closedAt || !program.nextSendAt}
					<TableCell />
				{:else if program.pausedAt}
					<TableCell>
						<span title="Held while paused">Held</span>
					</TableCell>
				{:else}
					<TableCell>
						<span title={new Date(program.nextSendAt).toLocaleString()}
							>{shortRelative(program.nextSendAt, relativeNow)}</span
						>
					</TableCell>
				{/if}
				<TableCellEmpty />
				<TableCellAction>
					<TableDropDownEllipsis>
						<TableViewButton on:click={() => goto(`/program/${program.id}`)} />
						{#if !program.closedAt && (program.status === 'draft' || program.status === 'scheduled')}
							<TableDropDownButton name="Start Now" on:click={() => openStartAlert(program)} />
						{/if}
						{#if !program.closedAt}
							<TableUpdateButton on:click={() => openUpdateModal(program.id)} />
						{/if}
						<TableDropDownButton name="Clone" on:click={() => openCloneModal(program)} />
						{#if !program.closedAt && program.status !== 'draft' && program.status !== 'scheduled'}
							<TableDropDownButton
								name={program.pausedAt ? 'Resume' : 'Pause'}
								on:click={() => pauseResume(program)}
							/>
						{/if}
						{#if !program.closedAt}
							<TableDropDownButton name="Close" on:click={() => openCloseAlert(program)} />
						{/if}
						<TableDeleteButton on:click={() => openDeleteAlert(program)} />
					</TableDropDownEllipsis>
				</TableCellAction>
			</TableRow>
		{/each}
	</Table>

	<ProgramWizardModal
		bind:this={wizard}
		{contextCompanyID}
		on:saved={(e) => (e.detail?.created && e.detail.id ? goto(`/program/${e.detail.id}`) : refreshPrograms())}
	/>

	<Alert
		headline="Start program"
		ok="Start"
		tone="affirmative"
		bind:visible={isStartAlertVisible}
		onConfirm={onStart}
	>
		<p class="mt-4 mb-2 text-gray-700 dark:text-gray-200 transition-colors duration-200">
			Start <strong>{startValues.name}</strong>?
			{#if startValues.people != null}
				{plural(startValues.people, 'person is', 'people are')} enrolled and the first emails go out
				from the next sending time.
			{:else}
				The first emails go out from the next sending time.
			{/if}
		</p>
	</Alert>

	<Alert headline="Close program" bind:visible={isCloseAlertVisible} onConfirm={onClose}>
		<div>
			<ul
				class="list-disc ml-8 mt-4 mb-4 text-gray-700 dark:text-gray-200 transition-colors duration-200"
			>
				<li>Stop enrolling and sending</li>
				<li>Links in sent emails and landing pages will stop working</li>
				<li>Results are kept</li>
				<li>A closed program cannot be resumed or edited</li>
			</ul>
		</div>
	</Alert>

	<DeleteAlert
		list={[
			'Deleting removes the program, its emails and all results',
			'Close the program instead to keep the results'
		]}
		name={deleteValues.name}
		onClick={onDelete}
		bind:isVisible={isDeleteAlertVisible}
	></DeleteAlert>

	<ProgramCloneModal
		bind:program={cloneProgram}
		{companies}
		{contextCompanyID}
		on:created={refreshPrograms}
		on:done={(e) => openUpdateModal(e.detail.id)}
	/>
</main>
