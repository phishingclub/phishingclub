<script>
	// The people of a named program with progress, next email, status and risk.
	// The rows are paged and sorted by the server through params.
	import StatusBadge from '$lib/components/StatusBadge.svelte';
	import Table from '$lib/components/table/Table.svelte';
	import TableRow from '$lib/components/table/TableRow.svelte';
	import TableCell from '$lib/components/table/TableCell.svelte';
	import TableCellLink from '$lib/components/table/TableCellLink.svelte';
	import { shortRelative } from '$lib/utils/shortRelative.js';
	import {
		NONE_YET,
		RISK_NONE_TIP,
		RISK_TIP,
		capitalize,
		formatDateTime,
		riskTone
	} from '$lib/utils/programText.js';

	export let params;
	export let rows = [];
	export let hasNextPage = false;
	export let isLoading = false;
	export let stepCount = 0;
	export let isClosed = false;
	export let isPaused = false;
	// the time used to tell overdue emails, in ms
	export let now = Date.now();

	// phones show person, status and risk only
	const peopleColumns = [
		{ column: 'Person', size: 'large', compact: true },
		{ column: 'Progress', size: 'medium', compact: true, hideBelow: 'md' },
		{ column: 'Emails', size: 'small', alignText: 'right', compact: true, hideBelow: 'md' },
		{ column: 'Next email', size: 'small', compact: true, hideBelow: 'md' },
		{ column: 'Status', size: 'small', compact: true },
		{ column: 'Risk', size: 'small', compact: true }
	];
	const peopleSortable = ['Person', 'Next email', 'Status', 'Risk'];

	const isPast = (v, at) => !!v && new Date(v).getTime() <= at;

	const personStatus = (r) => {
		if (r.status === 'completed') return { label: 'Completed', tone: 'info' };
		if (r.status === 'exited') return { label: 'Removed', tone: 'neutral', title: 'Removed from the groups' };
		if (r.status === 'stopped') {
			return { label: 'Closed', tone: 'neutral', title: 'Still active when the program closed' };
		}
		if (isClosed) return { label: 'Closed', tone: 'neutral' };
		if (isPaused) return { label: 'Paused', tone: 'warning' };
		return { label: 'Active', tone: 'success' };
	};
</script>

<div class="mt-3">
	<Table
		columns={peopleColumns}
		sortable={peopleSortable}
		pagination={params}
		hasData={!!rows.length}
		{hasNextPage}
		hasActions={false}
		isGhost={isLoading}
		plural="recipients"
	>
		{#each rows as r (r.recipientID)}
			{@const st = personStatus(r)}
			{@const level = r.riskLevel || ''}
			<TableRow>
				<TableCellLink href="/recipient/{r.recipientID}" title={r.email || r.name}>
					<span class="block truncate">{r.name || r.email || 'Recipient'}</span>
					{#if r.name && r.email}
						<span class="block truncate text-xs text-gray-500 dark:text-gray-400">{r.email}</span>
					{/if}
				</TableCellLink>
				<TableCell hideBelow="md">
					{@const done = Math.min(r.position ?? 0, stepCount)}
					{@const round = (r.loopCount ?? 0) + 1}
					<span
						class="tabular-nums"
						title="Had {done} of {stepCount} templates, round {round}"
						>{done} of {stepCount}{round > 1 ? `, round ${round}` : ''}</span
					>
				</TableCell>
				<TableCell alignText="right" hideBelow="md">
					<span class="tabular-nums">{r.sends ?? 0}</span>
				</TableCell>
				{#if isClosed || r.status !== 'active' || !r.nextDueAt}
					<TableCell hideBelow="md" />
				{:else if isPaused}
					<TableCell hideBelow="md">
						<span title="Held while paused">Held</span>
					</TableCell>
				{:else if isPast(r.nextDueAt, now)}
					<TableCell hideBelow="md">
						<span
							class="text-yellow-800 dark:text-yellow-300"
							title="Overdue since {formatDateTime(r.nextDueAt)}">Overdue</span
						>
					</TableCell>
				{:else}
					<TableCell hideBelow="md">
						<span title={new Date(r.nextDueAt).toLocaleString()}
							>{shortRelative(r.nextDueAt, now)}</span
						>
					</TableCell>
				{/if}
				<TableCell>
					<span><StatusBadge tone={st.tone} title={st.title || ''}>{st.label}</StatusBadge></span>
				</TableCell>
				<TableCell>
					{#if r.riskScore == null}
						<span class="text-gray-400 dark:text-gray-500 cursor-help" title={RISK_NONE_TIP}
							>{NONE_YET}</span
						>
					{:else}
						<span class="cursor-help"
							><StatusBadge
								tone={riskTone[level] ?? 'neutral'}
								title="Risk score {Math.round(r.riskScore)} of 100. {RISK_TIP}"
								>{capitalize(level)} {Math.round(r.riskScore)}</StatusBadge
							></span
						>
					{/if}
				</TableCell>
			</TableRow>
		{/each}
	</Table>
</div>
