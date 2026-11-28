<script>
	// The recipients of a program with their status, progress, sends, next
	// template and, with Language by, language. The rows are paged and sorted by the server through params. The
	// Events and Schedule actions open the events and the plan of that recipient
	// in this program. Before the program starts the rows are the members of
	// its groups, not started, with no actions.
	import Table from '$lib/components/table/Table.svelte';
	import TableRow from '$lib/components/table/TableRow.svelte';
	import TableCell from '$lib/components/table/TableCell.svelte';
	import TableCellLink from '$lib/components/table/TableCellLink.svelte';
	import TableCellEmpty from '$lib/components/table/TableCellEmpty.svelte';
	import TableCellAction from '$lib/components/table/TableCellAction.svelte';
	import TableDropDownEllipsis from '$lib/components/table/TableDropDownEllipsis.svelte';
	import TableDropDownButton from '$lib/components/table/TableDropDownButton.svelte';
	import EventName from '$lib/components/table/EventName.svelte';
	import ProgramRecipientEventsModal from '$lib/components/program/ProgramRecipientEventsModal.svelte';
	import ProgramScheduleModal from '$lib/components/program/ProgramScheduleModal.svelte';
	import RelativeTime from '$lib/components/RelativeTime.svelte';
	import { shortRelative } from '$lib/utils/shortRelative.js';
	import { formatDateTime } from '$lib/utils/programText.js';
	import { languageName } from '$lib/utils/languages.js';

	export let params;
	export let programID;
	// an anonymous program shows no sends per recipient
	export let anonymous = false;
	export let rows = [];
	export let hasNextPage = false;
	export let isLoading = false;
	export let stepCount = 0;
	// a program that starts over shows the round with the progress
	export let loops = false;
	export let isClosed = false;
	export let isPaused = false;
	// a draft or scheduled program lists the people it would enroll at start
	export let notStarted = false;
	// when a scheduled program starts, null for a draft
	export let startAt = null;
	// the time the relative times are shown against, in ms
	export let now = Date.now();
	// a program with Language by shows the language of each person's next or
	// last send
	export let showLanguage = false;

	// Progress and Last template show from xl, as a 1024 screen with the menu
	// has no room for them. Phones show the recipient only, with the status and next due time in a
	// subline as on the campaign Recipients overview. Status is the most notable
	// event across the recipient's sends.
	$: peopleColumns = [
		{ column: 'Recipient', size: 'large', compact: true },
		{ column: 'Status', size: 'medium', compact: true, hideBelow: 'sm' },
		...(showLanguage ? [{ column: 'Language', size: 'small', compact: true, hideBelow: 'xl' }] : []),
		{ column: 'Progress', size: loops ? 'medium' : 'small', compact: true, hideBelow: 'xl' },
		{ column: 'Sent', size: 'small', compact: true, hideBelow: 'md' },
		{ column: 'Last template', size: 'medium', compact: true, hideBelow: 'xl' },
		{ column: 'Next template', size: 'medium', compact: true, hideBelow: 'md' }
	];
	// people not yet enrolled share one next time, so only the recipient sorts
	$: peopleSortable = notStarted ? ['Recipient'] : ['Recipient', 'Next template'];

	// a recipient that gets no more templates shows why in place of the next one
	const endedText = {
		completed: ['Completed', 'Had every template'],
		exited: ['Removed', 'Removed from the groups'],
		stopped: ['Stopped at close', 'Still active when the program closed']
	};
	const muted = 'text-gray-400 dark:text-gray-500';
	const isNotStarted = (r) => r.status === 'not_started';

	let eventsModal;
	let scheduleModal;
	const titleOf = (r) => r.name || r.email || 'Recipient';
	// the next due time is shown only while a template is still to come
	$: hasUpcoming = (r) => !endedText[r.status] && !isClosed && !isPaused && !!r.nextDueAt;
</script>

<Table
	columns={peopleColumns}
	sortable={peopleSortable}
	pagination={params}
	hasData={!!rows.length}
	{hasNextPage}
	hasActions={!anonymous && !notStarted}
	isGhost={isLoading}
	plural="recipients"
