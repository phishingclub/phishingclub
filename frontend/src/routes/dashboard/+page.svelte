<script>
	import Headline from '$lib/components/Headline.svelte';
	import HeadTitle from '$lib/components/HeadTitle.svelte';
	import SubHeadline from '$lib/components/SubHeadline.svelte';
	import { AppStateService } from '$lib/service/appState';
	import { api } from '$lib/api/apiProxy.js';
	import { onMount } from 'svelte';
	import { get } from 'svelte/store';
	import { showIsLoading, hideIsLoading } from '$lib/store/loading.js';
	import { addToast } from '$lib/store/toast';
	import StatTiles from '$lib/components/StatTiles.svelte';
	import CampaignCalender from '$lib/components/CampaignCalendar.svelte';
	import CampaignTrendChart from '$lib/components/CampaignTrendChart.svelte';
	import { fetchAllRows } from '$lib/utils/api-utils';
	import { goto } from '$app/navigation';
	import DashboardNav from '$lib/components/DashboardNav.svelte';
	import DashboardToolbar from '$lib/components/DashboardToolbar.svelte';
	import { dashboardIncludeTest } from '$lib/store/dashboardIncludeTest.js';
	import { plural } from '$lib/utils/programText.js';
	import { displayMode, showsSimulation } from '$lib/store/displayMode';

	// services
	const appStateService = AppStateService.instance;

	// local state
	let contextCompanyID = null;

	// campaign counts from the stats endpoint, null until loaded
	let stats = null;
	let repeatOffenders = 0;
	let repeatOffenderMonths = 12;
	// program counts per state, null until loaded
	let programStats = null;

	let calendarCampaigns = [];
	let calendarPrograms = [];
	let campaignStats = [];

	// training completed across closed training campaigns, from the campaign
	// stats snapshots already loaded for the trend chart
	$: trainingCompleted = (campaignStats || [])
		.filter((s) => s.isTraining)
		.reduce((sum, s) => sum + (s.trainingCompleted || 0), 0);
	let isCampaignStatsLoading = true; // start as true to show ghost on initial load
	let isLoading = false;

	let calendarStartDate = null;
	let calendarEndDate = null;

	// the inbox settings: the company Reporting tab or the global inbox in Settings
	$: reportInboxHref = contextCompanyID ? `/company/${contextCompanyID}#reporting` : '/settings#reporting';

	// what needs a look, shown only when there is something
	$: attention = [
		stats?.paused > 0 && {
			key: 'paused',
			tone: 'warning',
			text: `${plural(stats.paused, 'campaign')} paused`,
			href: '/dashboard/campaigns/#active'
		},
		showPrograms && programStats?.needsAttention > 0 && {
			key: 'programs',
			tone: 'warning',
			text: `${plural(programStats.needsAttention, 'program')} ${programStats.needsAttention === 1 ? 'needs' : 'need'} attention`,
			href: '/dashboard/programs/#attention'
		},
		stats?.failedSending > 0 && {
			key: 'failed',
			tone: 'danger',
			text: `${plural(stats.failedSending, 'failed send')} in the last ${plural(stats.failedSendingDays, 'day')}`,
			href: '/dashboard/events/?eventType=campaign_recipient_message_failed'
		},
		stats?.reportInboxErrors > 0 && {
			key: 'inbox',
			tone: 'danger',
			text:
				stats.reportInboxErrors === 1
					? 'Report inbox error'
					: `${stats.reportInboxErrors} report inbox errors`,
			href: reportInboxHref
		}
	].filter(Boolean);

	$: activeTip = stats
		? `Started and not closed, paused campaigns left out.${stats.selfManaged ? ` ${plural(stats.selfManaged, 'is', 'are')} self managed, with no delivery window, and stay active until closed.` : ''}`
		: '';

	$: repeatTip =
		`Clicked a link or submitted data on 2 or more sends within ${plural(repeatOffenderMonths || 12, 'month')}, the repeat offender setting. Test campaigns never count.` +
		(contextCompanyID ? '' : ' The recipient list shows the recipients without a company.');

	// the recipient list sorted on its Repeat column
	const repeatOffendersHref = `/recipient?sortBy=${encodeURIComponent('Repeat offender')}&sortOrder=desc`;

	// programs, repeat offenders and training are awareness concepts, shown
	// only in the modes that show phishing simulation
	$: showPrograms = showsSimulation($displayMode);

	$: tiles = [
		{
			key: 'active',
			title: 'Active campaigns',
			tip: activeTip,
			value: stats?.active ?? 0,
			color: 'text-campaign-active',
			icon: 'active',
			href: '/dashboard/campaigns/#active'
		},
		{
			key: 'scheduled',
			title: 'Scheduled campaigns',
			value: stats?.upcoming ?? 0,
			color: 'text-campaign-scheduled',
			icon: 'scheduled',
			href: '/dashboard/campaigns/#scheduled'
		},
		{
			key: 'completed',
			title: 'Completed campaigns',
			value: stats?.finished ?? 0,
			color: 'text-campaign-completed',
			icon: 'completed',
			href: '/dashboard/campaigns/#completed'
		},
		{
			key: 'programs',
			title: 'Active programs',
			tip: 'Running programs, those that need attention left out.',
			value: programStats?.active ?? 0,
			color: 'text-campaign-active',
			// the Programs icon, so it differs from the Active campaigns tile beside it
			icon: 'programs',
			href: '/dashboard/programs/#active'
		},
		{
			key: 'repeat',
			title: 'Repeat offenders',
			tip: repeatTip,
			value: repeatOffenders,
			color: 'text-repeat-offenders',
			icon: 'people',
			href: repeatOffendersHref
		},
		{
			key: 'training',
			title: 'Training completed',
			tip: 'Recipients who completed the training of a closed training campaign. Test campaigns never count.',
			value: trainingCompleted,
			color: 'text-training-completed',
			icon: 'training',
			href: '/dashboard/campaigns/#completed'
		}
	].filter((tile) =>
		// red team mode shows what runs, what is next and what is done
		showPrograms
			? tile.key !== 'completed'
			: !['programs', 'repeat', 'training'].includes(tile.key)
	);

	// hooks
	onMount(() => {
		const context = appStateService.getContext();
		if (context) {
			contextCompanyID = context.companyID;
		}
		refresh();
	});

	// loads everything at once and shows one error for any part that fails.
	// A refresh asked for while one runs is done when it ends.
	let refreshPending = false;
	let monthsRead = false;
	let campaignStatsKey = '';
	const refresh = async (showLoading = true) => {
		if (isLoading) {
			refreshPending = true;
			return;
		}
		isLoading = true;
		if (showLoading) {
			showIsLoading();
			isCampaignStatsLoading = true;
		}
		const includeTest = get(dashboardIncludeTest);
		try {
			const [statsRes, repeatRes, monthsRes, programRes, campaignStatsRes] = await Promise.all([
				api.campaign.getStats(contextCompanyID, { includeTest }),
				api.recipient.countRepeatOffenders(contextCompanyID),
				// the setting is read once per visit
				monthsRead ? null : api.option.get('repeat_offender_months'),
				api.program.getStats(contextCompanyID),
				api.campaign.getAllCampaignStats(contextCompanyID),
				loadCalendar()
			]);
			for (const res of [statsRes, repeatRes, programRes, campaignStatsRes]) {
				if (!res.success) throw res.error;
			}
			stats = statsRes.data;
			repeatOffenders = repeatRes.data;
			programStats = programRes.data;
			// the trend chart draws again only when its rows changed
			const rows = campaignStatsRes.data.rows || [];
			const rowsKey = JSON.stringify(rows);
			if (rowsKey !== campaignStatsKey) {
				campaignStatsKey = rowsKey;
				campaignStats = rows;
			}
			// the setting falls back to its default when it cannot be read
			if (monthsRes) {
				const months = Number(monthsRes.data?.value);
				if (monthsRes.success) monthsRead = true;
				if (monthsRes.success && months > 0) repeatOffenderMonths = months;
			}
		} catch (e) {
			addToast('Failed to load the dashboard', 'Error');
			console.error('failed to load the dashboard', e);
		} finally {
			isLoading = false;
			isCampaignStatsLoading = false;
			if (showLoading) {
				hideIsLoading();
			}
		}
		if (refreshPending) {
			refreshPending = false;
			await refresh(false);
		}
	};

	// the campaigns and programs of the calendar range, throws on failure
	const loadCalendar = async () => {
		if (!calendarStartDate || !calendarEndDate) {
			return;
		}
		const start = calendarStartDate.toISOString();
		const end = calendarEndDate.toISOString();
		const includeTest = get(dashboardIncludeTest);
		const [campaigns, programs] = await Promise.all([
			fetchAllRows((options) =>
				api.campaign.getWithinDates(start, end, { ...options, includeTest }, contextCompanyID)
			),
			fetchAllRows((options) => api.program.getWithinDates(start, end, options, contextCompanyID))
		]);
		calendarCampaigns = campaigns;
		calendarPrograms = programs;
	};

	// the calendar moved to another range
	const onCalendarChange = async () => {
		try {
			await loadCalendar();
		} catch (e) {
			addToast('Failed to load the calendar', 'Error');
			console.error('failed to load the calendar', e);
		}
	};
