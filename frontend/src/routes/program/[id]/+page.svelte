<script>
	import { onMount } from 'svelte';
	import { page } from '$app/stores';
	import { goto } from '$app/navigation';
	import { api } from '$lib/api/apiProxy.js';
	import Headline from '$lib/components/Headline.svelte';
	import SubHeadline from '$lib/components/SubHeadline.svelte';
	import HeadTitle from '$lib/components/HeadTitle.svelte';
	import Breadcrumb from '$lib/components/Breadcrumb.svelte';
	import ActionButton from '$lib/components/ActionButton.svelte';
	import AutoRefresh from '$lib/components/AutoRefresh.svelte';
	import StatusBadge from '$lib/components/StatusBadge.svelte';
	import ToolTip from '$lib/components/ToolTip.svelte';
	import StatsCard from '$lib/components/StatsCard.svelte';
	import Table from '$lib/components/table/Table.svelte';
	import TableRow from '$lib/components/table/TableRow.svelte';
	import TableCell from '$lib/components/table/TableCell.svelte';
	import TableDropDownEllipsis from '$lib/components/table/TableDropDownEllipsis.svelte';
	import TableDropDownButton from '$lib/components/table/TableDropDownButton.svelte';
	import { shortRelative } from '$lib/utils/shortRelative.js';
	import ProgramTrendChart from '$lib/components/program/ProgramTrendChart.svelte';
	import Alert from '$lib/components/Alert.svelte';
	import DeleteAlert from '$lib/components/modal/DeleteAlert.svelte';
	import ConditionalDisplay from '$lib/components/ConditionalDisplay.svelte';
	import ProgramCloneModal from '$lib/components/program/ProgramCloneModal.svelte';
	import ProgramPeopleTable from '$lib/components/program/ProgramPeopleTable.svelte';
	import ProgramReportModal from '$lib/components/program/ProgramReportModal.svelte';
	import ProgramReportedUpload from '$lib/components/program/ProgramReportedUpload.svelte';
	import { addToast } from '$lib/store/toast';
	import { newTableURLParams } from '$lib/service/tableURLParams.js';
	import { AppStateService } from '$lib/service/appState';
	import { fetchAllRows } from '$lib/utils/api-utils';
	import {
		ALL_WEEK_DAYS,
		HIDDEN_TIP,
		NONE_YET,
		browserTimeZone,
		RISK_TIP,
		MINUTES_PER_DAY,
		MINUTES_PER_HOUR,
		capitalize,
		describeBlackout,
		describeDays,
		describeDuration,
		difficultyTone,
		formatDate,
		formatDateTime,
		plural,
		programStatus,
		ratePercent,
		weekDaysFromMask
	} from '$lib/utils/programText.js';

	const programID = $page.params.id;
	const appStateService = AppStateService.instance;
	let contextCompanyID = '';

	let program = null;
	let stats = null;
	let view = null;
	let results = { months: [], templates: [] };
	// the groups of the program as { id, name }
	let groups = [];
	let showMonthTable = false;
	// earliest upcoming send, taken from the program list
	let nextSendAt = null;
	let isReportPDFEnabled = false;
	// set when the program could not be loaded
	let loadError = '';

	// people table, paged by the server
	const peopleParams = newTableURLParams({
		sortBy: 'risk',
		sortOrder: 'desc',
		prefix: 'people',
		noScroll: true
	});
	let peopleRows = [];
	let peopleHasNextPage = false;
	let isPeopleLoading = false;
	let now = Date.now();
	// column names in the table header mapped to the sort names of the endpoint
	const peopleSortNames = {
		person: 'name',
		status: 'status',
		risk: 'risk',
		'next email': 'next_due_at'
	};

	const loadGroupNames = async (ids) => {
		if (!ids?.length) {
			groups = [];
			return;
		}
		try {
			const res = await Promise.all(ids.map((id) => api.recipient.getGroupByID(id)));
			groups = res
				.filter((r) => r.success && r.data?.name)
				.map((r) => ({ id: r.data.id, name: r.data.name }));
		} catch (e) {
			groups = [];
		}
	};

	// names for the settings card, only fetched while the card is shown
	let settingNames = { templates: {}, lists: [], denyPage: '', evasionPage: '', webhooks: [] };
	let settingNamesFor = '';
	// only the newest request may set the names
	let settingNamesSeq = 0;
	const nameOf = async (fn, id) => {
		try {
			const res = await fn(id);
			return res.success ? res.data : null;
		} catch (e) {
			return null;
		}
	};
	const loadSettingNames = async (p) => {
		const key = JSON.stringify([
			p.steps?.map((s) => s.campaignTemplateID),
			p.allowDenyIDs,
			p.denyPageID,
			p.evasionPageID,
			p.webhooks?.map((w) => w.webhookID)
		]);
		if (key === settingNamesFor) return;
		settingNamesFor = key;
		const seq = ++settingNamesSeq;
		const templateIDs = [...new Set((p.steps ?? []).map((s) => s.campaignTemplateID))];
		const [templates, lists, denyPage, evasionPage, webhooks] = await Promise.all([
			Promise.all(templateIDs.map((id) => nameOf(api.campaignTemplate.getByID, id))),
			Promise.all((p.allowDenyIDs ?? []).map((id) => nameOf(api.allowDeny.getByID, id))),
			p.denyPageID ? nameOf(api.page.getByID, p.denyPageID) : null,
			p.evasionPageID ? nameOf(api.page.getByID, p.evasionPageID) : null,
			Promise.all((p.webhooks ?? []).map((w) => nameOf(api.webhook.getByID, w.webhookID)))
		]);
		if (seq !== settingNamesSeq) return;
		settingNames = {
			templates: Object.fromEntries(
				templateIDs.map((id, i) => [id, templates[i]?.name ?? 'Unknown template'])
			),
			lists: lists.filter(Boolean),
			denyPage: denyPage?.name ?? '',
			evasionPage: evasionPage?.name ?? '',
			webhooks: webhooks.filter(Boolean).map((w) => w.name)
		};
	};

	// people a draft or scheduled program would email when it starts
	let audienceCount = null;
	const loadAudience = async (p) => {
		if (!p.recipientGroupIDs?.length) {
			audienceCount = 0;
			return;
		}
		try {
			const res = await api.program.audience(p.recipientGroupIDs, p.companyID);
			audienceCount = res.success ? (res.data?.people ?? null) : null;
		} catch (e) {
			audienceCount = null;
		}
	};

	// only the newest people request may set the rows
	let peopleSeq = 0;
	const loadPeople = async () => {
		const seq = ++peopleSeq;
		isPeopleLoading = true;
		try {
			const sortBy = peopleSortNames[(peopleParams.sortBy || '').toLowerCase()] ?? 'risk';
			const res = await api.program.recipients(programID, {
				currentPage: peopleParams.currentPage,
				perPage: peopleParams.perPage,
				sortBy,
				sortOrder: peopleParams.sortOrder,
				search: peopleParams.search
			});
			if (seq !== peopleSeq) return;
			// an unknown or hidden program shows the error state instead
			if (!res.success && [400, 403, 404].includes(res.statusCode)) return;
			if (!res.success) {
				throw res.error;
			}
			view = res.data;
			peopleRows = res.data?.rows ?? [];
			peopleHasNextPage = !!res.data?.hasNextPage;
		} catch (e) {
			if (seq === peopleSeq) {
				addToast('Failed to load the people of the program', 'Error');
			}
			console.error('failed to load program people', e);
		} finally {
			if (seq === peopleSeq) {
				isPeopleLoading = false;
			}
		}
	};

	const load = async () => {
		try {
			const [pRes, sRes, rRes] = await Promise.all([
				api.program.getByID(programID),
				api.program.statistics(programID),
				api.program.results(programID),
				loadPeople()
			]);
			if (rRes.success) results = rRes.data ?? { months: [], templates: [] };
			if (sRes.success) stats = sRes.data;
			if (!pRes.success && !program) {
				// an unknown id or a program in a company the user can not see
				loadError = pRes.error || 'Program not found';
				return;
			}
			if (pRes.success) {
				const groupsChanged =
					(program?.recipientGroupIDs ?? []).join() !== (pRes.data.recipientGroupIDs ?? []).join();
				program = pRes.data;
				nextSendAt = program.nextSendAt ?? null;
				if (groupsChanged || !groups.length) loadGroupNames(program.recipientGroupIDs);
				loadSettingNames(program);
				if (program.status === 'draft' || program.status === 'scheduled') loadAudience(program);
			}
			now = Date.now();
		} catch (e) {
			addToast('Failed to load program', 'Error');
			console.error('failed to load program detail', e);
		}
	};

	onMount(() => {
		contextCompanyID = appStateService.getContext()?.companyID || '';
		load();
		(async () => {
			const opt = await api.option.get('report_pdf_enabled');
			isReportPDFEnabled = opt.success && opt.data?.value === 'true';
		})();
		peopleParams.onChange(loadPeople);
		// keeps the short relative times current
		const tick = setInterval(() => (now = Date.now()), 60000);
		return () => {
			peopleParams.unsubscribe();
			clearInterval(tick);
		};
	});

	const pauseResume = async () => {
		try {
			const res = program.pausedAt
				? await api.program.resume(program.id)
				: await api.program.pause(program.id);
			if (!res.success) {
				throw res.error;
			}
			addToast(program.pausedAt ? 'Program resumed' : 'Program paused', 'Success');
			load();
		} catch (e) {
			addToast(
				program.pausedAt ? 'Failed to resume the program' : 'Failed to pause the program',
				'Error'
			);
			console.error('failed to change program state', e);
		}
	};

	// start now asks first, with the number of people enrolled, as on the list page
	let isStartAlertVisible = false;
	const openStartAlert = () => {
		if (program?.recipientGroupIDs?.length) loadAudience(program);
		isStartAlertVisible = true;
	};
	// the alert shows the error text of the server, such as an end that has passed
	const onStart = async () => {
		const res = await api.program.start(program.id);
		if (res.success) {
			addToast('Program started', 'Success');
			load();
		}
		return res;
	};

	let isCloseAlertVisible = false;

	const onClose = async () => {
		const res = await api.program.close(program.id);
		if (res.success) {
			addToast('Program closed', 'Success');
			isCloseAlertVisible = false;
			load();
		}
		return res;
	};

	let isDeleteAlertVisible = false;
	const onDelete = async () => {
		const res = await api.program.delete(program.id);
		if (res.success) {
			addToast('Program deleted', 'Success');
			goto('/program');
		}
		return res;
	};

	const openUpdate = () => goto(`/program?update=${program.id}`);

	const onExportCSV = async () => {
		try {
			const res = await api.program.exportCSV(program.id);
			if (!res.success) {
				addToast(res.error || 'Failed to export the program results', 'Error');
			}
		} catch (e) {
			addToast('Failed to export the program results', 'Error');
			console.error('failed to export program results', e);
		}
	};

	// pdf report, reported csv upload and clone dialogs
	let reportModal;
	let reportedUpload;
	let cloneProgram = null;
	let companies = [];
	const openClone = async () => {
		try {
			companies = await fetchAllRows((options) => api.company.getAll(options));
		} catch (e) {
			companies = [];
		}
		cloneProgram = program;
	};

	// formatting helpers
	// the day list is left out when every day or no day is set
	const daysText = (mask) =>
		!mask || mask === ALL_WEEK_DAYS ? '' : describeDays(weekDaysFromMask(mask));
	const rateText = (part, sent) => (sent > 0 ? `${Math.round((part / sent) * 100)}%` : NONE_YET);
	const pct = (frac) => (frac == null ? NONE_YET : `${Math.round(frac * 100)}%`);
	const fmtMinutes = (m) => {
		if (m == null) return NONE_YET;
		if (m < MINUTES_PER_HOUR) return plural(Math.max(1, Math.round(m)), 'minute');
		if (m < MINUTES_PER_DAY) {
			const h = Math.floor(m / MINUTES_PER_HOUR);
			const rest = Math.round(m % MINUTES_PER_HOUR);
			return rest ? `${h} h ${rest} min` : plural(h, 'hour');
		}
		return plural(Math.round(m / MINUTES_PER_DAY), 'day');
	};
	const isHidden = (row, key = 'sent') => !!row?.suppressed || row?.[key] == null;
	const isPast = (v, at) => !!v && new Date(v).getTime() <= at;

	// state
	$: isClosed = program?.status === 'closed' || !!program?.closedAt;
	$: isDraft = !isClosed && program?.status === 'draft';
	$: isScheduled = !isClosed && program?.status === 'scheduled';
	$: notStarted = isDraft || isScheduled;
	$: isPaused = !isClosed && !notStarted && !!program?.pausedAt;
	$: warnings = !isClosed ? (program?.warnings ?? []) : [];
	$: programState = programStatus(program);
	$: status = programState.label;
	$: statusTone = programState.tone;

	// people
	$: isAnonymous = !!view?.anonymous || !!program?.isAnonymous;
	$: stepCount = view?.stepCount ?? (program?.steps ?? []).length;

	// health of an active program
	$: activeCount = view?.active ?? 0;
	$: staleAfterMs =
		((program?.stepIntervalMinutes ?? 0) + (program?.spreadMinutes ?? 0)) * 60000 + 86400000;
	$: sinceLastMs = results?.lastSentAt ? now - new Date(results.lastSentAt).getTime() : 0;
	$: isStale = status === 'Active' && activeCount > 0 && sinceLastMs > staleAfterMs;
	$: staleDays = Math.floor(sinceLastMs / 86400000);
	$: isOverdue = isPast(nextSendAt, now);
	// the count is exact only when every person fits on the first page without a search
	$: dueNow =
		!isAnonymous &&
		!peopleHasNextPage &&
		(peopleParams.currentPage ?? 1) <= 1 &&
		!peopleParams.search
			? peopleRows.filter((r) => r.status === 'active' && isPast(r.nextDueAt, now)).length
			: null;
	// nobody enrolled and nothing sent, so the page has nothing to show
	$: notEnrolled =
		!isClosed && warnings.length > 0 && (view?.total ?? 0) === 0 && !(stats?.emailsSent > 0);
	$: showSettings = notStarted || warnings.length > 0;

	// results
	$: months = results?.months ?? [];
	$: totalSent = months.reduce((n, m) => n + (m.sent ?? 0), 0);
	$: nothingSent = !results?.lastSentAt && totalSent === 0 && !(stats?.emailsSent > 0);
	$: last30 = results?.last30 ?? { sent: 0, clicked: 0, submitted: 0, reported: 0 };
	$: prev30 = results?.previous30 ?? { sent: 0, clicked: 0, submitted: 0, reported: 0 };
	$: last30Hidden = isHidden(last30);
	$: openCount = last30?.open ?? 0;
	// fewer emails than this in either period make a change meaningless
	const MIN_COMPARE = 20;
	const compare = (key, lowerIsBetter, cur30, before30) => {
		const hidden = isHidden(cur30, key);
		const cur = hidden ? null : ratePercent(cur30[key], cur30.sent);
		const prev = isHidden(before30, key) ? null : ratePercent(before30[key], before30.sent);
		const tooFew =
			cur !== null && prev !== null && (cur30.sent < MIN_COMPARE || before30.sent < MIN_COMPARE);
		const delta = cur !== null && prev !== null && !tooFew ? Math.round(cur - prev) : null;
		const good = delta === null || delta === 0 ? null : lowerIsBetter ? delta < 0 : delta > 0;
		const base = cur !== null ? `${cur30[key]} of ${cur30.sent}` : '';
		return { hidden, cur, prev, delta, good, tooFew, base };
	};
	$: reportKpi = compare('reported', false, last30, prev30);
	$: recentKpis = [
		{ label: 'Click rate, 30 days', ...compare('clicked', true, last30, prev30) },
		{ label: 'Submit rate, 30 days', ...compare('submitted', true, last30, prev30) }
	];
	$: phishProne = results?.phishProne ?? null;
	$: repeat = results?.repeatClickers ?? null;
	$: coverage = results?.coverage ?? null;
	$: coverageLow = coverage?.rate != null && coverage.rate < 0.9;
	$: resilience = results?.resilience ?? null;
	// values an anonymous program leaves out are listed by name
	$: resilienceHidden = (key) => !!resilience?.withheld?.includes(key);
	$: reportCount = resilience?.sent
		? Math.round(resilience.sent * (resilience.reportRate ?? 0))
		: 0;
	$: ratioText = (() => {
		const r = resilience?.resilienceRatio;
		if (r == null || r <= 0) return '';
		if (r >= 1) return `${r.toFixed(1)} reports for every click.`;
		return `1 report for every ${Math.round(1 / r)} clicks.`;
	})();
	$: exposure = results?.exposure ?? [];
	$: exposureShown = exposure.filter(
		(e) => !e.suppressed && e.clickRate != null && e.reportRate != null && e.sent > 0
	);
	// with the same order for everyone the numbers mostly show the templates, so no sentence
	$: improvementText = (() => {
		if (program?.orderMode === 'sequential' || exposureShown.length < 2) return '';
		const a = exposureShown[0];
		const b = exposureShown[exposureShown.length - 1];
		const move = (what, from, to) =>
			pct(from) === pct(to)
				? `${what} stayed at ${pct(from)} from the ${a.label} email to the ${b.label}.`
				: `${what} went from ${pct(from)} on the ${a.label} email to ${pct(to)} on the ${b.label}.`;
		return `${move('Click rate', a.clickRate, b.clickRate)} ${move('Report rate', a.reportRate, b.reportRate)}`;
	})();

	$: templates = (results?.templates ?? [])
		.map((t) => ({
			...t,
			clickRate: isHidden(t, 'clicked') ? null : (ratePercent(t.clicked, t.sent) ?? 0)
		}))
		.sort((a, b) => (b.clickRate ?? -1) - (a.clickRate ?? -1));
	$: visibleTemplates = templates.filter((t) => t.clickRate !== null);
	$: templateSent = visibleTemplates.reduce((n, t) => n + (t.sent ?? 0), 0);
	$: avgClickRate =
		templateSent > 0
			? (visibleTemplates.reduce((n, t) => n + (t.clicked ?? 0), 0) / templateSent) * 100
			: 0;
	// marked Most clicked only with enough sends to mean something
	const WEAK_MIN_SENT = 20;
	const WEAK_MIN_POINTS = 5;
	$: isWeak = (t) =>
		t.clickRate !== null &&
		t.sent >= WEAK_MIN_SENT &&
		t.clickRate >= avgClickRate + WEAK_MIN_POINTS;
	$: weakTip = `At least ${WEAK_MIN_SENT} emails and a click rate ${WEAK_MIN_POINTS} or more points above the program average of ${Math.round(avgClickRate)}%`;

	// program details rows, named like the wizard fields
	$: windowZone =
		program?.timeZone && program.timeZone !== browserTimeZone ? `, ${program.timeZone}` : '';
	$: scheduledSteps = [...(program?.steps ?? [])].sort(
		(a, b) => (a.position ?? 0) - (b.position ?? 0)
	);
	$: filterText = (() => {
		const lists = settingNames.lists;
		if (!program?.allowDenyIDs?.length) return 'Off';
		const allow = lists[0]?.allowed;
		const names = lists.map((l) => l.name).join(', ');
		return `${allow ? 'Only allow' : 'Block'}: ${names || plural(program.allowDenyIDs.length, 'list')}`;
	})();
	$: detailRows = !program
		? []
		: [
				{ label: 'Recipient Groups', groups: true },
				{
					label: 'Templates',
					list: scheduledSteps.map(
						(s) => settingNames.templates[s.campaignTemplateID] ?? 'Loading...'
					),
					ordered: true
				},
				{ label: 'Time Between Emails', value: describeDuration(program.stepIntervalMinutes) },
				{
					label: 'Spread Emails Over',
					value: program.spreadMinutes > 0 ? describeDuration(program.spreadMinutes) : 'All at once'
				},
				...(stepCount > 1
					? [
							{
								label: 'Template Order',
								value:
									program.orderMode === 'sequential'
										? 'Same order for everyone'
										: 'Random order per person'
							}
						]
					: []),
				{
					label: 'After the Last Template',
					value: program.loopMode === 'complete' ? 'Stop' : 'Start over'
				},
				{
					label: 'First Email to New Members',
					value:
						program.newMemberDelayMinutes > 0
							? `After ${describeDuration(program.newMemberDelayMinutes)}`
							: 'Right away'
				},
				{
					label: 'Sending Days',
					value: daysText(program.constraintWeekDays)
						? capitalize(daysText(program.constraintWeekDays))
						: 'Any day'
				},
				{
					label: 'Sending Hours',
					value:
						program.constraintStartTime && program.constraintEndTime
							? `${program.constraintStartTime} to ${program.constraintEndTime}${windowZone}`
							: 'Any time'
				},
				{
					label: 'Skip Dates',
					list: (program.blackoutDates ?? []).map((r) => describeBlackout(r)),
					value: 'None'
				},
				{
					label: 'Start',
					value: program.startedAt
						? `Started ${formatDateTime(program.startedAt)}`
						: isScheduled
							? formatDateTime(program.startAt)
							: 'Not set, saved as a draft'
				},
				{
					label: 'End',
					value: program.endAt
						? formatDateTime(program.endAt)
						: 'Never, runs until you close it'
				},
				{ label: 'Anonymous', value: program.isAnonymous ? 'Yes' : 'No' },
				{
					label: 'Webhooks',
					value: program.webhooks?.length
						? settingNames.webhooks.join(', ') || plural(program.webhooks.length, 'webhook')
						: 'None'
				},
				{ label: 'Filtering', value: filterText, blackbox: true },
				{ label: 'Deny Page', value: settingNames.denyPage || 'None', blackbox: true },
				{ label: 'Evasion Page', value: settingNames.evasionPage || 'None', blackbox: true }
			];

	// lifetime reach, counted in people
	// a count is null when it is hidden to protect anonymity, it is then left out
	$: reachText = !stats
		? ''
		: [
				['emailsSent', (n) => `${plural(n, 'person', 'people')} emailed`],
				['clickedLink', (n) => `${n} clicked at least once`],
				['submittedData', (n) => `${n} submitted data`],
				['reported', (n) => `${n} reported`]
			]
				.filter(([key]) => stats[key] !== null)
				.map(([key, text]) => text(stats[key] ?? 0))
				.join(', ');

	// card, tile and table cell looks shared with the campaign page
	const card =
		'bg-white dark:bg-gray-900/80 rounded-lg shadow-md dark:shadow-none dark:ring-1 dark:ring-gray-600/30 transition-colors duration-200';
	const cardTitle =
		'text-lg font-semibold text-pc-darkblue dark:text-white mb-4 pb-2 border-b border-gray-200 dark:border-gray-700 transition-colors duration-200';
	// stat card icons, outline paths drawn on a 24 grid
	const iconCls = 'h-5 w-5 ml-2';
	const icons = {
		users:
			'M12 4.354a4 4 0 110 5.292M15 21H3v-1a6 6 0 0112 0v1zm0 0h6v-1a6 6 0 00-9-5.197M13 7a4 4 0 11-8 0 4 4 0 018 0z',
		clock: 'M12 6v6h4.5m4.5 0a9 9 0 11-18 0 9 9 0 0118 0z',
		mail: 'M21.75 6.75v10.5a2.25 2.25 0 01-2.25 2.25h-15a2.25 2.25 0 01-2.25-2.25V6.75m19.5 0A2.25 2.25 0 0019.5 4.5h-15a2.25 2.25 0 00-2.25 2.25m19.5 0v.243a2.25 2.25 0 01-1.07 1.916l-7.5 4.615a2.25 2.25 0 01-2.36 0L3.32 8.91a2.25 2.25 0 01-1.07-1.916V6.75',
		list: 'M8.25 6.75h12M8.25 12h12m-12 5.25h12M3.75 6.75h.007v.008H3.75V6.75zm.375 0a.375.375 0 11-.75 0 .375.375 0 01.75 0zM3.75 12h.007v.008H3.75V12zm.375 0a.375.375 0 11-.75 0 .375.375 0 01.75 0zm-.375 5.25h.007v.008H3.75v-.008zm.375 0a.375.375 0 11-.75 0 .375.375 0 01.75 0z',
		link: 'M13.19 8.688a4.5 4.5 0 011.242 7.244l-4.5 4.5a4.5 4.5 0 01-6.364-6.364l1.757-1.757m13.35-.622l1.757-1.757a4.5 4.5 0 00-6.364-6.364l-4.5 4.5a4.5 4.5 0 001.242 7.244',
		flag: 'M3 3v1.5M3 21v-6m0 0l2.77-.693a9 9 0 016.208.682l.108.054a9 9 0 006.086.71l3.114-.732a48.524 48.524 0 01-.005-10.499l-3.11.732a9 9 0 01-6.085-.711l-.108-.054a9 9 0 00-6.208-.682L3 4.5M3 15V4.5',
		repeat:
			'M16.023 9.348h4.992v-.001M2.985 19.644v-4.992m0 0h4.992m-4.993 0l3.181 3.183a8.25 8.25 0 0013.803-3.7M4.031 9.865a8.25 8.25 0 0113.803-3.7l3.181 3.182m0-4.991v4.99',
		check: 'M9 12.75L11.25 15 15 9.75M21 12a9 9 0 11-18 0 9 9 0 0118 0z'
	};
	const stripLabel = 'text-sm font-semibold text-slate-500 dark:text-gray-400';
	const stripValue = 'text-xl font-bold tabular-nums text-pc-darkblue dark:text-gray-100';
	const intro = 'mt-1 text-sm text-gray-600 dark:text-gray-400 max-w-3xl';
	const infoBox =
		'rounded-md bg-grayblue-light dark:bg-gray-800/60 px-4 py-3 text-sm text-slate-600 dark:text-gray-300 transition-colors duration-200';
	const warnBox =
		'rounded-md bg-yellow-50 dark:bg-yellow-900/30 px-4 py-3 text-sm text-yellow-800 dark:text-yellow-200 transition-colors duration-200';
	const inlineLink =
		'pc-focus rounded-sm font-medium text-cta-blue dark:text-highlight-blue hover:underline';
	const hiddenCls = 'cursor-help underline decoration-dotted';
	const muted = 'text-gray-400 dark:text-gray-500';
	const clickBar = 'bg-page-visited';
	const reportBar = 'bg-reported dark:bg-slate-400';
	// the label and value of a program details row, as on the campaign page
	const detailLabel = 'shrink-0 text-gray-600 dark:text-gray-400';
	const detailValue = 'min-w-0 text-right text-pc-darkblue dark:text-white break-words';
	// a rate change against the 30 days before, as "Up 4 points from 12%"
	const changeText = (k) =>
		`${k.delta > 0 ? 'Up' : 'Down'} ${Math.abs(k.delta)} ${Math.abs(k.delta) === 1 ? 'point' : 'points'} from ${Math.round(k.prev)}%`;
