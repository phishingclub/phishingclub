<script>
	import { onMount } from 'svelte';
	import { api } from '$lib/api/apiProxy.js';
	import { addToast } from '$lib/store/toast';
	import { newTableURLParams } from '$lib/service/tableURLParams.js';
	import Table from '$lib/components/table/Table.svelte';
	import TableRow from '$lib/components/table/TableRow.svelte';
	import TableCell from '$lib/components/table/TableCell.svelte';

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

	const triggerLabel = (t) =>
		t === 'on_demand' ? 'On demand' : t === 'on_finish' ? 'On finish' : t;
</script>

<Table
	columns={[
		{ column: 'Date', size: 'medium' },
		{ column: 'Campaign', size: 'large' },
		{ column: 'Group', size: 'medium' },
		{ column: 'Trigger', size: 'small' },
		{ column: 'Status', size: 'small' },
		{ column: 'Recipients', size: 'small' }
	]}
	sortable={['Date', 'Campaign', 'Group', 'Trigger', 'Status', 'Recipients']}
	hasData={!!rows.length}
	{hasNextPage}
	plural="deliveries"
	pagination={tableURLParams}
	isGhost={isLoading}
	hasActions={false}
>
	{#each rows as row}
		<TableRow>
			<TableCell value={row.createdAt} isDate />
			<TableCell value={row.campaignName || '-'} />
			<TableCell value={row.groupName || '-'} />
			<TableCell value={triggerLabel(row.trigger)} />
			<TableCell>
				<div>
					<span
						class="inline-block text-xs font-semibold px-2 py-1 rounded-full
							{row.status === 'sent'
							? 'bg-green-100 text-green-700 dark:bg-green-900/40 dark:text-green-300'
							: 'bg-red-100 text-red-700 dark:bg-red-900/40 dark:text-red-300'}"
						title={row.status !== 'sent' ? row.errorMessage : ''}
					>
						{row.status === 'sent' ? 'Sent' : 'Failed'}
					</span>
				</div>
			</TableCell>
			<TableCell>
				<span title={row.recipients}>{row.recipientCount}</span>
			</TableCell>
		</TableRow>
	{/each}
</Table>
