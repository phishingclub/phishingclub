<script>
	import Headline from '$lib/components/Headline.svelte';
	import HeadTitle from '$lib/components/HeadTitle.svelte';
	import SubHeadline from '$lib/components/SubHeadline.svelte';
	import { AppStateService } from '$lib/service/appState';
	import { api } from '$lib/api/apiProxy.js';
	import { onMount } from 'svelte';
	import { showIsLoading, hideIsLoading } from '$lib/store/loading.js';
	import { addToast } from '$lib/store/toast';
	import StatsCard from '$lib/components/StatsCard.svelte';
	import CampaignCalender from '$lib/components/CampaignCalendar.svelte';
	import CampaignTrendChart from '$lib/components/CampaignTrendChart.svelte';
	import { fetchAllRows } from '$lib/utils/api-utils';
	import { tick, onDestroy } from 'svelte';
	import TextFieldSelect from '$lib/components/TextFieldSelect.svelte';
	import { autoRefreshStore, setPageAutoRefresh, getPageAutoRefresh } from '$lib/store/autoRefresh';
	import { BiMap } from '$lib/utils/maps';
	import { goto } from '$app/navigation';
	import DashboardNav from '$lib/components/DashboardNav.svelte';
	import { activeFormElement } from '$lib/store/activeFormElement';

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

	let active = 0;
	let scheduled = 0;
	let finished = 0;
	let repeatOffenders = 0;

	let calendarCampaigns = [];
	let campaignStats = [];

	// training completion rate across closed training campaigns, derived from the
	// campaign stats snapshots already loaded for the trend chart
	$: trainingStats = (campaignStats || []).filter((s) => s.isTraining);
	$: trainingStarted = trainingStats.reduce((sum, s) => sum + (s.trainingStarted || 0), 0);
	$: trainingCompleted = trainingStats.reduce((sum, s) => sum + (s.trainingCompleted || 0), 0);
	$: trainingCompletionRate =
		trainingStarted > 0 ? Math.round((trainingCompleted / trainingStarted) * 100) : 0;
	let isCampaignStatsLoading = true; // start as true to show ghost on initial load

	let calendarStartDate = null;
	let calendarEndDate = null;

	let includeTestCampaigns = false;
	let autoRefreshIntervalId = null;

	// handler for when toggle changes
	const handleToggleChange = async () => {
		await tick();
		await refresh(false);
	};

	const handleAutoRefreshChange = (optKey) => {
		const value = Number(autoRefreshOptions.byKey(optKey));
		// batch the update to prevent multiple reactive triggers
		autoRefreshStore.set({
			enabled: value > 0,
			interval: value
		});
		setPageAutoRefresh('dashboard', $autoRefreshStore);
		startAutoRefresh();
	};

	const startAutoRefresh = () => {
		stopAutoRefresh();
		if ($autoRefreshStore.enabled && $autoRefreshStore.interval > 0) {
			autoRefreshIntervalId = setInterval(async () => {
				// skip refresh if disabled or a dropdown is open
				if (!$autoRefreshStore.enabled || $activeFormElement !== null) return;
				await refresh(false);
			}, $autoRefreshStore.interval);
		}
	};

	const stopAutoRefresh = () => {
		if (autoRefreshIntervalId) {
			clearInterval(autoRefreshIntervalId);
			autoRefreshIntervalId = null;
		}
	};

	// hooks
	onMount(() => {
		const context = appStateService.getContext();
		if (context) {
			contextCompanyID = context.companyID;
			contextCompanyName = context.companyName;
		}
		// load saved auto-refresh settings for this page
		const savedSettings = getPageAutoRefresh('dashboard');
		if (savedSettings) {
			autoRefreshStore.set(savedSettings);
		}
		refresh();
		startAutoRefresh();
	});

	onDestroy(() => {
		stopAutoRefresh();
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
		} catch (e) {
			addToast('Failed to load campaign statistics', 'Error');
			console.error('Failed to load campaign statistics', e);
		} finally {
			if (showLoading) {
				isCampaignStatsLoading = false;
			}
		}
	};
