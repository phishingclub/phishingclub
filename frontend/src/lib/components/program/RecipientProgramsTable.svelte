<script>
	// The programs of a recipient as one table, one row per program, paged and
	// sorted by the server. The events and the schedule of this recipient in a
	// program open in a modal. An anonymous program shows neither.
	import { onMount } from 'svelte';
	import { goto } from '$app/navigation';
	import { api } from '$lib/api/apiProxy.js';
	import { addToast } from '$lib/store/toast';
	import { newTableURLParams } from '$lib/service/tableURLParams.js';
	import SubHeadline from '$lib/components/SubHeadline.svelte';
	import Table from '$lib/components/table/Table.svelte';
	import TableRow from '$lib/components/table/TableRow.svelte';
	import TableCell from '$lib/components/table/TableCell.svelte';
	import TableCellLink from '$lib/components/table/TableCellLink.svelte';
	import TableCellEmpty from '$lib/components/table/TableCellEmpty.svelte';
	import TableCellAction from '$lib/components/table/TableCellAction.svelte';
	import TableDropDownEllipsis from '$lib/components/table/TableDropDownEllipsis.svelte';
	import TableDropDownButton from '$lib/components/table/TableDropDownButton.svelte';
	import StatusBadge from '$lib/components/StatusBadge.svelte';
	import RelativeTime from '$lib/components/RelativeTime.svelte';
	import ProgramRecipientEventsModal from '$lib/components/program/ProgramRecipientEventsModal.svelte';
	import ProgramScheduleModal from '$lib/components/program/ProgramScheduleModal.svelte';
	import { programStatus } from '$lib/utils/programText.js';

	export let recipientID;
	// set when the recipient is in any program, the section shows only then
	export let hasPrograms = false;

	// the state of this recipient in the program, shown in the status tooltip
	const enrollmentText = {
		completed: 'Completed all templates',
		exited: 'Removed from the program groups',
		stopped: 'Still active when the program closed'
	};
	// the program status of the recipient endpoint as programStatus() input
	const stateOf = (p) =>
		programStatus({
			status: p.programStatus === 'paused' ? 'active' : p.programStatus,
			pausedAt: p.programStatus === 'paused' ? 'paused' : null,
			closingAt: p.programClosingAt,
			deletingAt: p.programDeletingAt
		});
	const stateTip = (p) => {
		// an anonymous program counts people who completed as stopped too, so it
		// only says the program closed
		const own =
			p.anonymous && p.enrollmentStatus === 'stopped'
				? 'The program closed'
				: enrollmentText[p.enrollmentStatus];
		return own ? `This recipient: ${own.toLowerCase()}` : stateOf(p).tip || '';
	};
	const hiddenCls =
		'text-sm italic text-gray-500 dark:text-gray-400 cursor-help underline decoration-dotted underline-offset-2';
	const hiddenTip = 'Not shown per recipient for an anonymous program';

	// the table is paged and sorted by the server, the state is kept in the url
	const params = newTableURLParams({
		sortBy: 'name',
		sortOrder: 'asc',
		prefix: 'program',
		noScroll: true
	});
	let programRows = [];
	let programsHasNextPage = false;
	let isLoading = false;
	// only the newest request may set the rows
	let loadSeq = 0;
	export const refresh = async () => {
		const seq = ++loadSeq;
		isLoading = true;
		try {
			const res = await api.recipient.getPrograms(recipientID, params);
			if (seq !== loadSeq) return;
			if (!res.success) {
				throw res.error;
			}
			programRows = res.data?.rows ?? [];
			programsHasNextPage = !!res.data?.hasNextPage;
			hasPrograms = programRows.length > 0 || params.currentPage > 1;
		} catch (e) {
			if (seq === loadSeq) {
				addToast('Failed to load the programs of the recipient', 'Error');
			}
			console.error('failed to load recipient programs', e);
		} finally {
			if (seq === loadSeq) {
				isLoading = false;
			}
		}
	};

	// the next due time, shown while the program still sends to this recipient
	const nextDueOf = (p) =>
		!p.anonymous &&
		p.nextDueAt &&
		p.programStatus === 'active' &&
		!enrollmentText[p.enrollmentStatus]
			? p.nextDueAt
			: null;

	// the events and schedule of this recipient in a program
	let eventsModal;
	let scheduleModal;

	onMount(() => {
		refresh();
		params.onChange(refresh);
		return () => {
			params.unsubscribe();
		};
	});
