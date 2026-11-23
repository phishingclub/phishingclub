<script>
	import { formatTimeConstraint, timeFormat } from '$lib/utils/date.js';
	import { onDestroy, onMount } from 'svelte';
	import { page } from '$app/stores';
	import { goto } from '$app/navigation';
	import { api } from '$lib/api/apiProxy.js';
	import Headline from '$lib/components/Headline.svelte';
	import SubHeadline from '$lib/components/SubHeadline.svelte';
	import HeadTitle from '$lib/components/HeadTitle.svelte';
	import Breadcrumb from '$lib/components/Breadcrumb.svelte';
	import ActionButton from '$lib/components/ActionButton.svelte';
	import Alert from '$lib/components/Alert.svelte';
	import AutoRefresh from '$lib/components/AutoRefresh.svelte';
	import Note from '$lib/components/Note.svelte';
	import StatusBadge from '$lib/components/StatusBadge.svelte';
	import ToolTip from '$lib/components/ToolTip.svelte';
	import ReportInboxRow from '$lib/components/ReportInboxRow.svelte';
	import { reportInboxStatus, reportInboxSettingsHref } from '$lib/utils/reportInbox.js';
	import StatTiles from '$lib/components/StatTiles.svelte';
	import Table from '$lib/components/table/Table.svelte';
	import TableRow from '$lib/components/table/TableRow.svelte';
	import TableCell from '$lib/components/table/TableCell.svelte';
	import TableDropDownEllipsis from '$lib/components/table/TableDropDownEllipsis.svelte';
	import TableDropDownButton from '$lib/components/table/TableDropDownButton.svelte';
	import EventName from '$lib/components/table/EventName.svelte';
	import CellCopy from '$lib/components/table/CopyCell.svelte';
	import EventDataCell from '$lib/components/table/EventDataCell.svelte';
	import SessionCaptureModal from '$lib/components/SessionCaptureModal.svelte';
	import { shortRelative } from '$lib/utils/shortRelative.js';
	import ProgramTrendChart from '$lib/components/program/ProgramTrendChart.svelte';
	import ProgramScheduleTimeline from '$lib/components/program/ProgramScheduleTimeline.svelte';
	import ProgramStartAlert from '$lib/components/program/ProgramStartAlert.svelte';
	import ProgramCloseAlert from '$lib/components/program/ProgramCloseAlert.svelte';
	import ProgramPauseAlert from '$lib/components/program/ProgramPauseAlert.svelte';
	import ProgramResumeAlert from '$lib/components/program/ProgramResumeAlert.svelte';
	import Modal from '$lib/components/Modal.svelte';
	import DeleteAlert from '$lib/components/modal/DeleteAlert.svelte';
	import ConditionalDisplay from '$lib/components/ConditionalDisplay.svelte';
	import ProgramPeopleTable from '$lib/components/program/ProgramPeopleTable.svelte';
	import ProgramReportModal from '$lib/components/program/ProgramReportModal.svelte';
	import ReportedUploadModal from '$lib/components/ReportedUploadModal.svelte';
	import { addToast } from '$lib/store/toast';
	import { newTableURLParams } from '$lib/service/tableURLParams.js';
	import { newTableParams } from '$lib/service/tableParams.js';
	import { AppStateService } from '$lib/service/appState';
	import { resourceContext } from '$lib/store/resourceContext';
	import { browserTimeZone } from '$lib/utils/dailySlots.js';
	import {
		ALL_WEEK_DAYS,
		HIDDEN_RESULTS_NOTE,
		HIDDEN_TIP,
		ROW_HIDDEN_TIP,
		SENT_HIDDEN_TIP,
		NEXT_HIDDEN_TIP,
		capitalize,
		describeBlackout,
		describeDays,
		describeDuration,
		reportDeliveryHref,
		reportDeliveryStatus,
		formatDate,
		formatDateTime,
		formatMonthYear,
		plural,
		programStatus,
		programStoppingAction,
		weekDaysFromMask
	} from '$lib/utils/programText.js';

	const programID = $page.params.id;
	const appStateService = AppStateService.instance;

	let program = null;
	let view = null;
	let results = { months: [], templates: [], summary: null };
	// the groups of the program as { id, name }
	let groups = [];
	let showMonthTable = false;
	// earliest upcoming send, taken from the program list
	let nextSendAt = null;
	let isReportPDFEnabled = false;
	// set when the program could not be loaded, notFound when the server said 404
	let loadError = '';
	let notFound = false;

	// people table, paged by the server
	const peopleParams = newTableURLParams({
		sortBy: 'recipient',
		sortOrder: 'asc',
		prefix: 'people',
		noScroll: true
	});
	let peopleRows = [];
	let peopleHasNextPage = false;
	let isPeopleLoading = false;
	let now = Date.now();
	// column names in the table header mapped to the sort names of the endpoint
	const peopleSortNames = {
		recipient: 'name',
		'next template': 'next_due_at'
	};

	// events table, paged and sorted by the server like the campaign events
	const eventsParams = newTableURLParams({
		sortBy: 'created_at',
		sortOrder: 'desc',
		prefix: 'event',
		noScroll: true
	});
	let events = [];
	let eventsHasNextPage = false;
	let isEventsLoading = false;
	// event type id to event name, as on the campaign page
	let eventNames = {};
	// only the newest events request may set the rows
	let eventsSeq = 0;
	const loadEvents = async () => {
		const seq = ++eventsSeq;
		isEventsLoading = true;
		try {
			const res = await api.program.events(programID, eventsParams);
			if (seq !== eventsSeq) return;
			if (!res.success) {
				if ([400, 403, 404].includes(res.statusCode)) return;
				throw res.error;
			}
			events = res.data?.rows ?? [];
			eventsHasNextPage = !!res.data?.hasNextPage;
		} catch (e) {
			if (seq === eventsSeq) {
				addToast('Failed to load the events of the program', 'Error');
			}
			console.error('failed to load program events', e);
		} finally {
			if (seq === eventsSeq) {
				isEventsLoading = false;
			}
		}
	};
	const loadEventNames = async () => {
		try {
			const res = await api.campaign.getAllEventTypes();
			if (res.success) {
				eventNames = Object.fromEntries((res.data ?? []).map((t) => [t.id, t.name]));
			}
		} catch (e) {
			console.error('failed to load event types', e);
		}
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
		// the program read names everything it refers to, the single reads below
		// are only for a server that does not send the names. The names are taken
		// on every read, so a renamed template, list, page or webhook shows its
		// new name
		const n = p.names;
		if (n) {
			settingNamesFor = '';
			++settingNamesSeq;
			const lists = (n.allowDeny ?? []).map((l) => ({ ...l, allowed: n.allowDenyAllowed }));
			settingNames = {
				templates: Object.fromEntries(
					orderedTemplateIDs(p).map((id) => [id, n.templates?.[id] ?? 'Unknown template'])
				),
				lists,
				denyPage: n.denyPage?.name ?? '',
				evasionPage: n.evasionPage?.name ?? '',
				webhooks: (n.webhooks ?? []).map((w) => w.name)
			};
			return;
		}
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
		// only the templates the details card names, the rest load with the list
		const templateIDs = firstTemplateIDs(p);
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

	// the details card names the first templates of the schedule and counts
	// the rest, which open in a list like the template pool of a campaign
	const TEMPLATES_INLINE_LIMIT = 3;
	const orderedTemplateIDs = (p) =>
		[...(p?.steps ?? [])]
			.sort((a, b) => (a.position ?? 0) - (b.position ?? 0))
			.map((s) => s.campaignTemplateID);
	const firstTemplateIDs = (p) => [
		...new Set(orderedTemplateIDs(p).slice(0, TEMPLATES_INLINE_LIMIT))
	];
	let isTemplatesModalVisible = false;
	let allTemplateNames = {};
	const openTemplatesModal = async () => {
		isTemplatesModalVisible = true;
		const missing = [...new Set(orderedTemplateIDs(program))].filter(
			(id) => !allTemplateNames[id] && !settingNames.templates[id]
		);
		// a few at a time, so a long schedule does not fire every request at once
		for (let i = 0; i < missing.length; i += 10) {
			const chunk = missing.slice(i, i + 10);
			const found = await Promise.all(chunk.map((id) => nameOf(api.campaignTemplate.getByID, id)));
			allTemplateNames = {
				...allTemplateNames,
				...Object.fromEntries(chunk.map((id, n) => [id, found[n]?.name ?? 'Unknown template']))
			};
		}
	};
	const templateNameOf = (id, names, all) => names[id] ?? all[id] ?? 'Loading...';

	// people a draft or scheduled program would email when it starts
	let audienceCount = null;
	// counted again only when the groups or the program change
	let audienceFor = '';
	let audienceSeq = 0;
	const loadAudience = async (p) => {
		const key = JSON.stringify([p.recipientGroupIDs, p.companyID, p.updatedAt]);
		if (key === audienceFor) return;
		audienceFor = key;
		const seq = ++audienceSeq;
		if (!p.recipientGroupIDs?.length) {
			audienceCount = 0;
			return;
		}
		try {
			const res = await api.program.audience(p.recipientGroupIDs, p.companyID);
			if (seq !== audienceSeq) return;
			audienceCount = res.success ? (res.data?.people ?? null) : null;
			// a failed count is tried again on the next load
			if (!res.success) audienceFor = '';
		} catch (e) {
			if (seq !== audienceSeq) return;
			audienceCount = null;
			audienceFor = '';
		}
	};

	// whether the automated report can be delivered, checked against the
	// delivery settings of the program company or the global ones
	let reportDelivery = null;
	// checked again only when the report setting or the program change
	let reportDeliveryFor = '';
	let reportDeliverySeq = 0;
	const loadReportDelivery = async (p) => {
		// a closed program still sends its final report while reportNextAt is set
		const on = p.reportEnabled && (!p.closedAt || !!p.reportNextAt);
		const key = on ? JSON.stringify([p.companyID || null, p.updatedAt]) : 'off';
		if (key === reportDeliveryFor) return;
		reportDeliveryFor = key;
		const seq = ++reportDeliverySeq;
		if (!on) {
			reportDelivery = null;
			return;
		}
		try {
			const res = await api.program.reportDelivery(p.companyID || null);
			if (seq !== reportDeliverySeq) return;
			reportDelivery = res.success ? res.data : null;
			if (!res.success) reportDeliveryFor = '';
		} catch (e) {
			if (seq !== reportDeliverySeq) return;
			reportDelivery = null;
			reportDeliveryFor = '';
		}
	};
	$: reportDeliveryBadge = reportDelivery ? reportDeliveryStatus(reportDelivery) : null;
	// the automated report is on but cannot be sent, said under the header
	$: reportDeliveryProblem =
		!!program?.reportEnabled &&
		(!isClosed || !!program?.reportNextAt) &&
		!!reportDelivery &&
		!reportDelivery.ready
			? reportDelivery.reason === 'pdfDisabled'
				? 'Automated report is on but PDF reports are turned off.'
				: reportDelivery.reason === 'incomplete'
					? 'Automated report is on but report delivery is incomplete.'
					: 'Automated report is on but report delivery is not set up.'
			: '';

	// the program request of the first load, the people wait for it to know
	// whether the program has started
	let firstProgramReq = null;
	const programStarted = (p) => !!p && p.status !== 'draft' && p.status !== 'scheduled';
	// only the newest people request may set the rows
	let peopleSeq = 0;
	const loadPeople = async () => {
		const seq = ++peopleSeq;
		isPeopleLoading = true;
		try {
			let sortBy = peopleSortNames[(peopleParams.sortBy || '').toLowerCase()] ?? 'name';
			// before start the rows are the group members, which sort by recipient only
			let started = program ? programStarted(program) : null;
			if (started === null && firstProgramReq) {
				const pRes = await firstProgramReq;
				started = pRes?.success ? programStarted(pRes.data) : false;
			}
			if (!started && sortBy === 'next_due_at') sortBy = 'name';
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
				addToast('Failed to load the recipients of the program', 'Error');
			}
			console.error('failed to load program people', e);
		} finally {
			if (seq === peopleSeq) {
				isPeopleLoading = false;
			}
		}
	};

	// the timeline loads on its own and again on each refresh
	let scheduleTimeline = null;
	let showTimelineTable = false;

	// only the newest load may set the program and results
	let loadSeq = 0;
	// true while a load runs, so an auto refresh does not start another
	let isRefreshing = false;
	// set while loads keep failing, so the auto refresh shows one toast per
	// run of failures and not one per poll
	let refreshFailing = false;
	const refreshFailed = (message) => {
		if (!refreshFailing) addToast(message, 'Error');
		refreshFailing = true;
	};
	const load = async () => {
		const seq = ++loadSeq;
		isRefreshing = true;
		const requestedRange = tileRangeParam;
		const rSeq = ++resultsSeq;
		try {
			const programReq = api.program.getByID(programID);
			if (!program) firstProgramReq = programReq;
			const [pRes, rRes] = await Promise.all([
				programReq,
				api.program.results(programID, requestedRange),
				loadPeople(),
				loadEvents(),
				scheduleTimeline?.refresh()
			]);
			if (seq !== loadSeq) return;
			const resultsFailed = !rRes.success && resultsSeq === rSeq;
			if (rRes.success && resultsSeq === rSeq) {
				results = rRes.data ?? { months: [], templates: [], summary: null };
				// the rows by department or position refresh with the results
				if (groupBy !== 'template' && showTemplateResults) loadGroups();
			}
			if (loadedTileRange === null) loadedTileRange = requestedRange;
			if (!pRes.success && (!program || pRes.statusCode === 404)) {
				// a 404 is an unknown id, a program in a company the user cannot
				// see, or a program deleted since the page loaded. Any other error
				// on the first load can be tried again.
				program = null;
				notFound = pRes.statusCode === 404;
				loadError = pRes.error || 'Program could not be loaded';
				resourceContext.clear();
				return;
			}
			if (pRes.success) {
				loadError = '';
				notFound = false;
				const groupsChanged =
					(program?.recipientGroupIDs ?? []).join() !== (pRes.data.recipientGroupIDs ?? []).join();
				program = pRes.data;
				nextSendAt = program.nextSendAt ?? null;
				setResourceContext(program);
				if (program.names?.groups) {
					groups = program.names.groups;
				} else if (groupsChanged || !groups.length) {
					loadGroupNames(program.recipientGroupIDs);
				}
				loadSettingNames(program);
				if (program.status === 'draft' || program.status === 'scheduled') loadAudience(program);
				loadReportDelivery(program);
				loadReportInbox();
			}
			// a failed refresh keeps the data shown before, the toast tells it is old
			if (!pRes.success) {
				refreshFailed(pRes.error || 'Failed to load program');
			} else if (resultsFailed) {
				refreshFailed(rRes.error || 'Failed to load the program results');
			} else {
				refreshFailing = false;
			}
			now = Date.now();
		} catch (e) {
			if (seq !== loadSeq) return;
			// a failed first load shows the error state with a retry
			if (!program) {
				notFound = false;
				loadError = 'Program could not be loaded';
			}
			refreshFailed('Failed to load program');
			console.error('failed to load program detail', e);
		} finally {
			if (seq === loadSeq) isRefreshing = false;
		}
	};

	// the company banner shows which company the program belongs to
	let resourceCompanyFor = undefined;
	const setResourceContext = async (p) => {
		const companyID = p.companyID || null;
		if (resourceCompanyFor === companyID) return;
		resourceCompanyFor = companyID;
		let companyName = p.names?.company?.name ?? p.company?.name ?? null;
		if (companyID && !companyName) {
			try {
				const res = await api.company.getByID(companyID);
				companyName = res.success ? (res.data?.name ?? null) : null;
			} catch (e) {
				companyName = null;
			}
		}
		resourceContext.setResource('program', companyID, companyName);
	};

	// actions that change the program are only allowed in the view the program belongs to
	const isContextMismatch = (p) => {
		if (!p) return false;
		const context = appStateService.getContext();
		if (!p.companyID) {
			return context.current !== AppStateService.CONTEXT.SHARED;
		}
		return context.current !== AppStateService.CONTEXT.COMPANY || context.companyID !== p.companyID;
	};
	// why the program actions are disabled in the current view, empty when they are not
	const contextTitle = (p) => {
		if (!isContextMismatch(p)) return '';
		return p.companyID
			? 'Switch to company view to perform this action'
			: 'Switch to global view to perform this action';
	};

	onMount(() => {
		load();
		loadEventNames();
		(async () => {
			const opt = await api.option.get('report_pdf_enabled');
			isReportPDFEnabled = opt.success && opt.data?.value === 'true';
		})();
		peopleParams.onChange(loadPeople);
		eventsParams.onChange(loadEvents);
		// the inbox checks every 2 minutes, so reloading its status once a minute is
		// enough. It also keeps the minutes since the last check current.
		const reportInboxInterval = setInterval(loadReportInbox, 60000);
		return () => {
			clearInterval(reportInboxInterval);
			peopleParams.unsubscribe();
			eventsParams.unsubscribe();
			resourceContext.clear();
		};
	});

	// reporting inbox that covers this program, as on the campaign page: the
	// company inbox, or the global one for a program without a company. null
	// when none is configured or not readable. The read needs the same
	// permission as managing the inbox, so a user who sees it can open its settings.
	let reportInbox = null;
	let reportInboxNow = Date.now();
	let reportInboxFor;
	const loadReportInbox = async () => {
		if (!program || program.closedAt) {
			reportInbox = null;
			return;
		}
		const companyID = program.companyID ?? null;
		// a status of another scope is never shown
		if (reportInboxFor !== companyID) reportInbox = null;
		reportInboxFor = companyID;
		try {
			const res = await api.company.reportInbox.getByCompanyID(companyID);
			if (reportInboxFor !== companyID) return;
			if (res && res.success && res.data) {
				reportInbox = res.data;
			} else if (res && res.statusCode === 404) {
				// no inbox for this scope
				reportInbox = null;
			}
			// any other failure keeps the last known status
		} catch (e) {
			// keep the last known status when one reload fails
		}
		reportInboxNow = Date.now();
	};
	// the inbox row shows only when the inbox for this program's scope is
	// enabled; the inbox skips closed programs
	$: showReportInboxRow = !!(
		reportInbox?.enabled &&
		reportInboxStatus(reportInbox, reportInboxNow) &&
		!isClosed
	);
	$: inboxSettingsHref = reportInboxSettingsHref(
		program?.companyID ?? null,
		appStateService.getContext()?.companyID ?? null
	);

	// pause and resume ask first, as on the campaign page
	let isPauseAlertVisible = false;
	let isResumeAlertVisible = false;
	// a refused pause or resume shows its error in the open alert
	const pauseResume = async () => {
		const wasPaused = !!program.pausedAt;
		const res = wasPaused
			? await api.program.resume(program.id)
			: await api.program.pause(program.id);
		if (res.success) {
			addToast(wasPaused ? 'Program is resuming' : 'Program paused', 'Success');
			load();
		}
		return res;
	};

	// a pause while a resume runs stops the resume, the program stays paused
	const onStopResume = async () => {
		const res = await api.program.pause(program.id);
		if (res.success) {
			addToast('Resume stopped, the program stays paused', 'Success');
		} else {
			addToast(res.error || 'Failed to stop the resume', 'Error');
		}
		load();
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
			addToast('Program is closing', 'Success');
			isCloseAlertVisible = false;
			load();
		}
		return res;
	};

	// Anonymize All and Anonymize Data, as on the campaign page
	let isAnonymizeModalVisible = false;
	let isAnonymizeDataModalVisible = false;
	const onConfirmAnonymize = async () => {
		const res = await api.program.anonymize(program.id);
		if (res.success) {
			isAnonymizeModalVisible = false;
			addToast('Anonymize All started', 'Success');
			load();
		}
		return res;
	};
	const onConfirmAnonymizeData = async () => {
		const res = await api.program.anonymizeData(program.id);
		if (res.success) {
			isAnonymizeDataModalVisible = false;
			addToast('Anonymize Data started', 'Success');
			load();
		}
		return res;
	};

	// delete is offered here for a program that never sent and to finish a
	// delete that did not finish. The server removes the program in the
	// background, the list shows it as Deleting until it is gone
	let isDeleteAlertVisible = false;
	const onDelete = async () => {
		const res = await api.program.delete(program.id);
		if (res.success) {
			addToast('Program is being deleted', 'Success');
			goto('/program/');
		}
		return res;
	};

	// while work runs in the background the page is read again until it ends
	const busyRefreshEvery = 5000;
	let busyTimer = null;
	let pageGone = false;
	const watchBusy = (busy) => {
		if (pageGone || !busy) {
			clearTimeout(busyTimer);
			busyTimer = null;
			return;
		}
		if (busyTimer) return;
		busyTimer = setTimeout(async () => {
			await load();
			busyTimer = null;
			watchBusy(backgroundBusy);
		}, busyRefreshEvery);
	};
	$: watchBusy(backgroundBusy);
	onDestroy(() => {
		pageGone = true;
		clearTimeout(busyTimer);
		busyTimer = null;
	});

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

	// the Session Sushi import help for captured cookies or tokens
	let isSessionCaptureVisible = false;
	let sessionCaptureData = '';
	/** @param {string} formatted */
	const showSessionCapture = (formatted) => {
		sessionCaptureData = formatted;
		isSessionCaptureVisible = true;
	};

	// pdf report and reported csv upload dialogs
	let reportModal;
	let reportedUpload;

	// formatting helpers
	// the day list is left out when every day or no day is set
	const daysText = (mask) =>
		!mask || mask === ALL_WEEK_DAYS ? '' : describeDays(weekDaysFromMask(mask));
	// a row count is hidden when an anonymous program withholds it. A count a
	// named program row does not hold, as the phishing counts of a training
	// template, is null and shows as empty.
	const isHidden = (row, key = 'sent') =>
		!!row?.suppressed || (row?.[key] == null && !!row?.outcomesHidden);
	// a row is hidden for its size, or because an anonymous program gives no
	// results per row
	const rowHiddenTip = (row, key) =>
		row?.suppressed || key === 'sent' ? SENT_HIDDEN_TIP : ROW_HIDDEN_TIP;

	// state
	$: isClosed = program?.status === 'closed' || !!program?.closedAt;
	$: isDraft = !isClosed && program?.status === 'draft';
	$: isScheduled = !isClosed && program?.status === 'scheduled';
	$: notStarted = isDraft || isScheduled;
	$: isMismatch = isContextMismatch(program);
	// a close or delete that did not finish, 'close', 'delete' or ''
	$: stopping = programStoppingAction(program);
	// work that runs in the background after a request answered. Anonymize All
	// of a closed program runs in the background pass once its date passed or
	// it began, until the program is anonymized.
	$: anonymizeAllBusy =
		!!program &&
		!program.anonymizedAt &&
		!!program.closedAt &&
		!program.isAnonymous &&
		(!!program.anonymizingAt ||
			(!!program.anonymizeAt && new Date(program.anonymizeAt) <= new Date()));
	$: anonymizeDataBusy =
		(!!program?.anonymizeRunning || !!program?.dataAnonymizingAt) && !anonymizeAllBusy;
	// a failure of an action that ran in the background, shown while it still
	// applies: a resume only while paused, the others while not closed or
	// anonymized as they need
	$: actionFailure = (() => {
		const f = program?.actionFailure;
		if (!f) return null;
		if (f.action === 'close') return stopping === 'close' ? f : null;
		if (stopping) return null;
		if (f.action === 'resume') return isPaused ? f : null;
		if (f.action === 'start') return isClosed ? null : f;
		if (f.action === 'anonymize') return program.anonymizedAt ? null : f;
		return f;
	})();
	$: actionFailureText = !actionFailure
		? ''
		: {
				close: `The close did not finish: ${actionFailure.reason}. It is tried again in about 10 minutes, or use Retry Close.`,
				resume: `The resume did not finish: ${actionFailure.reason}. The program stays paused, resume it again.`,
				start: `Enrolling the recipients did not finish: ${actionFailure.reason}. It is tried again within a few minutes.`,
				anonymize: `Anonymize All did not finish: ${actionFailure.reason}. It is tried again within a few minutes, or use Retry Anonymize All.`,
				anonymizeData: `Anonymize Data did not finish: ${actionFailure.reason}. It is tried again within a few minutes, or run it again. The parts already done are passed over.`
			}[actionFailure.action] || '';
	$: resuming = !!program?.resuming;
	$: enrolling = !!program?.enrolling;
	$: backgroundBusy =
		!!stopping || resuming || enrolling || anonymizeAllBusy || anonymizeDataBusy;
	$: isPaused = !isClosed && !notStarted && !stopping && !!program?.pausedAt;
	// a draft or scheduled program that never sent is deleted instead of closed,
	// as there are no links to stop and no results to keep
	$: neverSent = notStarted && !program?.startedAt && !results?.lastSentAt && !summary?.hasSent;
	$: warnings = !isClosed && !stopping ? (program?.warnings ?? []) : [];
	$: programState = programStatus(program);
	$: status = programState.label;
	$: statusTone = programState.tone;

	// people
	$: isAnonymous = !!view?.anonymous || !!program?.isAnonymous;
	// the people of an anonymized program are replaced with pseudonyms. An
	// anonymous program is anonymized when it closes.
	$: isAnonymized = !!program?.anonymizedAt || (isAnonymous && isClosed);
	$: peopleAnonymized = !!view?.anonymized || !!program?.anonymizedAt || !!program?.anonymizingAt;
	// the data of a closed program anonymized after the close holds nothing newer
	$: dataDoneAfterClose =
		isClosed &&
		!!program?.dataAnonymizedAt &&
		!!program?.closedAt &&
		new Date(program.dataAnonymizedAt) >= new Date(program.closedAt);
	$: stepCount = view?.stepCount ?? (program?.steps ?? []).length;

	// why the program needs attention, decided by the server as on the list and
	// the dashboard. A reason the warnings already give in more detail is left
	// out.
	$: attentionNote =
		!isClosed && !stopping && program?.attentionReason && !warnings.length
			? program.attentionReason
			: '';
	// nobody enrolled and nothing sent, so the page has nothing to show
	$: notEnrolled =
		!isClosed && warnings.length > 0 && (view?.total ?? 0) === 0 && !summary?.hasSent;

	// the time range of the result tiles, kept in the url as range. Without one
	// the server picks the default of the program.
	// an anonymous program counts whole months, so its ranges are named as in
	// the report dialog
	const namedTileRanges = [
		{ label: 'Last 30 days', value: '30d' },
		{ label: 'Last 90 days', value: '90d' },
		{ label: 'Last 12 months', value: '12m' },
		{ label: 'All time', value: 'all' }
	];
	const monthTileRanges = [
		{ label: 'Last full month', value: '30d' },
		{ label: 'Last 3 full months', value: '90d' },
		{ label: 'Last 12 full months', value: '12m' },
		{ label: 'All time', value: 'all' }
	];
	$: tileRanges = isAnonymous ? monthTileRanges : namedTileRanges;
	// the time range of the tiles in words, for the Sent title and the tips
	$: tileRangeWords = Object.fromEntries(
		tileRanges.map((r) => [r.value, r.value === 'all' ? 'all time' : r.label.toLowerCase()])
	);
	const validTileRange = (v) => (namedTileRanges.some((r) => r.value === v) ? v : '');
	$: tileRangeParam = validTileRange($page.url.searchParams.get('range'));
	// the range shown, the one the server counted until another is chosen
	$: tileRange = tileRangeParam || results?.tiles?.range || '';
	const chooseTileRange = (value) => {
		const range = validTileRange(value);
		if (!range || range === tileRangeParam) return;
		const url = new URL(window.location.toString());
		url.searchParams.set('range', range);
		goto(`?${url.searchParams.toString()}`, {
			replaceState: true,
			noScroll: true,
			keepFocus: true,
			invalidateAll: false
		});
	};
	// only the newest results request may set the results
	let resultsSeq = 0;
	const loadResults = async (range) => {
		const seq = ++resultsSeq;
		try {
			const res = await api.program.results(programID, range);
			if (seq !== resultsSeq) return;
			if (!res.success) throw res.error;
			results = res.data ?? { months: [], templates: [], summary: null };
		} catch (e) {
			if (seq === resultsSeq) addToast('Failed to load the program results', 'Error');
			console.error('failed to load program results', e);
		}
	};
	// a range chosen after the first load, or one taken back by the browser,
	// loads the tiles again
	let loadedTileRange = null;
	$: if (program && loadedTileRange !== null && tileRangeParam !== loadedTileRange) {
		loadedTileRange = tileRangeParam;
		loadResults(tileRangeParam);
	}

	// results
	$: summary = results?.summary ?? null;
	$: tiles = results?.tiles ?? null;
	$: months = results?.months ?? [];
	$: templates = results?.templates ?? [];
	$: nothingSent = !results?.lastSentAt && !summary?.hasSent;
	// part of base as a whole percent
	const pctOf = (part, base) => (base > 0 ? Math.round((part / base) * 100) : 0);
	// the result tiles, the concepts, names, colors and icons of the campaign page.
	// They count sends in the chosen time range, each send once per outcome.
	$: resultTiles = (() => {
		if (!tiles) return [];
		// no share is shown when the count or its base is withheld
		const of = (v, label, base) =>
			v == null || base == null
				? []
				: [{ value: pctOf(v, base), relativeTo: label, baseValue: base }];
		const left = [
			tiles.completed > 0 ? `${tiles.completed} completed` : '',
			tiles.removed > 0 ? `${tiles.removed} removed` : ''
		].filter(Boolean);
		// a closed program counts everyone it had, as nobody is active after the close
		const everyone = (tiles.recipients ?? 0) + (tiles.completed ?? 0) + (tiles.removed ?? 0);
		const words = tileRangeWords[tileRange] ?? '';
		// the sends counted in the range, as "in the last 90 days" or "since the start"
		const inRange = !words
			? 'in the time range'
			: tileRange === 'all'
				? 'since the start'
				: `in the ${words}`;
		// a program of only training shows its training sends as Sent, as a
		// training campaign does
		const trainingOnly = !tiles.hasPhishing && tiles.hasTraining;
		const list = [
			{
				key: 'recipients',
				title: 'Recipients',
				tip: isClosed
					? `Everyone the program had. ${tiles.recipients ?? 0} still active at close${left.length ? `, ${left.join(', ')}` : ''}.`
					: 'Recipients active in the program now.' +
						(left.length ? ` ${capitalize(left.join(', '))}.` : ''),
				value: isClosed ? everyone : (tiles.recipients ?? 0),
				color: 'text-recipients',
				icon: 'recipients'
			}
		];
		if (trainingOnly) {
			list.push({
				key: 'sent',
				title: words ? `Sent, ${words}` : 'Sent',
				tip: `Training templates sent ${inRange}.`,
				value: tiles.trainingSent,
				color: 'text-message-sent',
				icon: 'sent'
			});
		}
		if (tiles.hasPhishing) {
			list.push(
				{
					key: 'sent',
					title: words ? `Sent, ${words}` : 'Sent',
					tip: `Phishing templates sent ${inRange}.`,
					value: tiles.sent,
					color: 'text-message-sent',
					icon: 'sent'
				},
				{
					key: 'clickedLink',
					title: 'Clicked link',
					tip: `Sends ${inRange} where the link was clicked.`,
					value: tiles.clickedLink,
					color: 'text-page-visited',
					icon: 'clicked',
					percentages: of(tiles.clickedLink, 'of sent', tiles.sent)
				},
				{
					key: 'submittedData',
					title: 'Data submitted',
					tip: `Sends ${inRange} where data was submitted.`,
					value: tiles.submittedData,
					color: 'text-submitted-data',
					icon: 'submitted',
					percentages: of(tiles.submittedData, 'of sent', tiles.sent)
				},
				{
					key: 'reported',
					title: 'Reported',
					tip: `Sends ${inRange} that were reported.`,
					value: tiles.reported,
					color: 'text-reported dark:text-slate-300',
					icon: 'reported',
					percentages: of(tiles.reported, 'of sent', tiles.sent)
				}
			);
		}
		if (tiles.hasTraining) {
			list.push(
				{
					key: 'trainingStarted',
					title: 'Training started',
					tip: `Training sends ${inRange} where the training was started.`,
					value: tiles.trainingStarted,
					color: 'text-training-started',
					icon: 'training',
					percentages: of(tiles.trainingStarted, 'of training sent', tiles.trainingSent)
				},
				{
					key: 'trainingCompleted',
					title: 'Training completed',
					tip: `Training sends ${inRange} where the training was completed.`,
					value: tiles.trainingCompleted,
					color: 'text-training-completed',
					icon: 'completed',
					percentages: of(tiles.trainingCompleted, 'of started', tiles.trainingStarted)
				}
			);
		}
		// an anonymous program never names a repeat offender, so the tile is left
		// out, as it is without phishing
		if (isAnonymous || !tiles.hasPhishing) return list;
		list.push({
			key: 'repeatOffenders',
			title: 'Repeat offenders',
			tip: `Recipients who clicked a link or submitted data on 2 or more sends in the last ${plural(tiles.repeatOffenderMonths || 12, 'month')}, the repeat offender setting. Not limited by the time range.`,
			value: tiles.repeatOffenders,
			color: 'text-repeat-offenders',
			icon: 'people'
		});
		return list;
	})();
	$: anyTileHidden = resultTiles.some((t) => t.value == null);
	// the trend shows the last months by default, with the time ranges of the
	// dashboard trend
	const trendRanges = [
		{ label: 'Last 12 months', value: '12' },
		{ label: 'Last 24 months', value: '24' },
		{ label: 'Last 36 months', value: '36' },
		{ label: 'All time', value: 'all' }
	];
	let trendRange = '12';
	const monthKey = (d) => `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}`;
	$: trendFrom =
		trendRange === 'all'
			? ''
			: monthKey(
					new Date(
						new Date(now).getFullYear(),
						new Date(now).getMonth() - Number(trendRange) + 1,
						1
					)
				);
	$: trendMonths = trendFrom ? months.filter((m) => m.key >= trendFrom) : months;
	// the range choice shows only when the program has more months than the shortest range
	$: hasLongTrend =
		months.length > 0 &&
		months[0].key <
			monthKey(new Date(new Date(now).getFullYear(), new Date(now).getMonth() - 11, 1));

	// the month and template tables are paged in the page, the results hold one
	// row per month and per template
	const monthParams = newTableParams({ sortBy: '', sortOrder: 'desc' });
	const templateParams = newTableParams({ sortBy: '', sortOrder: 'desc' });
	let pageTick = 0;
	monthParams.onChange(() => pageTick++);
	templateParams.onChange(() => pageTick++);
	const pageOf = (list, params, _tick) => {
		const start = (params.currentPage - 1) * params.perPage;
		return list.slice(start, start + params.perPage);
	};
	const morePages = (list, params, _tick) => list.length > params.currentPage * params.perPage;
	// the month table lists the newest month first
	$: monthsNewest = [...months].reverse();
	$: monthRows = pageOf(monthsNewest, monthParams, pageTick);
	$: monthsHasNextPage = morePages(monthsNewest, monthParams, pageTick);
	// results by group: the template rows come with the results, the
	// department and position rows of a named program are read when chosen,
	// as the grouped results of a campaign
	const groupByLabels = { template: 'Template', department: 'Department', position: 'Position' };
	let groupBy = 'template';
	let groupRows = [];
	let groupOmitted = 0;
	let groupsLoading = false;
	let groupsFailed = false;
	// only the newest groups request may set the rows
	let groupsSeq = 0;
	const loadGroups = async () => {
		if (groupBy === 'template') return;
		const seq = ++groupsSeq;
		groupsLoading = true;
		try {
			const res = await api.program.resultGroups(programID, groupBy);
			if (seq !== groupsSeq) return;
			if (!res.success) throw res.error;
			groupRows = res.data?.rows ?? [];
			groupOmitted = res.data?.omitted ?? 0;
			groupsFailed = false;
		} catch (e) {
			if (seq !== groupsSeq) return;
			console.error('failed to load program results by group', e);
			// cleared so a failed switch does not leave old rows under the new heading
			groupRows = [];
			groupOmitted = 0;
			groupsFailed = true;
		} finally {
			if (seq === groupsSeq) groupsLoading = false;
		}
	};
	const onChangeGroupBy = (by) => {
		if (by === groupBy) return;
		groupBy = by;
		groupRows = [];
		groupOmitted = 0;
		groupsFailed = false;
		// setting the page size again goes back to the first page
		templateParams.perPage = templateParams.perPage;
		loadGroups();
	};
	// an anonymous or anonymized program groups by template only, a group can
	// be small enough to point at one person
	$: canGroup = !!program && !isAnonymous && !program.anonymizedAt && !program.anonymizingAt;
	$: if (!canGroup && groupBy !== 'template') {
		groupBy = 'template';
		groupsSeq++;
		groupsLoading = false;
	}
	$: groupList = groupBy === 'template' ? templates : groupRows;
	$: templateRows = pageOf(groupList, templateParams, pageTick);
	$: templatesHasNextPage = morePages(groupList, templateParams, pageTick);
	// results by template start open for a short list and hidden for a long
	// one, as the grouped results of a campaign, until the user toggles them
	const TEMPLATES_OPEN_LIMIT = 6;
	let showTemplateResults = false;
	let templateResultsToggled = false;
	$: if (!templateResultsToggled) {
		showTemplateResults = templates.length > 0 && templates.length <= TEMPLATES_OPEN_LIMIT;
	}

	// a count with its percent of its base, as in the campaign results by
	// group: a phishing outcome of the sends, a training started of the
	// training sends, a training completed of the trainings started
	const pctBase = { trainingStarted: 'trainingSent', trainingCompleted: 'trainingStarted' };
	const countPct = (row, key) => ({
		count: row[key],
		pct: pctOf(row[key], row[pctBase[key] ?? 'sent'])
	});
	// the kinds of template the program has or had, for the month and template
	// tables and the trend
	$: hasPhishing = summary?.hasPhishing ?? true;
	$: hasTraining = !!summary?.hasTraining;
	// an anonymous program of only training has empty month and template rows
	$: anonTrainingOnly = isAnonymous && hasTraining && !hasPhishing;
	// the count columns of the month and template tables: the phishing ones
	// with phishing, the training ones with training. A program of only
	// training names its training sends Sent, as a training campaign does.
	// An anonymous program keeps training out of its rows, the tiles carry the
	// training totals, so it has no training columns.
	// [key, label, hideBelow, plain] where plain shows the count without a
	// percent
	$: rowColumns = [
		...(hasPhishing
			? [
					['sent', 'Sent', '', true],
					['clicked', 'Clicked link', '', false],
					['submitted', 'Data submitted', 'md', false],
					['reported', 'Reported', 'md', false]
				]
			: []),
		...(hasTraining && !isAnonymous
			? [
					['trainingSent', hasPhishing ? 'Training sent' : 'Sent', hasPhishing ? 'xl' : '', true],
					['trainingStarted', 'Training started', hasPhishing ? 'lg' : '', false],
					['trainingCompleted', 'Training completed', hasPhishing ? 'lg' : 'md', false]
				]
			: [])
	];
	// the month table shows on its own for a program without phishing, as the
	// trend draws phishing rates only
	$: monthTableShown = months.length > 0 && (!hasPhishing || months.length === 1 || showMonthTable);
	$: rowTableColumns = rowColumns.map(([, column, hideBelow]) => ({
		column,
		size: 'small',
		compact: true,
		...(hideBelow ? { hideBelow } : {})
	}));

	// program details rows, named like the wizard fields
	$: windowZone =
		program?.timeZone && program.timeZone !== browserTimeZone() ? `, ${program.timeZone}` : '';
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
	const reportEvery = { monthly: 'Monthly', quarterly: 'Quarterly', yearly: 'Yearly' };
	// the recipient field a random order program mixes templates by
	const mixFieldNames = { department: 'Department', position: 'Position', city: 'City' };
	// the three details cards, named like the wizard fields
	$: detailCards = !program
		? []
		: [
				{
					title: 'Program Details',
					rows: [
						{ label: 'Status', status: true },
						{ label: 'Recipient groups', groups: true },
						{ label: 'Templates', templates: true },
						...(stepCount > 1
							? [
									{
										label: 'Template order',
										value:
											program.orderMode === 'sequential'
												? 'Same order for everyone'
												: 'Random order per recipient'
									}
								]
							: []),
						...(stepCount > 1 && program.orderMode !== 'sequential'
							? [
									{
										label: 'Balance per',
										value: mixFieldNames[program.templateMixBy] || 'None'
									}
								]
							: []),
						{
							label: 'After the last template',
							value: program.loopMode === 'complete' ? 'Stop' : 'Start over'
						},
						{ label: 'Anonymous', value: program.isAnonymous ? 'Yes' : 'No' },
						{
							label: 'Data saving',
							value: !program.isAnonymous && program.saveSubmittedData ? 'Enabled' : 'Disabled'
						},
						{
							label: 'Webhooks',
							value: program.webhooks?.length
								? settingNames.webhooks.join(', ') || plural(program.webhooks.length, 'webhook')
								: ''
						},
						{ label: 'Filtering', value: filterText, blackbox: true },
						{ label: 'Deny page', value: settingNames.denyPage, blackbox: true },
						{ label: 'Evasion page', value: settingNames.evasionPage, blackbox: true }
					]
				},
				{
					title: 'Schedule',
					rows: [
						...(notStarted
							? []
							: [
									{ label: 'Next template', next: true },
									// an anonymous program gives the month of the last send
									results?.lastSentAt && isAnonymous
										? {
												label: 'Last template',
												value: formatMonthYear(results.lastSentAt, program.timeZone)
											}
										: results?.lastSentAt
											? { label: 'Last template', at: results.lastSentAt }
											: { label: 'Last template', value: '' },
									summary?.sentTotalHidden
										? { label: 'Sent in total', hidden: true }
										: { label: 'Sent in total', value: String(summary?.sentTotal ?? 0) }
								]),
						{
							label: 'Start',
							value: program.startedAt
								? formatDateTime(program.startedAt)
								: isScheduled
									? formatDateTime(program.startAt)
									: '',
							empty: 'Not set'
						},
						{
							label: 'End',
							value: program.endAt ? formatDateTime(program.endAt) : '',
							empty: 'Never'
						},
						{
							label: 'Time between templates',
							value: describeDuration(program.stepIntervalMinutes)
						},
						{
							label: 'Spread sends over',
							value:
								program.spreadMinutes > 0 ? describeDuration(program.spreadMinutes) : 'All at once'
						},
						{
							label: 'Sending days',
							value: daysText(program.constraintWeekDays)
								? capitalize(daysText(program.constraintWeekDays))
								: 'Any day'
						},
						{
							label: 'Sending hours',
							value:
								program.constraintStartTime && program.constraintEndTime
									? `${formatTimeConstraint(program.constraintStartTime, $timeFormat)} to ${formatTimeConstraint(program.constraintEndTime, $timeFormat)}${windowZone}`
									: 'Any time'
						},
						{
							label: 'Skip dates',
							list: (program.blackoutDates ?? []).map((r) => describeBlackout(r)),
							value: ''
						},
						{
							label: 'First template to new members',
							value:
								program.newMemberDelayMinutes > 0
									? `After ${describeDuration(program.newMemberDelayMinutes)}`
									: 'No wait'
						}
					]
				},
				{
					title: 'Reporting',
					rows: [
						{ label: 'Automated report', value: program.reportEnabled ? 'On' : 'Off' },
						...(program.reportEnabled
							? [
									{ label: 'Send every', value: reportEvery[program.reportInterval] || 'Monthly' },
									{
										label: 'Report covers',
										value:
											program.reportPeriod === 'start'
												? 'Since the program started'
												: 'The period that ended'
									},
									program.reportNextAt
										? { label: 'Next report', at: program.reportNextAt }
										: { label: 'Next report', value: '' },
									...(reportDeliveryBadge ? [{ label: 'Report delivery', delivery: true }] : [])
								]
							: []),
						...(showReportInboxRow ? [{ label: 'Reporting inbox', inbox: true }] : []),
						// an anonymous program takes no import, its reports come from the report
						// inbox. The sends of an anonymized program name no one to match.
						...(!notStarted && !isAnonymous && !program.anonymizedAt && !program.anonymizingAt
							? [
									{
										label: 'Reported emails',
										importReported: true,
										tip: 'Mark recipients as reported from a CSV with an email column and an optional report date column.'
									}
								]
							: [])
					]
				}
			];
	$: showResults = !notStarted && !notEnrolled && !nothingSent;

	// card, tile and table cell looks shared with the campaign page
	const card =
		'bg-white dark:bg-gray-900/80 rounded-lg shadow-md dark:shadow-none dark:ring-1 dark:ring-gray-600/30 transition-colors duration-200';
	const cardTitle =
		'text-lg font-semibold text-pc-darkblue dark:text-white mb-4 pb-2 border-b border-gray-200 dark:border-gray-700 transition-colors duration-200';
	// a state note under the header
	const note = 'text-sm text-gray-600 dark:text-gray-400 transition-colors duration-200';
	const inlineLink =
		'pc-focus rounded-sm font-medium text-cta-blue dark:text-highlight-blue hover:underline';
	// a value hidden for anonymity, as on the campaign page
	const hiddenCls =
		'text-sm italic text-gray-500 dark:text-gray-400 cursor-help underline decoration-dotted underline-offset-2';
	// an empty value, as in the campaign Timeline card
	const emptyCls = 'text-gray-400 dark:text-gray-500 text-right';
	const pctCls = 'text-gray-400 dark:text-gray-500';
	// the label and value of a program details row, as on the campaign page
	// a section ending in a table, whose pagination already adds 2rem below
	const endsInTable = '-mb-8';
	const detailLabel = 'min-w-0 text-gray-600 dark:text-gray-400';
	const detailValue = 'min-w-0 text-right text-pc-darkblue dark:text-white break-words';
