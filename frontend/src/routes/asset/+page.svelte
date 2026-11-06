<script>
	import { onMount } from 'svelte';
	import { api } from '$lib/api/apiProxy.js';
	import TableRow from '$lib/components/table/TableRow.svelte';
	import TableCell from '$lib/components/table/TableCell.svelte';
	import { AppStateService } from '$lib/service/appState';
	import { newTableURLParams } from '$lib/service/tableURLParams.js';
	import Headline from '$lib/components/Headline.svelte';
	import Table from '$lib/components/table/Table.svelte';
	import HeadTitle from '$lib/components/HeadTitle.svelte';
	import BigButton from '$lib/components/BigButton.svelte';
	import { goto } from '$app/navigation';

	// services
	const appStateService = AppStateService.instance;

	// data
	let domains = [];
	let domainsHasNextPage = true;
	let contextCompanyID = '';
	let isTableLoading = false;
	const tableURLParams = newTableURLParams();

	// hooks
	onMount(() => {
		if (appStateService.getContext()) {
			contextCompanyID = appStateService.getContext().companyID;
		}
		refresh();
		tableURLParams.onChange(refresh);
		return () => {
			tableURLParams.unsubscribe();
		};
	});

	// component logic
	const refresh = async () => {
		try {
			isTableLoading = true;
			const res = await api.domain.getAllSubsetWithoutProxies(tableURLParams, contextCompanyID);
			if (!res.success) {
				throw res.error;
			}
			domains = res.data?.rows ?? [];
			domainsHasNextPage = res.data?.hasNextPage;
		} catch (e) {
			console.error('failed to get asset domains', e);
		} finally {
			isTableLoading = false;
		}
	};
</script>

<HeadTitle title="Assets" />
<main>
	<Headline docSlug="assets">Assets by domain</Headline>
	{#if !contextCompanyID}
		<BigButton
			variant="secondary"
			on:click={() => {
				goto('/asset/shared/');
			}}>Open shared assets</BigButton
		>
	{:else}
		<BigButton
			variant="secondary"
			on:click={() => {
				goto('/asset/company/');
			}}>Open company assets</BigButton
		>
	{/if}
	<Table
		columns={[{ column: 'Name', size: 'large' }]}
		sortable={['Name']}
		hasData={domains.length > 0}
		hasNextPage={domainsHasNextPage}
		plural="domains"
		pagination={tableURLParams}
		hasActions={false}
		reserveSelect
		isGhost={isTableLoading}
	>
		{#each domains as domain}
			<TableRow>
				<TableCell wrap>
					<a
						class="pc-cell-link group w-full !flex items-center justify-between gap-2 min-h-10"
						href="/asset/{domain.name}"
						title="Open assets for {domain.name}"
					>
						<span class="truncate">{domain.name}</span>
						<svg
							class="h-4 w-4 shrink-0 text-gray-400 dark:text-gray-500 group-hover:text-slate-600 dark:group-hover:text-gray-200"
							viewBox="0 0 20 20"
							fill="currentColor"
							aria-hidden="true"
						>
							<path
								fill-rule="evenodd"
								d="M7.21 14.77a.75.75 0 0 1 .02-1.06L11.17 10 7.23 6.29a.75.75 0 1 1 1.04-1.08l4.5 4.25a.75.75 0 0 1 0 1.08l-4.5 4.25a.75.75 0 0 1-1.06-.02Z"
								clip-rule="evenodd"
							/>
						</svg>
					</a>
				</TableCell>
			</TableRow>
		{/each}
	</Table>
</main>