</script>

<HeadTitle title="Dashboard" />
<main>
	<Headline>Dashboard</Headline>

	<DashboardNav />

	<div class="flex justify-between items-center mb-6">
		<SubHeadline>Overview</SubHeadline>
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

	{#if contextCompanyName}
		<SubHeadline>{contextCompanyName}</SubHeadline>
	{/if}

	<div class="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-5 gap-6 mb-8 mt-4">
		<a href="/dashboard/campaigns" class="block h-full">
			<StatsCard
				title="Active campaigns"
				value={active}
				borderColor="border-blue-500"
				iconColor="text-blue-500"
			>
				<svg
					slot="icon"
					xmlns="http://www.w3.org/2000/svg"
					class="h-8 w-8"
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

		<a href="/dashboard/campaigns" class="block h-full">
			<StatsCard
				title="Upcoming campaigns"
				value={scheduled}
				borderColor="border-indigo-500"
				iconColor="text-indigo-500"
			>
				<svg
					slot="icon"
					xmlns="http://www.w3.org/2000/svg"
					class="h-8 w-8"
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

		<a href="/dashboard/campaigns" class="block h-full">
			<StatsCard
				title="Completed campaigns"
				value={finished}
				borderColor="border-green-500"
				iconColor="text-green-500"
			>
				<svg
					slot="icon"
					xmlns="http://www.w3.org/2000/svg"
					class="h-8 w-8"
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

		<a href="/recipient" class="block h-full">
			<StatsCard
				title="Repeat offenders"
				value={repeatOffenders}
				borderColor="border-red-500"
				iconColor="text-red-500"
			>
				<svg
					slot="icon"
					xmlns="http://www.w3.org/2000/svg"
					class="h-8 w-8"
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

		<a href="/campaign" class="block h-full">
			<StatsCard
				title="Trainings completed"
				value={trainingCompleted}
				borderColor="border-training-completed"
				iconColor="text-training-completed"
			>
				<svg
					slot="icon"
					xmlns="http://www.w3.org/2000/svg"
					class="h-8 w-8"
					fill="none"
					viewBox="0 0 24 24"
					stroke="currentColor"
					stroke-width="1.5"
				>
					<path
						stroke-linecap="round"
						stroke-linejoin="round"
						d="M4.26 10.147a60.436 60.436 0 00-.491 6.347A48.627 48.627 0 0112 20.904a48.627 48.627 0 018.232-4.41 60.46 60.46 0 00-.491-6.347m-15.482 0a50.57 50.57 0 00-2.658-.813A59.905 59.905 0 0112 3.493a59.902 59.902 0 0110.399 5.84c-.896.248-1.783.52-2.658.814m-15.482 0A50.697 50.697 0 0112 13.489a50.702 50.702 0 017.74-3.342M6.75 15a.75.75 0 100-1.5.75.75 0 000 1.5zm0 0v-3.675A55.378 55.378 0 0112 8.443m-7.007 11.55A5.981 5.981 0 006.75 15.75v-1.5"
					/>
				</svg>
			</StatsCard>
		</a>
	</div>

	<SubHeadline>{contextCompanyName ? 'Campaign Trends' : 'Shared Campaign Trends'}</SubHeadline>
	<div class="mb-8 w-full min-h-[300px]">
		<CampaignTrendChart
			{campaignStats}
			isLoading={isCampaignStatsLoading}
			onCampaignClick={(id) => goto(`/campaign/${id}`)}
		/>
	</div>

	<SubHeadline>{contextCompanyName ? 'Calendar' : 'Shared Calendar'}</SubHeadline>
	<div class="mb-8 min-h-[600px]">
		<CampaignCalender
			campaigns={calendarCampaigns}
			bind:start={calendarStartDate}
			bind:end={calendarEndDate}
			onChangeDate={refreshCalendarCampaings}
			showCompany={!contextCompanyID}
		/>
	</div>
</main>