</script>

<HeadTitle title={program ? program.name : 'Program'} />
<main>
	{#if program}
		<Breadcrumb
			crumbs={[{ label: 'Programs', href: '/program/' }, { label: program.name ?? '' }]}
		/>
		<div class="mb-6 flex flex-wrap items-center justify-between gap-x-6 gap-y-3">
			<div class="min-w-0 flex flex-wrap items-center gap-x-3 gap-y-1">
				<Headline docSlug="programs" preserveCase
					><span class="select-text">{program.name}</span></Headline
				>
				<StatusBadge tone={statusTone} title={programState.tip || ''}>{status}</StatusBadge>
			</div>
			<!-- ml-auto keeps the actions on the right when a long name wraps them to their own line -->
			<div class="ml-auto flex flex-wrap items-center justify-end gap-x-4 gap-y-3">
				<div class="flex flex-wrap items-center gap-2" role="group" aria-label="Program actions">
					{#if stopping === 'close'}
						<!-- a close that did not finish can only be tried again -->
						<div title={contextTitle(program)}>
							<ActionButton
								variant="danger-outline"
								icon="close"
								disabled={isMismatch}
								on:click={() => (isCloseAlertVisible = true)}>Retry Close</ActionButton
							>
						</div>
					{:else if stopping === 'delete'}
						<!-- a delete that did not finish can only be tried again -->
						<div title={contextTitle(program)}>
							<ActionButton
								variant="danger-outline"
								icon="delete"
								disabled={isMismatch}
								on:click={() => (isDeleteAlertVisible = true)}>Retry Delete</ActionButton
							>
						</div>
					{:else}
						{#if notStarted}
							<div title={contextTitle(program)}>
								<ActionButton icon="play" disabled={isMismatch} on:click={openStartAlert}
									>Start Now</ActionButton
								>
							</div>
						{/if}
						{#if !isClosed}
							<div title={contextTitle(program)}>
								<ActionButton
									variant={notStarted || isPaused ? 'secondary' : 'primary'}
									icon="edit"
									disabled={isMismatch || resuming}
									on:click={openUpdate}>Update</ActionButton
								>
							</div>
							{#if isPaused && resuming}
								<!-- a pause stops a resume under way, the program stays paused -->
								<div title={contextTitle(program) || 'Stops the resume, the program stays paused'}>
									<ActionButton
										variant="secondary"
										icon="pause"
										disabled={isMismatch}
										on:click={onStopResume}>Pause</ActionButton
									>
								</div>
							{:else if isPaused}
								<div title={contextTitle(program)}>
									<ActionButton
										icon="play"
										disabled={isMismatch}
										on:click={() => (isResumeAlertVisible = true)}>Resume</ActionButton
									>
								</div>
							{:else if program.status === 'active'}
								<!-- pause only an active program, as on the campaign page -->
								<div title={contextTitle(program)}>
									<ActionButton
										variant="secondary"
										icon="pause"
										disabled={isMismatch}
										on:click={() => (isPauseAlertVisible = true)}>Pause</ActionButton
									>
								</div>
							{/if}
						{/if}
						{#if !notStarted}
							<TableDropDownEllipsis text="Export" label="Export program data">
								<TableDropDownButton name="CSV" on:click={onExportCSV} />
								<TableDropDownButton name="Report" on:click={() => reportModal.open()} />
							</TableDropDownEllipsis>
						{/if}
						{#if neverSent}
							<div title={contextTitle(program)}>
								<ActionButton
									variant="danger-outline"
									icon="delete"
									disabled={isMismatch}
									on:click={() => (isDeleteAlertVisible = true)}>Delete</ActionButton
								>
							</div>
						{:else if !isClosed}
							<div title={contextTitle(program) || 'Stops all sending for good. Results are kept.'}>
								<ActionButton
									variant="danger-outline"
									icon="close"
									disabled={isMismatch}
									on:click={() => (isCloseAlertVisible = true)}>Close</ActionButton
								>
							</div>
						{/if}
					{/if}
				</div>
				<div class="sm:pl-4 sm:border-l sm:border-gray-200 dark:sm:border-gray-700">
					<AutoRefresh isLoading={isRefreshing} onRefresh={load} />
				</div>
			</div>
		</div>
		{#if contextTitle(program)}
			<p class="mt-2 text-sm text-gray-600 dark:text-gray-400">
				{contextTitle(program)} to change this program.
			</p>
		{/if}
		{#if !isAnonymous}
			<ReportedUploadModal
				bind:this={reportedUpload}
				id="program-reported"
				dateOptional
				upload={(file, emailColumn, dateColumn) =>
					api.program.uploadReportedCSV(program.id, file, emailColumn, dateColumn)}
				onImported={load}
			/>
		{/if}

		<!-- state and health notes, plain lines like the notes on the campaign page -->
		<div id="program-notes" class="space-y-1">
			{#if attentionNote}
				<Note id="program-attention-note">
					<span class="flex flex-wrap items-center gap-x-3 gap-y-1">
						<span>{attentionNote}</span>
						{#if !isMismatch}
							<button type="button" class={inlineLink} on:click={openUpdate}>Update program</button>
						{/if}
					</span>
				</Note>
			{/if}
			{#if isDraft && !stopping}
				<p class={note} role="status">
					Draft. Nothing is sent until you start it.{#if audienceCount}{' '}Starting sends the first
						templates to {plural(audienceCount, 'recipient')} from the next sending time.{/if}
				</p>
			{/if}
			{#if isScheduled && !stopping}
				<p class={note} role="status">
					Starts {formatDateTime(program.startAt)}.{#if audienceCount}{' '}The first templates go to {plural(
							audienceCount,
							'recipient'
						)}.{/if}
				</p>
			{/if}
			{#each warnings as w}
				<Note>
					<span class="flex flex-wrap items-center gap-x-3 gap-y-1">
						<span>{w}</span>
						{#if !isMismatch}
							<button type="button" class={inlineLink} on:click={openUpdate}>Update program</button>
						{/if}
					</span>
				</Note>
			{/each}
			{#if reportDeliveryProblem}
				<Note id="program-report-delivery-note">
					<span class="flex flex-wrap items-center gap-x-3 gap-y-1">
						<span>{reportDeliveryProblem}</span>
						<a class={inlineLink} href={reportDeliveryHref(reportDelivery, program.companyID)}
							>Set up report delivery</a
						>
					</span>
				</Note>
			{/if}
			{#if stopping}
				<p id="program-stopping-note" class={note} role="status">
					{stopping === 'close'
						? 'Closing. Sending has stopped and the close finishes in the background. If it is still closing after a few minutes, use Retry Close.'
						: 'Deleting. The program is stopped and removed in the background. If it is still here after a few minutes, use Retry Delete.'}
				</p>
			{/if}
			{#if isPaused}
				<p class={note} role="status">
					{#if resuming}
						Resuming. Held templates are planned again in the background, the program stays paused
						until it is done.
					{:else}
						Paused since {formatDateTime(program.pausedAt)}. Nothing is sent. On resume, held
						templates are spread over the next sending window.
					{/if}
				</p>
			{/if}
			{#if enrolling && !isClosed}
				<p id="program-enrolling-note" class={note} role="status">
					Enrolling the recipients in the background. The Recipients table fills in when it is done.
				</p>
			{/if}
			{#if actionFailureText}
				<Note id="program-action-failure">{actionFailureText}</Note>
			{/if}
			{#if anonymizeAllBusy}
				<p id="program-anonymizing-note" class={note} role="status">
					Anonymizing. Anonymize All runs in the background. If it stops, it is tried again within a
					few minutes, or use Retry Anonymize All.
				</p>
			{:else if anonymizeDataBusy}
				<p id="program-anonymizing-note" class={note} role="status">
					Anonymizing the collected data in the background.
				</p>
			{/if}
			{#if isClosed}
				<p class={note} role="status">
					Closed on {formatDateTime(program.closedAt)}.
					{nothingSent ? 'Nothing was sent before the program was closed.' : 'Results are kept.'}
				</p>
			{/if}
		</div>

		<!-- result tiles, the same block as the campaign page, with the time
		     range select of the dashboard trend above them -->
		{#if !notStarted && !notEnrolled && resultTiles.length}
			<div class="mt-4 flex justify-end">
				<label
					class="flex items-center gap-1 text-xs text-gray-700 dark:text-gray-300 transition-colors duration-200"
				>
					Time range:
					<select
						id="program-tiles-range"
						value={tileRange}
						on:change={(e) => chooseTileRange(e.currentTarget.value)}
						class="pc-select pc-focus h-6 py-0"
					>
						{#each tileRanges as range}
							<option value={range.value}>{range.label}</option>
						{/each}
					</select>
				</label>
			</div>
			<StatTiles
				id="program-results"
				tiles={resultTiles.map((t) => ({
					...t,
					hiddenTip: t.key === 'sent' ? SENT_HIDDEN_TIP : HIDDEN_TIP
				}))}
				class="mt-2"
			/>
			<!-- one note line under the tiles. An anonymous program adds how it counts -->
			{#if isAnonymous}
				<p id="program-anon-delay-note" class="{note} mt-3">
					{#if anyTileHidden}{HIDDEN_RESULTS_NOTE}{' '}{/if}{#if tiles?.rangeText}Counts {tiles.rangeText}.{:else}No
						month counted yet.{/if} An anonymous program counts whole months, 3 days after each month
					ends.
				</p>
			{:else if anyTileHidden}
				<p id="program-hidden-note" class="{note} mt-3">{HIDDEN_RESULTS_NOTE}</p>
			{/if}
		{/if}

		<!-- sections are 2rem apart as on the campaign page. A table already ends
		     with 2rem below its pagination, so a section that ends in a table takes
		     that space back and the next heading sits as close as on the campaign page -->
		<div class="mt-8 flex flex-col gap-8">
			<!-- the sections in the DOM in the order they show. Once sending started
			     the timeline and the results come first and the details cards follow,
			     as on the campaign page. Before that the details cards come right under
			     the header -->
			{#if !notStarted && !notEnrolled}
				<!-- sends, outcomes and planned sends per day or week, right under the
				     tiles and above the trend. An anonymous program has no timeline, as a
				     day of sends can point to a person -->
				{#if !isAnonymous}
					<section id="program-timeline" class={showTimelineTable ? endsInTable : ''}>
						<div class="mb-3 flex flex-wrap items-baseline gap-x-2 gap-y-1">
							<SubHeadline>Timeline</SubHeadline>
							<button
								type="button"
								id="program-timeline-table-toggle"
								class="pc-focus rounded-sm text-sm font-medium text-cta-blue hover:underline dark:text-highlight-blue transition-colors duration-200"
								aria-expanded={showTimelineTable}
								on:click={() => (showTimelineTable = !showTimelineTable)}
								>{showTimelineTable ? 'Hide table' : 'Show table'}</button
							>
						</div>
						<ProgramScheduleTimeline
							bind:this={scheduleTimeline}
							bind:showTable={showTimelineTable}
							programID={program.id}
							companyID={program.companyID || null}
							timeZone={program.timeZone || 'UTC'}
							{hasPhishing}
							{hasTraining}
						/>
					</section>
				{/if}

				{#if nothingSent}
					<!-- a closed program says so in the note under the header -->
					{#if !isClosed}
						<p class={note}>
							Nothing sent yet. The trend and results by template show here after the first
							templates go out.
						</p>
					{/if}
				{:else if anonTrainingOnly}
					<!-- an anonymous program keeps training out of its month and template
					     rows, so a program of only training has no rows to show -->
					<p id="program-anon-training-note" class={note}>
						Anonymous programs show training results in the totals only.
					</p>
				{:else}
					<!-- trend by month -->
					<section id="program-trend" class={monthTableShown ? endsInTable : ''}>
						<!-- the toggle sits next to the heading and starts at the left edge when it wraps -->
						<div class="mb-3 flex flex-wrap items-baseline gap-x-2 gap-y-1">
							<SubHeadline>Trend by Month</SubHeadline>
							{#if months.length >= 2 && hasPhishing}
								<button
									type="button"
									class="pc-focus rounded-sm text-sm font-medium text-cta-blue hover:underline dark:text-highlight-blue transition-colors duration-200"
									aria-expanded={showMonthTable}
									on:click={() => (showMonthTable = !showMonthTable)}
									>{showMonthTable ? 'Hide table' : 'Show table'}</button
								>
							{/if}
						</div>
						{#if !hasPhishing}
							<!-- the trend draws phishing rates, so a program of only training shows the table -->
						{:else if months.length >= 2 && isAnonymous}
							<!-- an anonymous program has no outcomes per month, so there is no line to draw -->
							<p id="program-trend-anon-note" class={note}>
								Anonymous program, so results per month are not shown. The table shows the sends per
								month.
							</p>
						{:else if months.length >= 2}
							<div class="{card} p-4 sm:p-6">
								{#if hasLongTrend}
									<div class="mb-3 flex justify-end">
										<label
											class="flex items-center gap-1 text-xs text-gray-700 dark:text-gray-300 transition-colors duration-200"
										>
											Time range:
											<select
												id="program-trend-range"
												bind:value={trendRange}
												class="pc-select h-6 py-0"
											>
												{#each trendRanges as range}
													<option value={range.value}>{range.label}</option>
												{/each}
											</select>
										</label>
									</div>
								{/if}
								{#if trendMonths.length >= 2}
									<ProgramTrendChart months={trendMonths} />
								{:else}
									<p class="text-sm text-gray-600 dark:text-gray-400">
										Trend needs two months of results.
									</p>
								{/if}
							</div>
						{:else if months.length === 1}
							<p class="text-sm text-gray-600 dark:text-gray-400">
								Trend needs two months of results.
							</p>
						{/if}
						{#if monthTableShown}
							<div class="mt-3">
								<Table
									columns={[{ column: 'Month', size: 'large' }, ...rowTableColumns]}
									pagination={monthParams}
									noSearch
									hasActions={false}
									plural="months"
									hasData={months.length > 0}
									hasNextPage={monthsHasNextPage}
								>
									{#each monthRows as m (m.key)}
										<TableRow>
											<TableCell>
												{m.name}{#if m.partial}{' '}<span
														class={pctCls}
														title="The program ran only part of this month">(partial)</span
													>{/if}
											</TableCell>
											{#each rowColumns as [key, , hideBelow, plain]}
												<TableCell {hideBelow}>
													{#if isHidden(m, key)}
														<span title={rowHiddenTip(m, key)} class={hiddenCls}>Hidden</span>
													{:else if m[key] == null}
														<span class={pctCls}>-</span>
													{:else if plain}
														<span class="tabular-nums">{m[key]}</span>
													{:else}
														{@const c = countPct(m, key)}
														<span class="tabular-nums"
															>{c.count} <span class={pctCls}>({c.pct}%)</span></span
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
					<section id="program-templates" class={showTemplateResults ? endsInTable : ''}>
						<!-- Show sits next to the heading, as the grouped results of a campaign -->
						<div
							class="flex flex-wrap items-baseline gap-x-2 gap-y-1"
							class:mb-3={showTemplateResults}
						>
							<SubHeadline>Results by {groupByLabels[groupBy]}</SubHeadline>
							<button
								type="button"
								id="program-templates-toggle"
								class="pc-focus rounded-sm text-sm font-medium text-cta-blue hover:underline dark:text-highlight-blue transition-colors duration-200"
								aria-expanded={showTemplateResults}
								on:click={() => {
									templateResultsToggled = true;
									showTemplateResults = !showTemplateResults;
								}}>{showTemplateResults ? 'Hide' : 'Show'}</button
							>
							<!-- the counts are not limited by the time range of the tiles -->
							<span
								id="program-templates-range"
								class="ml-auto text-xs text-gray-700 dark:text-gray-300 transition-colors duration-200"
								>All time</span
							>
						</div>
						{#if showTemplateResults}
							{#if canGroup}
								<!-- the same choice as the grouped results of a campaign -->
								<div id="program-group-by" class="mb-4 flex gap-2">
									{#each ['template', 'department', 'position'] as by (by)}
										<button
											type="button"
											class="px-4 py-1.5 text-sm font-medium rounded-md transition-colors duration-200 {groupBy ===
											by
												? 'bg-cta-blue text-white'
												: 'bg-grayblue-light dark:bg-gray-800/60 text-slate-600 dark:text-gray-200 hover:bg-gray-200 dark:hover:bg-gray-700/60'}"
											aria-pressed={groupBy === by}
											on:click={() => onChangeGroupBy(by)}>{groupByLabels[by]}</button
										>
									{/each}
								</div>
							{/if}
							{#if groupBy !== 'template' && groupsFailed && !groupsLoading}
								<div class="flex items-baseline gap-x-2 text-sm text-slate-600 dark:text-gray-300">
									<span>Could not load results</span>
									<button
										type="button"
										class="text-sm font-medium text-cta-blue hover:underline dark:text-highlight-blue transition-colors duration-200"
										on:click={loadGroups}>Retry</button
									>
								</div>
							{:else}
								<div>
									{#if groupBy !== 'template' && groupOmitted > 0}
										<p id="program-groups-omitted" class="{note} mb-2">
											Shows the {groupRows.filter((r) => r.key !== 'deleted').length} groups with the
											most sends, {groupOmitted} more are left out.
										</p>
									{/if}
									<Table
										columns={[
											{ column: groupByLabels[groupBy], size: 'large' },
											...rowTableColumns
										]}
										pagination={templateParams}
										noSearch
										hasActions={false}
										plural="results"
										hasData={groupList.length > 0}
										hasNextPage={templatesHasNextPage}
										isGhost={groupBy !== 'template' && groupsLoading && groupRows.length === 0}
									>
										{#each templateRows as t (t.key)}
											<TableRow>
												<TableCell wrap>
													<span class="pc-cell-text" title={t.name}>{t.name}</span>
												</TableCell>
												{#each rowColumns as [key, , hideBelow, plain]}
													<TableCell {hideBelow}>
														{#if isHidden(t, key)}
															<span title={rowHiddenTip(t, key)} class={hiddenCls}>Hidden</span>
														{:else if t[key] == null}
															<span class={pctCls}>-</span>
														{:else if plain}
															<span class="tabular-nums">{t[key]}</span>
														{:else}
															{@const c = countPct(t, key)}
															<span class="tabular-nums"
																>{c.count} <span class={pctCls}>({c.pct}%)</span></span
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
					</section>
				{/if}
			{/if}

			<!-- details cards, as on the campaign page. The heading groups the cards
			     for screen readers, so the card titles sit one level below it -->
			<section aria-labelledby="program-details-heading">
				<h2 id="program-details-heading" class="sr-only">Details</h2>
				<div id="program-details" class="grid grid-cols-1 lg:grid-cols-3 gap-6">
					{#each detailCards as detailCard, ci (detailCard.title)}
						<!-- the last column holds the Data and Privacy card under the
						     Reporting card, as on the campaign page. The columns are as tall
						     as the tallest one: a single card fills its column and the Data
						     and Privacy card takes the room left under Reporting. -->
						<div class="flex flex-col gap-6">
							<section class="{card} p-6 {ci === detailCards.length - 1 ? '' : 'lg:flex-1'}">
								<h3 class={cardTitle}>{detailCard.title}</h3>
								<div class="space-y-2.5 text-sm">
									{#each detailCard.rows as row (row.label)}
										<ConditionalDisplay show={row.blackbox ? 'blackbox' : 'both'}>
											{#if row.inbox}
												<!-- the same row as on the campaign Reporting card -->
												<div id="program-report-inbox">
													<ReportInboxRow
														inbox={reportInbox}
														now={reportInboxNow}
														settingsHref={inboxSettingsHref}
													/>
												</div>
											{:else}
												<div class="flex justify-between gap-4">
													{#if row.tip}
														<span class="{detailLabel} flex items-center"
															>{row.label}:<ToolTip>{row.tip}</ToolTip></span
														>
													{:else}
														<span class={detailLabel}>{row.label}:</span>
													{/if}
													{#if row.delivery}
														<!-- the same status row as the report inbox on the campaign page -->
														<span
															id="program-report-delivery"
															class="flex shrink-0 items-center gap-x-3"
														>
															<StatusBadge
																tone={reportDeliveryBadge.tone}
																title={reportDeliveryBadge.tip}
																>{reportDeliveryBadge.label}</StatusBadge
															>
															<a
																href={reportDeliveryHref(reportDelivery, program.companyID)}
																class="pc-focus rounded-sm text-xs text-cta-blue dark:text-highlight-blue hover:underline"
																>Settings</a
															>
														</span>
													{:else if row.importReported}
														<div class="shrink-0" title={contextTitle(program)}>
															<ActionButton
																variant="secondary"
																icon="upload"
																disabled={isMismatch}
																on:click={() => reportedUpload?.pick()}
															>
																Import Reported
															</ActionButton>
														</div>
													{:else if row.status}
														<span><StatusBadge tone={statusTone}>{status}</StatusBadge></span>
													{:else if row.groups}
														<div class={detailValue}>
															{#if groups.length}
																{#each groups as group, i}
																	<a
																		class="pc-focus rounded-sm text-cta-blue dark:text-highlight-blue hover:underline"
																		href="/recipient/group/{group.id}">{group.name}</a
																	>{#if i < groups.length - 1},&nbsp;{/if}
																{/each}
															{:else}
																<span class={emptyCls}>None</span>
															{/if}
														</div>
													{:else if row.templates}
														<!-- the first templates of the schedule, the rest in a list as on the campaign page -->
														<div class={detailValue} id="program-details-templates">
															{#if scheduledSteps.length}
																<ol>
																	{#each scheduledSteps.slice(0, TEMPLATES_INLINE_LIMIT) as st, n}
																		<li>
																			{n + 1}. {templateNameOf(
																				st.campaignTemplateID,
																				settingNames.templates,
																				allTemplateNames
																			)}
																		</li>
																	{/each}
																</ol>
																{#if scheduledSteps.length > TEMPLATES_INLINE_LIMIT}
																	<button
																		type="button"
																		class="pc-focus rounded-sm block ml-auto text-cta-blue dark:text-highlight-blue hover:underline"
																		on:click={openTemplatesModal}
																		>+{scheduledSteps.length - TEMPLATES_INLINE_LIMIT} more</button
																	>
																{/if}
															{:else}
																<span class={emptyCls}>None</span>
															{/if}
														</div>
													{:else if row.list?.length}
														<ol class={detailValue}>
															{#each row.list as item}
																<li>{item}</li>
															{/each}
														</ol>
													{:else if row.next}
														<span class={detailValue}>
															{#if isClosed || stopping}
																<span class={emptyCls}>None</span>
															{:else if isPaused}
																<span title="Held while paused">Held</span>
															{:else if isAnonymous}
																<span class={hiddenCls} title={NEXT_HIDDEN_TIP}>Hidden</span>
															{:else if nextSendAt}
																<span title={shortRelative(nextSendAt, now)}
																	>{formatDateTime(nextSendAt)}</span
																>
															{:else}
																<span class={emptyCls}>None</span>
															{/if}
														</span>
													{:else if row.at}
														<span class={detailValue} title={shortRelative(row.at, now)}
															>{formatDateTime(row.at)}</span
														>
													{:else if row.hidden}
														<span title={SENT_HIDDEN_TIP} class={hiddenCls}>Hidden</span>
													{:else if row.value}
														<span class={detailValue}>{row.value}</span>
													{:else}
														<span class={emptyCls}>{row.empty || 'None'}</span>
													{/if}
												</div>
											{/if}
										</ConditionalDisplay>
									{/each}
								</div>
							</section>
							{#if ci === detailCards.length - 1}
								<section id="program-privacy" class="{card} p-6 lg:flex-1">
									<h3 class={cardTitle}>Data and Privacy</h3>
									<div class="divide-y divide-gray-200 dark:divide-gray-700 text-sm">
										<div
											class="flex flex-col items-start gap-2 sm:flex-row sm:items-center sm:justify-between sm:gap-x-4 pb-3"
										>
											<div class="min-w-0">
												<p class="flex items-center text-gray-600 dark:text-gray-400">
													Recipients
													<ToolTip
														>{isAnonymous
															? 'An anonymous program replaces recipients with anonymous IDs when it closes.'
															: 'Replaces recipients with anonymous IDs in all results of the closed program. Permanent.'}</ToolTip
													>
												</p>
												{#if program.anonymizedAt}
													<p class="text-xs text-gray-500 dark:text-gray-400">
														{formatDateTime(program.anonymizedAt)}
													</p>
												{:else if isAnonymous && isClosed}
													<p class="text-xs text-gray-500 dark:text-gray-400">
														{formatDateTime(program.closedAt)}
													</p>
												{:else if isAnonymous}
													<p class="text-xs text-gray-500 dark:text-gray-400">At close</p>
												{:else if program.anonymizingAt}
													<p class="text-xs text-gray-500 dark:text-gray-400">
														Started {formatDateTime(program.anonymizingAt)}, not finished
													</p>
												{:else if anonymizeAllBusy}
													<p class="text-xs text-gray-500 dark:text-gray-400">
														Running in the background
													</p>
												{:else if program.anonymizeAt}
													<p class="text-xs text-gray-500 dark:text-gray-400">
														Scheduled {formatDateTime(program.anonymizeAt)}
													</p>
												{:else if !isClosed}
													<p
														id="program-anonymize-reason"
														class="text-xs text-gray-500 dark:text-gray-400"
													>
														Close the program to anonymize it
													</p>
												{/if}
											</div>
											{#if isAnonymized}
												<StatusBadge tone="success">Anonymized</StatusBadge>
											{:else if !isAnonymous}
												<div
													id="program-anonymize"
													title={contextTitle(program) ||
														(!isClosed ? 'Close the program to anonymize it' : '')}
												>
													<ActionButton
														variant="danger-outline"
														icon="anonymize"
														disabled={isMismatch || !isClosed || !!stopping || !!program.anonymizeRunning}
														on:click={() => (isAnonymizeModalVisible = true)}
													>
														{program.anonymizingAt ? 'Retry Anonymize All' : 'Anonymize All'}
													</ActionButton>
												</div>
											{/if}
										</div>
										<div
											class="flex flex-col items-start gap-2 sm:flex-row sm:items-center sm:justify-between sm:gap-x-4 pt-3"
										>
											<div class="min-w-0">
												<p class="flex items-center text-gray-600 dark:text-gray-400">
													Collected data
													<ToolTip
														>Anonymizes submitted data, IPs, user agents and browser metadata.
														Recipients stay linked. Permanent.</ToolTip
													>
												</p>
												{#if !isAnonymized && (program.dataAnonymizeAt || program.dataAnonymizedAt)}
													<p class="text-xs text-gray-500 dark:text-gray-400">
														{#if program.dataAnonymizeAt && (!program.dataAnonymizedAt || new Date(program.dataAnonymizedAt) < new Date(program.dataAnonymizeAt))}
															Scheduled {formatDateTime(program.dataAnonymizeAt)}
														{:else if dataDoneAfterClose}
															{formatDateTime(program.dataAnonymizedAt)}
														{:else}
															Last run {formatDateTime(program.dataAnonymizedAt)}
														{/if}
													</p>
												{/if}
											</div>
											{#if isAnonymized}
												<StatusBadge tone="success">Anonymized</StatusBadge>
											{:else if dataDoneAfterClose}
												<StatusBadge tone="success">Data anonymized</StatusBadge>
											{:else}
												<div id="program-anonymize-data" title={contextTitle(program)}>
													<ActionButton
														variant="danger-outline"
														icon="anonymize"
														disabled={isMismatch || !!stopping || !!program.anonymizeRunning || anonymizeAllBusy}
														on:click={() => (isAnonymizeDataModalVisible = true)}
													>
														Anonymize Data
													</ActionButton>
												</div>
											{/if}
										</div>
									</div>
								</section>
							{/if}
						</div>
					{/each}
				</div>
			</section>

			{#if notStarted}
				<p class={note}>Results show here once the first templates go out.</p>
				<!-- before the start the recipients are the members of the program groups -->
				{#if !isAnonymous}
					<section id="program-recipients">
						<SubHeadline>Recipients</SubHeadline>
						<ProgramPeopleTable
							programID={program.id}
							params={peopleParams}
							rows={peopleRows}
							hasNextPage={peopleHasNextPage}
							isLoading={isPeopleLoading}
							{stepCount}
							{now}
							notStarted
							startAt={isScheduled ? program.startAt : null}
						/>
					</section>
				{/if}
			{:else if notEnrolled}
				<p class={note}>Nobody is enrolled yet, so there are no results.</p>
			{:else}
				<!-- recipients -->
				<!-- an anonymous program ends in a line of text, which needs room
			     below it; a table brings its own -->
				<section
					id="program-recipients"
					class={isAnonymous || peopleAnonymized ? 'mb-8' : endsInTable}
				>
					<SubHeadline>Recipients</SubHeadline>
					{#if peopleAnonymized && !isAnonymous}
						<!-- the sends of an anonymized program name no one, so there are no
						     rows per recipient -->
						<p id="program-anonymized-people" class="{note} mt-3">
							{plural(view?.total ?? 0, 'recipient')}. The recipients are anonymized, so progress
							and results per recipient are not shown.
						</p>
					{:else if isAnonymous}
						<!-- one line of counts, as an anonymous program has no rows per recipient -->
						<p id="program-anon-people" class="{note} mt-3">
							{plural(view?.total ?? 0, 'recipient')}: {isClosed
								? `${view?.stopped ?? 0} still active at close`
								: `${view?.active ?? 0} active`}, {view?.exited ?? 0} removed. Progress and results per
							recipient are not shown for an anonymous program.
						</p>
					{:else}
						<ProgramPeopleTable
							programID={program.id}
							anonymous={isAnonymous}
							params={peopleParams}
							rows={peopleRows}
							hasNextPage={peopleHasNextPage}
							isLoading={isPeopleLoading}
							{stepCount}
							loops={program.loopMode === 'loop'}
							{isClosed}
							{isPaused}
							{now}
						/>
					{/if}
				</section>

				<!-- events across the sends of the program, as on the campaign page. An
			     anonymous program lists none, as an event tells when one person acted -->
				{#if !isAnonymous}
					<section id="program-events">
						<SubHeadline>Events</SubHeadline>
						<Table
							columns={[
								{ column: 'Created at', size: 'medium' },
								{ column: 'First name', size: 'small' },
								{ column: 'Last name', size: 'small' },
								{ column: 'Email', size: 'large' },
								{ column: 'Event', size: 'large' },
								{ column: 'Details', size: 'small' },
								// the user agent shows on wide screens only, so the IP stays in view
								{ column: 'User-Agent', size: 'small', hideBelow: '2xl' },
								{ title: 'IP', column: 'Ip', size: 'small' },
								{ column: 'Metadata', size: 'small' }
							]}
							sortable={['Created at']}
							pagination={eventsParams}
							plural="events"
							hasActions={false}
							hasData={!!events.length}
							hasNextPage={eventsHasNextPage}
							isGhost={isEventsLoading}
						>
							{#each events as event (event.id)}
								<TableRow>
									<TableCell isDate value={event.createdAt} />
									<TableCell>
										{#if event.recipient?.firstName}
											<a
												href={`/recipient/${event.recipient.id}`}
												class="pc-cell-link block w-full py-1"
											>
												{event.recipient.firstName}
											</a>
										{:else if !event.recipient}
											anonymized
										{/if}
									</TableCell>
									<TableCell>
										{#if event.recipient?.lastName}
											<a
												href={`/recipient/${event.recipient.id}`}
												class="pc-cell-link block w-full py-1"
											>
												{event.recipient.lastName}
											</a>
										{:else if !event.recipient}
											anonymized
										{/if}
									</TableCell>
									<TableCell>
										{#if event.recipient?.email}
											<div class="min-w-0">
												<a
													href={`/recipient/${event.recipient.id}`}
													class="pc-cell-link block w-full truncate py-1"
													title={event.recipient.email}
												>
													{event.recipient.email}
												</a>
												<span class="block sm:hidden pb-1"
													><EventName eventName={eventNames[event.eventID]} /></span
												>
											</div>
										{:else if !event.recipient}
											<!-- a removed or anonymized recipient, as on the campaign page -->
											<div class="min-w-0">
												<span class="block py-1">anonymized</span>
												<span class="block sm:hidden pb-1"
													><EventName eventName={eventNames[event.eventID]} /></span
												>
											</div>
										{/if}
									</TableCell>
									<TableCell>
										<EventName eventName={eventNames[event.eventID]} />
									</TableCell>
									<TableCell>
										<EventDataCell
											data={event.data || ''}
											eventName={eventNames[event.eventID]}
											on:capture={(e) => showSessionCapture(e.detail)}
										/>
									</TableCell>
									<TableCell hideBelow="2xl">
										<CellCopy text={event.userAgent || ''} />
									</TableCell>
									<TableCell>
										<CellCopy text={event.ip || ''} />
									</TableCell>
									<TableCell>
										<CellCopy text={event.metadata || ''} />
									</TableCell>
								</TableRow>
							{/each}
						</Table>
					</section>
				{/if}
			{/if}
		</div>

		<ProgramStartAlert
			bind:visible={isStartAlertVisible}
			name={program.name}
			people={audienceCount}
			{program}
			onConfirm={onStart}
			onEndMoved={load}
		/>

		<Modal
			headerText="Templates"
			visible={isTemplatesModalVisible}
			onClose={() => (isTemplatesModalVisible = false)}
		>
			<div class="py-6 w-full max-w-xl" id="program-templates-modal">
				<p class="mb-4 text-sm text-gray-600 dark:text-gray-300">
					{scheduledSteps.length} templates, in the order of the schedule.
				</p>
				<ol class="max-h-96 overflow-y-auto divide-y divide-gray-200 dark:divide-gray-700">
					{#each scheduledSteps as st, n}
						<li class="py-2 text-sm text-pc-darkblue dark:text-gray-200">
							{n + 1}. {templateNameOf(
								st.campaignTemplateID,
								settingNames.templates,
								allTemplateNames
							)}
						</li>
					{/each}
				</ol>
			</div>
		</Modal>

		<ProgramCloseAlert bind:visible={isCloseAlertVisible} name={program.name} onConfirm={onClose} />
		<Alert
			headline="anonymize"
			bind:visible={isAnonymizeModalVisible}
			onConfirm={onConfirmAnonymize}
			verification="confirm"
		>
			<div>
				<ul
					class="list-disc ml-8 mt-4 mb-4 text-gray-700 dark:text-gray-200 transition-colors duration-200"
				>
					<li>Recipients are replaced with anonymous IDs in all results</li>
					<li>Result counts are kept</li>
					<li>Anonymization is permanent and not reversible</li>
				</ul>
			</div>
		</Alert>
		<Alert
			headline="anonymize data"
			bind:visible={isAnonymizeDataModalVisible}
			onConfirm={onConfirmAnonymizeData}
			verification="confirm"
		>
			<div>
				<ul
					class="list-disc ml-8 mt-4 mb-4 text-gray-700 dark:text-gray-200 transition-colors duration-200"
				>
					<li>Submitted data, user agent, ip and browser metadata will be anonymized</li>
					<li>The recipient relation is kept</li>
					<li>Anonymization is permanent and not reversible</li>
					{#if !isClosed}
						<li>The program stays open and keeps tracking events</li>
					{/if}
				</ul>
			</div>
		</Alert>
		<ProgramPauseAlert
			bind:visible={isPauseAlertVisible}
			name={program.name}
			hasEndDate={!!program.endAt}
			onConfirm={pauseResume}
		/>
		<ProgramResumeAlert
			bind:visible={isResumeAlertVisible}
			name={program.name}
			onConfirm={pauseResume}
		/>
		<DeleteAlert
			list={stopping === 'delete'
				? ['Finishes deleting the program and its results']
				: neverSent
					? ['Deleting removes the program']
					: [
							'Deleting removes the program, its sends and all results',
							'Close the program instead to keep the results'
						]}
			name={program.name}
			onClick={onDelete}
			bind:isVisible={isDeleteAlertVisible}
		></DeleteAlert>

		<SessionCaptureModal bind:visible={isSessionCaptureVisible} bind:data={sessionCaptureData} />

		<ProgramReportModal
			bind:this={reportModal}
			programID={program.id}
			programName={program.name}
			enabled={isReportPDFEnabled}
			anonymous={isAnonymous}
			timeZone={program.timeZone || ''}
		/>
	{:else if loadError}
		<Breadcrumb
			crumbs={[
				{ label: 'Programs', href: '/program/' },
				{ label: notFound ? 'Not found' : 'Program' }
			]}
		/>
		<div id="program-load-error" class="{note} mt-4 max-w-2xl" role="alert">
			{#if notFound}
				<p>
					This program could not be found. It may have been deleted, or it belongs to a company you
					cannot view.
				</p>
			{:else}
				<p class="flex flex-wrap items-baseline gap-x-2">
					<span>This program could not be loaded.</span>
					<button type="button" class={inlineLink} disabled={isRefreshing} on:click={load}
						>Retry</button
					>
				</p>
			{/if}
			<a class="{inlineLink} mt-2 inline-block" href="/program/">Back to programs</a>
		</div>
	{/if}
</main>
