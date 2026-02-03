<script>
	import { api } from '$lib/api/apiProxy.js';
	import { newTableURLParams } from '$lib/service/tableURLParams.js';
	import { onMount } from 'svelte';
	import CellCopy from '$lib/components/table/CopyCell.svelte';
	import Headline from '$lib/components/Headline.svelte';
	import TableRow from '$lib/components/table/TableRow.svelte';
	import TableCell from '$lib/components/table/TableCell.svelte';
	import { addToast } from '$lib/store/toast';
	import TableCellEmpty from '$lib/components/table/TableCellEmpty.svelte';
	import Table from '$lib/components/table/Table.svelte';
	import HeadTitle from '$lib/components/HeadTitle.svelte';
	import { showIsLoading, hideIsLoading } from '$lib/store/loading.js';
	import { page } from '$app/stores';
	import SubHeadline from '$lib/components/SubHeadline.svelte';
	import { goto } from '$app/navigation';
	import TableCellAction from '$lib/components/table/TableCellAction.svelte';
	import BigButton from '$lib/components/BigButton.svelte';
	import { toEvent } from '$lib/utils/events';
	import TableDropDownEllipsis from '$lib/components/table/TableDropDownEllipsis.svelte';
	import TableDropDownButton from '$lib/components/table/TableDropDownButton.svelte';
	import AutoRefresh from '$lib/components/AutoRefresh.svelte';
	import StatsCard from '$lib/components/StatsCard.svelte';
	import EventName from '$lib/components/table/EventName.svelte';

	// @ts-ignore
	const tableURLParams = newTableURLParams({
		sortBy: 'created',
		sortOrder: 'desc'
	});
	let recipient = {
		id: $page.params.id,
		groups: []
	};
	let events = [];
	let eventsHasNextPage = true;
	let stats = {
		campaignsParticiated: 0,
		campaignsTrackingPixelLoaded: 0,
		campaignsPhishingPageLoaded: 0,
		campaignsDataSubmitted: 0,
		campaignsReported: 0
	};
	let isGroupsLoading = false;
	let isEventsLoading = false;

	// hooks
	onMount(() => {
		getRecipient();
		getStats();
		getRecipientEvents();
		tableURLParams.onChange(getRecipientEvents);

		return () => {
			tableURLParams.onChange(() => {}); // Replace with empty function first
			tableURLParams.unsubscribe();
		};
	});

	// component logic
	const refresh = async (showLoading = true) => {
		try {
			if (showLoading) {
				showIsLoading();
			}
			await getRecipient();
			await getStats();
			await getRecipientEvents(showLoading);
		} finally {
			if (showLoading) {
				hideIsLoading();
			}
		}
	};

	const getRecipientEvents = async (showLoading = true) => {
		try {
			if (showLoading) {
				isEventsLoading = true;
			}
			const res = await api.recipient.getEvents($page.params.id, tableURLParams);
			if (res.success) {
				events = res.data.rows;
				eventsHasNextPage = res.data.hasNextPage;
				return;
			}
			throw res.error;
		} catch (e) {
			addToast('Failed to load recipients', 'Error');
			console.error('failed to load recipients', e);
		} finally {
			if (showLoading) {
				isEventsLoading = false;
			}
		}
	};

	const getRecipient = async () => {
		try {
			const res = await api.recipient.getByID($page.params.id);
			if (!res.success) {
				throw res.error;
			}
			recipient = res.data;
			if (!res.data.groups) {
				recipient.groups = [];
			}
		} catch (e) {
			addToast('failed to get recipient', 'Error');
			console.error(e);
		}
	};

	const getStats = async () => {
		try {
			const res = await api.recipient.getStatsByID($page.params.id);
			if (!res.success) {
				throw res.error;
			}
			stats = res.data;
		} catch (e) {
			addToast('failed to get recipient campaign statistics', 'Error');
			console.error(e);
		}
	};

	const onClickExport = async () => {
		try {
			showIsLoading();
			api.recipient.export($page.params.id);
		} catch (e) {
			addToast('Failed to export recipient', 'Error');
			console.error('failed to export recipient', e);
		} finally {
			hideIsLoading();
		}
	};
</script>

