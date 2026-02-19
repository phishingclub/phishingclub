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
	import AutoRefresh from '$lib/components/AutoRefresh.svelte';
	import { tick } from 'svelte';
	import EventName from '$lib/components/table/EventName.svelte';
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
	let eventsTableURLParams = newTableURLParams({
		prefix: 'events',
		sortBy: 'created_at',
		sortOrder: 'desc',
		noScroll: true
	});

	let isEventsLoading = false;

	let events = [];
	let eventsHasNextPage = true;
	let eventTypesIDToNameMap = {};

	let includeTestCampaigns = false;

	// handler for when toggle changes
	const handleToggleChange = async () => {
		await tick();
		await refreshEvents(true);
	};

	const handleAutoRefreshChange = (optKey) => {
		const value = Number(autoRefreshOptions.byKey(optKey));
		// batch the update to prevent multiple reactive triggers
		autoRefreshStore.set({
			enabled: value > 0,
			interval: value
		});
		setPageAutoRefresh('dashboard-events', $autoRefreshStore);
	};

	// hooks
	onMount(async () => {
		const context = appStateService.getContext();
		if (context) {
			contextCompanyID = context.companyID;
			contextCompanyName = context.companyName;
		}
		setEventTypes();
		await refreshEvents(true);
		eventsTableURLParams.onChange(() => refreshEvents(true));

		return () => {
			eventsTableURLParams.unsubscribe();
		};
	});

	const setEventTypes = async () => {
		try {
			const res = await api.campaign.getAllEventTypes();
			if (!res.success) {
				addToast('Failed to load event types', 'Error');
				console.error('failed to load event types', res.error);
				return;
			}
			res.data.map((t) => (eventTypesIDToNameMap[t.id] = t.name));
		} catch (e) {
			addToast('Failed to load event types', 'Error');
			console.error('failed to load event types', e);
		}
	};

	const refreshEvents = async (showIsLoading = true) => {
		try {
			if (showIsLoading) {
				isEventsLoading = true;
			}
			const options = {
				page: eventsTableURLParams.page,
				perPage: eventsTableURLParams.perPage,
				sortBy: eventsTableURLParams.sortBy,
				sortOrder: eventsTableURLParams.sortOrder,
				search: eventsTableURLParams.search,
				includeTest: includeTestCampaigns
			};
			const res = await api.campaign.getAllEvents(options, contextCompanyID);
			if (res.success) {
				events = res.data?.rows ?? [];
				eventsHasNextPage = res.data?.hasNextPage ?? false;
			}
		} catch (e) {
			addToast('Failed to load events', 'Error');
			console.error('failed to load events', e);
		} finally {
			if (showIsLoading) {
				isEventsLoading = false;
			}
		}
	};
</script>

<HeadTitle title="Dashboard - Events" />
<main>
	<Headline>Dashboard</Headline>

	<DashboardNav />

	<div class="flex justify-between items-center mb-6">
		<SubHeadline>Recent Events</SubHeadline>
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
		pageId="dashboard-events"
		onRefresh={async () => {
			const eventsOptions = {
				page: eventsTableURLParams.currentPage,
				perPage: eventsTableURLParams.perPage,
				sortBy: eventsTableURLParams.sortBy,
				sortOrder: eventsTableURLParams.sortOrder,
				search: eventsTableURLParams.search,
				includeTest: includeTestCampaigns
			};
			const eventsRes = await api.campaign.getAllEvents(eventsOptions, contextCompanyID);
			if (eventsRes.success) {
				events = eventsRes.data?.rows ?? [];
				eventsHasNextPage = eventsRes.data?.hasNextPage ?? false;
			}
		}}
	/>

	<div class="min-h-[300px] mb-8">
		<Table
			columns={[
				{ column: 'Time', size: 'large' },
				{ column: 'Event', size: 'large' },
				{ column: 'Campaign', size: 'large' },
				{ column: 'Email', size: 'large' },
				...(contextCompanyID ? [] : [{ column: 'Company', size: 'large' }])
			]}
			pagination={eventsTableURLParams}
			plural="events"
			hasData={!!events.length}
			hasNextPage={eventsHasNextPage}
			isGhost={isEventsLoading}
			noSearch={true}
			hasActions={false}
		>
			{#each events as event (event.id)}
				<TableRow>
					<TableCell isDate isRelative value={event.createdAt} />
					<TableCell>
						<EventName eventName={eventTypesIDToNameMap[event.eventID]} />
					</TableCell>
					<TableCell>
						{#if event.campaign?.name}
							<a href={`/campaign/${event.campaignID}`} class="block w-full py-1">
								{event.campaign.name}
							</a>
						{/if}
					</TableCell>
					<TableCell>
						{#if event.recipient?.email}
							<a href={`/recipient/${event.recipient.id}`} class="block w-full py-1">
								{event.recipient.email}
							</a>
						{/if}
					</TableCell>
					{#if !contextCompanyID}
						<TableCell>
							{#if event.campaign?.company?.name}
								{event.campaign.company.name}
							{/if}
						</TableCell>
					{/if}
				</TableRow>
			{/each}
		</Table>
	</div>
</main>
