<script>
	import Headline from '$lib/components/Headline.svelte';
	import HeadTitle from '$lib/components/HeadTitle.svelte';
	import SubHeadline from '$lib/components/SubHeadline.svelte';
	import { AppStateService } from '$lib/service/appState';
	import { api } from '$lib/api/apiProxy.js';
	import { onMount } from 'svelte';
	import { showIsLoading, hideIsLoading } from '$lib/store/loading.js';
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
	import StatsCard from '$lib/components/StatsCard.svelte';
	import CampaignCalender from '$lib/components/CampaignCalendar.svelte';
	import CampaignTrendChart from '$lib/components/CampaignTrendChart.svelte';
	import { fetchAllRows } from '$lib/utils/api-utils';

	// services
	const appStateService = AppStateService.instance;

	// local state
	let contextCompanyID = null;
	let contextCompanyName = '';
	let completedTableURLParams = newTableURLParams({
		prefix: 'completed',
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
	let activeTableURLParams = newTableURLParams({
		prefix: 'active',
		sortBy: 'send_start_at',
		sortOrder: 'desc',
		noScroll: true
	});

	let isActiveCampaignsLoading = false;
	let isUpcomingCampaignsLoading = false;
	let isFinishedCampaignsLoading = false;

	let active = 0;
	let scheduled = 0;
	let finished = 0;
	let repeatOffenders = 0;

	let calendarCampaigns = [];
	let activeCampaigns = [];
	let scheduledCampaigns = [];
	let completedCampaigns = [];
	let campaignStats = [];
	let isCampaignStatsLoading = false;

	let calendarStartDate = null;
	let calendarEndDate = null;

	// hooks
	onMount(() => {
		const context = appStateService.getContext();
		if (context) {
			contextCompanyID = context.companyID;
			contextCompanyName = context.companyName;
		}
		refresh();
		activeTableURLParams.onChange(refreshActiveCampaigns);
		scheduledTableURLParams.onChange(refreshScheduledCampaigns);
		completedTableURLParams.onChange(refreshFinishedCampaigns);

		return () => {
			activeTableURLParams.unsubscribe();
			scheduledTableURLParams.unsubscribe();
			completedTableURLParams.unsubscribe();
		};
	});

	const refresh = async (showLoading = true) => {
		try {
			if (showLoading) {
				showIsLoading();
			}
			let res = await api.campaign.getStats(contextCompanyID);
			if (!res.success) {
				throw res.error;
			}
			await refreshRepeatOffenders();

			active = res.data.active;
			scheduled = res.data.upcoming;
			finished = res.data.finished;
			await refreshCalendarCampaings();
			await refreshActiveCampaigns(showLoading);
			await refreshScheduledCampaigns(showLoading);
			await refreshFinishedCampaigns(showLoading);
			await refreshCampaignStats(showLoading);
		} catch (e) {
			addToast('Failed to load data', 'Error');
		} finally {
			if (showLoading) {
				hideIsLoading();
			}
		}
	};

	const refreshCalendarCampaings = async () => {
		if (!calendarStartDate || !calendarEndDate) {
			return [];
		}

		try {
			const rows = await fetchAllRows((options) => {
				const a = api.campaign.getWithinDates(
					calendarStartDate.toISOString(),
					calendarEndDate.toISOString(),
					options,
					contextCompanyID
				);
				return a;
			});
			calendarCampaigns = rows;
		} catch (e) {
			addToast('Failed to load calendar campaigns', 'Error');
			console.error('Failed to load calendar campaigns', e);
		} finally {
		}
	};

	const refreshActiveCampaigns = async (showLoading = true) => {
		if (showLoading) {
			isActiveCampaignsLoading = true;
		}
		try {
			const res = await api.campaign.getAllActive(activeTableURLParams, contextCompanyID);
			if (!res.success) {
				throw res.error;
			}
			activeCampaigns = res.data.rows;
		} catch (e) {
			addToast('Failed to load active campaigns', 'Error');
			console.error('Failed to load active campaigns', e);
		} finally {
			if (showLoading) {
				isActiveCampaignsLoading = false;
			}
		}
	};

	const refreshScheduledCampaigns = async (showLoading = true) => {
		if (showLoading) {
			isUpcomingCampaignsLoading = true;
		}
		try {
			const res = await api.campaign.getAllUpcoming(scheduledTableURLParams, contextCompanyID);
			if (!res.success) {
				throw res.error;
			}
			scheduledCampaigns = res.data.rows;
		} catch (e) {
			addToast('Failed to load scheduled campaigns', 'Error');
			console.error('Failed to load scheduled campaigns', e);
		} finally {
			if (showLoading) {
				isUpcomingCampaignsLoading = false;
			}
		}
	};

	const refreshFinishedCampaigns = async (showLoading = true) => {
		if (showLoading) {
			isFinishedCampaignsLoading = true;
		}
		try {
			const res = await api.campaign.getAllFinished(completedTableURLParams, contextCompanyID);
			if (!res.success) {
				throw res.error;
			}
			completedCampaigns = res.data.rows;
		} catch (e) {
			addToast('Failed to load finshed campaigns', 'Error');
			console.error('Failed to load finshed campaigns', e);
		} finally {
			if (showLoading) {
				isFinishedCampaignsLoading = false;
			}
		}
	};

	const refreshRepeatOffenders = async () => {
		try {
			const res = await api.recipient.countRepeatOffenders(contextCompanyID);
			if (!res.success) {
				throw res.error;
			}
			repeatOffenders = res.data;
		} catch (e) {
			addToast('Failed to load repeat offenders', 'Error');
			console.error('Failed to load repeat offenders', e);
		}
	};

	const refreshCampaignStats = async (showLoading = true) => {
		if (showLoading) {
			isCampaignStatsLoading = true;
		}
		try {
			const statsParams = newTableURLParams({
				sortBy: 'campaign_closed_at',
				sortOrder: 'desc',
				perPage: 10
			});
			const res = await api.campaign.getAllCampaignStats(statsParams, contextCompanyID);
			if (!res.success) {
				throw res.error;
			}
			campaignStats = res.data.rows || [];
		} catch (e) {
			addToast('Failed to load campaign statistics', 'Error');
			console.error('Failed to load campaign statistics', e);
		} finally {
			if (showLoading) {
				isCampaignStatsLoading = false;
			}
		}
	};

	/** @param {string} id */
	const onClickViewCampaign = (id) => {
		goto(`/campaign/${id}`);
	};
</script>

<HeadTitle title="Dashboard" />
<main>
	<div class="flex justify-between">
		<Headline>Dashboard</Headline>
		<AutoRefresh
			isLoading={false}
			onRefresh={async () => {
				await refresh(false);
			}}
		/>
	</div>
	{#if contextCompanyName}
		<SubHeadline>{contextCompanyName}</SubHeadline>
	{/if}

	<div class="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-4 gap-6 mb-8 mt-4">
		<a href="/campaign">
			<StatsCard
				title="Active Campaigns"
				value={active}
				borderColor="border-campaign-active"
				iconColor="text-campaign-active"
			>
				<svg
					slot="icon"
					xmlns="http://www.w3.org/2000/svg"
					class="h-5 w-5 ml-2 text-campaign-active"
					fill="none"
					viewBox="0 0 24 24"
					stroke="currentColor"
				>
					<path
						stroke-linecap="round"
						stroke-linejoin="round"
						stroke-width="2"
						d="M13 10V3L4 14h7v7l9-11h-7z"
					/>
				</svg>
			</StatsCard>
		</a>

		<a href="/campaign">
			<StatsCard
				title="Scheduled Campaigns"
				value={scheduled}
				borderColor="border-campaign-scheduled"
				iconColor="text-campaign-scheduled"
			>
				<svg
					slot="icon"
					xmlns="http://www.w3.org/2000/svg"
					class="h-5 w-5 ml-2 text-campaign-scheduled"
					fill="none"
					viewBox="0 0 24 24"
					stroke="currentColor"
				>
					<path
						stroke-linecap="round"
						stroke-linejoin="round"
						stroke-width="2"
						d="M8 7V3m8 4V3m-9 8h10M5 21h14a2 2 0 002-2V7a2 2 0 00-2-2H5a2 2 0 00-2 2v12a2 2 0 002 2z"
					/>
				</svg>
			</StatsCard>
		</a>

		<a href="/campaign">
			<StatsCard
				title="Completed Campaigns"
				value={finished}
				borderColor="border-campaign-completed"
				iconColor="text-campaign-completed"
			>
				<svg
					slot="icon"
					xmlns="http://www.w3.org/2000/svg"
					class="h-5 w-5 ml-2 text-campaign-completed"
					fill="none"
					viewBox="0 0 24 24"
					stroke="currentColor"
				>
					<path
						stroke-linecap="round"
						stroke-linejoin="round"
						stroke-width="2"
						d="M9 12l2 2 4-4m6 2a9 9 0 11-18 0 9 9 0 0118 0z"
					/>
				</svg>
			</StatsCard>
		</a>

		<a href="/recipient">
			<StatsCard
				title="Repeat Offenders"
				value={repeatOffenders}
				borderColor="border-repeart-submissions"
				iconColor="text-repeart-submissions"
			>
				<svg
					slot="icon"
					xmlns="http://www.w3.org/2000/svg"
					class="h-5 w-5 ml-2 text-repeart-submissions"
					fill="none"
					viewBox="0 0 24 24"
					stroke="currentColor"
				>
					<path
						stroke-linecap="round"
						stroke-linejoin="round"
						stroke-width="2"
						d="M12 9v2m0 4h.01m-6.938 4h13.856c1.54 0 2.502-1.667 1.732-3L13.732 4c-.77-1.333-2.694-1.333-3.464 0L3.34 16c-.77 1.333.192 3 1.732 3z"
					/>
				</svg>
			</StatsCard>
		</a>
	</div>

	<SubHeadline>Campaign Trends</SubHeadline>
	<div class="mb-8 w-full min-h-[300px]">
		<CampaignTrendChart {campaignStats} isLoading={isCampaignStatsLoading} />
	</div>

	<SubHeadline>Calendar</SubHeadline>
	<div class="mb-8 min-h-[600px]">
		<CampaignCalender
			campaigns={calendarCampaigns}
			onChangeDate={refreshCalendarCampaings}
			bind:start={calendarStartDate}
			bind:end={calendarEndDate}
		/>
	</div>

	<SubHeadline>Recent Campaigns</SubHeadline>
	<div class="min-h-[300px] mb-8">
		<Table
			isGhost={isCampaignStatsLoading}
			columns={[
				{ column: 'Campaign', size: 'large' },
				{ column: 'Template', size: 'medium' },
				{ column: 'Recipients', size: 'small' },
				{ column: 'Open Rate', size: 'small' },
				{ column: 'Click Rate', size: 'small' },
				{ column: 'Submission Rate', size: 'small' },
				{ column: 'Closed', size: 'small' }
			]}
			hasData={!!campaignStats.length}
			plural="campaign statistics"
			hasActions={false}
		>
			{#each campaignStats as stat}
				<TableRow>
					<TableCell>
						<a href={`/campaign/${stat.campaignId}`}>
							{stat.campaignName}
						</a>
					</TableCell>
					<TableCell value={stat.templateName} />
					<TableCell value={stat.totalRecipients} />
					<TableCell value="{Math.round(stat.openRate)}%" />
					<TableCell value="{Math.round(stat.clickRate)}%" />
					<TableCell value="{Math.round(stat.submissionRate)}%" />
					<TableCell value={stat.campaignClosedAt} isDate isRelative />
				</TableRow>
			{/each}
		</Table>
	</div>
	<SubHeadline>Active campaigns</SubHeadline>
	<div class="min-h-[300px] mb-8">
		<Table
			isGhost={isActiveCampaignsLoading}
			columns={[
				{ column: 'Name', size: 'large' },
				{ column: 'Company', size: 'medium' },
				{ title: 'Delivery started', column: 'Send start at', size: 'small' },
				{ title: 'Delivery finished', column: 'Send end at', size: 'small' }
			]}
			hasData={!!activeCampaigns.length}
			plural="active campaigns"
			pagination={activeTableURLParams}
		>
			{#each activeCampaigns as campaign}
				<TableRow>
					<TableCell>
						{#if campaign.isTest}
							<TestLabel />
						{/if}

						<a href={`/campaign/${campaign.id}`}>
							{campaign.name}
						</a>
					</TableCell>
					<TableCell value={campaign.company?.name} />
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
			isGhost={isUpcomingCampaignsLoading}
			columns={[
				{ column: 'Name', size: 'large' },
				{ column: 'Company', size: 'medium' },
				{ title: 'Delivery started', column: 'Send start at', size: 'small' },
				{ title: 'Delivery finished', column: 'Send end at', size: 'small' }
			]}
			hasData={!!scheduledCampaigns.length}
			plural="scheduled campaigns"
			pagination={scheduledTableURLParams}
		>
			{#each scheduledCampaigns as campaign}
				<TableRow>
					<TableCell>
						{#if campaign.isTest}
							<TestLabel />
						{/if}

						<a href={`/campaign/${campaign.id}`}>
							{campaign.name}
						</a>
					</TableCell>
					<TableCell value={campaign.company?.name} />
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
			isGhost={isFinishedCampaignsLoading}
			columns={[
				{ column: 'Name', size: 'large' },
				{ column: 'Company', size: 'medium' },
				{ title: 'Delivery started', column: 'Send start at', size: 'small' },
				{ title: 'Delivery finished', column: 'Send end at', size: 'small' }
			]}
			hasData={!!completedCampaigns.length}
			plural="completed campaigns"
			pagination={completedTableURLParams}
		>
			{#each completedCampaigns as campaign}
				<TableRow>
					<TableCell>
						{#if campaign.isTest}
							<TestLabel />
						{/if}

						<a href={`/campaign/${campaign.id}`}>
							{campaign.name}
						</a>
					</TableCell>
					<TableCell value={campaign.company?.name} />
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
