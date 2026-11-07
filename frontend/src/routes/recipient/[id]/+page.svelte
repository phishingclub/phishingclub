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
	import StatsCard from '$lib/components/StatsCard.svelte';
	import EventName from '$lib/components/table/EventName.svelte';
	import RelativeTime from '$lib/components/RelativeTime.svelte';
	import StatusBadge from '$lib/components/StatusBadge.svelte';
	import { shortRelative } from '$lib/utils/shortRelative.js';
	import {
		NONE_YET,
		RISK_NONE_TIP,
		RISK_TIP,
		capitalize,
		formatDateTime,
		riskTone
	} from '$lib/utils/programText.js';

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
	let programs = [];
	let isEventsLoading = false;

	// heading email split at the last "@" so phones break there
	$: emailAt = (recipient.email || '').lastIndexOf('@');
	$: emailLocal = emailAt > 0 ? recipient.email.slice(0, emailAt) : recipient.email || '';
	$: emailDomain = emailAt > 0 ? recipient.email.slice(emailAt + 1) : '';

	// hooks
	onMount(() => {
		getRecipient();
		getStats();
		getPrograms();
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
			await getPrograms();
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

	const getPrograms = async () => {
		try {
			const res = await api.recipient.getPrograms($page.params.id);
			if (!res.success) {
				throw res.error;
			}
			programs = res.data ?? [];
		} catch (e) {
			addToast('failed to get recipient programs', 'Error');
			console.error(e);
		}
	};

	// program and enrollment states as StatusBadge text and tone
	const programStates = {
		draft: ['Draft', 'neutral'],
		scheduled: ['Scheduled', 'info'],
		active: ['Active', 'success'],
		paused: ['Paused', 'warning'],
		closed: ['Closed', 'neutral']
	};
	const enrollmentStates = {
		active: ['Active', 'success'],
		completed: ['Completed', 'info'],
		exited: ['Removed', 'neutral'],
		// still active when the program closed
		stopped: ['Closed', 'neutral']
	};
	const sendColumns = [
		{ column: 'Template', size: 'large', compact: true },
		{ column: 'Sent', size: 'small', compact: true },
		{ column: 'Opened', size: 'small', compact: true, hideBelow: 'md' },
		{ column: 'Clicked', size: 'small', compact: true },
		{ column: 'Submitted', size: 'small', compact: true, hideBelow: 'md' },
		{ column: 'Reported', size: 'small', compact: true },
		{ column: 'Training done', title: 'Trained', size: 'small', compact: true, hideBelow: 'md' }
	];
	// one line summary of the template emails of a program
	const sendSummary = (sends) => {
		const list = (sends ?? []).filter((x) => x.kind !== 'training');
		const n = (key) => list.filter((x) => x[key]).length;
		return `${list.length} ${list.length === 1 ? 'email' : 'emails'}, ${n('clickedAt')} clicked, ${n('reportedAt')} reported.`;
	};
	// event column, phones hide it below md, and its mark colour
	const sendEvents = [
		['openedAt', 'md', 'text-slate-600 dark:text-gray-300'],
		['clickedAt', '', 'text-red-700 dark:text-red-400'],
		['submittedAt', 'md', 'text-red-700 dark:text-red-400'],
		['reportedAt', '', 'text-green-700 dark:text-green-400'],
		['trainingCompletedAt', 'md', 'text-green-700 dark:text-green-400']
	];
	const programCard =
		'bg-white dark:bg-gray-900/80 rounded-lg shadow-md dark:shadow-none dark:ring-1 dark:ring-gray-600/30 p-4 sm:p-6 transition-colors duration-200';
	let relativeNow = Date.now();
	onMount(() => {
		const id = setInterval(() => (relativeNow = Date.now()), 60000);
		return () => clearInterval(id);
	});

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
		<p
			class="mt-8 text-xs font-semibold text-gray-600 dark:text-gray-400"
			title="These counters cover campaigns. Programs are shown below."
		>
			Campaigns only
		</p>
		<div class="pc-stats-grid grid grid-cols-2 sm:grid-cols-3 lg:grid-cols-7 gap-3 sm:gap-6 mb-8 mt-2">
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
				borderColor="border-reported dark:border-slate-400"
				iconColor="text-reported dark:text-slate-300"
			>
				<svg
					slot="icon"
					xmlns="http://www.w3.org/2000/svg"
					class="h-5 w-5 ml-2 text-reported dark:text-slate-300"
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
			<div class="sm:col-span-3 lg:col-span-1">
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
		</div>

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
		{#if programs.length}
			<SubHeadline>Programs</SubHeadline>
			<div class="space-y-4 mt-3 mb-8">
				{#each programs as p (p.programID)}
					{@const ps = programStates[p.programStatus] ?? [p.programStatus, 'neutral']}
					{@const es = enrollmentStates[p.enrollmentStatus] ?? [p.enrollmentStatus, 'neutral']}
					<div class={programCard}>
						<div
							class="flex flex-wrap items-center gap-x-3 gap-y-2 pb-2 border-b border-gray-200 dark:border-gray-700"
						>
							<a
								href="/program/{p.programID}"
								class="pc-focus rounded-sm text-lg font-semibold text-pc-darkblue dark:text-white hover:underline underline-offset-2 break-words"
								>{p.name}</a
							>
							<StatusBadge tone={ps[1]} title="Program status">{ps[0]}</StatusBadge>
							<StatusBadge tone={es[1]} title="This person in the program">{es[0]}</StatusBadge>
							{#if !p.anonymous}
								<span class="flex items-center gap-2 text-sm text-gray-600 dark:text-gray-400 sm:ml-auto">
									Risk
									{#if p.riskLevel && p.riskScore != null}
										<span class="cursor-help"
											><StatusBadge
												tone={riskTone[p.riskLevel] ?? 'neutral'}
												title="Risk score {Math.round(p.riskScore)} of 100. {RISK_TIP}"
												>{capitalize(p.riskLevel)} {Math.round(p.riskScore)}</StatusBadge
											></span
										>
									{:else}
										<span class="cursor-help text-gray-400 dark:text-gray-500" title={RISK_NONE_TIP}
											>{NONE_YET}</span
										>
									{/if}
								</span>
							{/if}
						</div>
						{#if p.anonymous}
							<p class="mt-3 text-sm text-gray-600 dark:text-gray-400">
								Anonymous program. Results are not shown per person.
							</p>
						{:else}
							<p class="mt-3 text-sm text-gray-600 dark:text-gray-400 tabular-nums">
								{#if p.sends?.length}{sendSummary(p.sends)}{:else}No emails sent yet.{/if}
								{#if p.nextDueAt && p.programStatus === 'active'}
									Next email <RelativeTime value={p.nextDueAt} short />.
								{/if}
							</p>
							{#if p.sends?.length}
								<div class="mt-3">
									<Table
										columns={sendColumns}
										pagination={null}
										noSearch
										hasActions={false}
										plural="emails"
										hasData={p.sends.length > 0}
										hasNextPage={false}
									>
										{#each p.sends as s}
											<TableRow>
												<TableCell wrap>
													<span class="flex items-center gap-2 min-w-0" title={s.template}>
														<span class="truncate">{s.template || 'Unknown'}</span>
														{#if s.kind === 'training'}
															<span class="shrink-0"
																><StatusBadge tone="info">Training</StatusBadge></span
															>
														{/if}
													</span>
												</TableCell>
												<TableCell>
													<span title={formatDateTime(s.sentAt)}
														>{shortRelative(s.sentAt, relativeNow)}</span
													>
												</TableCell>
												{#each sendEvents as [key, hide, color]}
													<TableCell hideBelow={hide}>
														{#if s[key]}
															<span
																class="inline-flex items-center gap-1 {color}"
																title={formatDateTime(s[key])}
															>
																<svg
																	class="w-4 h-4 shrink-0"
																	viewBox="0 0 20 20"
																	fill="currentColor"
																	aria-hidden="true"
																>
																	<path
																		fill-rule="evenodd"
																		d="M16.7 5.3a1 1 0 010 1.4l-8 8a1 1 0 01-1.4 0l-4-4a1 1 0 011.4-1.4L8 12.6l7.3-7.3a1 1 0 011.4 0z"
																		clip-rule="evenodd"
																	/>
																</svg>
																<span class="sr-only">Yes,</span>
																<span class="text-xs tabular-nums"
																	>{shortRelative(s[key], relativeNow)}</span
																>
															</span>
														{:else}
															<span class="text-gray-400 dark:text-gray-500"
																><span aria-hidden="true">-</span><span class="sr-only">No</span></span
															>
														{/if}
													</TableCell>
												{/each}
											</TableRow>
										{/each}
									</Table>
								</div>
							{/if}
						{/if}
					</div>
				{/each}
			</div>
		{/if}
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