</script>

<HeadTitle title="Dashboard" />
<main>
	<Headline docSlug="dashboard">Dashboard</Headline>

	<DashboardNav />

	<DashboardToolbar
		{isLoading}
		allCompanies={!contextCompanyID}
		pageId="dashboard"
		onRefresh={() => refresh(false)}
		on:change={() => refresh(false)}
	/>

	{#if attention.length}
		<!-- what needs a look, as notices with a bar in the state color, each
		linking to where it is listed -->
		<div class="mt-4 flex flex-wrap items-center gap-3" data-attention>
			{#each attention as item (item.key)}
				<a
					href={item.href}
					class="pc-focus group flex items-center gap-3 rounded-r-md border-l-4 py-2 pl-3 pr-4 text-sm font-semibold text-slate-800 transition-colors duration-200 dark:text-gray-100 {item.tone ===
					'danger'
						? 'border-pc-red bg-pc-red/10'
						: 'border-failed-sending bg-failed-sending/15 dark:bg-failed-sending/10'}"
				>
					<span>{item.text}</span>
					<span class="text-cta-blue underline-offset-2 group-hover:underline dark:text-highlight-blue"
						>View</span
					>
				</a>
			{/each}
		</div>
	{/if}

	<StatTiles {tiles} class="mt-4" />

	<section class="mt-8" data-section="trend">
		<SubHeadline>Campaign Trends</SubHeadline>
		<div class="mt-4 w-full">
			<CampaignTrendChart
				{campaignStats}
				isLoading={isCampaignStatsLoading}
				onCampaignClick={(id) => goto(`/campaign/${id}`)}
			/>
		</div>
	</section>

	<section class="mt-8 mb-8" data-section="calendar">
		<SubHeadline>Calendar</SubHeadline>
		<div class="mt-4">
			<CampaignCalender
				campaigns={calendarCampaigns}
				programs={calendarPrograms}
				bind:start={calendarStartDate}
				bind:end={calendarEndDate}
				onChangeDate={onCalendarChange}
				showCompany={!contextCompanyID}
			/>
		</div>
	</section>
</main>
