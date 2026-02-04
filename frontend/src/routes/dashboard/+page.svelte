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
	import CheckboxField from '$lib/components/CheckboxField.svelte';
	import { fetchAllRows } from '$lib/utils/api-utils';
	import { tick } from 'svelte';

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
	let finishedCustomStats = 0;

	let calendarCampaigns = [];
	let activeCampaigns = [];
	let activeCampaignsHasNextPage = true;
	let scheduledCampaigns = [];
	let scheduledCampaignsHasNextPage = true;
	let completedCampaigns = [];
	let completedCampaignsHasNextPage = true;
	let campaignStats = [];
	let isCampaignStatsLoading = false;

	let calendarStartDate = null;
	let calendarEndDate = null;

	// Toggle for including test campaigns
	let includeTestCampaigns = false;

	// Use consistent colors with campaign detail page - these are already well-suited for both light and dark modes

	// Handler for when toggle changes
	const handleToggleChange = async () => {
		// Wait for binding to update
		await tick();
		// Refresh all data with new toggle state
		await refresh(false);
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
		completedTableURLParams.onChange(() => refreshFinishedCampaigns(true));

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
			let res = await api.campaign.getStats(contextCompanyID, {
				includeTest: includeTestCampaigns
			});
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
					{ ...options, includeTest: includeTestCampaigns },
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
			const options = {
				page: activeTableURLParams.currentPage,
				perPage: activeTableURLParams.perPage,
				sortBy: activeTableURLParams.sortBy,
				sortOrder: activeTableURLParams.sortOrder,
				search: activeTableURLParams.search,
				includeTest: includeTestCampaigns
			};
			const res = await api.campaign.getAllActive(options, contextCompanyID);
			if (!res.success) {
				throw res.error;
			}
			activeCampaigns = res.data.rows;
			activeCampaignsHasNextPage = res.data.hasNextPage;
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
			const options = {
				page: scheduledTableURLParams.currentPage,
				perPage: scheduledTableURLParams.perPage,
				sortBy: scheduledTableURLParams.sortBy,
				sortOrder: scheduledTableURLParams.sortOrder,
				search: scheduledTableURLParams.search,
				includeTest: includeTestCampaigns
			};
			const res = await api.campaign.getAllUpcoming(options, contextCompanyID);
			if (!res.success) {
				throw res.error;
			}
			scheduledCampaigns = res.data.rows;
			scheduledCampaignsHasNextPage = res.data.hasNextPage;
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
			const options = {
				page: completedTableURLParams.currentPage,
				perPage: completedTableURLParams.perPage,
				sortBy: completedTableURLParams.sortBy,
				sortOrder: completedTableURLParams.sortOrder,
				search: completedTableURLParams.search,
				includeTest: includeTestCampaigns
			};
			const res = await api.campaign.getAllFinished(options, contextCompanyID);
			if (!res.success) {
				throw res.error;
			}
			completedCampaigns = res.data.rows;
			completedCampaignsHasNextPage = res.data.hasNextPage;
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
			const res = await api.campaign.getAllCampaignStats(contextCompanyID);
			if (!res.success) {
				throw res.error;
			}
			campaignStats = res.data.rows || [];
			// stats without a campaign ID is custom stats
			finishedCustomStats = res.data.rows.filter((c) => !c.campaignId).length;
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
		<div class="flex gap-4 items-center">
			<CheckboxField
				bind:value={includeTestCampaigns}
				on:change={handleToggleChange}
				id="includeTestCampaigns"
				inline={true}
			>
				Include test campaigns
			</CheckboxField>

			<AutoRefresh
				isLoading={false}
				onRefresh={async () => {
					// refresh all data
					let res = await api.campaign.getStats(contextCompanyID, {
						includeTest: includeTestCampaigns
					});
					if (!res.success) {
						throw res.error;
					}
					await refreshRepeatOffenders();

					active = res.data.active;
					scheduled = res.data.upcoming;
					finished = res.data.finished;

					// refresh table data directly like campaign page does
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
						activeCampaigns = [];
						await tick();
						activeCampaigns = activeRes.data.rows;
					}

					const scheduledOptions = {
						page: scheduledTableURLParams.currentPage,
						perPage: scheduledTableURLParams.perPage,
						sortBy: scheduledTableURLParams.sortBy,
						sortOrder: scheduledTableURLParams.sortOrder,
						search: scheduledTableURLParams.search,
						includeTest: includeTestCampaigns
					};
					const scheduledRes = await api.campaign.getAllUpcoming(
						scheduledOptions,
						contextCompanyID
					);
					if (scheduledRes.success) {
						scheduledCampaigns = [];
						await tick();
						scheduledCampaigns = scheduledRes.data.rows;
					}

					const completedOptions = {
						page: completedTableURLParams.currentPage,
						perPage: completedTableURLParams.perPage,
						sortBy: completedTableURLParams.sortBy,
						sortOrder: completedTableURLParams.sortOrder,
						search: completedTableURLParams.search,
						includeTest: includeTestCampaigns
					};
					const completedRes = await api.campaign.getAllFinished(
						completedOptions,
						contextCompanyID
					);
					if (completedRes.success) {
						completedCampaigns = [];
						await tick();
						completedCampaigns = completedRes.data.rows;
					}

					const statsRes = await api.campaign.getAllCampaignStats(contextCompanyID);
					if (statsRes.success) {
						campaignStats = [];
						await tick();
						campaignStats = statsRes.data.rows || [];
					}

					await refreshCalendarCampaings();
				}}
			/>
		</div>
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
				value={finished > 0 ? finished : finishedCustomStats}
				borderColor="border-message-read"
				iconColor="text-message-read"
			>
				<svg
					slot="icon"
					xmlns="http://www.w3.org/2000/svg"
					class="h-5 w-5 ml-2 text-message-read"
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
				borderColor="border-submitted-data"
				iconColor="text-submitted-data"
			>
				<svg
					slot="icon"
					xmlns="http://www.w3.org/2000/svg"
					class="h-5 w-5 ml-2 text-submitted-data"
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

	<SubHeadline>{contextCompanyName ? 'Campaign Trends' : 'Shared Campaign Trends'}</SubHeadline>
	<div class="mb-8 w-full min-h-[300px]">
		<CampaignTrendChart {campaignStats} isLoading={isCampaignStatsLoading} />
	</div>

	<SubHeadline>{contextCompanyName ? 'Calendar' : 'Shared Calendar'}</SubHeadline>
	<div class="mb-8 min-h-[600px]">
		<CampaignCalender
			campaigns={calendarCampaigns}
			onChangeDate={refreshCalendarCampaings}
			bind:start={calendarStartDate}
			bind:end={calendarEndDate}
			showCompany={!contextCompanyID}
		/>
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
