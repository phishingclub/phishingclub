<script>
	import { onMount } from 'svelte';
	import { api } from '$lib/api/apiProxy.js';
	import { addToast } from '$lib/store/toast';
	import { newTableURLParams } from '$lib/service/tableURLParams.js';
	import Table from '$lib/components/table/Table.svelte';
	import TableRow from '$lib/components/table/TableRow.svelte';
	import TableCell from '$lib/components/table/TableCell.svelte';
	import TableCellLink from '$lib/components/table/TableCellLink.svelte';
	import StatusBadge from '$lib/components/StatusBadge.svelte';

	// the company whose delivery log is shown
	export let companyId;

	const tableURLParams = newTableURLParams({
		sortBy: 'date',
		sortOrder: 'desc',
		prefix: 'log',
		noScroll: true
	});

	let rows = [];
	let hasNextPage = false;
	let isLoading = false;

	const refresh = async () => {
		try {
			isLoading = true;
			const res = await api.company.reportConfig.getLog(companyId, tableURLParams);
			if (res.success) {
				rows = res.data?.rows ?? [];
				hasNextPage = !!res.data?.hasNextPage;
			} else {
				addToast(res.error ?? 'Failed to load delivery log', 'Error');
			}
		} catch (e) {
			console.error('failed to load delivery log', e);
			addToast('Failed to load delivery log', 'Error');
		} finally {
			isLoading = false;
		}
	};

	onMount(() => {
		refresh();
		tableURLParams.onChange(refresh);
		return () => tableURLParams.unsubscribe();
	});

	// a skipped row is a report that was not sent on purpose, such as a test
	// campaign closing, the reason is in its title
	const statusTone = (s) => (s === 'sent' ? 'success' : s === 'skipped' ? 'neutral' : 'danger');
	const statusLabel = (s) => (s === 'sent' ? 'Sent' : s === 'skipped' ? 'Skipped' : 'Failed');

	const triggerLabel = (t) =>
		t === 'on_demand'
			? 'On demand'
			: t === 'on_finish'
				? 'On finish'
				: t === 'scheduled'
					? 'Program schedule'
					: t;

	// a log row links to the program or campaign it was sent for, while it exists
	const sourceHref = (row) => {
		if (!row.sourceID) {
			return '';
		}
		return row.source === 'program' ? `/program/${row.sourceID}` : `/campaign/${row.sourceID}`;
	};
</script>

<Table
	columns={[
		{ column: 'Date', size: 'medium' },
		{ column: 'Source', size: 'large' },
		{ column: 'Group', size: 'medium' },
		{ column: 'Trigger', size: 'small' },
		{ column: 'Status', size: 'small' },
		{ column: 'Recipients', size: 'small' }
	]}
	sortable={['Date', 'Source', 'Group', 'Trigger', 'Status', 'Recipients']}
	hasData={!!rows.length}
	{hasNextPage}
	plural="deliveries"
	pagination={tableURLParams}
	isGhost={isLoading}
	hasActions={false}
>
	{#each rows as row}
		{@const href = sourceHref(row)}
		<TableRow>
			<TableCell value={row.createdAt} isDate />
			{#if href}
				<TableCellLink {href} title={row.sourceName}>{row.sourceName || ''}</TableCellLink>
			{:else}
				<TableCell value={row.sourceName || '-'} />
			{/if}
			<TableCell value={row.groupName || '-'} />
			<TableCell value={triggerLabel(row.trigger)} />
			<TableCell>
				<div>
					<StatusBadge
						tone={statusTone(row.status)}
						title={row.status !== 'sent' ? row.errorMessage : ''}
					>
						{statusLabel(row.status)}
					</StatusBadge>
				</div>
			</TableCell>
			<TableCell>
				<span title={row.recipients}>{row.recipientCount}</span>
			</TableCell>
		</TableRow>
	{/each}
</Table>
