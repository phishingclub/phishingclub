<script>
	// The plan of one recipient in a program: the round they are in, a round
	// being one whole pass through the templates, and one row per template of
	// it. A template not sent yet shows the window its interval may send in,
	// never a time, as the scheduler draws the time when the interval starts.
	// Opened with open(programID, recipientID, title).
	import { onMount } from 'svelte';
	import { api } from '$lib/api/apiProxy.js';
	import { newTableParams } from '$lib/service/tableParams.js';
	import { addToast } from '$lib/store/toast';
	import Modal from '$lib/components/Modal.svelte';
	import Table from '$lib/components/table/Table.svelte';
	import TableRow from '$lib/components/table/TableRow.svelte';
	import TableCell from '$lib/components/table/TableCell.svelte';
	import EventName from '$lib/components/table/EventName.svelte';
	import StatusBadge from '$lib/components/StatusBadge.svelte';
	import RelativeTime from '$lib/components/RelativeTime.svelte';
	import { dayDate } from '$lib/utils/programSchedule.js';
	import { formatDateTime, formatWeekDay, plural } from '$lib/utils/programText.js';
	import { shortRelative } from '$lib/utils/shortRelative.js';

	/** @type {{ title: string, nextDueAt: string | null } | null} */
	let selected = null;
	/** @type {any} */
	let plan = null;
	let isLoading = false;
	let seq = 0;

	// the state of a recipient who gets no more templates
	const endedText = {
		completed: 'Completed all templates',
		exited: 'Removed from the program groups',
		stopped: 'Still active when the program closed'
	};
	const statusText = { sent: 'Sent', scheduled: 'Scheduled', notSent: 'Not sent yet' };
	const muted = 'text-sm text-gray-400 dark:text-gray-500';
	// how many coming windows the random order line names
	const windowsShown = 4;

	const windowText = (w) => {
		if (!w) return '';
		if (w.afterEnd) return 'After the program ends';
		return w.firstDay === w.lastDay
			? formatWeekDay(dayDate(w.firstDay))
			: `${formatWeekDay(dayDate(w.firstDay))} to ${formatWeekDay(dayDate(w.lastDay))}`;
	};

	// the rows are paged in the modal
	const params = newTableParams({ sortBy: '', sortOrder: 'asc' });
	let pageTick = 0;
	params.onChange(() => pageTick++);
	$: rows = plan?.rows ?? [];
	$: pageRows = ((_tick) => {
		const start = (params.currentPage - 1) * params.perPage;
		return rows.slice(start, start + params.perPage);
	})(pageTick);
	$: hasNextPage = ((_tick) => rows.length > params.currentPage * params.perPage)(pageTick);
	$: ended = plan ? endedText[plan.enrollmentStatus] : '';
	// once the round is all sent, the next template is due in the next round
	$: nextDueAt =
		plan && !ended && plan.sent >= plan.templateCount ? (selected?.nextDueAt ?? null) : null;
	$: nextWindows = (plan?.nextWindows ?? []).filter((w) => !w.afterEnd);

	/**
	 * @param {string} programID
	 * @param {string} recipientID
	 * @param {string} title
	 * @param {string | null} [nextDueAt] when the next template is due, if one is to come
	 */
	export const open = async (programID, recipientID, title, nextDueAt = null) => {
		selected = { title, nextDueAt };
		plan = null;
		params.reset();
		const mine = ++seq;
		isLoading = true;
		try {
			const res = await api.program.recipientSchedule(programID, recipientID);
			if (mine !== seq) return;
			if (!res.success) {
				throw res.error;
			}
			plan = res.data;
		} catch (e) {
			if (mine === seq) {
				addToast('Failed to load the schedule', 'Error');
			}
			console.error('failed to load program recipient schedule', e);
		} finally {
			if (mine === seq) {
				isLoading = false;
			}
		}
	};
	const close = () => {
		selected = null;
		seq++;
	};

	onMount(() => () => params.unsubscribe());
</script>

<Modal
	headerText={selected ? `Schedule: ${selected.title}` : 'Schedule'}
	visible={!!selected}
	onClose={close}
	width="xl"
>
	<div class="py-6 w-full" id="program-recipient-schedule">
		{#if plan}
			<p class="mb-1 text-sm text-gray-700 dark:text-gray-200">
				Round {plan.round}: {plan.sent} of {plural(plan.templateCount, 'template')} sent this round.
			</p>
			{#if nextDueAt}
				<p
					class="mb-1 text-sm text-gray-700 dark:text-gray-200"
					id="program-schedule-next"
					title={formatDateTime(nextDueAt)}
				>
					Next template: {shortRelative(nextDueAt, Date.now())}
				</p>
			{/if}
			<p class="mb-3 text-sm text-gray-600 dark:text-gray-300">
				{#if ended}
					{ended}.
				{:else}
					After the last template: {plan.loopMode === 'complete' ? 'Stop' : 'Start over'}.
				{/if}
			</p>
		{/if}
		<Table
			columns={[
				{ column: 'Template', size: 'medium' },
				{ column: 'Status', size: 'small', compact: true, hideBelow: 'sm' },
				{ column: 'When', size: 'medium' },
				{ column: 'Outcome', size: 'medium', compact: true }
			]}
			pagination={params}
			noSearch
			hasActions={false}
			plural="templates"
			hasData={rows.length > 0}
			{hasNextPage}
			isGhost={isLoading}
		>
			{#each pageRows as r, i (`${r.status}-${r.templateName}-${i}`)}
				<TableRow>
					<TableCell wrap>
						<span class="flex items-center gap-2 min-w-0" title={r.templateName}>
							<span class="truncate">{r.templateName}</span>
							{#if r.training}
								<span class="shrink-0"
									><StatusBadge tone="success" title="Awareness training">Training</StatusBadge
									></span
								>
							{/if}
						</span>
					</TableCell>
					<TableCell hideBelow="sm">
						{#if r.status === 'notSent'}
							<span class={muted}>{statusText[r.status]}</span>
						{:else}
							<span
								><StatusBadge tone={r.status === 'sent' ? 'neutral' : 'info'}
									>{statusText[r.status]}</StatusBadge
								></span
							>
						{/if}
					</TableCell>
					<TableCell>
						{#if r.status === 'sent' && r.sentAt}
							<RelativeTime value={r.sentAt} short />
						{:else if r.status === 'scheduled' && r.scheduledAt}
							<RelativeTime value={r.scheduledAt} short />
						{:else if r.window}
							<span title="The send time is picked inside this window when its interval starts"
								>{r.window.afterEnd ? '' : 'Window: '}{windowText(r.window)}</span
							>
						{:else if r.status === 'notSent'}
							<span class={muted}>Picked at send time</span>
						{/if}
					</TableCell>
					<TableCell>
						{#if r.status === 'sent' && r.notableEventName}
							<EventName eventName={r.notableEventName} />
						{/if}
					</TableCell>
				</TableRow>
			{/each}
		</Table>
		{#if plan && plan.orderMode !== 'sequential' && !ended && nextWindows.length}
			<p class="mt-3 text-sm text-gray-600 dark:text-gray-300" id="program-schedule-windows">
				Picked at send time; next windows: {nextWindows
					.slice(0, windowsShown)
					.map(windowText)
					.join(', ')}{nextWindows.length > windowsShown
					? `, and ${nextWindows.length - windowsShown} more`
					: ''}.
			</p>
		{/if}
	</div>
</Modal>
