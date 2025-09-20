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
			const res = await api.domain.getAllSubset(tableURLParams, contextCompanyID);
			if (!res.success) {
				throw res.error;
			}
			domains = res.data?.rows ?? [];
		} catch (e) {
			console.error('failed to get asset domains', e);
		} finally {
			isTableLoading = false;
		}
	};
</script>

<HeadTitle title="Assets" />
<main>
	<Headline>Asset by domains</Headline>
	{#if !contextCompanyID}
		<BigButton
			on:click={() => {
				goto('/asset/shared/');
			}}>Open shared assets</BigButton
		>
	{/if}
	<Table
		columns={[{ column: 'Name', size: 'large' }]}
		sortable={['Name']}
		hasData={domains.length > 0}
		plural="domains"
		pagination={tableURLParams}
		hasActions={false}
		isGhost={isTableLoading}
	>
		{#each domains as domain}
			<TableRow>
				<TableCell>
					<a class="w-full flex" href="/asset/{domain.name}">{domain.name}</a>
				</TableCell>
			</TableRow>
		{/each}
	</Table>
</main>