</script>

{#if hasPrograms}
	<SubHeadline>Programs</SubHeadline>
{/if}
<!-- kept mounted while empty so a refresh can show the section -->
<div class:hidden={!hasPrograms}>
	<Table
		columns={[
			{ column: 'Name', size: 'large' },
			{ column: 'Status', size: 'medium', hideBelow: 'sm' },
			{ column: 'Sent', size: 'small', compact: true, hideBelow: 'md' },
			{ column: 'Clicked', size: 'small', compact: true, hideBelow: 'md' },
			{ column: 'Reported', size: 'small', compact: true, hideBelow: 'md' },
			{ column: 'Next template', size: 'medium', hideBelow: 'md' }
		]}
		sortable={['Name']}
		pagination={params}
		noSearch
		plural="programs"
		hasData={programRows.length > 0}
		hasNextPage={programsHasNextPage}
		isGhost={isLoading}
	>
		{#each programRows as p (p.programID)}
			{@const state = stateOf(p)}
			<TableRow>
				<TableCellLink href="/program/{p.programID}" title={p.name} wrap
					><span class="block truncate">{p.name}</span>
					<!-- the Status column is off screen on phones -->
					<span class="flex sm:hidden mt-1 min-w-0 items-center gap-2"
						><StatusBadge tone={state.tone} title={stateTip(p)}>{state.label}</StatusBadge></span
					></TableCellLink
				>
				<TableCell hideBelow="sm">
					<span><StatusBadge tone={state.tone} title={stateTip(p)}>{state.label}</StatusBadge></span
					>
				</TableCell>
				{#if p.anonymous || !p.totals}
					{#each [0, 1, 2] as _}
						<TableCell hideBelow="md">
							<span class={hiddenCls} title={hiddenTip}>Hidden</span>
						</TableCell>
					{/each}
				{:else}
					<TableCell hideBelow="md" value={String(p.totals.sent ?? 0)} />
					<TableCell hideBelow="md" value={String(p.totals.clicked ?? 0)} />
					<TableCell hideBelow="md" value={String(p.totals.reported ?? 0)} />
				{/if}
				<TableCell hideBelow="md">
					{#if p.anonymous}
						<span class={hiddenCls} title={hiddenTip}>Hidden</span>
					{:else if nextDueOf(p)}
						<RelativeTime value={p.nextDueAt} short />
					{/if}
				</TableCell>
				<TableCellEmpty />
				<TableCellAction>
					<TableDropDownEllipsis>
						<TableDropDownButton
							name="Events"
							disabled={p.anonymous}
							title={p.anonymous ? hiddenTip : ''}
							on:click={() => !p.anonymous && eventsModal.open(p.programID, recipientID, p.name)}
						/>
						<TableDropDownButton
							name="Schedule"
							disabled={p.anonymous}
							title={p.anonymous ? hiddenTip : ''}
							on:click={() =>
								!p.anonymous && scheduleModal.open(p.programID, recipientID, p.name, nextDueOf(p))}
						/>
						<TableDropDownButton
							name="Go to program"
							on:click={() => goto(`/program/${p.programID}`)}
						/>
					</TableDropDownEllipsis>
				</TableCellAction>
			</TableRow>
		{/each}
	</Table>
</div>

<ProgramRecipientEventsModal bind:this={eventsModal} />
<ProgramScheduleModal bind:this={scheduleModal} />