<HeadTitle title="Recipients" />
<section>
	<div class="flex justify-between">
		<Headline>
			<span class="select-text">{recipient.email}</span>
		</Headline>
		<AutoRefresh
			isLoading={false}
			onRefresh={() => {
				refresh(false);
			}}
		/>
	</div>
	{#if recipient.firstName || recipient.lastName}
		<SubHeadline>
			{recipient.firstName}
			{recipient.lastName}
		</SubHeadline>
	{/if}
	<BigButton on:click={onClickExport}>Export events</BigButton>
	<div>
		<div class="grid mr-1/12 md:grid-cols-2 lg:grid-cols-7 gap-6 mb-8 mt-4">
			<!-- Campaigns card -->
			<StatsCard
				title="Campaigns"
				value={stats.campaignsParticiated}
				borderColor="border-cta-blue"
				iconColor="text-cta-blue"
			>
				<svg
					slot="icon"
					xmlns="http://www.w3.org/2000/svg"
					class="h-5 w-5 ml-2 text-cta-blue"
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

			<!-- Emails read card -->
			<StatsCard
				title="Emails read"
				value={stats.campaignsTrackingPixelLoaded}
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
						d="M15 12a3 3 0 11-6 0 3 3 0 016 0z"
					/>
					<path
						stroke-linecap="round"
						stroke-linejoin="round"
						stroke-width="2"
						d="M2.458 12C3.732 7.943 7.523 5 12 5c4.478 0 8.268 2.943 9.542 7-1.274 4.057-5.064 7-9.542 7-4.477 0-8.268-2.943-9.542-7z"
					/>
				</svg>
			</StatsCard>

			<!-- Clicked link card -->
			<StatsCard
				title="Clicked link"
				value={stats.campaignsPhishingPageLoaded}
				borderColor="border-clicked-link"
				iconColor="text-clicked-link"
			>
				<svg
					slot="icon"
					xmlns="http://www.w3.org/2000/svg"
					class="h-5 w-5 ml-2 text-clicked-link"
					fill="none"
					viewBox="0 0 24 24"
					stroke="currentColor"
				>
					<path
						stroke-linecap="round"
						stroke-linejoin="round"
						stroke-width="2"
						d="M21 12a9 9 0 01-9 9m9-9a9 9 0 00-9-9m9 9H3m9 9a9 9 0 01-9-9m9 9c1.657 0 3-4.03 3-9s-1.343-9-3-9m0 18c-1.657 0-3-4.03-3-9s1.343-9 3-9m-9 9a9 9 0 019-9"
					/>
				</svg>
			</StatsCard>

			<!-- Data submitted card -->
			<StatsCard
				title="Data submitted"
				value={stats.campaignsDataSubmitted}
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
						d="M9 17v-2m3 2v-4m3 4v-6m2 10H7a2 2 0 01-2-2V5a2 2 0 012-2h5.586a1 1 0 01.707.293l5.414 5.414a1 1 0 01.293.707V19a2 2 0 01-2 2z"
					/>
				</svg>
			</StatsCard>

			<!-- Reported card -->
			<StatsCard
				title="Reported"
				value={stats.campaignsReported}
				borderColor="border-reported"
				iconColor="text-reported"
			>
				<svg
					slot="icon"
					xmlns="http://www.w3.org/2000/svg"
					class="h-5 w-5 ml-2 text-reported"
					fill="none"
					viewBox="0 0 24 24"
					stroke="currentColor"
				>
					<path
						stroke-linecap="round"
						stroke-linejoin="round"
						stroke-width="2"
						d="M12 9v2m0 4h.01m-6.938 4h13.856c1.54 0 2.502-1.667 1.732-2.5L13.732 4c-.77-.833-1.964-.833-2.732 0L4.072 16.5c-.77.833.192 2.5 1.732 2.5z"
					/>
				</svg>
			</StatsCard>

			<!-- Repeat link clicks card -->
			<StatsCard
				title="Repeat link clicks"
				value={stats.repeatLinkClicks}
				borderColor="border-repeat-link-clicks"
				iconColor="text-repeat-link-clicks"
			>
				<svg
					slot="icon"
					xmlns="http://www.w3.org/2000/svg"
					class="h-5 w-5 ml-2 text-repeat-link-clicks"
					fill="none"
					viewBox="0 0 24 24"
					stroke="currentColor"
				>
					<path
						stroke-linecap="round"
						stroke-linejoin="round"
						stroke-width="2"
						d="M4 4v5h.582m15.356 2A8.001 8.001 0 004.582 9m0 0H9m11 11v-5h-.581m0 0a8.003 8.003 0 01-15.357-2m15.357 2H15"
					/>
				</svg>
			</StatsCard>

			<!-- Repeat submissions card -->
			<StatsCard
				title="Repeat submissions"
				value={stats.repeatSubmissions}
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
		</div>
		<SubHeadline>Groups</SubHeadline>
		<Table
			columns={['Name']}
			sortable={[]}
			hasData={!!recipient.groups}
			plural="events"
			isGhost={isGroupsLoading}
		>
			{#each recipient.groups as group}
				<TableRow>
					<TableCell value={group.name} />
					<TableCellEmpty />
					<TableCellAction>
						<TableDropDownEllipsis>
							<TableDropDownButton
								name={'Go to group'}
								on:click={() => {
									goto(`/recipient/group/${group.id}/`);
								}}
							/>
						</TableDropDownEllipsis>
					</TableCellAction>
				</TableRow>
			{/each}
		</Table>
		<SubHeadline>Events</SubHeadline>
		<Table
			columns={['Event', 'Created', 'Campaign', 'Details', 'User-Agent', 'IP', 'Metadata']}
			sortable={['Event', 'Created', 'Campaign', 'Details', 'User-Agent', 'IP', 'Metadata']}
			hasData={!!events?.length}
			hasNextPage={eventsHasNextPage}
			plural="events"
			pagination={tableURLParams}
			isGhost={isEventsLoading}
		>
			{#each events as event}
				<TableRow>
					<TableCell>
						<EventName eventName={event.name} />
					</TableCell>
					<TableCell isDate value={event.createdAt} />
					<TableCell value={event.campaignName} />
					<TableCell>
						<CellCopy text={event.data} />
					</TableCell>

					<TableCell>
						<CellCopy text={event.userAgent} />
					</TableCell>
					<TableCell>
						<CellCopy text={event.ip} />
					</TableCell>
					<TableCell>
						<CellCopy text={event.metadata || ''} />
					</TableCell>
					<TableCellEmpty />
					<TableCellAction>
						<TableDropDownEllipsis>
							<TableDropDownButton
								name={'Go to campaign'}
								on:click={() => {
									goto(`/campaign/${event.campaignID}/`);
								}}
							/>
						</TableDropDownEllipsis>
					</TableCellAction>
				</TableRow>
			{/each}
		</Table>
	</div>
</section>