</script>

<HeadTitle title={program ? program.name : 'Program'} />
<main>
	{#if program}
		<Breadcrumb crumbs={[{ label: 'Programs', href: '/program/' }, { label: program.name ?? '' }]} />
		<div class="mb-6 flex flex-wrap items-center justify-between gap-x-6 gap-y-3">
			<div class="min-w-0 flex flex-wrap items-center gap-x-3 gap-y-1">
				<Headline docSlug="programs" preserveCase
					><span class="select-text">{program.name}</span></Headline
				>
				<StatusBadge tone={statusTone}>{status}</StatusBadge>
			</div>
			<div class="flex flex-wrap items-center gap-x-4 gap-y-3">
				<div class="flex flex-wrap items-center gap-2" role="group" aria-label="Program actions">
					{#if notStarted}
						<ActionButton icon="play" on:click={openStartAlert}>Start Now</ActionButton>
					{/if}
					{#if !isClosed}
						<ActionButton
							variant={notStarted || isPaused ? 'secondary' : 'primary'}
							icon="edit"
							on:click={openUpdate}>Update</ActionButton
						>
						{#if isPaused}
							<ActionButton icon="play" on:click={pauseResume}>Resume</ActionButton>
						{:else if !notStarted}
							<ActionButton variant="secondary" icon="pause" on:click={pauseResume}>Pause</ActionButton>
						{/if}
					{/if}
					<TableDropDownEllipsis text="Actions" label="More program actions">
						{#if !notStarted}
							<TableDropDownButton name="Export CSV" on:click={onExportCSV} />
							<TableDropDownButton name="PDF Report" on:click={() => reportModal.open()} />
							<TableDropDownButton
								name="Import Reported Emails"
								disabled={isAnonymous}
								title={isAnonymous
									? 'Not available for an anonymous program, reports come from the report inbox'
									: ''}
								on:click={() => reportedUpload.pick()}
							/>
						{/if}
						<TableDropDownButton name="Clone" on:click={openClone} />
						<TableDropDownButton name="Delete" on:click={() => (isDeleteAlertVisible = true)} />
					</TableDropDownEllipsis>
					{#if !isClosed}
						<ActionButton
							variant="danger-outline"
							icon="close"
							title="Stops all sending for good. Results are kept."
							on:click={() => (isCloseAlertVisible = true)}>Close</ActionButton
						>
					{/if}
				</div>
				<div class="sm:pl-4 sm:border-l sm:border-gray-200 dark:sm:border-gray-700">
					<AutoRefresh isLoading={false} onRefresh={load} />
				</div>
			</div>
		</div>
		<ProgramReportedUpload
			bind:this={reportedUpload}
			programID={program.id}
			anonymous={isAnonymous}
			on:uploaded={load}
		/>

		<!-- state and health notices -->
		<div class="space-y-3">
			{#if status === 'Active' && isStale}
				<div class="{warnBox} flex flex-wrap items-center justify-between gap-x-4 gap-y-2" role="status">
					<p>
						No email sent for {plural(staleDays, 'day')}.{#if dueNow}
							{dueNow === 1 ? '1 person is' : `${dueNow} people are`} due now.{/if} Check the sender
						and the sending window.
					</p>
					<button type="button" class={inlineLink} on:click={openUpdate}>Update program</button>
				</div>
			{/if}
			{#if isDraft}
				<div class={infoBox} role="status">
					Draft. Nothing is sent until you start it.{#if audienceCount != null}{' '}Starting sends
						the first emails to {plural(audienceCount, 'person', 'people')} from the next sending time.{/if}
				</div>
			{/if}
			{#if isScheduled}
				<div class={infoBox} role="status">
					Starts {formatDateTime(program.startAt)}.{#if audienceCount != null}{' '}The first
						emails go to {plural(audienceCount, 'person', 'people')}.{/if}
				</div>
			{/if}
			{#if warnings.length}
				<div class={warnBox} role="status">
					<p class="font-semibold">Needs attention</p>
					<ul class="mt-1 space-y-1">
						{#each warnings as w}
							<li class="flex flex-wrap gap-x-3">
								<span>{w}</span>
								<button type="button" class={inlineLink} on:click={openUpdate}>Update program</button>
							</li>
						{/each}
					</ul>
				</div>
			{/if}
			{#if isPaused}
				<div class={warnBox} role="status">
					Paused since {formatDateTime(program.pausedAt)}. No emails are sent. On resume, held emails
					are spread over the next sending window.
				</div>
			{/if}
			{#if isClosed}
				<div class={infoBox} role="status">
					Closed on {formatDateTime(program.closedAt)}.
					{nothingSent ? 'No emails were sent before the program was closed.' : 'Results are kept.'}
				</div>
			{/if}
		</div>

		<!-- facts -->
		{#if !notStarted && !notEnrolled}
			<div class="pc-stats-grid grid grid-cols-2 lg:grid-cols-4 gap-3 sm:gap-6 mt-4">
				<!-- a closed program has nobody active, so it shows everyone it reached -->
				<StatsCard
					title={isClosed ? 'People' : 'Active'}
					value={isClosed ? (view?.total ?? 0) : (view?.active ?? 0)}
					borderColor="border-recipients"
					iconColor="text-recipients"
				>
					<svg slot="icon" class="{iconCls} text-recipients" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="1.5" aria-hidden="true"
										><path stroke-linecap="round" stroke-linejoin="round" d={icons.users} /></svg
									>
					<svelte:fragment slot="sub">
						{view?.completed ?? 0} completed,{#if isClosed}{' '}{view?.stopped ?? 0} stopped at close,{/if}
						{view?.exited ?? 0} removed from groups
					</svelte:fragment>
				</StatsCard>
				<StatsCard
					title={isClosed ? 'Closed' : 'Next email'}
					borderColor="border-campaign-scheduled"
					iconColor="text-campaign-scheduled"
				>
					<svg slot="icon" class="{iconCls} text-campaign-scheduled" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="1.5" aria-hidden="true"
										><path stroke-linecap="round" stroke-linejoin="round" d={icons.clock} /></svg
									>
					<svelte:fragment slot="value">
						{#if isClosed}
							<span title={formatDateTime(program.closedAt)}>{formatDate(program.closedAt)}</span>
						{:else if isPaused}
							<span title="Held while paused">Held</span>
						{:else if isAnonymous}
							<span class={hiddenCls} title="Not shown for an anonymous program">Hidden</span>
						{:else if nextSendAt && isOverdue}
							<span class="text-yellow-800 dark:text-yellow-300">Overdue</span>
						{:else if nextSendAt}
							<span title={formatDateTime(nextSendAt)}>{shortRelative(nextSendAt, now)}</span>
						{:else}
							None
						{/if}
					</svelte:fragment>
					<svelte:fragment slot="sub">
						{#if !isClosed && !isPaused && !isAnonymous && nextSendAt && isOverdue}
							<span title={formatDateTime(nextSendAt)}>since {shortRelative(nextSendAt, now)}</span>
						{/if}
					</svelte:fragment>
				</StatsCard>
				<StatsCard title="Last email" borderColor="border-message-sent" iconColor="text-message-sent">
					<svg slot="icon" class="{iconCls} text-message-sent" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="1.5" aria-hidden="true"
										><path stroke-linecap="round" stroke-linejoin="round" d={icons.mail} /></svg
									>
					<svelte:fragment slot="value">
						{#if results?.lastSentAt}<span title={formatDateTime(results.lastSentAt)}
								>{shortRelative(results.lastSentAt, now)}</span
							>{:else if isClosed}Never{:else}None{/if}
					</svelte:fragment>
					<svelte:fragment slot="sub">{plural(totalSent, 'email')} sent</svelte:fragment>
				</StatsCard>
				{#if !isClosed || coverage?.rate != null}
				<StatsCard
					title="Emailed, 90 days"
					tip="Active people who got an email in the last 90 days."
					borderColor="border-campaign-active"
					iconColor="text-campaign-active"
				>
					<svg slot="icon" class="{iconCls} text-campaign-active" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="1.5" aria-hidden="true"
										><path stroke-linecap="round" stroke-linejoin="round" d={icons.check} /></svg
									>
					<svelte:fragment slot="value">
						{#if !coverage || coverage.rate == null}
							{NONE_YET}
						{:else}
							<span class={coverageLow ? 'text-yellow-800 dark:text-yellow-300' : ''}
								>{pct(coverage.rate)}</span
							>
						{/if}
					</svelte:fragment>
					<svelte:fragment slot="sub">
						{#if coverage && coverage.rate != null}
							{coverage.covered} of {coverage.active} active{coverageLow ? ', below 90%' : ''}
						{/if}
					</svelte:fragment>
				</StatsCard>
				{/if}
			</div>
		{/if}

		<div class="mt-10 flex flex-col gap-10">
			<!-- details, on top before the start or when something needs fixing, else last -->
			<section
				id="program-details"
				class="{card} p-6 max-w-2xl"
				class:order-last={!showSettings}
			>
				<h3 class={cardTitle}>Program Details</h3>
				<div class="space-y-2.5 text-sm">
					{#each detailRows as row}
						{#if row.blackbox}
							<ConditionalDisplay show="blackbox">
								<div class="flex justify-between gap-4">
									<span class={detailLabel}>{row.label}:</span>
									<span class={detailValue}>{row.value || 'None'}</span>
								</div>
							</ConditionalDisplay>
						{:else}
							<div class="flex justify-between gap-4">
								<span class={detailLabel}>{row.label}:</span>
								{#if row.groups}
									<div class={detailValue}>
										{#if groups.length}
											{#each groups as group, i}
												<a
													class="pc-focus rounded-sm text-cta-blue dark:text-highlight-blue hover:underline"
													href="/recipient/group/{group.id}">{group.name}</a
												>{#if i < groups.length - 1},&nbsp;{/if}
											{/each}
										{:else}
											None
										{/if}
									</div>
								{:else if row.list?.length}
									<ol class={detailValue}>
										{#each row.list as item, n}
											<li>{row.ordered ? `${n + 1}. ` : ''}{item}</li>
										{/each}
									</ol>
								{:else}
									<span class={detailValue}>{row.value || 'None'}</span>
								{/if}
							</div>
						{/if}
					{/each}
				</div>
				{#if notStarted}
					<p class="mt-4 text-sm text-gray-600 dark:text-gray-400">
						Results show here once the first emails go out.
					</p>
				{/if}
			</section>

			{#if notStarted}
				<!-- nothing more to show before the start -->
			{:else if notEnrolled}
				<p class={infoBox}>Nobody is enrolled yet, so there are no results.</p>
			{:else}
				{#if nothingSent}
					<div class={infoBox}>
						{#if isClosed}
							No emails were sent before the program was closed.
						{:else}
							No emails sent yet. Rates, the trend and results by template show here after the
							first emails go out.
						{/if}
					</div>
				{:else}
					<!-- key results -->
					<section id="program-key-results">
						<SubHeadline>Key Results</SubHeadline>
						{#if isAnonymous}
							<p id="program-anon-delay-note" class={intro}>
								Emails from the last 3 days are not counted yet.
							</p>
						{/if}
						<div class="grid grid-cols-2 lg:grid-cols-4 gap-3 sm:gap-6 mt-3">
							<StatsCard
								title="Report rate, 30 days"
								tip="Emails reported, out of emails counted in the last 30 days."
								borderColor="border-reported dark:border-slate-400"
								iconColor="text-reported dark:text-slate-300"
							>
								<svg slot="icon" class="{iconCls} text-reported dark:text-slate-300" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="1.5" aria-hidden="true"
										><path stroke-linecap="round" stroke-linejoin="round" d={icons.flag} /></svg
									>
								<svelte:fragment slot="value">
									{#if reportKpi.hidden}
										<span title={HIDDEN_TIP} class={hiddenCls}>Hidden</span>
									{:else if reportKpi.cur === null}
										{NONE_YET}
									{:else}
										<span title={reportKpi.base}>{Math.round(reportKpi.cur)}%</span>
									{/if}
								</svelte:fragment>
								<svelte:fragment slot="sub">
									{#if reportKpi.hidden}
										<!-- nothing to compare -->
									{:else if reportKpi.cur === null}
										{openCount > 0
											? `${plural(openCount, 'email')} too new to count`
											: 'No emails in 30 days'}
									{:else}
										<span class="flex flex-wrap items-center gap-1 tabular-nums">
											{#if reportKpi.tooFew}
										Too few emails to compare
									{:else if reportKpi.delta === null}
										No comparison yet
									{:else if reportKpi.delta === 0}
										No change
									{:else}
										<span
											class="inline-flex items-center gap-1 font-semibold {reportKpi.good
												? 'text-green-700 dark:text-green-400'
												: 'text-red-700 dark:text-red-400'}"
										>
											<svg class="w-3 h-3" viewBox="0 0 12 12" fill="currentColor" aria-hidden="true">
												{#if reportKpi.delta > 0}<path d="M6 1l5 7H1z" />{:else}<path d="M6 11L1 4h10z" />{/if}
											</svg>
											{changeText(reportKpi)}
										</span>
									{/if}
										</span>
									{/if}
								</svelte:fragment>
							</StatsCard>
							<StatsCard
								title="Phish prone, 12 months"
								tip="People who clicked at least once in the last 12 months, out of everyone emailed in that time."
								borderColor="border-page-visited"
								iconColor="text-page-visited"
							>
								<svg slot="icon" class="{iconCls} text-page-visited" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="1.5" aria-hidden="true"
										><path stroke-linecap="round" stroke-linejoin="round" d={icons.link} /></svg
									>
								<svelte:fragment slot="value">
									{#if !phishProne || phishProne.suppressed}
										<span title={HIDDEN_TIP} class={hiddenCls}>Hidden</span>
									{:else if !phishProne.people || phishProne.rate == null}
										{NONE_YET}
									{:else}
										{pct(phishProne.rate)}
									{/if}
								</svelte:fragment>
								<svelte:fragment slot="sub">
									{#if phishProne && !phishProne.suppressed && phishProne.people}
										{phishProne.prone} of {plural(phishProne.people, 'person', 'people')}
									{/if}
								</svelte:fragment>
							</StatsCard>
							<StatsCard
								title="Reported no click, all time"
								tip="Reported and did not click, of all emails sent. The best outcome."
								borderColor="border-training-completed"
								iconColor="text-training-completed"
							>
								<svg slot="icon" class="{iconCls} text-training-completed" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="1.5" aria-hidden="true"
										><path stroke-linecap="round" stroke-linejoin="round" d={icons.check} /></svg
									>
								<svelte:fragment slot="value">
									{#if resilience?.suppressed || resilienceHidden('reportedWithoutClickRate')}
										<span title={HIDDEN_TIP} class={hiddenCls}>Hidden</span>
									{:else if !resilience?.sent}
										{NONE_YET}
									{:else}
										{pct(resilience.reportedWithoutClickRate)}
									{/if}
								</svelte:fragment>
							</StatsCard>
							<StatsCard
								title="Median time to report, all time"
								tip="From the email to the report."
								borderColor="border-message-sent"
								iconColor="text-message-sent"
							>
								<svg slot="icon" class="{iconCls} text-message-sent" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="1.5" aria-hidden="true"
										><path stroke-linecap="round" stroke-linejoin="round" d={icons.clock} /></svg
									>
								<svelte:fragment slot="value">
									{#if resilience?.suppressed || resilienceHidden('medianMinutesToReport')}
										<span title={HIDDEN_TIP} class={hiddenCls}>Hidden</span>
									{:else}
										{fmtMinutes(resilience?.medianMinutesToReport)}
									{/if}
								</svelte:fragment>
								<svelte:fragment slot="sub">
									{#if reportCount}From {plural(reportCount, 'report')}{/if}
								</svelte:fragment>
							</StatsCard>
						</div>

						<!-- the secondary numbers -->
						<dl
							id="program-secondary"
							class="{card} mt-3 sm:mt-6 grid grid-cols-2 lg:grid-cols-4 [&>div]:border-gray-200 dark:[&>div]:border-gray-700 [&>div:nth-child(n+3)]:border-t lg:[&>div:nth-child(n+3)]:border-t-0 [&>div:nth-child(even)]:border-l lg:[&>div:not(:first-child)]:border-l"
						>
							{#each recentKpis as k}
								<div class="px-4 py-3 sm:px-6 sm:py-4 min-w-0">
									<dt class={stripLabel}>{k.label}</dt>
									{#if k.hidden}
										<dd class={stripValue}>
											<span title={HIDDEN_TIP} class={hiddenCls}>Hidden</span>
										</dd>
									{:else if k.cur === null}
										<dd class={stripValue}>{NONE_YET}</dd>
									{:else}
										<dd class="flex flex-wrap items-baseline gap-x-2 tabular-nums">
											<span class={stripValue}>{Math.round(k.cur)}%</span>
											<span class="text-xs text-gray-600 dark:text-gray-400">({k.base})</span>
										</dd>
										<dd class="mt-0.5 text-xs text-gray-600 dark:text-gray-400 tabular-nums">
											{#if k.tooFew}
										Too few emails to compare
									{:else if k.delta === null}
										No comparison yet
									{:else if k.delta === 0}
										No change
									{:else}
										<span
											class="inline-flex items-center gap-1 font-semibold {k.good
												? 'text-green-700 dark:text-green-400'
												: 'text-red-700 dark:text-red-400'}"
										>
											<svg class="w-3 h-3" viewBox="0 0 12 12" fill="currentColor" aria-hidden="true">
												{#if k.delta > 0}<path d="M6 1l5 7H1z" />{:else}<path d="M6 11L1 4h10z" />{/if}
											</svg>
											{changeText(k)}
										</span>
									{/if}
										</dd>
									{/if}
								</div>
							{/each}
							<div class="px-4 py-3 sm:px-6 sm:py-4 min-w-0">
								<dt class="{stripLabel} flex items-center">
									Reports per click, all time
									<ToolTip
										>{ratioText} Above 1 means more people report than click.</ToolTip
									>
								</dt>
								<dd class={stripValue}>
									{#if resilience?.suppressed || resilienceHidden('resilienceRatio')}
										<span title={HIDDEN_TIP} class={hiddenCls}>Hidden</span>
									{:else if !resilience?.sent}
										{NONE_YET}
									{:else}
										{resilience.resilienceRatio == null
											? 'No clicks'
											: resilience.resilienceRatio.toFixed(1)}
									{/if}
								</dd>
							</div>
							<div class="px-4 py-3 sm:px-6 sm:py-4 min-w-0">
								<dt class="{stripLabel} flex items-center">
									Emails counted
									<ToolTip
										>Emails older than 3 days. Newer emails are counted once people had time to react.</ToolTip
									>
								</dt>
								<dd class={stripValue}>
									{#if last30Hidden}
										<span title={HIDDEN_TIP} class={hiddenCls}>Hidden</span>
									{:else}
										{last30.sent ?? 0}
									{/if}
								</dd>
								{#if openCount > 0}
									<dd class="mt-0.5 text-xs text-gray-600 dark:text-gray-400 tabular-nums">
										{plural(openCount, 'email')} too new to count
									</dd>
								{/if}
							</div>
						</dl>
					</section>

					<!-- trend by month -->
					<section>
						<!-- the toggle sits next to the heading and starts at the left edge when it wraps -->
						<div class="mb-3 flex flex-wrap items-baseline gap-x-2 gap-y-1">
							<SubHeadline>Trend by Month</SubHeadline>
							{#if months.length >= 2}
								<button
									type="button"
									class="pc-focus rounded-sm text-sm font-medium text-cta-blue hover:underline dark:text-highlight-blue transition-colors duration-200"
									aria-expanded={showMonthTable}
									on:click={() => (showMonthTable = !showMonthTable)}
									>{showMonthTable ? 'Hide table' : 'Show table'}</button
								>
							{/if}
						</div>
						{#if months.length >= 2}
							<div class="{card} p-4 sm:p-6">
								<ProgramTrendChart {months} />
							</div>
						{:else if months.length === 1}
							<p class="text-sm text-gray-600 dark:text-gray-400">
								The trend shows from the second month.
							</p>
						{/if}
						{#if (showMonthTable && months.length >= 2) || months.length === 1}
							<div class="mt-3 -mb-12">
								<Table
									columns={[
										{ column: 'Month', size: 'large' },
										{ column: 'Emails', size: 'small', hideBelow: 'md', compact: true },
										{ column: 'Click rate', size: 'small', compact: true },
										{ column: 'Submit rate', size: 'small', hideBelow: 'md', compact: true },
										{ column: 'Report rate', size: 'small', compact: true }
									]}
									pagination={null}
									noSearch
									hasActions={false}
									plural="months"
									hasData={months.length > 0}
									hasNextPage={false}
								>
									{#each months as m (m.key)}
										<TableRow>
											<TableCell value={m.name} />
											{#each [['sent', 'md'], ['clicked', ''], ['submitted', 'md'], ['reported', '']] as [key, hideBelow]}
												<TableCell {hideBelow}>
													{#if isHidden(m, key)}
														<span title={HIDDEN_TIP} class={hiddenCls}>Hidden</span>
													{:else}
														<span class="tabular-nums"
															>{key === 'sent' ? m.sent : rateText(m[key], m.sent)}</span
														>
													{/if}
												</TableCell>
											{/each}
										</TableRow>
									{/each}
								</Table>
							</div>
						{/if}
					</section>

					<!-- results by template -->
					<section>
						<div class="mb-3 flex items-center">
							<SubHeadline>Results by Template</SubHeadline>
							<ToolTip>Highest click rate first.</ToolTip>
						</div>
						<div class="-mb-12">
							<Table
								columns={[
									{ column: 'Template', size: 'large' },
									{ column: 'Emails', size: 'small', hideBelow: 'md', compact: true },
									{ column: 'Clicked', size: 'medium', compact: true },
									{ column: 'Submitted', size: 'small', hideBelow: 'md', compact: true },
									{ column: 'Reported', size: 'medium', compact: true }
								]}
								pagination={null}
								noSearch
								hasActions={false}
								plural="results"
								hasData={templates.length > 0}
								hasNextPage={false}
							>
								{#each templates as t}
									{@const hidden = isHidden(t)}
									<TableRow>
										<TableCell wrap>
											<span class="flex items-center gap-2 min-w-0" title={t.name}>
												<span class="truncate">{t.name}</span>
												{#if t.difficulty}
													<span class="shrink-0"
														><StatusBadge tone={difficultyTone[t.difficulty] ?? 'neutral'}
															>{t.difficulty}</StatusBadge
														></span
													>
												{/if}
												{#if visibleTemplates.length > 1 && isWeak(t)}
													<span class="shrink-0 cursor-help"
														><StatusBadge tone="warning" title={weakTip}>Most clicked</StatusBadge></span
													>
												{/if}
											</span>
										</TableCell>
										{#if hidden}
											{#each ['md', '', 'md', ''] as hideBelow}
												<TableCell {hideBelow}>
													<span title={HIDDEN_TIP} class={hiddenCls}>Hidden</span>
												</TableCell>
											{/each}
										{:else}
											<TableCell hideBelow="md">
												<span class="tabular-nums">{t.sent}</span>
											</TableCell>
											{#each [['clicked', clickBar, ''], ['submitted', '', 'md'], ['reported', reportBar, '']] as [key, color, hideBelow]}
												<TableCell {hideBelow}>
													{#if isHidden(t, key)}
														<span title={HIDDEN_TIP} class={hiddenCls}>Hidden</span>
													{:else if color}
														<span class="flex items-center gap-2">
															<span class="w-16 shrink-0 tabular-nums">{rateText(t[key], t.sent)}</span>
															<span
																class="hidden sm:block w-16 h-1.5 rounded-full bg-gray-200 dark:bg-gray-700 overflow-hidden"
																aria-hidden="true"
															>
																<span
																	class="block h-full rounded-full {color}"
																	style="width: {Math.min(100, ratePercent(t[key], t.sent) ?? 0)}%"
																/>
															</span>
														</span>
													{:else}
														<span class="tabular-nums">{rateText(t[key], t.sent)}</span>
													{/if}
												</TableCell>
											{/each}
										{/if}
									</TableRow>
								{/each}
							</Table>
						</div>
					</section>

					<!-- repeat clickers -->
					<section id="program-repeat" class="grid grid-cols-2 lg:grid-cols-4 gap-3 sm:gap-6">
						<StatsCard
							title="Repeat clickers"
							tip="Clicked 2 or more of their last 6 emails."
							borderColor="border-repeat-link-clicks"
							iconColor="text-repeat-link-clicks"
						>
							<svg slot="icon" class="{iconCls} text-repeat-link-clicks" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="1.5" aria-hidden="true"
										><path stroke-linecap="round" stroke-linejoin="round" d={icons.repeat} /></svg
									>
							<svelte:fragment slot="value">
								{#if !repeat || repeat.suppressed || (repeat.eligible2 > 0 && repeat.twoPlus == null)}
									<span title={HIDDEN_TIP} class={hiddenCls}>Hidden</span>
								{:else if !repeat.eligible2}
									{NONE_YET}
								{:else}
									{repeat.twoPlus}
								{/if}
							</svelte:fragment>
							<svelte:fragment slot="sub">
								{#if repeat && !repeat.suppressed && repeat.twoPlus != null && repeat.eligible2}
									of {plural(repeat.eligible2, 'person', 'people')}
								{/if}
							</svelte:fragment>
						</StatsCard>
					</section>

					<!-- by email number -->
					<section>
						<div class="flex items-center">
							<SubHeadline>By Email Number</SubHeadline>
							<ToolTip>Click and report rate on each person's 1st, 2nd, 3rd email and so on.</ToolTip>
						</div>
						{#if program.orderMode === 'sequential'}
							<p class={intro}>
								Everyone gets the same order, so this mostly shows how hard each template is.
							</p>
						{:else if improvementText}
							<p class={intro}>{improvementText}</p>
						{/if}
						{#if exposure.length}
							<!-- the table keeps paging room below itself, which these tables do not use -->
							<div class="mt-3 -mb-12">
								<Table
									columns={[
										{ column: 'Email number', size: 'large' },
										{ column: 'Emails', size: 'small', hideBelow: 'md', compact: true },
										{ column: 'Click rate', size: 'medium', compact: true },
										{ column: 'Report rate', size: 'medium', compact: true }
									]}
									pagination={null}
									noSearch
									hasActions={false}
									plural="emails"
									hasData={exposure.length > 0}
									hasNextPage={false}
								>
									{#each exposure as e (e.n)}
										{@const hidden = (key) => e.suppressed || (e[key] == null && e.sent > 0)}
										<TableRow>
											<TableCell value={e.label} />
											<TableCell hideBelow="md">
												{#if e.sent == null}
													<span title={HIDDEN_TIP} class={hiddenCls}>Hidden</span>
												{:else}
													<span class="tabular-nums">{e.sent}</span>
												{/if}
											</TableCell>
											{#each [['clickRate', clickBar], ['reportRate', reportBar]] as [key, color]}
												<TableCell>
													{#if hidden(key)}
														<span title={HIDDEN_TIP} class={hiddenCls}>Hidden</span>
													{:else}
														<span class="flex items-center gap-2">
															<span class="w-16 shrink-0 tabular-nums">{pct(e[key])}</span>
															<span
																class="hidden sm:block w-20 h-1.5 rounded-full bg-gray-200 dark:bg-gray-700 overflow-hidden"
																aria-hidden="true"
															>
																<span
																	class="block h-full rounded-full {color}"
																	style="width: {Math.min(100, (e[key] ?? 0) * 100)}%"
																/>
															</span>
														</span>
													{/if}
												</TableCell>
											{/each}
										</TableRow>
									{/each}
								</Table>
							</div>
						{/if}
					</section>
				{/if}

				<!-- people -->
				<section>
					<div class="flex items-center">
						<SubHeadline>People</SubHeadline>
						{#if !isAnonymous}
							<ToolTip>Risk: {RISK_TIP}</ToolTip>
						{/if}
					</div>
					<p class={intro}>
						{plural(view?.total ?? 0, 'recipient')}.{#if reachText}{' '}All time: {reachText}.{/if}
					</p>
					{#if isAnonymous}
						<p class="{infoBox} mt-3">
							Anonymous program, so progress and risk per person are not shown. Counts cover all
							recipients.
						</p>
						<div class="pc-stats-grid grid grid-cols-2 lg:grid-cols-4 gap-3 sm:gap-6 mt-3">
							{#each [['Total', view?.total ?? 0, 'border-recipients', 'text-recipients'], (isClosed ? ['Stopped at close', view?.stopped ?? 0, 'border-campaign-active', 'text-campaign-active'] : ['Active', view?.active ?? 0, 'border-campaign-active', 'text-campaign-active']), ['Completed', view?.completed ?? 0, 'border-training-completed', 'text-training-completed'], ['Removed', view?.exited ?? 0, 'border-closed', 'text-closed']] as [label, value, border, color]}
								<StatsCard title={label} {value} borderColor={border} iconColor={color}>
									<svg slot="icon" class="{iconCls} {color}" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="1.5" aria-hidden="true"
										><path stroke-linecap="round" stroke-linejoin="round" d={icons.users} /></svg
									>
								</StatsCard>
							{/each}
						</div>
					{:else}
						<ProgramPeopleTable
							params={peopleParams}
							rows={peopleRows}
							hasNextPage={peopleHasNextPage}
							isLoading={isPeopleLoading}
							{stepCount}
							{isClosed}
							{isPaused}
							{now}
						/>
					{/if}
				</section>
			{/if}
		</div>

		<Alert
			headline="Start program"
			ok="Start"
			tone="affirmative"
			bind:visible={isStartAlertVisible}
			onConfirm={onStart}
		>
			<p class="mt-4 mb-2 text-gray-700 dark:text-gray-200 transition-colors duration-200">
				Start <strong>{program.name}</strong>?
				{#if audienceCount != null}
					{plural(audienceCount, 'person is', 'people are')} enrolled and the first emails go out
					from the next sending time.
				{:else}
					The first emails go out from the next sending time.
				{/if}
			</p>
		</Alert>

		<Alert headline="Close program" bind:visible={isCloseAlertVisible} onConfirm={onClose}>
			<div>
				<ul
					class="list-disc ml-8 mt-4 mb-4 text-gray-700 dark:text-gray-200 transition-colors duration-200"
				>
					<li>Stop enrolling and sending</li>
					<li>Links in sent emails and landing pages will stop working</li>
					<li>Results are kept</li>
					<li>A closed program cannot be resumed or edited</li>
				</ul>
			</div>
		</Alert>

		<ProgramReportModal
			bind:this={reportModal}
			programID={program.id}
			programName={program.name}
			enabled={isReportPDFEnabled}
			anonymous={isAnonymous}
			timeZone={program.timeZone || ''}
		/>

		<ProgramCloneModal
			bind:program={cloneProgram}
			{companies}
			{contextCompanyID}
			on:done={(e) => goto(`/program?update=${e.detail.id}`)}
		/>

		<DeleteAlert
			list={[
				'Deleting removes the program, its emails and all results',
				'Close the program instead to keep the results'
			]}
			name={program.name}
			onClick={onDelete}
			bind:isVisible={isDeleteAlertVisible}
		></DeleteAlert>
	{:else if loadError}
		<Breadcrumb crumbs={[{ label: 'Programs', href: '/program/' }, { label: 'Not found' }]} />
		<div id="program-load-error" class="{infoBox} mt-4 max-w-2xl" role="alert">
			<p>
				This program could not be loaded. It may have been deleted, or it belongs to a company
				you can not view.
			</p>
			<a class="{inlineLink} mt-2 inline-block" href="/program/">Back to programs</a>
		</div>
	{/if}
</main>
