<script>
	import { displayMode, showsSimulation } from '$lib/store/displayMode';
	import Headline from '$lib/components/Headline.svelte';
	import HeadTitle from '$lib/components/HeadTitle.svelte';
	import { AppStateService } from '$lib/service/appState';
	import { api } from '$lib/api/apiProxy.js';
	import { onMount } from 'svelte';
	import { goto } from '$app/navigation';
	import { addToast } from '$lib/store/toast';
	import { newTableURLParams } from '$lib/service/tableURLParams';
	import Table from '$lib/components/table/Table.svelte';
	import TableRow from '$lib/components/table/TableRow.svelte';
	import TableCell from '$lib/components/table/TableCell.svelte';
	import TableFilterSelect from '$lib/components/table/TableFilterSelect.svelte';
	import EventName from '$lib/components/table/EventName.svelte';
	import DashboardNav from '$lib/components/DashboardNav.svelte';
	import DashboardToolbar from '$lib/components/DashboardToolbar.svelte';
	import { dashboardIncludeTest } from '$lib/store/dashboardIncludeTest.js';
	import { toEvent } from '$lib/utils/events';

	// services
	const appStateService = AppStateService.instance;

	// local state
	let contextCompanyID = null;
	let eventsTableURLParams = newTableURLParams({
		prefix: 'events',
		sortBy: 'created_at',
		sortOrder: 'desc',
		noScroll: true
	});

	let isEventsLoading = false;
	let isLoading = false;

	let events = [];
	let eventsHasNextPage = true;
	let eventTypesIDToNameMap = {};
	let eventTypeOptions = [];

	// the event type filter lives in the url as ?eventType=, by event name
	const readEventType = () => new URL(window.location.href).searchParams.get('eventType') || '';
	let eventType = readEventType();

	const onEventTypeChange = async () => {
		const url = new URL(window.location.href);
		if (eventType) {
			url.searchParams.set('eventType', eventType);
		} else {
			url.searchParams.delete('eventType');
		}
		await goto(`?${url.searchParams.toString()}`, { replaceState: true, noScroll: true, keepFocus: true });
		// a page change reloads through the table listener
		if (eventsTableURLParams.currentPage !== 1) {
			eventsTableURLParams.currentPage = 1;
		} else {
			await refreshEvents(true);
		}
	};

	// hooks
	onMount(() => {
		const context = appStateService.getContext();
		if (context) {
			contextCompanyID = context.companyID;
		}
		setEventTypes();
		refreshEvents(true);
		eventsTableURLParams.onChange(() => refreshEvents(true));

		return () => {
			eventsTableURLParams.unsubscribe();
		};
	});

	const setEventTypes = async () => {
		try {
			const res = await api.campaign.getAllEventTypes();
			if (!res.success) {
				throw res.error;
			}
			const map = {};
			res.data.forEach((t) => (map[t.id] = t.name));
			eventTypesIDToNameMap = map;
			eventTypeOptions = res.data
				.map((t) => ({ value: t.name, label: toEvent(t.name).name || t.name }))
				.sort((a, b) => a.label.localeCompare(b.label));
		} catch (e) {
			addToast('Failed to load event types', 'Error');
			console.error('failed to load event types', e);
		}
	};

	const refreshEvents = async (showIsLoading = true) => {
		isLoading = true;
		try {
			if (showIsLoading) {
				isEventsLoading = true;
			}
			const options = {
				page: eventsTableURLParams.currentPage,
				perPage: eventsTableURLParams.perPage,
				sortBy: eventsTableURLParams.sortBy,
				sortOrder: eventsTableURLParams.sortOrder,
				search: eventsTableURLParams.search,
				includeTest: $dashboardIncludeTest
			};
			const res = await api.campaign.getAllEvents(options, contextCompanyID, eventType);
			if (!res.success) {
				throw res.error;
			}
			events = res.data?.rows ?? [];
			eventsHasNextPage = res.data?.hasNextPage ?? false;
		} catch (e) {
			addToast('Failed to load events', 'Error');
			console.error('failed to load events', e);
		} finally {
			isLoading = false;
			if (showIsLoading) {
				isEventsLoading = false;
			}
		}
	};

	// texts leave out programs in the modes that hide them
	$: showPrograms = showsSimulation($displayMode);
</script>

<HeadTitle title="Dashboard - Events" />
<main>
	<Headline docSlug="dashboard">Dashboard</Headline>

	<DashboardNav />

	<DashboardToolbar
		{isLoading}
		allCompanies={!contextCompanyID}
		pageId="dashboard-events"
		onRefresh={() => refreshEvents(false)}
		on:change={() => refreshEvents(true)}
	/>

	<div class="mt-4 mb-8">
		<Table
			reserveSelect
			columns={[
				{ column: 'Time', size: 'small' },
				{ column: 'Event', size: 'medium' },
				{ column: showPrograms ? 'Campaign or program' : 'Campaign', size: 'large' },
				{ column: 'Recipient', size: 'large' },
				...(contextCompanyID ? [] : [{ column: 'Company', size: 'medium' }])
			]}
			pagination={eventsTableURLParams}
			plural="events"
			hasData={!!events.length}
			hasNextPage={eventsHasNextPage}
			isGhost={isEventsLoading}
			noSearch={true}
			hasActions={false}
		>
			<TableFilterSelect
				slot="toolbar"
				label="Event"
				options={eventTypeOptions}
				bind:value={eventType}
				on:change={onEventTypeChange}
			/>
			{#each events as event (event.id)}
				<TableRow>
					<TableCell isDate isRelative value={event.createdAt} />
					<TableCell>
						<EventName eventName={eventTypesIDToNameMap[event.eventID]} full />
					</TableCell>
					<TableCell>
						{#if event.programID}
							<a href={`/program/${event.programID}`} class="pc-cell-link block w-full py-1">
								{event.programName}
								<span class="text-slate-400 dark:text-gray-500">(program)</span>
							</a>
						{:else if event.campaign?.name}
							<a href={`/campaign/${event.campaignID}`} class="pc-cell-link block w-full py-1">
								{event.campaign.name}
							</a>
						{/if}
					</TableCell>
					<TableCell>
						{#if event.recipient?.email}
							<a href={`/recipient/${event.recipient.id}`} class="pc-cell-link block w-full py-1">
								{event.recipient.email}
							</a>
						{/if}
					</TableCell>
					{#if !contextCompanyID}
						<TableCell>
							{#if event.campaign?.company?.name || event.company?.name}
								{event.campaign?.company?.name || event.company?.name}
							{:else}
								<span class="text-slate-400 dark:text-gray-500">Shared</span>
							{/if}
						</TableCell>
					{/if}
				</TableRow>
			{/each}
		</Table>
	</div>
</main>
