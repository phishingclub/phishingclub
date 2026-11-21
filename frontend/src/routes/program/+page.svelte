<script>
	import { api } from '$lib/api/apiProxy.js';
	import { newTableURLParams } from '$lib/service/tableURLParams.js';
	import { onDestroy, onMount, tick } from 'svelte';
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
	import TableCopyButton from '$lib/components/table/TableCopyButton.svelte';
	import RelativeTime from '$lib/components/RelativeTime.svelte';
	import BigButton from '$lib/components/BigButton.svelte';
	import DeleteAlert from '$lib/components/modal/DeleteAlert.svelte';
	import ProgramStartAlert from '$lib/components/program/ProgramStartAlert.svelte';
	import ProgramCloseAlert from '$lib/components/program/ProgramCloseAlert.svelte';
	import ProgramPauseAlert from '$lib/components/program/ProgramPauseAlert.svelte';
	import ProgramResumeAlert from '$lib/components/program/ProgramResumeAlert.svelte';
	import StatusBadge from '$lib/components/StatusBadge.svelte';
	import ProgramCloneModal from '$lib/components/program/ProgramCloneModal.svelte';
	import ProgramWizardModal from '$lib/components/program/ProgramWizardModal.svelte';
	import AllowListingModal from '$lib/components/AllowListingModal.svelte';
	import { addToast } from '$lib/store/toast';
	import { AppStateService } from '$lib/service/appState';
	import TableFilterSelect from '$lib/components/table/TableFilterSelect.svelte';
	import {
		formatDateTime,
		NEXT_HIDDEN_TIP,
		programStatus,
		programStoppingAction,
		PROGRAM_LIST_STATUSES
	} from '$lib/utils/programText.js';
	import { globalButtonDisabledAttributes } from '$lib/utils/form.js';

	const appStateService = AppStateService.instance;
	const tableURLParams = newTableURLParams();

	let contextCompanyID = '';
	let programs = [];
	let programsHasNextPage = false;
	let isTableLoading = false;
	// companies are only offered when copying, loaded when the copy dialog
	// first opens, a missing list is fine
	let companies = [];
	let companiesLoaded = false;
	let wizard;

	// the status filter lives in the url as ?status=, an unknown value lists every program
	const readStatusFilter = () => {
		const value = new URL(window.location.href).searchParams.get('status') || '';
		return PROGRAM_LIST_STATUSES.some((s) => s.value === value) ? value : '';
	};
	let statusFilter = readStatusFilter();

	const onStatusFilterChange = async () => {
		const url = new URL(window.location.href);
		if (statusFilter) {
			url.searchParams.set('status', statusFilter);
		} else {
			url.searchParams.delete('status');
		}
		url.searchParams.set('page', '1');
		await goto(`?${url.searchParams.toString()}`, {
			replaceState: true,
			keepFocus: true,
			noScroll: true
		});
		if (tableURLParams.currentPage !== 1) {
			tableURLParams.currentPage = 1;
		} else {
			refreshPrograms();
		}
	};

	let isDeleteAlertVisible = false;
	let allowListingModal;
	let deleteValues = { id: null, name: null, neverSent: false };

	const openCreateModal = () => wizard.openCreateModal();
	const openUpdateModal = (id) => wizard.openUpdateModal(id);

	// the ?update= link opens the wizard only when the Update button would be
	// enabled: the program is found, not closed or stopping, and belongs to this view
	const openUpdateLink = async (id) => {
		const res = await api.program.getByID(id);
		if (!res.success) {
			addToast('Failed to load the program', 'Error');
			return;
		}
		const program = res.data;
		if (program.closedAt || programStoppingAction(program)) {
			addToast(
				program.closedAt
					? 'A closed program cannot be updated'
					: 'A program being closed or deleted cannot be updated',
				'Error'
			);
			return;
		}
		const check = changeDisabledAndTitle(program);
		if (check.disabled) {
			addToast(check.title || 'This program cannot be updated in this view', 'Error');
			return;
		}
		await openUpdateModal(id);
	};

	onMount(() => {
		const context = appStateService.getContext();
		if (context) {
			contextCompanyID = context.companyID;
		}
		refreshPrograms();
		tableURLParams.onChange(() => {
			statusFilter = readStatusFilter();
			refreshPrograms();
		});
		(async () => {
			const updateID = $page.url.searchParams.get('update');
			if (updateID) {
				// let the wizard see the company context before it loads
				await tick();
				const url = new URL($page.url);
				url.searchParams.delete('update');
				goto(url.pathname + url.search, { replaceState: true, noScroll: true, keepFocus: true });
				await openUpdateLink(updateID);
			}
		})();
		return () => {
			tableURLParams.unsubscribe();
		};
	});

	// only the newest list request may set the rows
	let refreshSeq = 0;
	const refreshPrograms = async (useTableLoading = true) => {
		const seq = ++refreshSeq;
		try {
			if (useTableLoading) {
				isTableLoading = true;
			}
			const res = await api.program.getAll(tableURLParams, contextCompanyID, statusFilter);
			if (seq !== refreshSeq) return;
			if (!res.success) {
				throw res.error;
			}
			programs = res.data?.rows ?? [];
			programsHasNextPage = res.data?.hasNextPage ?? false;
		} catch (e) {
			if (seq === refreshSeq) {
				addToast('Failed to load programs', 'Error');
			}
			console.error('failed to load programs', e);
		} finally {
			if (seq === refreshSeq) {
				isTableLoading = false;
			}
		}
	};

	// list actions

	// pause and resume ask first, as on the campaign page
	let pauseResumeTarget = null;
	let isPauseAlertVisible = false;
	let isResumeAlertVisible = false;
	const openPauseResume = (program) => {
		pauseResumeTarget = program;
		if (program.pausedAt) {
			isResumeAlertVisible = true;
		} else {
			isPauseAlertVisible = true;
		}
	};
	const onPauseResume = () => pauseResume(pauseResumeTarget);

	// a refused pause or resume shows its error in the open alert
	const pauseResume = async (program) => {
		const res = program.pausedAt
			? await api.program.resume(program.id)
			: await api.program.pause(program.id);
		if (res.success) {
			addToast(program.pausedAt ? 'Program is resuming' : 'Program paused', 'Success');
			refreshPrograms();
		}
		return res;
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
			addToast('Program is closing', 'Success');
			isCloseAlertVisible = false;
			refreshPrograms();
		}
		return res;
	};

	// a draft or scheduled program that never started has nothing to close, it
	// is deleted instead
	const neverStarted = (program) =>
		!program.closedAt &&
		!program.startedAt &&
		(program.status === 'draft' || program.status === 'scheduled');

	const openDeleteAlert = (program) => {
		deleteValues = { id: program.id, name: program.name, neverSent: neverStarted(program) };
		isDeleteAlertVisible = true;
	};

	// the server stops the program and removes it in the background, so it
	// shows as Deleting until it is gone
	const onDelete = async () => {
		const res = await api.program.delete(deleteValues.id);
		if (res.success) {
			addToast('Program is being deleted', 'Success');
			refreshPrograms();
		}
		return res;
	};

	// while a listed program is being closed, deleted or resumed in the
	// background the list is read again until that ends
	const deletingRefreshEvery = 5000;
	let deletingTimer = null;
	let isDestroyed = false;
	const watchDeleting = (rows) => {
		if (isDestroyed || !rows.some((p) => p.deletingAt || p.resuming || (p.closingAt && !p.closedAt))) {
			clearTimeout(deletingTimer);
			deletingTimer = null;
			return;
		}
		if (deletingTimer) return;
		deletingTimer = setTimeout(async () => {
			await refreshPrograms(false);
			deletingTimer = null;
			watchDeleting(programs);
		}, deletingRefreshEvery);
	};
	$: watchDeleting(programs);
	onDestroy(() => {
		isDestroyed = true;
		clearTimeout(deletingTimer);
		deletingTimer = null;
	});

	// start now asks first, with the number of recipients enrolled
	let isStartAlertVisible = false;
	let startValues = { id: null, name: '', people: null, program: null };

	const openStartAlert = async (program) => {
		startValues = { id: program.id, name: program.name, people: null, program: null };
		isStartAlertVisible = true;
		try {
			const res = await api.program.getByID(program.id);
			// the full program lets the alert check the end against the schedule
			if (res.success && startValues.id === program.id) {
				startValues = { ...startValues, program: res.data };
			}
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

	// copy, the dialog opens while cloneProgram is set
	let cloneProgram = null;
	const openCloneModal = async (program) => {
		if (!companiesLoaded) {
			try {
				companies = await fetchAllRows((options) => api.company.getAll(options));
				companiesLoaded = true;
			} catch (e) {
				companies = [];
			}
		}
		cloneProgram = program;
	};

	// actions that change a program are only allowed in the view the program belongs to
	const isContextMismatch = (program) => {
		const context = appStateService.getContext();
		if (!program.companyID) {
			return context.current !== AppStateService.CONTEXT.SHARED;
		}
		return (
			context.current !== AppStateService.CONTEXT.COMPANY || context.companyID !== program.companyID
		);
	};
	const changeDisabledAndTitle = (program) => {
		if (isContextMismatch(program)) {
			return {
				disabled: true,
				title: program.companyID
					? 'Switch to company view to perform this action'
					: 'Switch to global view to perform this action'
			};
		}
		return globalButtonDisabledAttributes(program, contextCompanyID);
	};

	// a value hidden for anonymity, as on the program page
	const hiddenCls =
		'text-sm italic text-gray-500 dark:text-gray-400 cursor-help underline decoration-dotted underline-offset-2';

	// details for the status tooltip
	const statusTip = (program) => {
		const state = programStatus(program);
		if (state.tip) return state.tip;
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
			{ column: 'Status', size: 'medium' },
			{ column: 'Templates', size: 'small' },
			{ column: 'Recipients', size: 'small' },
			{ column: 'Next template', size: 'medium' }
		]}
		sortable={['Name']}
		hasData={!!programs.length}
		hasNextPage={programsHasNextPage}
		plural="programs"
		pagination={tableURLParams}
		isGhost={isTableLoading}
	>
		<TableFilterSelect
			slot="toolbar"
			label="Status"
			options={PROGRAM_LIST_STATUSES}
			bind:value={statusFilter}
			on:change={onStatusFilterChange}
		/>
		{#each programs as program}
			{@const state = programStatus(program)}
			{@const tip = statusTip(program)}
			{@const stopping = programStoppingAction(program)}
			<TableRow>
				<TableCellLink href={`/program/${program.id}`} title={program.name} wrap>
					<!-- one line for the name, so a long name can not push the status out
					     of the two lines the cell shows -->
					<span class="block truncate">{program.name}</span>
					<!-- the Status column is off screen on phones, so the status shows under the name -->
					<span class="flex sm:hidden mt-1 min-w-0 items-center gap-2"
						><StatusBadge tone={state.tone} title={tip}>{state.label}</StatusBadge></span
					>
				</TableCellLink>
				<TableCell>
					<span><StatusBadge tone={state.tone} title={tip}>{state.label}</StatusBadge></span>
				</TableCell>
				<TableCell value={String(program.simulationCount ?? 0)} />
				<!-- a closed program counts everyone it had, as its page does -->
				<TableCell
					value={String(
						(program.closedAt ? (program.enrolledTotal ?? program.stopped) : program.enrolled) ?? 0
					)}
				/>
				{#if stopping}
					<TableCell />
				{:else if program.status === 'scheduled' && program.startAt && !program.closedAt}
					<TableCell>
						<RelativeTime value={program.startAt} short />
					</TableCell>
				{:else if program.status === 'draft' || program.closedAt}
					<TableCell />
				{:else if !program.nextSendAt && !program.isAnonymous}
					<TableCell />
				{:else if program.pausedAt}
					<TableCell>
						<span title="Held while paused">Held</span>
					</TableCell>
				{:else if program.isAnonymous}
					<TableCell>
						<span class={hiddenCls} title={NEXT_HIDDEN_TIP}>Hidden</span>
					</TableCell>
				{:else}
					<TableCell>
						<RelativeTime value={program.nextSendAt} short />
					</TableCell>
				{/if}
				<TableCellEmpty />
				<TableCellAction>
					<TableDropDownEllipsis>
						<TableViewButton
							on:click={() => goto(`/program/${program.id}`)}
							{...globalButtonDisabledAttributes(program, contextCompanyID)}
						/>
						{#if stopping}
							<!-- a close or delete that did not finish can only be tried again, and a
							     program being deleted cannot be copied -->
							{#if stopping === 'close'}
								<TableCopyButton
									title="Copy"
									on:click={() => openCloneModal(program)}
									{...globalButtonDisabledAttributes(program, contextCompanyID)}
								/>
								<TableDropDownButton
									name="Retry Close"
									on:click={() => openCloseAlert(program)}
									{...changeDisabledAndTitle(program)}
								/>
							{:else}
								<TableDeleteButton
									name="Retry Delete"
									on:click={() => openDeleteAlert(program)}
									{...globalButtonDisabledAttributes(program, contextCompanyID)}
								/>
							{/if}
						{:else}
							{#if !program.closedAt && (program.status === 'draft' || program.status === 'scheduled')}
								<TableDropDownButton
									name="Start Now"
									on:click={() => openStartAlert(program)}
									{...changeDisabledAndTitle(program)}
								/>
							{/if}
							{#if !program.closedAt}
								<TableUpdateButton
									on:click={() => openUpdateModal(program.id)}
									{...changeDisabledAndTitle(program)}
								/>
							{/if}
							<TableCopyButton
								title="Copy"
								on:click={() => openCloneModal(program)}
								{...globalButtonDisabledAttributes(program, contextCompanyID)}
							/>
							<TableDropDownButton
								name="Allow listing"
								on:click={() => allowListingModal.open(program.id, program.name)}
								{...globalButtonDisabledAttributes(program, contextCompanyID)}
							/>
							<!-- pause only an active program, resume only a paused one that is not
							     being resumed -->
							{#if !program.closedAt && !program.resuming && (program.status === 'active' || program.status === 'paused')}
								<TableDropDownButton
									name={program.pausedAt ? 'Resume' : 'Pause'}
									on:click={() => openPauseResume(program)}
									{...changeDisabledAndTitle(program)}
								/>
							{/if}
							{#if !program.closedAt && !neverStarted(program)}
								<TableDropDownButton
									name="Close"
									on:click={() => openCloseAlert(program)}
									{...changeDisabledAndTitle(program)}
								/>
							{/if}
							<TableDeleteButton
								on:click={() => openDeleteAlert(program)}
								{...globalButtonDisabledAttributes(program, contextCompanyID)}
							/>
						{/if}
					</TableDropDownEllipsis>
				</TableCellAction>
			</TableRow>
		{/each}
	</Table>

	<ProgramWizardModal
		bind:this={wizard}
		{contextCompanyID}
		on:saved={(e) =>
			e.detail?.created && e.detail.id ? goto(`/program/${e.detail.id}`) : refreshPrograms()}
	/>

	<ProgramStartAlert
		bind:visible={isStartAlertVisible}
		name={startValues.name}
		people={startValues.people}
		program={startValues.program}
		onConfirm={onStart}
		onEndMoved={() => refreshPrograms(false)}
	/>

	<ProgramCloseAlert
		bind:visible={isCloseAlertVisible}
		name={closeValues.name ?? ''}
		onConfirm={onClose}
	/>
	<ProgramPauseAlert
		bind:visible={isPauseAlertVisible}
		name={pauseResumeTarget?.name ?? ''}
		hasEndDate={!!pauseResumeTarget?.endAt}
		onConfirm={onPauseResume}
	/>
	<ProgramResumeAlert
		bind:visible={isResumeAlertVisible}
		name={pauseResumeTarget?.name ?? ''}
		onConfirm={onPauseResume}
	/>

	<DeleteAlert
		list={deleteValues.neverSent
			? ['Deleting removes the program']
			: [
					'Deleting removes the program, its sends and all results',
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
	<AllowListingModal bind:this={allowListingModal} kind="program" />
</main>
