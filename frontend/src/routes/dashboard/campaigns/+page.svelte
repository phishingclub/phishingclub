<script>
	import Headline from '$lib/components/Headline.svelte';
	import HeadTitle from '$lib/components/HeadTitle.svelte';
	import SubHeadline from '$lib/components/SubHeadline.svelte';
	import { AppStateService } from '$lib/service/appState';
	import { api } from '$lib/api/apiProxy.js';
	import { onMount } from 'svelte';
	import { addToast } from '$lib/store/toast';
	import { newTableURLParams } from '$lib/service/tableURLParams';
	import Table from '$lib/components/table/Table.svelte';
	import TableRow from '$lib/components/table/TableRow.svelte';
	import TableCell from '$lib/components/table/TableCell.svelte';
	import TableCellEmpty from '$lib/components/table/TableCellEmpty.svelte';
	import TableCellAction from '$lib/components/table/TableCellAction.svelte';
	import TableViewButton from '$lib/components/table/TableViewButton.svelte';
	import { goto } from '$app/navigation';
	import TableDropDownEllipsis from '$lib/components/table/TableDropDownEllipsis.svelte';
	import TestLabel from '$lib/components/TestLabel.svelte';
	import AutoRefresh from '$lib/components/AutoRefresh.svelte';
	import { tick } from 'svelte';
	import TextFieldSelect from '$lib/components/TextFieldSelect.svelte';
	import { autoRefreshStore, setPageAutoRefresh } from '$lib/store/autoRefresh';
	import { BiMap } from '$lib/utils/maps';
	import DashboardNav from '$lib/components/DashboardNav.svelte';

	// services
	const appStateService = AppStateService.instance;

	// auto-refresh options
	const autoRefreshOptions = new BiMap({
		Disabled: '0',
		'5s': '5000',
		'30s': '30000',
		'1m': '60000',
		'5m': '300000'
	});

	// local state
	let contextCompanyID = null;
	let contextCompanyName = '';
	let activeTableURLParams = newTableURLParams({
		prefix: 'active',
		sortBy: 'send_start_at',
		sortOrder: 'desc',
		noScroll: true
	});
	let scheduledTableURLParams = newTableURLParams({
		prefix: 'scheduled',
		sortBy: 'send_start_at',
		sortOrder: 'desc',
		noScroll: true
	});
	let completedTableURLParams = newTableURLParams({
		prefix: 'completed',
		sortBy: 'send_start_at',
		sortOrder: 'desc',
		noScroll: true
	});

	let isActiveCampaignsLoading = false;
	let isScheduledCampaignsLoading = false;
	let isCompletedCampaignsLoading = false;

	let activeCampaigns = [];
	let activeCampaignsHasNextPage = true;
	let scheduledCampaigns = [];
	let scheduledCampaignsHasNextPage = true;
	let completedCampaigns = [];
	let completedCampaignsHasNextPage = true;

	let includeTestCampaigns = false;

	// handler for when toggle changes
	const handleToggleChange = async () => {
		await tick();
		await refresh();
	};

	const handleAutoRefreshChange = (optKey) => {
		const value = Number(autoRefreshOptions.byKey(optKey));
		autoRefreshStore.setEnabled(value > 0);
		autoRefreshStore.setInterval(value);
		setPageAutoRefresh('dashboard-campaigns', $autoRefreshStore);
	};

	// hooks
	onMount(() => {
		const context = appStateService.getContext();
		if (context) {
			contextCompanyID = context.companyID;
			contextCompanyName = context.companyName;
		}
		refresh();
		activeTableURLParams.onChange(() => refreshActiveCampaigns(true));
		scheduledTableURLParams.onChange(() => refreshScheduledCampaigns(true));
		completedTableURLParams.onChange(() => refreshCompletedCampaigns(true));

		return () => {
			activeTableURLParams.unsubscribe();
			scheduledTableURLParams.unsubscribe();
			completedTableURLParams.unsubscribe();
		};
	});

	const refresh = async () => {
		await Promise.all([
			refreshActiveCampaigns(false),
			refreshScheduledCampaigns(false),
			refreshCompletedCampaigns(false)
		]);
	};

	const refreshActiveCampaigns = async (showLoading = true) => {
		if (showLoading) {
			isActiveCampaignsLoading = true;
		}
		try {
			const options = {
				page: activeTableURLParams.currentPage,
				perPage: activeTableURLParams.perPage,
				sortBy: activeTableURLParams.sortBy,
				sortOrder: activeTableURLParams.sortOrder,
				search: activeTableURLParams.search,
				includeTest: includeTestCampaigns
			};
			const res = await api.campaign.getAllActive(options, contextCompanyID);
			if (res.success) {
				activeCampaigns = res.data.rows;
				activeCampaignsHasNextPage = res.data.hasNextPage;
			}
		} catch (e) {
			addToast('Failed to load active campaigns', 'Error');
			console.error('failed to load active campaigns', e);
		} finally {
			if (showLoading) {
				isActiveCampaignsLoading = false;
			}
		}
	};

	const refreshScheduledCampaigns = async (showLoading = true) => {
		if (showLoading) {
			isScheduledCampaignsLoading = true;
		}
		try {
			const options = {
				page: scheduledTableURLParams.currentPage,
				perPage: scheduledTableURLParams.perPage,
				sortBy: scheduledTableURLParams.sortBy,
				sortOrder: scheduledTableURLParams.sortOrder,
				search: scheduledTableURLParams.search,
				includeTest: includeTestCampaigns
			};
			const res = await api.campaign.getAllUpcoming(options, contextCompanyID);
			if (res.success) {
				scheduledCampaigns = res.data.rows;
				scheduledCampaignsHasNextPage = res.data.hasNextPage;
			}
		} catch (e) {
			addToast('Failed to load scheduled campaigns', 'Error');
			console.error('failed to load scheduled campaigns', e);
		} finally {
			if (showLoading) {
				isScheduledCampaignsLoading = false;
			}
		}
	};

	const refreshCompletedCampaigns = async (showLoading = true) => {
		if (showLoading) {
			isCompletedCampaignsLoading = true;
		}
		try {
			const options = {
				page: completedTableURLParams.currentPage,
				perPage: completedTableURLParams.perPage,
				sortBy: completedTableURLParams.sortBy,
				sortOrder: completedTableURLParams.sortOrder,
				search: completedTableURLParams.search,
				includeTest: includeTestCampaigns
			};
			const res = await api.campaign.getAllFinished(options, contextCompanyID);
			if (res.success) {
				completedCampaigns = res.data.rows;
				completedCampaignsHasNextPage = res.data.hasNextPage;
			}
		} catch (e) {
			addToast('Failed to load completed campaigns', 'Error');
			console.error('failed to load completed campaigns', e);
		} finally {
			if (showLoading) {
				isCompletedCampaignsLoading = false;
			}
		}
	};

	const onClickViewCampaign = (id) => {
		goto(`/campaign/${id}`);
	};
