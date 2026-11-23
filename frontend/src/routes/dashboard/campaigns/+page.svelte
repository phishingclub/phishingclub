<script>
	import Headline from '$lib/components/Headline.svelte';
	import HeadTitle from '$lib/components/HeadTitle.svelte';
	import SubHeadline from '$lib/components/SubHeadline.svelte';
	import { AppStateService } from '$lib/service/appState';
	import { api } from '$lib/api/apiProxy.js';
	import { onMount, tick } from 'svelte';
	import { get } from 'svelte/store';
	import { page } from '$app/stores';
	import { addToast } from '$lib/store/toast';
	import { newTableURLParams } from '$lib/service/tableURLParams';
	import Table from '$lib/components/table/Table.svelte';
	import TableRow from '$lib/components/table/TableRow.svelte';
	import TableCell from '$lib/components/table/TableCell.svelte';
	import TestLabel from '$lib/components/TestLabel.svelte';
	import { campaignStatus } from '$lib/utils/campaignStatus.js';
	import DashboardNav from '$lib/components/DashboardNav.svelte';
	import DashboardToolbar from '$lib/components/DashboardToolbar.svelte';
	import StatusBadge from '$lib/components/StatusBadge.svelte';
	import StatTiles from '$lib/components/StatTiles.svelte';
	import { dashboardIncludeTest } from '$lib/store/dashboardIncludeTest.js';
	import { shortRelative } from '$lib/utils/shortRelative.js';
	import { scrollToTable } from '$lib/utils/scrollToTable.js';

	// services
	const appStateService = AppStateService.instance;

	// the table a tile link points at, read before the table paging rewrites
	// the url without it
	const initialHash = (get(page).url.hash || window.location.hash).slice(1);

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

	$: includeTestCampaigns = $dashboardIncludeTest;
	let isLoading = false;

	// campaign counts from the stats endpoint, null until loaded
	let stats = null;
	// an open campaign is active with a delivery window, else self managed
	const activeStatus = (c) =>
		campaignStatus({ notableEventName: c.sendStartAt ? 'campaign_active' : 'campaign_self_managed' });

	$: tiles = [
		{
			key: 'active',
			title: 'Active',
			tip: 'Started and not closed, paused campaigns left out.',
			value: stats?.active ?? 0,
			color: 'text-campaign-active',
			icon: 'active',
			href: '#active'
		},
		{
			key: 'scheduled',
			title: 'Scheduled',
			value: stats?.upcoming ?? 0,
			color: 'text-campaign-scheduled',
			icon: 'scheduled',
			href: '#scheduled'
		},
		{
			key: 'completed',
			title: 'Completed',
			value: stats?.finished ?? 0,
			color: 'text-campaign-completed',
			icon: 'completed',
			href: '#completed'
		},
		{
			key: 'paused',
			title: 'Paused',
			tip: 'Sending is paused. The campaigns are marked Paused in the Active campaigns list.',
			value: stats?.paused ?? 0,
			color: 'text-closed',
			icon: 'paused',
			href: '#active'
		}
	];

	// phones drop the Company column; the company moves into the name subline
	let isPhone = false;
	onMount(() => {
		const query = window.matchMedia('(max-width: 639px)');
		const update = () => (isPhone = query.matches);
		update();
		query.addEventListener('change', update);
		return () => query.removeEventListener('change', update);
	});
	$: showCompanyColumn = !contextCompanyID && !isPhone;

	// relative dates tick every minute, the full date is in the title
	let relativeNow = Date.now();
	onMount(() => {
		const id = setInterval(() => (relativeNow = Date.now()), 60000);
		return () => clearInterval(id);
	});

	// hooks
	onMount(() => {
		const context = appStateService.getContext();
		if (context) {
			contextCompanyID = context.companyID;
			contextCompanyName = context.companyName;
		}
		refresh().then(async () => {
			// the tables above have their height once loaded
			await tick();
			scrollToTable(initialHash);
		});
		activeTableURLParams.onChange(() => refreshActiveCampaigns(true));
		scheduledTableURLParams.onChange(() => refreshScheduledCampaigns(true));
		completedTableURLParams.onChange(() => refreshCompletedCampaigns(true));

		return () => {
			activeTableURLParams.unsubscribe();
			scheduledTableURLParams.unsubscribe();
			completedTableURLParams.unsubscribe();
		};
	});

	const refresh = async (showLoading = true) => {
		isLoading = true;
		try {
			await Promise.all([
				refreshStats(),
				refreshActiveCampaigns(showLoading),
				refreshScheduledCampaigns(showLoading),
				refreshCompletedCampaigns(showLoading)
			]);
		} finally {
			isLoading = false;
		}
	};

	const refreshStats = async () => {
		try {
			const res = await api.campaign.getStats(contextCompanyID, {
				includeTest: includeTestCampaigns
			});
			if (!res.success) {
				throw res.error;
			}
			stats = res.data;
		} catch (e) {
			addToast('Failed to load campaign counts', 'Error');
			console.error('failed to load campaign counts', e);
		}
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
</script>

<HeadTitle title="Dashboard - Campaigns" />
<main>
	<Headline docSlug="dashboard">Dashboard</Headline>

	<DashboardNav />

	<DashboardToolbar
		{isLoading}
		allCompanies={!contextCompanyID}
		pageId="dashboard-campaigns"
		onRefresh={() => refresh(false)}
		on:change={() => refresh(true)}
	/>

	<StatTiles {tiles} class="mt-4" />

	<section id="active" class="mt-8 scroll-mt-28">
	<SubHeadline>Active campaigns</SubHeadline>
	<div class="mt-4">
		<Table
			reserveSelect
			isGhost={isActiveCampaignsLoading}
			columns={[
				{ column: 'Name', size: 'large' },
				...(isPhone ? [] : [{ column: 'Status', size: 'medium' }]),
				...(showCompanyColumn ? [{ column: 'Company', size: 'large' }] : []),
				...(isPhone
					? []
					: [
							{ title: 'Delivery started', column: 'Send start at', size: 'medium' },
							{ title: 'Delivery finishes', column: 'Send end at', size: 'medium' }
						])
			]}
			hasData={!!activeCampaigns.length}
			hasNextPage={activeCampaignsHasNextPage}
			plural="active campaigns"
			pagination={activeTableURLParams}
			hasActions={false}
		>
			{#each activeCampaigns as campaign}
				<TableRow>
					<TableCell>
						<div class="min-w-0 max-sm:py-1.5">
							<span class="flex min-w-0 items-center gap-2 py-1">
								<a href={`/campaign/${campaign.id}`} class="pc-cell-link min-w-0 truncate">
									{campaign.name}
								</a>
								{#if campaign.isTest}
									<TestLabel />
								{/if}
							</span>
							<!-- phones show the status, company and delivery times here instead of in columns -->
							<span class="flex sm:hidden min-w-0 items-center gap-2 text-xs"
								>{#if campaign.pausedAt}<span data-paused-badge
										><StatusBadge tone="warning" title="Sending paused">Paused</StatusBadge></span
									>{:else}<StatusBadge tone={activeStatus(campaign).tone}
										>{activeStatus(campaign).label}</StatusBadge
									>{/if}<span class="min-w-0 truncate text-xs text-gray-500 dark:text-gray-400"
									>{#if !contextCompanyID && campaign.company?.name}{campaign.company.name}{#if campaign.sendStartAt || campaign.pausedAt}{' · '}{/if}{/if}{#if campaign.sendStartAt}{new Date(campaign.sendStartAt).getTime() > relativeNow ? 'Starts' : 'Started'} {shortRelative(campaign.sendStartAt, relativeNow)}{#if campaign.sendEndAt}{' · ends '}{shortRelative(campaign.sendEndAt, relativeNow)}{/if}{:else if campaign.pausedAt}Self managed{/if}</span
								></span
							>
						</div>
					</TableCell>
					{#if !isPhone}
						<TableCell>
							<!-- paused is a status word like Active, as in the campaign list -->
							{#if campaign.pausedAt}
								<span data-paused-badge
									><StatusBadge tone="warning" title="Sending paused">Paused</StatusBadge></span
								>
							{:else}
								<StatusBadge tone={activeStatus(campaign).tone}>{activeStatus(campaign).label}</StatusBadge>
							{/if}
						</TableCell>
					{/if}
					{#if showCompanyColumn}
						<TableCell value={campaign.company?.name}
							><span class="text-slate-400 dark:text-gray-500">Shared</span></TableCell
						>
					{/if}
					{#if isPhone}
						<!-- the subline in the name cell shows the delivery times on phones -->
					{:else if campaign.sendStartAt}
						<TableCell>
							<span title={new Date(campaign.sendStartAt).toLocaleString()}
								>{shortRelative(campaign.sendStartAt, relativeNow)}</span
							>
						</TableCell>
					{:else}
						<TableCell><span class="text-slate-400 dark:text-gray-500">Self managed</span></TableCell>
					{/if}
					{#if isPhone}
						<!-- shown in the subline -->
					{:else if campaign.sendEndAt}
						<TableCell>
							<span title={new Date(campaign.sendEndAt).toLocaleString()}
								>{shortRelative(campaign.sendEndAt, relativeNow)}</span
							>
						</TableCell>
					{:else}
						<TableCell />
					{/if}
				</TableRow>
			{/each}
		</Table>
	</div>

	</section>

	<section id="scheduled" class="mt-8 scroll-mt-28">
	<SubHeadline>Scheduled campaigns</SubHeadline>
	<div class="mt-4">
		<Table
			reserveSelect
			isGhost={isScheduledCampaignsLoading}
			columns={[
				{ column: 'Name', size: 'large' },
				...(showCompanyColumn ? [{ column: 'Company', size: 'large' }] : []),
				...(isPhone
					? []
					: [
							{ title: 'Delivery starts', column: 'Send start at', size: 'medium' },
							{ title: 'Delivery finishes', column: 'Send end at', size: 'medium' }
						])
			]}
			hasData={!!scheduledCampaigns.length}
			hasNextPage={scheduledCampaignsHasNextPage}
			plural="scheduled campaigns"
			pagination={scheduledTableURLParams}
			hasActions={false}
		>
			{#each scheduledCampaigns as campaign}
				<TableRow>
					<TableCell>
						<div class="min-w-0 max-sm:py-1.5">
							<span class="flex min-w-0 items-center gap-2 py-1">
								<a href={`/campaign/${campaign.id}`} class="pc-cell-link min-w-0 truncate">
									{campaign.name}
								</a>
								{#if campaign.isTest}
									<TestLabel />
								{/if}
							</span>
							<!-- phones show the company and delivery times here instead of in columns -->
							<span class="block sm:hidden text-xs text-gray-500 dark:text-gray-400"
								>{#if !contextCompanyID && campaign.company?.name}{campaign.company.name} · {/if}{#if campaign.sendStartAt}Starts {shortRelative(campaign.sendStartAt, relativeNow)}{#if campaign.sendEndAt}{' · ends '}{shortRelative(campaign.sendEndAt, relativeNow)}{/if}{:else}Self managed{/if}</span
							>
						</div>
					</TableCell>
					{#if showCompanyColumn}
						<TableCell value={campaign.company?.name}
							><span class="text-slate-400 dark:text-gray-500">Shared</span></TableCell
						>
					{/if}
					{#if isPhone}
						<!-- the subline in the name cell shows the delivery times on phones -->
					{:else if campaign.sendStartAt}
						<TableCell>
							<span title={new Date(campaign.sendStartAt).toLocaleString()}
								>{shortRelative(campaign.sendStartAt, relativeNow)}</span
							>
						</TableCell>
					{:else}
						<TableCell><span class="text-slate-400 dark:text-gray-500">Self managed</span></TableCell>
					{/if}
					{#if isPhone}
						<!-- shown in the subline -->
					{:else if campaign.sendEndAt}
						<TableCell>
							<span title={new Date(campaign.sendEndAt).toLocaleString()}
								>{shortRelative(campaign.sendEndAt, relativeNow)}</span
							>
						</TableCell>
					{:else}
						<TableCell />
					{/if}
				</TableRow>
			{/each}
		</Table>
	</div>

	</section>

	<section id="completed" class="mt-8 scroll-mt-28">
	<SubHeadline>Completed campaigns</SubHeadline>
	<div class="mt-4">
		<Table
			reserveSelect
			isGhost={isCompletedCampaignsLoading}
			columns={[
				{ column: 'Name', size: 'large' },
				...(showCompanyColumn ? [{ column: 'Company', size: 'large' }] : []),
				...(isPhone
					? []
					: [
							{ title: 'Delivery started', column: 'Send start at', size: 'medium' },
							{ title: 'Delivery finished', column: 'Send end at', size: 'medium' }
						])
			]}
			hasData={!!completedCampaigns.length}
			hasNextPage={completedCampaignsHasNextPage}
			plural="completed campaigns"
			pagination={completedTableURLParams}
			hasActions={false}
		>
			{#each completedCampaigns as campaign}
				<TableRow>
					<TableCell>
						<div class="min-w-0 max-sm:py-1.5">
							<span class="flex min-w-0 items-center gap-2 py-1">
								<a href={`/campaign/${campaign.id}`} class="pc-cell-link min-w-0 truncate">
									{campaign.name}
								</a>
								{#if campaign.isTest}
									<TestLabel />
								{/if}
							</span>
							<!-- phones show the company and delivery times here instead of in columns -->
							<span class="block sm:hidden text-xs text-gray-500 dark:text-gray-400"
								>{#if !contextCompanyID && campaign.company?.name}{campaign.company.name} · {/if}{#if campaign.sendStartAt && campaign.sendEndAt}Started {shortRelative(campaign.sendStartAt, relativeNow)} · finished {shortRelative(campaign.sendEndAt, relativeNow)}{:else if campaign.sendEndAt}Finished {shortRelative(campaign.sendEndAt, relativeNow)}{:else if campaign.sendStartAt}Started {shortRelative(campaign.sendStartAt, relativeNow)}{:else}Self managed{/if}</span
							>
						</div>
					</TableCell>
					{#if showCompanyColumn}
						<TableCell value={campaign.company?.name}
							><span class="text-slate-400 dark:text-gray-500">Shared</span></TableCell
						>
					{/if}
					{#if isPhone}
						<!-- the subline in the name cell shows the delivery times on phones -->
					{:else if campaign.sendStartAt}
						<TableCell>
							<span title={new Date(campaign.sendStartAt).toLocaleString()}
								>{shortRelative(campaign.sendStartAt, relativeNow)}</span
							>
						</TableCell>
					{:else}
						<TableCell><span class="text-slate-400 dark:text-gray-500">Self managed</span></TableCell>
					{/if}
					{#if isPhone}
						<!-- shown in the subline -->
					{:else if campaign.sendEndAt}
						<TableCell>
							<span title={new Date(campaign.sendEndAt).toLocaleString()}
								>{shortRelative(campaign.sendEndAt, relativeNow)}</span
							>
						</TableCell>
					{:else}
						<TableCell />
					{/if}
				</TableRow>
			{/each}
		</Table>
	</div>
	</section>
</main>
