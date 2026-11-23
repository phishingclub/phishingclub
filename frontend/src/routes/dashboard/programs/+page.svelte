<script>
	import Headline from '$lib/components/Headline.svelte';
	import HeadTitle from '$lib/components/HeadTitle.svelte';
	import SubHeadline from '$lib/components/SubHeadline.svelte';
	import { AppStateService } from '$lib/service/appState';
	import { api } from '$lib/api/apiProxy.js';
	import { onMount, tick } from 'svelte';
	import { get } from 'svelte/store';
	import { page } from '$app/stores';
	import { addToast } from '$lib/store/toast';
	import { newTableURLParams } from '$lib/service/tableURLParams';
	import Table from '$lib/components/table/Table.svelte';
	import TableRow from '$lib/components/table/TableRow.svelte';
	import TableCell from '$lib/components/table/TableCell.svelte';
	import DashboardNav from '$lib/components/DashboardNav.svelte';
	import DashboardToolbar from '$lib/components/DashboardToolbar.svelte';
	import StatTiles from '$lib/components/StatTiles.svelte';
	import { shortRelative } from '$lib/utils/shortRelative.js';
	import { scrollToTable } from '$lib/utils/scrollToTable.js';
	import { NEXT_HIDDEN_TIP } from '$lib/utils/programText.js';

	// services
	const appStateService = AppStateService.instance;

	// the table a tile link points at, read before the table paging rewrites
	// the url without it
	const initialHash = (get(page).url.hash || window.location.hash).slice(1);

	// local state
	let contextCompanyID = null;
	let isLoading = false;

	// program counts per state, null until loaded
	let stats = null;

	// one table per state, each with its own paging in the url
	const newTable = (status, prefix, sortBy, sortOrder) => ({
		status,
		params: newTableURLParams({ prefix, sortBy, sortOrder, noScroll: true }),
		rows: [],
		hasNextPage: false,
		isGhost: false
	});
	let tables = {
		attention: newTable('needs_attention', 'attention', 'started_at', 'desc'),
		active: newTable('active', 'active', 'started_at', 'desc'),
		scheduled: newTable('scheduled', 'scheduled', 'start_at', 'asc'),
		paused: newTable('paused', 'paused', 'paused_at', 'desc')
	};

	$: tiles = [
		{
			key: 'active',
			title: 'Active',
			tip: 'Running programs, those that need attention left out.',
			value: stats?.active ?? 0,
			color: 'text-campaign-active',
			icon: 'active',
			href: '#active'
		},
		{
			key: 'scheduled',
			title: 'Scheduled',
			tip: 'Set to start on a date. Drafts are left out.',
			value: stats?.scheduled ?? 0,
			color: 'text-campaign-scheduled',
			icon: 'scheduled',
			href: '#scheduled'
		},
		{
			key: 'paused',
			title: 'Paused',
			tip: 'Started and then paused. Nothing is sent until resumed.',
			value: stats?.paused ?? 0,
			color: 'text-closed',
			icon: 'paused',
			href: '#paused'
		},
		{
			key: 'attention',
			title: 'Needs attention',
			tip: 'Running, but a template cannot be sent, the groups are empty or sending has stalled.',
			value: stats?.needsAttention ?? 0,
			color: 'text-failed-sending',
			icon: 'attention',
			href: '#attention'
		}
	];

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
		}
		refresh().then(scrollToHash);
		for (const key of Object.keys(tables)) {
			tables[key].params.onChange(() => refreshTable(key, true));
		}
		return () => {
			for (const key of Object.keys(tables)) {
				tables[key].params.unsubscribe();
			}
		};
	});

	const refresh = async (showLoading = true) => {
		isLoading = true;
		try {
			await Promise.all([
				refreshStats(),
				...Object.keys(tables).map((key) => refreshTable(key, showLoading))
			]);
		} finally {
			isLoading = false;
		}
	};

	// the Needs attention and Paused tables show once the counts are in, so a
	// link to a table scrolls to it after the first load
	const scrollToHash = async () => {
		await tick();
		scrollToTable(initialHash);
	};

	const refreshStats = async () => {
		try {
			const res = await api.program.getStats(contextCompanyID);
			if (!res.success) {
				throw res.error;
			}
			stats = res.data;
		} catch (e) {
			addToast('Failed to load program counts', 'Error');
			console.error('failed to load program counts', e);
		}
	};

	// the latest load of each table, so an older response that lands last is
	// dropped
	const refreshSeq = {};
	const refreshTable = async (key, showLoading = true) => {
		const seq = (refreshSeq[key] = (refreshSeq[key] ?? 0) + 1);
		const table = tables[key];
		if (showLoading) {
			tables[key].isGhost = true;
		}
		try {
			const res = await api.program.getAll(table.params, contextCompanyID, table.status, true);
			if (seq !== refreshSeq[key]) return;
			if (!res.success) {
				throw res.error;
			}
			tables[key].rows = res.data?.rows ?? [];
			tables[key].hasNextPage = res.data?.hasNextPage ?? false;
		} catch (e) {
			if (seq === refreshSeq[key]) {
				addToast('Failed to load programs', 'Error');
			}
			console.error('failed to load programs', e);
		} finally {
			// the latest load also ends the loading look of an older one
			if (seq === refreshSeq[key]) {
				tables[key].isGhost = false;
			}
		}
	};

	// the Company column, or nothing when a company is selected or on phones
	$: companyColumn = showCompanyColumn ? [{ column: 'Company', size: 'large' }] : [];
	// phones show the name column only, the rest moves into the name subline
	$: wideColumns = (columns) => (isPhone ? [] : columns);
	$: companyPrefix = (program) =>
		!contextCompanyID && program.company?.name ? `${program.company.name} · ` : '';
	const recipientsText = (program) => {
		const n = program.enrolled ?? 0;
		return `${n} ${n === 1 ? 'recipient' : 'recipients'}`;
	};