</script>

<HeadTitle title="Dashboard - Campaigns" />
<main>
	<Headline>Dashboard</Headline>

	<DashboardNav />

	<div class="flex justify-between items-center mb-6">
		<SubHeadline>Campaigns</SubHeadline>
		<div class="flex items-center gap-4">
			<label class="flex items-center gap-2 cursor-pointer">
				<span class="font-semibold text-slate-600 dark:text-gray-300 whitespace-nowrap">
					Include test campaigns
				</span>
				<div class="relative flex items-center">
					<input
						type="checkbox"
						id="includeTestCampaigns"
						bind:checked={includeTestCampaigns}
						on:change={handleToggleChange}
						class="peer sr-only"
					/>
					<div
						class="w-5 h-5 border-2 border-slate-300 dark:border-gray-700/60 rounded
						       peer-checked:border-cta-blue dark:peer-checked:border-highlight-blue/80 peer-checked:bg-cta-blue dark:peer-checked:bg-highlight-blue/80
						       peer-focus:border-slate-400 dark:peer-focus:border-highlight-blue/80 peer-focus:bg-gray-100 dark:peer-focus:bg-gray-700/60
						       transition-all duration-200 ease-in-out
						       flex items-center justify-center
						       bg-slate-50 dark:bg-gray-900/60"
					>
						{#if includeTestCampaigns}
							<svg class="w-3 h-3 text-white" fill="none" stroke="currentColor" viewBox="0 0 24 24">
								<path
									stroke-linecap="round"
									stroke-linejoin="round"
									stroke-width="3"
									d="M5 13l4 4L19 7"
								/>
							</svg>
						{/if}
					</div>
				</div>
			</label>
			<div class="flex items-center gap-2">
				<span class="font-semibold text-slate-600 dark:text-gray-300 whitespace-nowrap">
					Auto-Refresh
				</span>
				<TextFieldSelect
					id="autoRefresh"
					value={$autoRefreshStore.enabled
						? autoRefreshOptions.byValue($autoRefreshStore.interval.toString())
						: 'Disabled'}
					onSelect={handleAutoRefreshChange}
					options={autoRefreshOptions.keys()}
					inline={true}
					size={'small'}
				/>
			</div>
		</div>
	</div>

	<AutoRefresh
		isLoading={false}
		pageId="dashboard-campaigns"
		onRefresh={async () => {
			const activeOptions = {
				page: activeTableURLParams.currentPage,
				perPage: activeTableURLParams.perPage,
				sortBy: activeTableURLParams.sortBy,
				sortOrder: activeTableURLParams.sortOrder,
				search: activeTableURLParams.search,
				includeTest: includeTestCampaigns
			};
			const activeRes = await api.campaign.getAllActive(activeOptions, contextCompanyID);
			if (activeRes.success) {
				activeCampaigns = activeRes.data.rows;
				activeCampaignsHasNextPage = activeRes.data.hasNextPage;
			}

			const scheduledOptions = {
				page: scheduledTableURLParams.currentPage,
				perPage: scheduledTableURLParams.perPage,
				sortBy: scheduledTableURLParams.sortBy,
				sortOrder: scheduledTableURLParams.sortOrder,
				search: scheduledTableURLParams.search,
				includeTest: includeTestCampaigns
			};
			const scheduledRes = await api.campaign.getAllUpcoming(scheduledOptions, contextCompanyID);
			if (scheduledRes.success) {
				scheduledCampaigns = scheduledRes.data.rows;
				scheduledCampaignsHasNextPage = scheduledRes.data.hasNextPage;
			}

			const completedOptions = {
				page: completedTableURLParams.currentPage,
				perPage: completedTableURLParams.perPage,
				sortBy: completedTableURLParams.sortBy,
				sortOrder: completedTableURLParams.sortOrder,
				search: completedTableURLParams.search,
				includeTest: includeTestCampaigns
			};
			const completedRes = await api.campaign.getAllFinished(completedOptions, contextCompanyID);
			if (completedRes.success) {
				completedCampaigns = completedRes.data.rows;
				completedCampaignsHasNextPage = completedRes.data.hasNextPage;
			}
		}}
	/>

	<SubHeadline>Active campaigns</SubHeadline>
	<div class="min-h-[300px] mb-8">
		<Table
			isGhost={isActiveCampaignsLoading}
			columns={[
				{ column: 'Name', size: 'large' },
				...(contextCompanyID ? [] : [{ column: 'Company', size: 'medium' }]),
				{ title: 'Delivery started', column: 'Send start at', size: 'small' },
				{ title: 'Delivery finishes', column: 'Send end at', size: 'small' }
			]}
			hasData={!!activeCampaigns.length}
			hasNextPage={activeCampaignsHasNextPage}
			plural="active campaigns"
			pagination={activeTableURLParams}
		>
			{#each activeCampaigns as campaign}
				<TableRow>
					<TableCell>
						<span class="inline-flex items-center gap-1 py-1">
							{#if campaign.isTest}
								<TestLabel />
							{/if}
							<a href={`/campaign/${campaign.id}`}>
								{campaign.name}
							</a>
						</span>
					</TableCell>
					{#if !contextCompanyID}
						<TableCell value={campaign.company?.name} />
					{/if}
					<TableCell value={campaign.sendStartAt} isDate isRelative />
					<TableCell value={campaign.sendEndAt} isDate isRelative />
					<TableCellEmpty />
					<TableCellAction>
						<TableDropDownEllipsis>
							<TableViewButton on:click={() => onClickViewCampaign(campaign.id)} />
						</TableDropDownEllipsis>
					</TableCellAction>
				</TableRow>
			{/each}
		</Table>
	</div>

	<SubHeadline>Scheduled campaigns</SubHeadline>
	<div class="min-h-[300px] mb-8">
		<Table
			isGhost={isScheduledCampaignsLoading}
			columns={[
				{ column: 'Name', size: 'large' },
				...(contextCompanyID ? [] : [{ column: 'Company', size: 'medium' }]),
				{ title: 'Delivery starts', column: 'Send start at', size: 'small' },
				{ title: 'Delivery finishes', column: 'Send end at', size: 'small' }
			]}
			hasData={!!scheduledCampaigns.length}
			hasNextPage={scheduledCampaignsHasNextPage}
			plural="scheduled campaigns"
			pagination={scheduledTableURLParams}
		>
			{#each scheduledCampaigns as campaign}
				<TableRow>
					<TableCell>
						<span class="inline-flex items-center gap-1 py-1">
							{#if campaign.isTest}
								<TestLabel />
							{/if}
							<a href={`/campaign/${campaign.id}`}>
								{campaign.name}
							</a>
						</span>
					</TableCell>
					{#if !contextCompanyID}
						<TableCell value={campaign.company?.name} />
					{/if}
					<TableCell value={campaign.sendStartAt} isDate isRelative />
					<TableCell value={campaign.sendEndAt} isDate isRelative />
					<TableCellEmpty />
					<TableCellAction>
						<TableDropDownEllipsis>
							<TableViewButton on:click={() => onClickViewCampaign(campaign.id)} />
						</TableDropDownEllipsis>
					</TableCellAction>
				</TableRow>
			{/each}
		</Table>
	</div>

	<SubHeadline>Completed campaigns</SubHeadline>
	<div class="min-h-[300px] mb-8">
		<Table
			isGhost={isCompletedCampaignsLoading}
			columns={[
				{ column: 'Name', size: 'large' },
				...(contextCompanyID ? [] : [{ column: 'Company', size: 'medium' }]),
				{ title: 'Delivery started', column: 'Send start at', size: 'small' },
				{ title: 'Delivery finished', column: 'Send end at', size: 'small' }
			]}
			hasData={!!completedCampaigns.length}
			hasNextPage={completedCampaignsHasNextPage}
			plural="completed campaigns"
			pagination={completedTableURLParams}
		>
			{#each completedCampaigns as campaign}
				<TableRow>
					<TableCell>
						<span class="inline-flex items-center gap-1 py-1">
							{#if campaign.isTest}
								<TestLabel />
							{/if}
							<a href={`/campaign/${campaign.id}`}>
								{campaign.name}
							</a>
						</span>
					</TableCell>
					{#if !contextCompanyID}
						<TableCell value={campaign.company?.name} />
					{/if}
					<TableCell value={campaign.sendStartAt} isDate isRelative />
					<TableCell value={campaign.sendEndAt} isDate isRelative />
					<TableCellEmpty />
					<TableCellAction>
						<TableDropDownEllipsis>
							<TableViewButton on:click={() => onClickViewCampaign(campaign.id)} />
						</TableDropDownEllipsis>
					</TableCellAction>
				</TableRow>
			{/each}
		</Table>
	</div>
</main>
