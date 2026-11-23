<script>
	import TableCellLink from '$lib/components/table/TableCellLink.svelte';
	import { api } from '$lib/api/apiProxy.js';
	import { newTableURLParams } from '$lib/service/tableURLParams.js';
	import { onMount } from 'svelte';
	import CellCopy from '$lib/components/table/CopyCell.svelte';
	import Headline from '$lib/components/Headline.svelte';
	import Breadcrumb from '$lib/components/Breadcrumb.svelte';
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
	import ActionButton from '$lib/components/ActionButton.svelte';
	import TableDropDownEllipsis from '$lib/components/table/TableDropDownEllipsis.svelte';
	import TableDropDownButton from '$lib/components/table/TableDropDownButton.svelte';
	import AutoRefresh from '$lib/components/AutoRefresh.svelte';
	import StatTiles from '$lib/components/StatTiles.svelte';
	import EventName from '$lib/components/table/EventName.svelte';
	import RecipientProgramsTable from '$lib/components/program/RecipientProgramsTable.svelte';
	import ConditionalDisplay from '$lib/components/ConditionalDisplay.svelte';
	import { displayMode, showsSimulation } from '$lib/store/displayMode';

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
		campaignsReported: 0,
		trainingsAssigned: 0,
		trainingsCompleted: 0
	};
	let isGroupsLoading = false;
	// the programs table loads its own pages
	let programsTable;
	let hasPrograms = false;
	let isEventsLoading = false;

	// heading email split at the last "@" so phones break there
	$: emailAt = (recipient.email || '').lastIndexOf('@');
	$: emailLocal = emailAt > 0 ? recipient.email.slice(0, emailAt) : recipient.email || '';
	$: emailDomain = emailAt > 0 ? recipient.email.slice(emailAt + 1) : '';

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
			await programsTable?.refresh();
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

	// the counters cover campaigns, said in their tips once programs are listed too.
	// programs are only listed in the modes that show them
	$: campaignTip = hasPrograms && showsSimulation($displayMode)
		? 'These counts cover campaigns only. Programs are listed under Programs.'
		: '';

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
	// the stat tiles, with the icons and colors of the campaign page
	$: statTiles = [
		{
			key: 'campaigns',
			title: 'Campaigns',
			tip: campaignTip,
			value: stats.campaignsParticiated,
			color: 'text-cta-blue dark:text-highlight-blue',
			icon: 'sent'
		},
		{
			key: 'read',
			title: 'Emails read',
			value: stats.campaignsTrackingPixelLoaded,
			color: 'text-message-read',
			icon: 'read'
		},
		{
			key: 'clicked',
			title: 'Clicked link',
			value: stats.campaignsPhishingPageLoaded,
			color: 'text-page-visited',
			icon: 'clicked'
		},
		{
			key: 'submitted',
			title: 'Data submitted',
			value: stats.campaignsDataSubmitted,
			color: 'text-submitted-data',
			icon: 'submitted'
		},
		{
			key: 'reported',
			title: 'Reported',
			value: stats.campaignsReported,
			color: 'text-reported dark:text-slate-300',
			icon: 'reported'
		},
		{
			key: 'repeatClicks',
			title: 'Repeat link clicks',
			value: stats.repeatLinkClicks,
			color: 'text-repeat-link-clicks',
			icon: 'repeat'
		},
		{
			key: 'repeatSubmissions',
			title: 'Repeat submissions',
			value: stats.repeatSubmissions,
			color: 'text-repeart-submissions',
			icon: 'repeat'
		}
	];
</script>

<HeadTitle title={recipient.email ? `Recipient ${recipient.email}` : 'Recipient'} />
<section>
	<Breadcrumb crumbs={[{ label: 'Recipients', href: '/recipient/' }, { label: 'Recipient' }]} />
	<div class="flex flex-wrap items-center justify-between gap-x-6 gap-y-3">
		<div class="min-w-0">
			<Headline docSlug="recipients" preserveCase>
				<!-- break after "@" first, inside a part only when it does not fit -->
				<span class="select-text [overflow-wrap:anywhere]"
					>{emailLocal}{#if emailDomain}@<wbr />{emailDomain}{/if}</span
				>
			</Headline>
			{#if recipient.firstName || recipient.lastName}
				<SubHeadline preserveCase>
					{recipient.firstName}
					{recipient.lastName}
				</SubHeadline>
			{/if}
		</div>
		<div class="flex flex-nowrap items-center gap-x-3 sm:gap-x-4">
			<ActionButton variant="secondary" icon="export" on:click={onClickExport}>
				<!-- short label on phones keeps the toolbar on one row -->
				<span class="sm:hidden">Export</span><span class="hidden sm:inline">Export Events</span>
			</ActionButton>
			<div class="sm:pl-4 sm:border-l sm:border-gray-200 dark:sm:border-gray-700">
				<AutoRefresh
					isLoading={false}
					onRefresh={() => {
						refresh(false);
					}}
				/>
			</div>
		</div>
	</div>
	<div>
		<StatTiles tiles={statTiles} class="mb-8 mt-8" />

		{#if stats.trainingsAssigned > 0}
			<div class="flex flex-wrap items-center gap-x-6 gap-y-2 mb-8 text-sm">
				<span class="font-semibold text-grayblue-dark dark:text-gray-400">Training</span>
				<span class="text-grayblue-dark dark:text-gray-400"
					>Assigned <span class="font-semibold text-pc-darkblue dark:text-gray-200"
						>{stats.trainingsAssigned}</span
					></span
				>
				<span class="text-grayblue-dark dark:text-gray-400"
					>Completed <span class="font-semibold text-training-completed"
						>{stats.trainingsCompleted}</span
					></span
				>
			</div>
		{/if}
		<ConditionalDisplay show="simulation">
			<RecipientProgramsTable
				bind:this={programsTable}
				bind:hasPrograms
				recipientID={$page.params.id}
			/>
		</ConditionalDisplay>
		<SubHeadline>Groups</SubHeadline>
		<Table
			columns={['Name']}
			sortable={[]}
			hasData={!!recipient.groups?.length}
			plural="groups"
			isGhost={isGroupsLoading}
		>
			{#each recipient.groups as group}
				<TableRow>
					<TableCellLink href={`/recipient/group/${group.id}/`} title={group.name}>
						{group.name}
					</TableCellLink>
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
		<div class="mb-2">
			<SubHeadline>Events</SubHeadline>
		</div>
		<Table
			columns={[
				{ column: 'Event', size: 'medium' },
				{ column: 'Created', size: 'medium' },
				{ column: 'Campaign', size: 'medium' },
				{ column: 'Details', size: 'large' },
				{ column: 'User-Agent', size: 'large' },
				{ column: 'IP', size: 'small' },
				{ column: 'Metadata', size: 'large' }
			]}
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
