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
	import TestLabel from '$lib/components/TestLabel.svelte';
	import AutoRefresh from '$lib/components/AutoRefresh.svelte';
	import { tick } from 'svelte';
	import DashboardNav from '$lib/components/DashboardNav.svelte';
	import { shortRelative } from '$lib/utils/shortRelative.js';

	// services
	const appStateService = AppStateService.instance;

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

	// handler for when toggle changes
	const handleToggleChange = async () => {
		await tick();
		await refresh();
	};

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
			refreshActiveCampaigns(true),
			refreshScheduledCampaigns(true),
			refreshCompletedCampaigns(true)
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
</script>

<HeadTitle title="Dashboard - Campaigns" />
<main>
	<Headline docSlug="dashboard">Dashboard</Headline>

	<DashboardNav />

	<div class="flex flex-wrap justify-between items-center gap-x-6 gap-y-3 mb-4">
		<SubHeadline>Active campaigns</SubHeadline>
		<div class="flex flex-wrap items-center gap-x-6 gap-y-3">
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
						class="peer-focus-visible:ring-2 peer-focus-visible:ring-cta-blue dark:peer-focus-visible:ring-highlight-blue peer-focus-visible:ring-offset-2 dark:peer-focus-visible:ring-offset-gray-900 w-5 h-5 border-2 border-slate-300 dark:border-gray-700/60 rounded
						       peer-checked:border-cta-blue dark:peer-checked:border-highlight-blue/80 peer-checked:bg-cta-blue dark:peer-checked:bg-highlight-blue/80
						       peer-focus:border-slate-400 dark:peer-focus:border-highlight-blue/80 peer-focus:bg-gray-100 dark:peer-focus:bg-gray-700/60
						       transition-all duration-200 ease-in-out
						       flex items-center justify-center
						       bg-slate-50 dark:bg-gray-900/60"
					>
						{#if includeTestCampaigns}
							<svg
								class="w-3 h-3 text-white"
								fill="none"
								stroke="currentColor"
								viewBox="0 0 24 24"
								aria-hidden="true"
							>
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
		</div>
	</div>

	<div class="mb-10">
		<Table
			reserveSelect
			isGhost={isActiveCampaignsLoading}
			columns={[
				{ column: 'Name', size: 'large' },
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
							<span class="flex min-w-0 items-center gap-1 py-1">
								{#if campaign.isTest}
									<TestLabel />
								{/if}
								<a href={`/campaign/${campaign.id}`} class="pc-cell-link min-w-0 truncate">
									{campaign.name}
								</a>
							</span>
							<!-- phones show the company and delivery times here instead of in columns -->
							<span class="block sm:hidden text-xs text-gray-500 dark:text-gray-400"
								>{#if !contextCompanyID && campaign.company?.name}{campaign.company.name} · {/if}{#if campaign.sendStartAt}Started {shortRelative(campaign.sendStartAt, relativeNow)}{#if campaign.sendEndAt}{' · ends '}{shortRelative(campaign.sendEndAt, relativeNow)}{/if}{:else}Self managed{/if}</span
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

	<SubHeadline>Scheduled campaigns</SubHeadline>
	<div class="mb-10">
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
							<span class="flex min-w-0 items-center gap-1 py-1">
								{#if campaign.isTest}
									<TestLabel />
								{/if}
								<a href={`/campaign/${campaign.id}`} class="pc-cell-link min-w-0 truncate">
									{campaign.name}
								</a>
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

	<SubHeadline>Completed campaigns</SubHeadline>
	<div class="mb-10">
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
							<span class="flex min-w-0 items-center gap-1 py-1">
								{#if campaign.isTest}
									<TestLabel />
								{/if}
								<a href={`/campaign/${campaign.id}`} class="pc-cell-link min-w-0 truncate">
									{campaign.name}
								</a>
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
</main>