</script>

<HeadTitle title="Dashboard - Programs" />
<main>
	<Headline docSlug="dashboard">Dashboard</Headline>

	<DashboardNav />

	<DashboardToolbar
		{isLoading}
		allCompanies={!contextCompanyID}
		pageId="dashboard-programs"
		includeTest={false}
		onRefresh={() => refresh(false)}
		on:change={() => refresh(true)}
	/>

	<StatTiles {tiles} class="mt-4" />

	{#if stats?.needsAttention > 0}
		<section id="attention" class="mt-8 scroll-mt-28">
			<SubHeadline>Needs attention</SubHeadline>
			<div class="mt-4">
				<Table
					reserveSelect
					isGhost={tables.attention.isGhost}
					columns={[
						{ column: 'Name', size: 'large' },
						...wideColumns([{ column: 'Reason', size: 'large' }]),
						...companyColumn,
						...wideColumns([{ column: 'Recipients', size: 'small' }])
					]}
					hasData={!!tables.attention.rows.length}
					hasNextPage={tables.attention.hasNextPage}
					plural="programs that need attention"
					pagination={tables.attention.params}
					hasActions={false}
				>
					{#each tables.attention.rows as program (program.id)}
						<TableRow>
							<TableCell>
								<div class="min-w-0 max-sm:py-1.5">
									<a
										href={`/program/${program.id}`}
										class="pc-cell-link block min-w-0 truncate py-1"
									>
										{program.name}
									</a>
									<!-- phones show the details here instead of in columns -->
									<span class="block sm:hidden text-xs text-gray-500 dark:text-gray-400"
										>{companyPrefix(program)}{program.attentionReason ||
											recipientsText(program)}</span
									>
								</div>
							</TableCell>
							{#if !isPhone}
								<TableCell value={program.attentionReason} />
							{/if}
							{#if showCompanyColumn}
								<TableCell value={program.company?.name}
									><span class="text-slate-400 dark:text-gray-500">Shared</span></TableCell
								>
							{/if}
							{#if !isPhone}
								<TableCell value={String(program.enrolled ?? 0)} />
							{/if}
						</TableRow>
					{/each}
				</Table>
			</div>
		</section>
	{/if}

	<section
		id="active"
		class="mt-8 scroll-mt-28"
		class:mb-8={!(stats?.scheduled > 0) && !(stats?.paused > 0)}
	>
		<SubHeadline>Active programs</SubHeadline>
		<div class="mt-4">
			<Table
				reserveSelect
				isGhost={tables.active.isGhost}
				columns={[
					{ column: 'Name', size: 'large' },
					...companyColumn,
					...wideColumns([
						{ column: 'Recipients', size: 'small' },
						{ column: 'Next template', size: 'medium' }
					])
				]}
				hasData={!!tables.active.rows.length}
				hasNextPage={tables.active.hasNextPage}
				plural="active programs"
				pagination={tables.active.params}
				hasActions={false}
			>
				{#each tables.active.rows as program (program.id)}
					<TableRow>
						<TableCell>
							<div class="min-w-0 max-sm:py-1.5">
								<a href={`/program/${program.id}`} class="pc-cell-link block min-w-0 truncate py-1">
									{program.name}
								</a>
								<!-- phones show the details here instead of in columns -->
								<span class="block sm:hidden text-xs text-gray-500 dark:text-gray-400"
									>{companyPrefix(program)}{recipientsText(
										program
									)}{#if program.isAnonymous}{' · next hidden'}{:else if program.nextSendAt}{' · next '}{shortRelative(
											program.nextSendAt,
											relativeNow
										)}{/if}</span
								>
							</div>
						</TableCell>
						{#if showCompanyColumn}
							<TableCell value={program.company?.name}
								><span class="text-slate-400 dark:text-gray-500">Shared</span></TableCell
							>
						{/if}
						{#if !isPhone}
							<TableCell value={String(program.enrolled ?? 0)} />
							{#if program.isAnonymous}
								<!-- as on the program page, an anonymous program does not show when it sends -->
								<TableCell>
									<span
										class="text-sm italic text-gray-500 dark:text-gray-400"
										title={NEXT_HIDDEN_TIP}>Hidden</span
									>
								</TableCell>
							{:else if program.nextSendAt}
								<TableCell>
									<span title={new Date(program.nextSendAt).toLocaleString()}
										>{shortRelative(program.nextSendAt, relativeNow)}</span
									>
								</TableCell>
							{:else}
								<TableCell />
							{/if}
						{/if}
					</TableRow>
				{/each}
			</Table>
		</div>
	</section>

	{#if stats?.scheduled > 0}
		<section id="scheduled" class="mt-8 scroll-mt-28" class:mb-8={!(stats?.paused > 0)}>
			<SubHeadline>Scheduled programs</SubHeadline>
			<div class="mt-4">
				<Table
					reserveSelect
					isGhost={tables.scheduled.isGhost}
					columns={[
						{ column: 'Name', size: 'large' },
						...companyColumn,
						...wideColumns([
							{ column: 'Recipients', size: 'small' },
							{ column: 'Starts', size: 'medium' }
						])
					]}
					hasData={!!tables.scheduled.rows.length}
					hasNextPage={tables.scheduled.hasNextPage}
					plural="scheduled programs"
					pagination={tables.scheduled.params}
					hasActions={false}
				>
					{#each tables.scheduled.rows as program (program.id)}
						<TableRow>
							<TableCell>
								<div class="min-w-0 max-sm:py-1.5">
									<a
										href={`/program/${program.id}`}
										class="pc-cell-link block min-w-0 truncate py-1"
									>
										{program.name}
									</a>
									<!-- phones show the details here instead of in columns -->
									<span class="block sm:hidden text-xs text-gray-500 dark:text-gray-400"
										>{companyPrefix(program)}{recipientsText(
											program
										)}{#if program.startAt}{' · Starts '}{shortRelative(
												program.startAt,
												relativeNow
											)}{/if}</span
									>
								</div>
							</TableCell>
							{#if showCompanyColumn}
								<TableCell value={program.company?.name}
									><span class="text-slate-400 dark:text-gray-500">Shared</span></TableCell
								>
							{/if}
							{#if !isPhone}
								<TableCell value={String(program.enrolled ?? 0)} />
								{#if program.startAt}
									<TableCell>
										<span title={new Date(program.startAt).toLocaleString()}
											>{shortRelative(program.startAt, relativeNow)}</span
										>
									</TableCell>
								{:else}
									<TableCell />
								{/if}
							{/if}
						</TableRow>
					{/each}
				</Table>
			</div>
		</section>
	{/if}

	{#if stats?.paused > 0}
		<section id="paused" class="mt-8 mb-8 scroll-mt-28">
			<SubHeadline>Paused programs</SubHeadline>
			<div class="mt-4">
				<Table
					reserveSelect
					isGhost={tables.paused.isGhost}
					columns={[
						{ column: 'Name', size: 'large' },
						...companyColumn,
						...wideColumns([
							{ column: 'Recipients', size: 'small' },
							{ column: 'Paused since', size: 'medium' }
						])
					]}
					hasData={!!tables.paused.rows.length}
					hasNextPage={tables.paused.hasNextPage}
					plural="paused programs"
					pagination={tables.paused.params}
					hasActions={false}
				>
					{#each tables.paused.rows as program (program.id)}
						<TableRow>
							<TableCell>
								<div class="min-w-0 max-sm:py-1.5">
									<a
										href={`/program/${program.id}`}
										class="pc-cell-link block min-w-0 truncate py-1"
									>
										{program.name}
									</a>
									<!-- phones show the details here instead of in columns -->
									<span class="block sm:hidden text-xs text-gray-500 dark:text-gray-400"
										>{companyPrefix(program)}{recipientsText(
											program
										)}{#if program.pausedAt}{' · Paused '}{shortRelative(
												program.pausedAt,
												relativeNow
											)}{/if}</span
									>
								</div>
							</TableCell>
							{#if showCompanyColumn}
								<TableCell value={program.company?.name}
									><span class="text-slate-400 dark:text-gray-500">Shared</span></TableCell
								>
							{/if}
							{#if !isPhone}
								<TableCell value={String(program.enrolled ?? 0)} />
								{#if program.pausedAt}
									<TableCell>
										<span title={new Date(program.pausedAt).toLocaleString()}
											>{shortRelative(program.pausedAt, relativeNow)}</span
										>
									</TableCell>
								{:else}
									<TableCell />
								{/if}
							{/if}
						</TableRow>
					{/each}
				</Table>
			</div>
		</section>
	{/if}
</main>