>
	{#each rows as r (r.recipientID)}
		{@const ended = endedText[r.status]}
		<TableRow>
			<TableCellLink href="/recipient/{r.recipientID}" title={r.email || r.name}>
				<span class="block truncate">{r.name || r.email || 'Recipient'}</span>
				{#if r.name && r.email}
					<span class="block truncate text-xs text-gray-500 dark:text-gray-400">{r.email}</span>
				{/if}
				<!-- the Status column is off screen on phones -->
				<span class="flex sm:hidden mt-1 min-w-0 items-center gap-2"
					>{#if isNotStarted(r)}<span class="{muted} text-xs">Not started</span>{#if startAt}<span
								class="min-w-0 truncate text-xs"><RelativeTime value={startAt} short /></span
							>{/if}{:else}<EventName
							eventName={r.notableEventName}
						/>{/if}{#if hasUpcoming(r)}<span
							class="min-w-0 truncate text-xs"
							title={formatDateTime(r.nextDueAt)}>{shortRelative(r.nextDueAt, now)}</span
						>{/if}</span
				>
			</TableCellLink>
			<TableCell hideBelow="sm">
				{#if isNotStarted(r)}
					<span class={muted}>Not started</span>
				{:else}
					<EventName eventName={r.notableEventName} />
				{/if}
			</TableCell>
			{#if showLanguage}
				<TableCell hideBelow="xl">
					{#if r.language}
						<span class="block truncate">{languageName(r.language)}</span>
					{:else}
						<!-- no language is picked until a template is planned -->
						<span class={muted}>-</span>
					{/if}
				</TableCell>
			{/if}
			<TableCell hideBelow="xl">
				{@const done = Math.min(r.position ?? 0, stepCount)}
				{#if loops}
					{@const round = (r.loopCount ?? 0) + 1}
					<span
						class="tabular-nums"
						title="Round {round}, had {done} of {stepCount} templates in this round"
						>Round {round}, {done} of {stepCount}</span
					>
				{:else}
					<span class="tabular-nums" title="Had {done} of {stepCount} templates"
						>{done} of {stepCount}</span
					>
				{/if}
			</TableCell>
			<TableCell hideBelow="md">
				<span class="tabular-nums">{r.sends ?? 0}</span>
			</TableCell>
			<TableCell hideBelow="xl">
				{#if r.lastTemplateName}
					<span class="block truncate" title={r.lastTemplateName}>{r.lastTemplateName}</span>
					{#if r.lastSentAt}
						<span class="block text-xs" title={formatDateTime(r.lastSentAt)}
							>{shortRelative(r.lastSentAt, now)}</span
						>
					{/if}
				{:else}
					<span class={muted}>None</span>
				{/if}
			</TableCell>
			<TableCell hideBelow="md">
				{#if isNotStarted(r)}
					{#if startAt}
						<RelativeTime value={startAt} short />
					{:else}
						<span class={muted} title="The first templates are planned when the program starts"
							>At start</span
						>
					{/if}
				{:else if ended}
					<span class={muted} title={ended[1]}>{ended[0]}</span>
				{:else if isPaused}
					<span title="Held while paused">Held</span>
				{:else if isClosed || !r.nextDueAt}
					<span class={muted}>None</span>
				{:else if r.nextTemplateName}
					<span class="block truncate" title={r.nextTemplateName}>{r.nextTemplateName}</span>
					<span class="block text-xs" title={formatDateTime(r.nextDueAt)}
						>{shortRelative(r.nextDueAt, now)}</span
					>
				{:else}
					<!-- the template is picked when it is sent, so only the time is known -->
					<span class="block truncate" title="Picked at send time"
						>{shortRelative(r.nextDueAt, now)}</span
					>
				{/if}
			</TableCell>
			{#if !anonymous && !notStarted}
				<TableCellEmpty />
				<TableCellAction>
					<TableDropDownEllipsis>
						<TableDropDownButton
							name="Events"
							on:click={() => eventsModal.open(programID, r.recipientID, titleOf(r))}
						/>
						<TableDropDownButton
							name="Schedule"
							on:click={() =>
								scheduleModal.open(
									programID,
									r.recipientID,
									titleOf(r),
									hasUpcoming(r) ? r.nextDueAt : null
								)}
						/>
					</TableDropDownEllipsis>
				</TableCellAction>
			{/if}
		</TableRow>
	{/each}
</Table>

{#if !anonymous}
	<ProgramRecipientEventsModal bind:this={eventsModal} />
	<ProgramScheduleModal bind:this={scheduleModal} />
{/if}
