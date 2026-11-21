<script>
	// The create and edit wizard of a program, shown in a modal.
	// openCreateModal and openUpdateModal load the options they need.
	// Fires "saved" after a program is created or updated.
	import { formatTimeConstraint, timeFormat } from '$lib/utils/date.js';
	import { api } from '$lib/api/apiProxy.js';
	import { createEventDispatcher, onMount, tick } from 'svelte';
	import { fetchAllRows } from '$lib/utils/api-utils.js';
	import { BiMap } from '$lib/utils/maps';
	import TextField from '$lib/components/TextField.svelte';
	import TextFieldSelect from '$lib/components/TextFieldSelect.svelte';
	import TextFieldMultiSelect from '$lib/components/TextFieldMultiSelect.svelte';
	import ToIcon from '$lib/components/ToIcon.svelte';
	import Modal from '$lib/components/Modal.svelte';
	import FormGrid from '$lib/components/FormGrid.svelte';
	import FormError from '$lib/components/FormError.svelte';
	import SelectSquare from '$lib/components/SelectSquare.svelte';
	import FormColumns from '$lib/components/FormColumns.svelte';
	import CheckboxField from '$lib/components/CheckboxField.svelte';
	import DateField from '$lib/components/DateField.svelte';
	import {
		binaryToWeekDays,
		browserTimeZone,
		timeZoneLabel,
		weekDaysToBinary
	} from '$lib/utils/dailySlots.js';
	import DateTimeField from '$lib/components/DateTimeField.svelte';
	import Datetime from '$lib/components/Datetime.svelte';
	import RelativeTime from '$lib/components/RelativeTime.svelte';
	import FormButton from '$lib/components/FormButton.svelte';
	import FieldLabel from '$lib/components/form/FieldLabel.svelte';
	import StatusBadge from '$lib/components/StatusBadge.svelte';
	import Note from '$lib/components/Note.svelte';
	import ConditionalDisplay from '$lib/components/ConditionalDisplay.svelte';
	import { displayMode, showsRedTeam } from '$lib/store/displayMode';
	import { addToast } from '$lib/store/toast';
	import {
		DEFAULT_INTERVAL_MINUTES,
		HOLIDAY_PRESETS,
		MINUTES_PER_DAY,
		MINUTES_PER_HOUR,
		MINUTES_PER_MONTH,
		MINUTES_PER_WEEK,
		capitalize,
		describeBlackout,
		describeDays,
		describeDuration,
		describeInterval,
		reportDeliveryHref,
		reportDeliveryStatus,
		formatDate,
		formatDateTime,
		fromZoneLocal,
		plural,
		toZoneLocal,
		weekDayOrder
	} from '$lib/utils/programText.js';
	import {
		blackoutRangeError,
		countSends,
		dayDate,
		hasSendDay,
		nextSendingDay,
		safeEnd,
		sendSchedule,
		sendWindows,
		windowClockProblem
	} from '$lib/utils/programSchedule.js';
	import ProgramEndTimeline from './ProgramEndTimeline.svelte';
	import {
		eventDisplayNames,
		webhookDataLevelOptions,
		webhookEventOptions,
		webhookEventsFromBinary,
		webhookEventsToBinary
	} from '$lib/consts/events.js';

	export let contextCompanyID = '';

	const dispatch = createEventDispatcher();

	const wizardSteps = [
		{ name: 'Setup' },
		{ name: 'Templates' },
		{ name: 'Schedule' },
		{ name: 'Review' }
	];

	// which wizard step each validated field lives on
	const fieldStep = {
		name: 1,
		groups: 1,
		webhooks: 1,
		denyPage: 1,
		allowDeny: 1,
		simulations: 2,
		orderMode: 2,
		templateMixBy: 2,
		loopMode: 2,
		startAt: 3,
		endAt: 3,
		endRounds: 3,
		interval: 3,
		spread: 3,
		days: 3,
		times: 3,
		blackout: 3,
		newMemberDelay: 3,
		anonymizeAt: 3,
		dataAnonymizeAt: 3,
		saveSubmittedData: 1
	};
	// the wizard field of a field name in a server error
	const serverFields = {
		startAt: 'startAt',
		endAt: 'endAt',
		stepIntervalMinutes: 'interval',
		spreadMinutes: 'spread',
		newMemberDelayMinutes: 'newMemberDelay',
		constraintWeekDays: 'days',
		constraintStartTime: 'times',
		constraintEndTime: 'times',
		blackoutDates: 'blackout',
		anonymizeAt: 'anonymizeAt',
		dataAnonymizeAt: 'dataAnonymizeAt',
		steps: 'simulations',
		recipientGroupIDs: 'groups',
		allowDenyIDs: 'allowDeny',
		webhooks: 'webhooks',
		denyPageID: 'denyPage',
		evasionPageID: 'denyPage',
		saveSubmittedData: 'saveSubmittedData'
	};
	// fields shown only under "Show advanced options"
	const advancedFields = ['webhooks', 'denyPage', 'allowDeny', 'saveSubmittedData'];
	// fields shown only under "Security Configuration"
	const securityFields = ['denyPage', 'allowDeny'];
	// the error text under each field, the id is used for aria-describedby
	const errorId = (field) => `program-${field}-error`;
	const errorClass = 'text-red-600 dark:text-red-400 text-sm';
	// the control to focus when a field has an error
	const fieldControl = {
		name: 'program-name',
		groups: 'program-groups',
		webhooks: () => `program-webhook-${webhookErrorIndex}`,
		denyPage: 'program-deny-page',
		allowDeny: 'program-allow-deny',
		simulations: 'program-simulation-template-0',
		orderMode: 'program-order',
		templateMixBy: 'program-mix',
		loopMode: 'program-loop',
		startAt: () => document.querySelector('#program-start-at input[type="date"]'),
		endAt: () => document.querySelector('#program-end-at input[type="date"]'),
		endRounds: 'program-end-rounds',
		interval: 'program-interval',
		spread: 'program-spread',
		days: 'program-day-1',
		times: 'program-end-time',
		blackout: () => `program-blackout-from-${blackoutErrorIndex}`,
		newMemberDelay: 'program-new-member-delay',
		anonymizeAt: () => document.querySelector('#program-anonymize-at input[type="date"]'),
		dataAnonymizeAt: () => document.querySelector('#program-data-anonymize-at input[type="date"]'),
		saveSubmittedData: () => document.querySelector('#program-save-data button')
	};
	// the first skip dates row or webhook with an error, for focus
	let blackoutErrorIndex = 0;
	let webhookErrorIndex = 0;

	// the selects show and bind the label, like the campaign wizard, and the
	// payload maps it back to the API value
	const orderModes = new BiMap({
		'Same order for everyone': 'sequential',
		'Random order per recipient': 'shuffle'
	});
	const loopModes = new BiMap({
		'Start over': 'loop',
		Stop: 'complete'
	});
	// recipient field templates are mixed by in random order, the same fields
	// as the campaign wizard. None is off.
	const templateMixes = new BiMap({
		None: '',
		Department: 'department',
		Position: 'position',
		City: 'city'
	});
	// minutes for each preset
	const intervalPresets = [
		MINUTES_PER_DAY,
		2 * MINUTES_PER_DAY,
		3 * MINUTES_PER_DAY,
		MINUTES_PER_WEEK,
		2 * MINUTES_PER_WEEK,
		MINUTES_PER_MONTH,
		3 * MINUTES_PER_MONTH
	];
	const spreadPresets = [
		0,
		MINUTES_PER_HOUR,
		4 * MINUTES_PER_HOUR,
		MINUTES_PER_DAY,
		2 * MINUTES_PER_DAY,
		3 * MINUTES_PER_DAY,
		MINUTES_PER_WEEK,
		2 * MINUTES_PER_WEEK,
		MINUTES_PER_MONTH,
		2 * MINUTES_PER_MONTH,
		3 * MINUTES_PER_MONTH
	];
	const delayPresets = [
		0,
		MINUTES_PER_DAY,
		2 * MINUTES_PER_DAY,
		3 * MINUTES_PER_DAY,
		MINUTES_PER_WEEK,
		2 * MINUTES_PER_WEEK,
		MINUTES_PER_MONTH
	];
	const intervalLabel = (m) => describeDuration(m);
	const spreadLabel = (m) => (m > 0 ? describeDuration(m) : 'All at once');
	const delayLabel = (m) => (m > 0 ? `After ${describeDuration(m)}` : 'No wait');
	// the presets plus a stored value that is not one of them, sorted
	const withValue = (presets, value) =>
		value == null || presets.includes(value) ? presets : [...presets, value].sort((a, b) => a - b);
	// the minutes of a picked label, null when it is not in the list
	const minutesOf = (list, toLabel, label) => {
		const m = list.find((v) => toLabel(v) === label);
		return m === undefined ? null : m;
	};
	const startModes = [
		{
			value: 'now',
			label: 'Now',
			description: 'From the next sending time',
			id: 'program-start-mode-now'
		},
		{
			value: 'date',
			label: 'On a date',
			description: 'Nothing is sent before it',
			id: 'program-start-mode-date'
		},
		{
			value: 'draft',
			label: 'Save as draft',
			description: 'Nothing is sent until started',
			id: 'program-start-mode-draft'
		}
	];
	// how long the program runs. Rounds sets the end to when the last template
	// of the rounds has had time to be acted on. More than one round is only
	// offered when the program starts over, and Rounds only before the start.
	const endModeOptions = (beforeStart) => [
		...(beforeStart
			? [
					{
						value: 'rounds',
						label: 'Rounds',
						description: 'Ends after the last round',
						id: 'program-end-mode-rounds'
					}
				]
			: []),
		{
			value: 'date',
			label: 'Custom date',
			description: 'Closes itself then',
			id: 'program-end-mode-date'
		},
		{
			value: 'never',
			label: 'Never',
			description: 'Runs until you close it',
			id: 'program-end-mode-never'
		}
	];
	const roundCounts = [1, 2, 3, 4, 5, 6, 7, 8, 9, 10];
	const roundsLabel = (n) => (n === 1 ? 'One round' : `${n} rounds`);
	const loopsOf = (v) => loopModes.byKey(v.loopMode) === 'loop';
	// the number of rounds the end is set for. One for a custom date, and with
	// Stop, where every template goes out once.
	const roundsOf = (v) =>
		v.endMode === 'rounds' && loopsOf(v)
			? roundCounts.find((n) => roundsLabel(n) === v.endRounds) || 1
			: 1;
	const hasEnd = (v) => v.endMode !== 'never';
	const filteringOptions = [
		{ value: 'none', label: 'Off', id: 'program-filter-none' },
		{ value: 'allow', label: 'Only allow', id: 'program-filter-allow' },
		{ value: 'deny', label: 'Block', id: 'program-filter-deny' }
	];
	const securityOptions = [
		{ value: false, label: 'Disabled', id: 'program-security-off' },
		{ value: true, label: 'Enabled', id: 'program-security-on' }
	];
	// below this many recipients most results of an anonymous program are hidden,
	// as each result needs at least 5 recipients who did, and 5 who did not, act
	const ANON_SMALL_AUDIENCE = 20;
	// the most of each list the server takes, see model/program.go
	const blackoutMax = 50;
	const webhookMax = 20;
	const stepMax = 200;
	const groupMax = 50;
	const allowDenyMax = 50;
	const SPREAD_TOO_LONG = 'The spread cannot be longer than the time between templates';
	const labelOf = (list, value) => list.find((p) => p.value === String(value))?.label || '';
	// shows the browser zone and the stored one when they differ
	const zoneNote = (zone) => (zone && zone !== browserTimeZone() ? timeZoneLabel(zone) : '');

	// the same classes the campaign wizard uses
	const noteClass = 'text-sm text-slate-500 dark:text-gray-400 transition-colors duration-200';
	const linkClass =
		'pc-focus rounded-sm text-sm font-medium text-cta-blue dark:text-highlight-blue hover:underline';
	const addButtonClass =
		'pc-focus inline-flex items-center px-4 py-2 rounded-md border border-cta-blue text-cta-blue bg-white hover:bg-blue-50 dark:bg-gray-800 dark:border-highlight-blue dark:text-blue-200 dark:hover:bg-highlight-blue/20 text-sm font-semibold transition-colors duration-200 disabled:opacity-50 disabled:cursor-not-allowed disabled:hover:bg-white dark:disabled:hover:bg-gray-800';
	const rowButtonClass =
		'pc-focus inline-flex items-center justify-center w-8 h-8 flex-shrink-0 rounded-md text-slate-600 dark:text-gray-400 hover:bg-gray-200 dark:hover:bg-gray-700/80 transition-colors duration-200 disabled:opacity-30 disabled:cursor-not-allowed disabled:hover:bg-transparent';
	const timeInputClass =
		'w-full sm:w-auto rounded-md py-2 px-2 text-slate-800 dark:text-gray-300 text-left border bg-grayblue-light dark:bg-gray-900/60 font-normal transition-colors duration-200 focus:outline-none focus:ring-2';
	const timeInputOkClass =
		'border-transparent dark:border-gray-700/60 focus:border-cta-blue focus:ring-cta-blue/30 dark:focus:border-highlight-blue dark:focus:ring-highlight-blue/30';
	const timeInputErrorClass =
		'border-red-500 dark:border-red-400 focus:border-red-500 focus:ring-red-500/30 dark:focus:ring-red-400/30';
	const wizardSecondaryButtonClass =
		'pc-focus min-w-[6rem] inline-flex items-center justify-center gap-1 px-4 py-2 rounded-md border border-gray-300 bg-transparent hover:bg-gray-100 text-slate-700 dark:bg-gray-700 dark:hover:bg-gray-600 dark:border-gray-500 dark:text-gray-100 text-sm font-bold uppercase transition-colors duration-200 disabled:opacity-50 disabled:cursor-not-allowed';
	const reviewCardClass =
		'min-w-0 bg-white dark:bg-gray-900/40 p-4 sm:p-5 rounded-lg ring-1 ring-gray-200 dark:ring-gray-700 transition-colors duration-200';
	const reviewTitleClass =
		'text-base font-semibold text-pc-darkblue dark:text-white mb-3 border-b border-gray-200 dark:border-gray-700 pb-2 transition-colors duration-200';
	const reviewGridClass =
		'grid grid-cols-[minmax(6rem,auto)_minmax(0,1fr)] gap-x-4 gap-y-2 text-sm [&>*]:min-w-0 [&>:nth-child(even)]:[overflow-wrap:anywhere]';
	const webhookEventOnClass =
		'bg-blue-50 dark:bg-highlight-blue/20 text-cta-blue dark:text-white border border-cta-blue dark:border-highlight-blue hover:bg-blue-100 dark:hover:bg-highlight-blue/30';
	const webhookEventOffClass =
		'bg-white dark:bg-gray-900/60 text-gray-700 dark:text-gray-300 border border-gray-200 dark:border-gray-700/60 hover:border-blue-300 dark:hover:border-highlight-blue/80 hover:bg-blue-50 dark:hover:bg-highlight-blue/20';
	const dayNames = ['Sunday', 'Monday', 'Tuesday', 'Wednesday', 'Thursday', 'Friday', 'Saturday'];
	const anonymousOptions = [
		{ value: true, label: 'Yes', id: 'program-anonymous-on' },
		{ value: false, label: 'No', id: 'program-anonymous-off' }
	];
	const saveSubmittedDataOptions = [
		{ value: true, label: 'Yes', id: 'program-save-data-on' },
		{ value: false, label: 'No', id: 'program-save-data-off' }
	];
	// automated report email
	const reportEnabledOptions = [
		{ value: true, label: 'On', id: 'program-report-on' },
		{ value: false, label: 'Off', id: 'program-report-off' }
	];
	const reportIntervalOptions = [
		{ value: 'monthly', label: 'Monthly', id: 'program-report-monthly' },
		{ value: 'quarterly', label: 'Quarterly', id: 'program-report-quarterly' },
		{ value: 'yearly', label: 'Yearly', id: 'program-report-yearly' }
	];
	const reportPeriodOptions = [
		{ value: 'last', label: 'The period that ended', id: 'program-report-period-last' },
		{ value: 'start', label: 'Since the program started', id: 'program-report-period-start' }
	];
	const reportText = (v) =>
		v.reportEnabled
			? `${labelOf(reportIntervalOptions, v.reportInterval)}, ${labelOf(reportPeriodOptions, v.reportPeriod).toLowerCase()}`
			: 'Off';
	const webhookEventDisplayOptions = webhookEventOptions.map((event) => ({
		value: event,
		label: eventDisplayNames[event] || event
	}));

	let templateMap = new BiMap({});
	let groupMap = new BiMap({});
	let pageMap = new BiMap({});
	let allAllowDeny = [];
	let webhookMap = new BiMap({});
	// the loaded rows of the company in depsCompanyID; the maps above are built
	// from them, plus stored items that could not be found when editing
	let loaded = { templates: [], groups: [], pages: [], allowDeny: [], webhooks: [] };
	let depsCompanyID = null;
	let dependenciesLoaded = false;
	// the company of the program being edited, or the context company
	let formCompanyID = '';
	// names of stored items the edit wizard could not find
	let missingItems = [];

	let form = null;
	let isModalVisible = false;
	let modalMode = 'create';
	let modalError = '';
	let isSubmitting = false;
	let currentStep = 1;
	let errors = {};
	let showAdvancedOptions = false;
	// deny page, evasion page and filtering show when this is enabled, as in the campaign wizard
	let showSecurityOptions = false;
	$: isBlackbox = showsRedTeam($displayMode);

	const emptyForm = () => ({
		id: null,
		name: null,
		status: null,
		startedAt: null,
		pausedAt: null,
		startMode: 'now',
		startAtLocal: '',
		endMode: 'never',
		endAtLocal: '',
		endRounds: roundsLabel(1),
		orderMode: orderModes.byValue('shuffle'),
		templateMixBy: templateMixes.byValue(''),
		loopMode: loopModes.byValue('loop'),
		interval: intervalLabel(DEFAULT_INTERVAL_MINUTES),
		spread: spreadLabel(MINUTES_PER_WEEK),
		newMemberDelay: delayLabel(2 * MINUTES_PER_WEEK),
		// sending hours and skip dates are in this zone, it is set from the browser
		// on create and kept on update
		timeZone: browserTimeZone(),
		weekDays: [false, true, true, true, true, true, false],
		startTime: '09:00',
		endTime: '17:00',
		blackout: [],
		denyPage: null,
		evasionPage: null,
		filterMode: 'none',
		allowDeny: [],
		webhooks: [],
		isAnonymous: false,
		saveSubmittedData: false,
		// the anonymize dates, as on the campaign wizard
		anonymizeAt: null,
		dataAnonymizeAt: null,
		reportEnabled: false,
		reportInterval: 'monthly',
		reportPeriod: 'last',
		steps: [{ id: null, template: null }],
		recipientGroups: []
	});

	let formValues = emptyForm();

	$: isEdit = modalMode === 'update';
	$: modalText = isEdit ? 'Update program' : 'New program';
	// anonymity is fixed once people are enrolled. The server counts the
	// enrollments, which exist from the start, also before the start mark is
	// stored, so any program past draft or scheduled is locked
	$: anonymousLocked =
		isEdit &&
		(!!formValues.startedAt ||
			(!!formValues.status && !['draft', 'scheduled'].includes(formValues.status)));
	// anonymous results are cut into months in the program zone, so the server
	// refuses another zone once an anonymous program has started
	const zoneFixed = (v) =>
		!!v.isAnonymous &&
		(!!v.startedAt || (!!v.status && !['draft', 'scheduled'].includes(v.status)));
	$: timeZoneLocked = isEdit && zoneFixed(formValues);
	// an anonymous program never saves submitted data
	$: if (formValues.isAnonymous && formValues.saveSubmittedData)
		formValues.saveSubmittedData = false;
	// an anonymous program is anonymized when it closes, so it takes no
	// anonymize dates, as the data date is hidden on the campaign wizard
	$: if (formValues.isAnonymous && (formValues.anonymizeAt || formValues.dataAnonymizeAt)) {
		formValues.anonymizeAt = null;
		formValues.dataAnonymizeAt = null;
	}
	// the start can be chosen until the program has started
	$: preStart = !isEdit || formValues.status === 'draft' || formValues.status === 'scheduled';
	$: submitText = !preStart
		? 'Update'
		: formValues.startMode === 'draft'
			? 'Save as draft'
			: formValues.startMode === 'date'
				? isEdit
					? 'Update and schedule'
					: 'Create and schedule'
				: isEdit
					? 'Update and start'
					: 'Create and start';
	// allow and deny lists cannot be mixed, only offer the chosen kind
	$: allowDenyOptions = allAllowDeny
		.filter((e) => (formValues.filterMode === 'allow' ? e.allowed : !e.allowed))
		.map((e) => e.name);
	// the stored minutes are kept in the lists when they are not a preset
	let storedMinutes = { interval: null, spread: null, delay: null };
	$: intervalList = withValue(intervalPresets, storedMinutes.interval);
	$: intervalMinutes = minutesOf(intervalList, intervalLabel, formValues.interval);
	// the spread can be as long as the time between templates, not longer
	$: spreadList = withValue(spreadPresets, storedMinutes.spread).filter(
		(m) => intervalMinutes == null || m <= intervalMinutes
	);
	$: spreadMinutes = minutesOf(
		withValue(spreadPresets, storedMinutes.spread),
		spreadLabel,
		formValues.spread
	);
	$: spreadTooLong =
		spreadMinutes != null && intervalMinutes != null && spreadMinutes > intervalMinutes;
	$: delayList = withValue(delayPresets, storedMinutes.delay);
	// the interval and the spread together in one plain sentence
	$: cadenceText =
		intervalMinutes == null || spreadMinutes == null
			? ''
			: spreadMinutes > 0
				? `A new template ${describeInterval(intervalMinutes)}, sent at a random time in the first ${describeDuration(spreadMinutes).replace(/^1 /, '')}.`
				: `A new template ${describeInterval(intervalMinutes)}, sent to everyone at the first sending time.`;
	$: loopsPicked = loopsOf(formValues);
	$: endModes = endModeOptions(preStart);
	// rounds keep the end on the safe end of those rounds
	// while the schedule changes. A custom or stored end is never moved.
	let roundsEndKey = '';
	$: syncRoundsEnd(
		formValues.endMode === 'rounds'
			? JSON.stringify([
					formValues.endMode,
					formValues.endRounds,
					formValues.steps.filter((s) => s.template).length,
					formValues.interval,
					formValues.spread,
					storedMinutes,
					formValues.loopMode,
					formValues.weekDays,
					formValues.blackout,
					formValues.startTime,
					formValues.endTime,
					formValues.timeZone,
					formValues.startMode,
					formValues.startAtLocal,
					formValues.startedAt,
					preStart
				])
			: ''
	);

	// the check page and the filter need a deny page, as in the campaign wizard
	$: if (!formValues.denyPage && (formValues.evasionPage || formValues.filterMode !== 'none')) {
		formValues.evasionPage = null;
		formValues.filterMode = 'none';
		formValues.allowDeny = [];
	}

	onMount(() => {
		return () => {
			clearTimeout(audienceTimer);
		};
	});

	// a shared item and a company item can have the same name, so a shared
	// item that shares its name gets " (shared)" and any name still taken gets
	// a number, which keeps each label unique for the mapping back to the id
	// a label that is not in use yet, " (2)" and up is added until it is free
	const freeLabel = (base, used) => {
		let label = base;
		for (let n = 2; used.has(label); n++) label = `${base} (${n})`;
		used.add(label);
		return label;
	};
	const uniqueNames = (rows, nameOf = (r) => r.name) => {
		const count = {};
		for (const r of rows) count[nameOf(r)] = (count[nameOf(r)] || 0) + 1;
		const used = new Set();
		return rows.map((r) => {
			let name = nameOf(r);
			if (count[name] > 1 && !r.companyID) name = `${name} (shared)`;
			return { ...r, name: freeLabel(name, used) };
		});
	};

	// the member count shows after the name, as in the campaign wizard
	const groupLabel = (g) =>
		`${g.name} (${g.recipientCount != null ? g.recipientCount : g.isDynamic ? 'dynamic' : 0})`;

	// builds the option maps from the loaded rows plus extra rows
	const buildMaps = (extra = {}) => {
		templateMap = BiMap.FromArrayOfObjects([...loaded.templates, ...(extra.templates || [])]);
		groupMap = BiMap.FromArrayOfObjects([...loaded.groups, ...(extra.groups || [])]);
		pageMap = BiMap.FromArrayOfObjects([...loaded.pages, ...(extra.pages || [])]);
		allAllowDeny = [...loaded.allowDeny, ...(extra.allowDeny || [])];
		webhookMap = BiMap.FromArrayOfObjects([...loaded.webhooks, ...(extra.webhooks || [])]);
	};

	// the rows of one company, the five lists load at the same time
	const fetchDependencies = async (companyID) => {
		const [templates, groups, allowDeny, pages, webhooks] = await Promise.all([
			fetchAllRows((options) => api.campaignTemplate.getAll(options, companyID, true)),
			fetchAllRows((options) => api.recipient.getAllGroups(options, companyID)),
			fetchAllRows((options) => api.allowDeny.getAllOverview(options, companyID)),
			fetchAllRows((options) => api.page.getOverviews(options, companyID)),
			fetchAllRows((options) => api.webhook.getAll(options, companyID))
		]);
		return {
			templates: uniqueNames(templates),
			groups: uniqueNames(groups).map((g) => ({ ...g, name: groupLabel(g) })),
			pages: uniqueNames(pages),
			allowDeny: uniqueNames(allowDeny),
			webhooks: uniqueNames(webhooks)
		};
	};

	// templates, groups, lists, pages and webhooks the wizard offers, for one
	// company. They are loaded each time the wizard opens, like the campaign
	// wizard, so new, deleted and changed rows show right away.
	let depsSeq = 0;
	export const loadDependencies = async (companyID = contextCompanyID) => {
		const id = companyID || '';
		const seq = ++depsSeq;
		try {
			const rows = await fetchDependencies(id);
			// a newer load was started meanwhile, it sets the options
			if (seq !== depsSeq) return false;
			loaded = rows;
			depsCompanyID = id;
			buildMaps();
			dependenciesLoaded = true;
			return true;
		} catch (e) {
			addToast('Failed to load templates, groups or pages', 'Error');
			console.error('failed to load program dependencies', e);
			return false;
		}
	};

	// audience size for the chosen groups, counted by the server
	let audienceCount = null;
	let audienceLoading = false;
	let audienceTimer = null;
	let audienceKey = '';
	let audienceSeq = 0;

	$: scheduleAudienceCount(formValues.recipientGroups, isModalVisible);

	const scheduleAudienceCount = (groups, visible) => {
		const ids = (groups || []).map((name) => groupMap.byValue(name)).filter(Boolean);
		const key = visible ? ids.join(',') : '';
		if (key === audienceKey) {
			return;
		}
		audienceKey = key;
		clearTimeout(audienceTimer);
		audienceSeq++;
		if (!visible || ids.length === 0) {
			audienceCount = null;
			audienceLoading = false;
			return;
		}
		audienceLoading = true;
		const seq = audienceSeq;
		audienceTimer = setTimeout(async () => {
			try {
				const res = await api.program.audience(ids, formCompanyID || contextCompanyID);
				if (seq !== audienceSeq) return;
				audienceCount = res.success ? (res.data?.people ?? null) : null;
			} catch (e) {
				if (seq === audienceSeq) audienceCount = null;
				console.error('failed to count program audience', e);
			} finally {
				if (seq === audienceSeq) audienceLoading = false;
			}
		}, 400);
	};

	// whether the automated report can be delivered for the program scope: a
	// shared program uses the global delivery settings, a company program the
	// delivery settings of its company
	let reportDelivery = null;
	let reportDeliveryKey = '';
	let reportDeliverySeq = 0;
	$: checkReportDelivery(
		formValues.reportEnabled,
		formCompanyID || contextCompanyID,
		isModalVisible
	);

	const checkReportDelivery = async (enabled, companyID, visible) => {
		const key = visible && enabled ? `on:${companyID || ''}` : '';
		if (key === reportDeliveryKey) return;
		reportDeliveryKey = key;
		const seq = ++reportDeliverySeq;
		if (!key) {
			reportDelivery = null;
			return;
		}
		try {
			const res = await api.program.reportDelivery(companyID || null);
			if (seq !== reportDeliverySeq) return;
			reportDelivery = res.success ? { ...res.data, companyID: companyID || '' } : null;
		} catch (e) {
			if (seq === reportDeliverySeq) reportDelivery = null;
			console.error('failed to check program report delivery', e);
		}
	};
	// the delivery status badge and where it is set up, null while unknown
	$: reportDeliveryBadge =
		formValues.reportEnabled && reportDelivery ? reportDeliveryStatus(reportDelivery) : null;

	const resetModalState = () => {
		modalError = '';
		errors = {};
		currentStep = 1;
		showAdvancedOptions = false;
		showSecurityOptions = false;
		audienceKey = '';
		audienceCount = null;
		clearTimeout(audienceTimer);
		audienceTimer = null;
		audienceSeq++;
		audienceLoading = false;
		reportDeliveryKey = '';
		reportDeliverySeq++;
		reportDelivery = null;
	};

	// the modal does not focus on open, so the stepper does not take focus
	const focusFirstField = async () => {
		await tick();
		await tick();
		document.getElementById('program-name')?.focus();
	};

	// a stored item that is not in the options, for example one that was
	// deleted, is kept as an extra option with a warning, so saving does not
	// drop it. Its name is looked up when possible.
	// isCurrent tells if this open is still the latest one
	const addMissingItems = async (p, isCurrent = () => true) => {
		// start from the loaded options, without extras of an earlier edit
		buildMaps();
		const extra = { templates: [], groups: [], pages: [], allowDeny: [], webhooks: [] };
		const names = [];
		const lookup = async (fn, id) => {
			try {
				const res = await fn(id);
				return res.success ? res.data : null;
			} catch (e) {
				return null;
			}
		};
		const taken = (map, list) => new Set([...map.values(), ...list.map((e) => e.name)]);
		const add = async (ids, map, list, fn, kind, label = (row) => row?.name) => {
			const used = taken(map, list);
			for (const id of new Set(ids.filter(Boolean))) {
				if (map.byKey(id)) continue;
				const row = await lookup(fn, id);
				const name = freeLabel(`${label(row) || `Unknown ${kind}`} (not found)`, used);
				list.push({ id, name });
				names.push(`${kind} ${label(row) || id}`);
			}
		};
		await add(
			(p.steps || []).map((s) => s.campaignTemplateID),
			templateMap,
			extra.templates,
			api.campaignTemplate.getByID,
			'template'
		);
		await add(
			p.recipientGroupIDs || [],
			groupMap,
			extra.groups,
			api.recipient.getGroupByID,
			'group'
		);
		await add([p.denyPageID, p.evasionPageID], pageMap, extra.pages, api.page.getByID, 'page');
		await add(
			(p.webhooks || []).map((w) => w.webhookID),
			webhookMap,
			extra.webhooks,
			api.webhook.getByID,
			'webhook'
		);
		// lists also need their kind, allow or deny
		const knownLists = new Set(allAllowDeny.map((e) => e.id));
		const usedLists = new Set(allAllowDeny.map((e) => e.name));
		for (const id of new Set((p.allowDenyIDs || []).filter(Boolean))) {
			if (knownLists.has(id)) continue;
			const row = await lookup(api.allowDeny.getByID, id);
			const sibling = allAllowDeny.find((e) => (p.allowDenyIDs || []).includes(e.id));
			extra.allowDeny.push({
				id,
				name: freeLabel(`${row?.name || 'Unknown list'} (not found)`, usedLists),
				allowed: row ? !!row.allowed : sibling ? sibling.allowed : true
			});
			names.push(`list ${row?.name || id}`);
		}
		if (!isCurrent()) return;
		missingItems = names;
		buildMaps(extra);
	};

	// counts the opens, so a slow load of an older open does not fill the form
	let openSeq = 0;

	export const openCreateModal = async () => {
		const seq = ++openSeq;
		formCompanyID = contextCompanyID || '';
		missingItems = [];
		const ok = await loadDependencies(formCompanyID);
		if (!ok || seq !== openSeq) return;
		buildMaps();
		modalMode = 'create';
		formValues = { ...emptyForm(), companyID: formCompanyID || null };
		storedMinutes = { interval: null, spread: null, delay: null };
		originalValues = null;
		loadedProgram = { startAt: null, timeZone: null };
		resetModalState();
		isModalVisible = true;
		focusFirstField();
	};

	export const openUpdateModal = async (id) => {
		const seq = ++openSeq;
		try {
			const res = await api.program.getByID(id);
			if (seq !== openSeq) return;
			if (!res.success) {
				throw res.error;
			}
			const p = res.data;
			if (p.closedAt) {
				addToast('A closed program cannot be edited', 'Error');
				return;
			}
			// the options come from the company of the program, which can differ
			// from the company picked in the menu
			formCompanyID = p.companyID || '';
			const ok = await loadDependencies(formCompanyID);
			if (!ok || seq !== openSeq) return;
			await addMissingItems(p, () => seq === openSeq);
			if (seq !== openSeq) return;
			const mask = p.constraintWeekDays;
			const pickedDays = binaryToWeekDays(mask);
			const weekDays = [0, 1, 2, 3, 4, 5, 6].map((d) => pickedDays.includes(d));
			const scheduled = [...(p.steps || [])]
				.sort((a, b) => (a.position ?? 0) - (b.position ?? 0))
				.map((s) => ({ id: s.id, template: templateMap.byKey(s.campaignTemplateID) }));
			const interval = p.stepIntervalMinutes ?? DEFAULT_INTERVAL_MINUTES;
			const spread = p.spreadMinutes ?? 0;
			const delay = p.newMemberDelayMinutes ?? 0;
			storedMinutes = { interval, spread, delay };
			const allowDenyIDs = new Set(p.allowDenyIDs || []);
			const chosenLists = allAllowDeny.filter((e) => allowDenyIDs.has(e.id));
			formValues = {
				id: p.id,
				companyID: p.companyID || null,
				name: p.name,
				status: p.status || null,
				startedAt: p.startedAt || null,
				pausedAt: p.pausedAt || null,
				startMode: p.status === 'draft' ? 'draft' : p.status === 'scheduled' ? 'date' : 'now',
				startAtLocal:
					p.status === 'scheduled' && p.startAt ? toZoneLocal(p.startAt, browserTimeZone()) : '',
				endMode: p.endAt ? 'date' : 'never',
				endAtLocal: p.endAt ? toZoneLocal(p.endAt, browserTimeZone()) : '',
				endRounds: roundsLabel(1),
				orderMode: orderModes.byValue(p.orderMode || 'shuffle'),
				templateMixBy: templateMixes.byValue(p.templateMixBy || ''),
				// a stored program without a loop mode stops after the last template,
				// as the backend reads it. New programs start over by default.
				loopMode: loopModes.byValue(p.loopMode || 'complete'),
				interval: intervalLabel(interval),
				spread: spreadLabel(spread),
				newMemberDelay: delayLabel(delay),
				// a fixed zone that was never set is UTC on the server
				timeZone: p.timeZone || (zoneFixed(p) ? 'UTC' : browserTimeZone()),
				// a program saved without sending hours sends on every day at any hour
				weekDays: mask != null ? weekDays : [true, true, true, true, true, true, true],
				startTime: mask != null ? p.constraintStartTime || '09:00' : '00:00',
				endTime: mask != null ? p.constraintEndTime || '17:00' : '23:59',
				blackout: (p.blackoutDates || []).map((r) => ({
					from: r.from,
					to: r.to,
					yearly: !!r.yearly
				})),
				denyPage: p.denyPageID ? pageMap.byKey(p.denyPageID) || null : null,
				evasionPage: p.evasionPageID ? pageMap.byKey(p.evasionPageID) || null : null,
				filterMode: chosenLists.length ? (chosenLists[0].allowed ? 'allow' : 'deny') : 'none',
				allowDeny: chosenLists.map((e) => e.name),
				webhooks: (p.webhooks || []).map((wh) => ({
					name: webhookMap.byKey(wh.webhookID) || null,
					includeData: wh.webhookIncludeData || 'full',
					events: webhookEventsFromBinary(wh.webhookEvents ?? 0)
				})),
				isAnonymous: !!p.isAnonymous,
				saveSubmittedData: !p.isAnonymous && !!p.saveSubmittedData,
				anonymizeAt: p.anonymizeAt || null,
				dataAnonymizeAt: p.dataAnonymizeAt || null,
				dataAnonymizedAt: p.dataAnonymizedAt || null,
				reportEnabled: !!p.reportEnabled,
				reportInterval: p.reportInterval || 'monthly',
				reportPeriod: p.reportPeriod || 'last',
				steps: scheduled.length ? scheduled : [{ id: null, template: null }],
				recipientGroups: (p.recipientGroupIDs || [])
					.map((gid) => groupMap.byKey(gid))
					.filter(Boolean)
			};
			originalValues = JSON.parse(JSON.stringify(formValues));
			loadedProgram = { startAt: p.startAt || null, timeZone: p.timeZone || null };
			modalMode = 'update';
			resetModalState();
			// open the advanced options when the program uses them, like the campaign wizard
			showSecurityOptions = !!(formValues.denyPage || formValues.evasionPage || chosenLists.length);
			showAdvancedOptions = !!(
				formValues.webhooks.length ||
				showSecurityOptions ||
				formValues.reportEnabled ||
				formValues.saveSubmittedData
			);
			isModalVisible = true;
			focusFirstField();
		} catch (e) {
			if (seq !== openSeq) return;
			addToast('Failed to load program', 'Error');
			console.error('failed to load program', e);
		}
	};

	const closeModal = () => {
		isModalVisible = false;
		if (form) form.reset();
		resetModalState();
	};

	// template list editing

	const addStep = async () => {
		formValues.steps = [...formValues.steps, { id: null, template: null }];
		await tick();
		document.getElementById(`program-simulation-template-${formValues.steps.length - 1}`)?.focus();
	};

	const removeStep = (index) => {
		formValues.steps = formValues.steps.filter((_, i) => i !== index);
		if (formValues.steps.length === 0) {
			formValues.steps = [{ id: null, template: null }];
		}
	};

	const moveStep = (index, delta) => {
		const target = index + delta;
		if (target < 0 || target >= formValues.steps.length) return;
		const steps = [...formValues.steps];
		[steps[index], steps[target]] = [steps[target], steps[index]];
		formValues.steps = steps;
	};

	// templates picked in other rows are not offered again
	const templateOptionsFor = (index, steps, map) => {
		const taken = new Set(steps.filter((s, i) => i !== index && s.template).map((s) => s.template));
		return map.values().filter((v) => !taken.has(v));
	};

	// a wall clock time in the program zone as a browser local value for the
	// date and time field
	const zoneToBrowserLocal = (local, zone) => {
		const at = fromZoneLocal(local, zone);
		return at ? toZoneLocal(at, browserTimeZone()) : '';
	};

	// a start date defaults to the next sending day at the start of the sending
	// hours, both in the program zone
	const pickStartMode = (mode) => {
		formValues.startMode = mode;
		if (mode === 'date' && !formValues.startAtLocal) {
			const zone = formValues.timeZone || browserTimeZone();
			const now = toZoneLocal(new Date(), zone);
			const clock = formValues.startTime;
			const day = nextSendingDay(formValues, now.slice(0, 10), () => now.slice(11, 16) >= clock);
			if (day) formValues.startAtLocal = zoneToBrowserLocal(`${day}T${clock}`, zone);
		}
	};

	// the safe end for the first sends templates of a recipient, see safeEnd,
	// from the start or from now for a draft. Null while the schedule has an
	// error or no templates
	const fitFor = (v, beforeStart, sends) => {
		const interval = minutesOf(intervalList, intervalLabel, v.interval);
		const spread = spreadOf(v);
		if (interval == null || spread == null || spread > interval) return null;
		if (!v.weekDays.some(Boolean) || !(v.endTime > v.startTime)) return null;
		const from = startMoment(v, beforeStart) ?? new Date();
		return safeEnd(v, from, interval, spread, sends, loopModes.byKey(v.loopMode) === 'loop');
	};
	// how many templates a recipient gets in a number of rounds
	const roundSends = (v, rounds) => {
		const n = v.steps.filter((s) => s.template).length;
		return loopModes.byKey(v.loopMode) === 'loop' ? rounds * n : n;
	};
	// set when the end of the picked rounds cannot be worked out, the end is
	// then cleared so an older end is not saved
	let roundsEndUnknown = false;
	const syncRoundsEnd = (key) => {
		if (key === roundsEndKey) return;
		roundsEndKey = key;
		roundsEndUnknown = false;
		if (!key) return;
		const v = formValues;
		const sends = roundSends(v, roundsOf(v));
		const fit = fitFor(v, preStart, sends);
		if (!fit) {
			// a schedule with an error shows that error instead
			const interval = minutesOf(intervalList, intervalLabel, v.interval);
			const spread = spreadOf(v);
			const valid =
				sends > 0 &&
				interval != null &&
				spread != null &&
				spread <= interval &&
				v.weekDays.some(Boolean) &&
				v.endTime > v.startTime;
			if (valid) {
				roundsEndUnknown = true;
				if (formValues.endAtLocal) formValues.endAtLocal = '';
			}
			return;
		}
		const local = toZoneLocal(fit.end, browserTimeZone());
		if (local !== formValues.endAtLocal) formValues.endAtLocal = local;
	};
	const ROUNDS_END_UNKNOWN =
		'The end of these rounds is too far ahead to work out. Pick fewer rounds or a custom date.';
	// an end date picked by hand makes the end a custom date
	const onEndDate = (value) => {
		if (value === formValues.endAtLocal) return;
		formValues.endAtLocal = value;
		formValues.endMode = 'date';
	};
	// moves the end to the safe end the forecast found, a custom date that cut
	// off templates becomes one round
	const fitTheEnd = () => {
		const fit = forecast.fitEnd;
		if (!fit) return;
		if (forecast.fitRound && formValues.endMode === 'date') {
			formValues.endMode = 'rounds';
			formValues.endRounds = roundsLabel(1);
		}
		formValues.endAtLocal = toZoneLocal(fit, browserTimeZone());
	};

	// a custom date defaults to the safe end of one round, or a year after the
	// start when that cannot be worked out, at the end of the sending hours in
	// the program zone
	const pickEndMode = (mode) => {
		formValues.endMode = mode;
		if (mode === 'date' && !formValues.endAtLocal) {
			const fit = fitFor(formValues, preStart, roundSends(formValues, 1));
			if (fit) {
				formValues.endAtLocal = toZoneLocal(fit.end, browserTimeZone());
				return;
			}
			const zone = formValues.timeZone || browserTimeZone();
			const start =
				formValues.startMode === 'date' && formValues.startAtLocal
					? fromZoneLocal(formValues.startAtLocal, browserTimeZone())
					: null;
			const from = toZoneLocal(start || new Date(), zone);
			const [y, m, d] = from.slice(0, 10).split('-').map(Number);
			const day = new Date(Date.UTC(y + 1, m - 1, d)).toISOString().slice(0, 10);
			const clock = formValues.endTime;
			formValues.endAtLocal = zoneToBrowserLocal(`${day}T${clock}`, zone);
		}
	};

	// skip dates rows

	const addBlackout = async () => {
		const last = formValues.blackout[formValues.blackout.length - 1];
		formValues.blackout = [
			...formValues.blackout,
			{ from: last?.to || '', to: last?.to || '', yearly: false }
		];
		await tick();
		document.getElementById(`program-blackout-from-${formValues.blackout.length - 1}`)?.focus();
	};

	// a holiday is added as a yearly skip day, dated in the current year
	const holidayDay = (holiday) => `${new Date().getFullYear()}-${holiday.day}`;
	const hasHoliday = (blackout, holiday) =>
		blackout.some((r) => r.yearly && r.from === r.to && r.from.slice(5) === holiday.day);
	const addHoliday = (holiday) => {
		if (hasHoliday(formValues.blackout, holiday)) return;
		const day = holidayDay(holiday);
		formValues.blackout = [...formValues.blackout, { from: day, to: day, yearly: true }];
	};

	const removeBlackout = (index) => {
		formValues.blackout = formValues.blackout.filter((_, i) => i !== index);
	};

	// the end date follows the start date until it is set later
	const onBlackoutFrom = (index) => {
		const r = formValues.blackout[index];
		if (r.from && (!r.to || r.to < r.from)) {
			r.to = r.from;
			formValues.blackout = [...formValues.blackout];
		}
	};

	// webhooks

	const addWebhook = async () => {
		formValues.webhooks = [
			...formValues.webhooks,
			{ name: null, includeData: 'basic', events: [...webhookEventOptions] }
		];
		await tick();
		document.getElementById(`program-webhook-${formValues.webhooks.length - 1}`)?.focus();
	};

	const removeWebhook = (index) => {
		formValues.webhooks = formValues.webhooks.filter((_, i) => i !== index);
	};

	// at least one event stays selected
	const toggleWebhookEvent = (index, event) => {
		const wh = formValues.webhooks[index];
		if (wh.events.includes(event)) {
			if (wh.events.length > 1) wh.events = wh.events.filter((e) => e !== event);
		} else {
			wh.events = [...wh.events, event];
		}
		formValues.webhooks = [...formValues.webhooks];
	};

	// webhooks picked in other rows are not offered again
	const webhookOptionsFor = (index, webhooks, map) => {
		const taken = new Set(webhooks.filter((w, i) => i !== index && w.name).map((w) => w.name));
		return map.values().filter((v) => !taken.has(v));
	};

	// validation

	const collectErrors = () => {
		const e = {};
		if (!formValues.name || !formValues.name.trim()) {
			e.name = 'Enter a name';
		}
		if (formValues.recipientGroups.length === 0) {
			e.groups = 'Pick at least one group';
		} else if (formValues.recipientGroups.length > groupMax) {
			e.groups = `At most ${groupMax} groups`;
		}
		const known = new Set(templateMap.values());
		const picked = formValues.steps.filter((s) => s.template);
		if (picked.length === 0 && formValues.steps.length <= 1) {
			e.simulations = 'Pick a template';
		} else if (picked.length < formValues.steps.length) {
			e.simulations = 'Pick a template for each row or remove it';
		} else if (picked.some((s) => !known.has(s.template))) {
			e.simulations = 'Pick each template from the list';
		} else if (new Set(picked.map((s) => s.template)).size !== picked.length) {
			e.simulations = 'Each template can only be used once';
		} else if (formValues.steps.length > stepMax) {
			e.simulations = `At most ${stepMax} templates`;
		}
		if (!orderModes.byKey(formValues.orderMode)) {
			e.orderMode = 'Pick a template order from the list';
		}
		if (!templateMixes.keys().includes(formValues.templateMixBy)) {
			e.templateMixBy = 'Pick a field to mix templates by from the list';
		}
		if (!loopModes.byKey(formValues.loopMode)) {
			e.loopMode = 'Pick what happens after the last template';
		}
		if (preStart && formValues.startMode === 'date') {
			const at = fromZoneLocal(formValues.startAtLocal, browserTimeZone());
			// a stored start the user did not change is sent as it is, and the
			// server starts the program when that time has passed
			const startKept =
				!!originalValues &&
				originalValues.startMode === 'date' &&
				originalValues.startAtLocal === formValues.startAtLocal;
			if (!at) {
				e.startAt = 'Set the start date and time';
			} else if (!startKept && at.getTime() <= Date.now()) {
				e.startAt = 'The start must be in the future';
			}
		}
		if (
			formValues.endMode === 'rounds' &&
			loopsOf(formValues) &&
			!roundCounts.map(roundsLabel).includes(formValues.endRounds)
		) {
			e.endRounds = 'Pick the number of rounds from the list';
		} else if (formValues.endMode === 'rounds' && roundsEndUnknown) {
			e.endRounds = ROUNDS_END_UNKNOWN;
		}
		if (hasEnd(formValues) && !e.endRounds) {
			const end = fromZoneLocal(formValues.endAtLocal, browserTimeZone());
			const start = startMoment(formValues, preStart);
			if (!end) {
				e.endAt = 'Set the end date and time';
			} else if (endChanged(formValues) && end.getTime() <= Date.now()) {
				// like the server, only a new end must be in the future
				e.endAt = 'The end must be in the future';
			} else if (start && end.getTime() <= start.getTime()) {
				e.endAt = 'The end must be after the start';
			}
		}
		// the anonymize dates follow the start and end, as on the campaign wizard
		{
			const end = hasEnd(formValues)
				? fromZoneLocal(formValues.endAtLocal, browserTimeZone())
				: null;
			const start = startMoment(formValues, preStart);
			if (hasEnd(formValues) && formValues.anonymizeAt && end) {
				const at = new Date(formValues.anonymizeAt);
				if (at.getTime() < end.getTime())
					e.anonymizeAt = 'Anonymize all data must be after the end';
			}
			// a data date that has run is not checked again, a new one is
			const dataRan =
				!!formValues.dataAnonymizedAt &&
				sameMinute(formValues.dataAnonymizeAt, originalValues?.dataAnonymizeAt) &&
				new Date(formValues.dataAnonymizedAt) >= new Date(formValues.dataAnonymizeAt);
			if (formValues.dataAnonymizeAt && !dataRan) {
				const at = new Date(formValues.dataAnonymizeAt);
				if (end && at.getTime() < end.getTime()) {
					e.dataAnonymizeAt = 'Anonymize Data must be after the end';
				} else if (start && at.getTime() < start.getTime()) {
					e.dataAnonymizeAt = 'Anonymize Data must be after the start';
				}
			}
		}
		const interval = minutesOf(intervalList, intervalLabel, formValues.interval);
		const spread = minutesOf(
			withValue(spreadPresets, storedMinutes.spread),
			spreadLabel,
			formValues.spread
		);
		if (interval == null) {
			e.interval = 'Pick the time between templates';
		}
		if (spread == null) {
			e.spread = 'Pick a spread from the list';
		} else if (interval != null && spread > interval) {
			e.spread = SPREAD_TOO_LONG;
		}
		if (!formValues.weekDays.some(Boolean)) {
			e.days = 'Pick at least one day';
		}
		const timePattern = /^\d{2}:\d{2}$/;
		if (
			!timePattern.test(formValues.startTime || '') ||
			!timePattern.test(formValues.endTime || '')
		) {
			e.times = 'Set a start and an end time';
		} else if (formValues.endTime <= formValues.startTime) {
			e.times = 'The end time must be after the start time';
		}
		// the range rules are the same as the backend, see blackoutRangeError
		const badRange = formValues.blackout.findIndex(
			(r) => !r.from || !r.to || r.to < r.from || blackoutRangeError(r)
		);
		if (badRange >= 0) {
			const r = formValues.blackout[badRange];
			e.blackout =
				!r.from || !r.to
					? 'Set both dates or remove the row'
					: r.to < r.from
						? 'The last day must be on or after the first day'
						: blackoutRangeError(r);
			blackoutErrorIndex = badRange;
		} else if (formValues.blackout.length > blackoutMax) {
			e.blackout = `At most ${blackoutMax} skip date ranges`;
			blackoutErrorIndex = blackoutMax;
		} else if (formValues.blackout.length && !e.days && !sendDayLeft(formValues)) {
			e.blackout =
				'The skip dates and sending days leave no day to send on. Remove a skip range or add a sending day.';
			blackoutErrorIndex = 0;
		}
		// the server checks the sending hours against clock changes when they or
		// the zone change
		if (!e.days && !e.times && (windowChanged(formValues) || zoneChanged(formValues))) {
			const clash = clockClash(formValues);
			if (clash) {
				e.times = `${clash.time} does not exist or occurs twice on ${formatDate(dayDate(clash.day), 'UTC')} in this time zone because of a clock change. Pick other hours.`;
			}
		}
		if (formValues.filterMode !== 'none' && formValues.allowDeny.length === 0) {
			e.allowDeny = 'Pick at least one list or turn filtering off';
		} else if (formValues.allowDeny.length > allowDenyMax) {
			e.allowDeny = `At most ${allowDenyMax} lists`;
		}
		if ((formValues.filterMode !== 'none' || formValues.evasionPage) && !formValues.denyPage) {
			e.denyPage = 'Pick a deny page';
		} else if (formValues.denyPage && !pageMap.values().includes(formValues.denyPage)) {
			e.denyPage = 'Pick a page from the list';
		}
		if (minutesOf(delayList, delayLabel, formValues.newMemberDelay) == null) {
			e.newMemberDelay = 'Pick when new members get their first template';
		}
		const badHook = formValues.webhooks.findIndex((w) => !w.name || !webhookMap.byValue(w.name));
		if (badHook >= 0) {
			e.webhooks = 'Pick an endpoint for each webhook or remove it';
			webhookErrorIndex = badHook;
		} else if (formValues.webhooks.length > webhookMax) {
			e.webhooks = `At most ${webhookMax} webhooks`;
			webhookErrorIndex = webhookMax;
		}
		return e;
	};

	// the schedule checks that read many days are kept for the last form they
	// ran on, as the errors are checked again on each change
	let sendDayMemo = { key: '', ok: true };
	const sendDayLeft = (v) => {
		const key = JSON.stringify([v.weekDays, v.blackout, v.timeZone, new Date().toDateString()]);
		if (key !== sendDayMemo.key) sendDayMemo = { key, ok: hasSendDay(v) };
		return sendDayMemo.ok;
	};
	let clockMemo = { key: '', clash: null };
	const clockClash = (v) => {
		const key = JSON.stringify([
			v.weekDays,
			v.startTime,
			v.endTime,
			v.timeZone,
			new Date().toDateString()
		]);
		if (key !== clockMemo.key) clockMemo = { key, clash: windowClockProblem(v) };
		return clockMemo.clash;
	};
	// whether the zone differs from the one the edit wizard loaded, it is not
	// sent while fixed
	const zoneChanged = (v) =>
		!timeZoneLocked && (!originalValues || v.timeZone !== loadedProgram.timeZone);

	const errorsForStep = (step) => {
		const all = collectErrors();
		const out = {};
		for (const [k, v] of Object.entries(all)) {
			if (fieldStep[k] === step) out[k] = v;
		}
		return out;
	};

	// drop an error as soon as its field is fixed
	$: pruneErrors(formValues);

	const pruneErrors = () => {
		const shown = Object.keys(errors).filter((k) => errors[k]);
		if (!shown.length) return;
		const current = collectErrors();
		let changed = false;
		const next = {};
		for (const k of shown) {
			if (current[k]) {
				next[k] = current[k];
				if (current[k] !== errors[k]) changed = true;
			} else {
				changed = true;
			}
		}
		if (changed) errors = next;
	};

	const showErrors = async (stepErrors) => {
		errors = stepErrors;
		const first = Object.keys(stepErrors)[0];
		if (!first) return;
		// a field under the advanced options is opened before it gets focus
		if (advancedFields.includes(first)) showAdvancedOptions = true;
		if (securityFields.includes(first)) showSecurityOptions = true;
		await tick();
		const control = fieldControl[first];
		const target = typeof control === 'function' ? control() : control;
		const el = typeof target === 'string' ? document.getElementById(target) : target;
		if (el) {
			el.scrollIntoView({ block: 'center', behavior: 'smooth' });
			el.focus({ preventScroll: true });
		}
	};

	const scrollWizardTop = async () => {
		await tick();
		const body = document.getElementById('program-wizard')?.closest('.modal-body');
		if (body) body.scrollTop = 0;
	};

	const goToStep = (step) => {
		modalError = '';
		errors = {};
		currentStep = step;
		scrollWizardTop();
	};

	const nextStep = () => {
		const stepErrors = errorsForStep(currentStep);
		if (Object.keys(stepErrors).length) {
			showErrors(stepErrors);
			return;
		}
		goToStep(Math.min(currentStep + 1, wizardSteps.length));
	};

	const previousStep = () => {
		goToStep(Math.max(currentStep - 1, 1));
	};

	const canJumpTo = (index) => isEdit || index + 1 < currentStep;

	// whether the end differs from the one the edit wizard loaded
	const endChanged = (v) =>
		!originalValues ||
		hasEnd(v) !== hasEnd(originalValues) ||
		(hasEnd(v) && v.endAtLocal !== originalValues.endAtLocal);
	// whether the sending days and hours differ from the ones the edit wizard
	// loaded
	const windowChanged = (v) =>
		!originalValues ||
		JSON.stringify([v.weekDays, v.startTime, v.endTime]) !==
			JSON.stringify([originalValues.weekDays, originalValues.startTime, originalValues.endTime]);

	const buildPayload = () => {
		const steps = formValues.steps
			.filter((s) => s.template)
			.map((s, i) => ({
				id: s.id || null,
				campaignTemplateID: templateMap.byValue(s.template),
				position: i
			}));
		const payload = {
			name: formValues.name,
			// an update keeps the company of the program
			companyID: isEdit ? formValues.companyID || null : contextCompanyID || null,
			orderMode: orderModes.byKey(formValues.orderMode),
			// kept for the same order too, the server uses it only in random order
			templateMixBy: templateMixes.byKey(formValues.templateMixBy),
			loopMode: loopModes.byKey(formValues.loopMode),
			stepIntervalMinutes: intervalMinutes,
			spreadMinutes: spreadMinutes,
			newMemberDelayMinutes: minutesOf(delayList, delayLabel, formValues.newMemberDelay),
			// a fixed zone or one that did not change is left out, so the stored one
			// is kept and a rename does not check the sending hours again
			timeZone: zoneChanged(formValues) ? formValues.timeZone || browserTimeZone() : undefined,
			isAnonymous: formValues.isAnonymous,
			saveSubmittedData: formValues.isAnonymous ? false : !!formValues.saveSubmittedData,
			reportEnabled: !!formValues.reportEnabled,
			reportInterval: formValues.reportInterval,
			reportPeriod: formValues.reportPeriod,
			steps,
			recipientGroupIDs: formValues.recipientGroups.map((name) => groupMap.byValue(name)),
			blackoutDates: formValues.blackout.map((r) => ({
				from: r.from,
				to: r.to,
				yearly: !!r.yearly
			})),
			denyPageID: formValues.denyPage ? pageMap.byValue(formValues.denyPage) || null : null,
			evasionPageID: formValues.evasionPage
				? pageMap.byValue(formValues.evasionPage) || null
				: null,
			allowDenyIDs:
				formValues.filterMode !== 'none'
					? formValues.allowDeny
							.map(
								(name) =>
									allAllowDeny.find(
										(e) => e.name === name && e.allowed === (formValues.filterMode === 'allow')
									)?.id
							)
							.filter(Boolean)
					: [],
			webhooks: formValues.webhooks.map((wh) => ({
				webhookID: webhookMap.byValue(wh.name),
				webhookIncludeData: wh.includeData,
				webhookEvents: webhookEventsToBinary(wh.events)
			}))
		};
		// the start is only sent before the program has started
		if (preStart) {
			if (formValues.startMode === 'draft') {
				payload.draft = true;
			} else if (formValues.startMode === 'date') {
				payload.draft = false;
				// a start that was not changed keeps its stored seconds
				payload.startAt =
					isEdit &&
					loadedProgram.startAt &&
					originalValues?.startMode === 'date' &&
					originalValues.startAtLocal === formValues.startAtLocal
						? loadedProgram.startAt
						: fromZoneLocal(formValues.startAtLocal, browserTimeZone()).toISOString();
			} else {
				payload.draft = false;
				payload.startAt = null;
			}
		}
		// an end that was not changed is left out, so it keeps its stored seconds
		// and does not count as a new end
		if (!isEdit || endChanged(formValues)) {
			payload.endAt = hasEnd(formValues)
				? fromZoneLocal(formValues.endAtLocal, browserTimeZone()).toISOString()
				: null;
		}
		// an anonymize date is sent when it changed, so a data date that has
		// run is kept as it is. Anonymize All needs an end, without one the
		// date is cleared
		const anonymizeAt = hasEnd(formValues) ? isoOrNull(formValues.anonymizeAt) : null;
		if (!isEdit || !sameMinute(anonymizeAt, originalValues.anonymizeAt)) {
			payload.anonymizeAt = anonymizeAt;
		}
		const dataAnonymizeAt = isoOrNull(formValues.dataAnonymizeAt);
		if (!isEdit || !sameMinute(dataAnonymizeAt, originalValues.dataAnonymizeAt)) {
			payload.dataAnonymizeAt = dataAnonymizeAt;
		}
		// the sending days and hours are sent when they changed, so a program
		// saved without them keeps sending at any time
		if (!isEdit || windowChanged(formValues)) {
			payload.constraintWeekDays =
				weekDaysToBinary(formValues.weekDays.flatMap((on, d) => (on ? [d] : []))) ?? 0;
			payload.constraintStartTime = formValues.startTime;
			payload.constraintEndTime = formValues.endTime;
		}
		return payload;
	};

	const onSubmit = async () => {
		// enter in a field moves forward instead of saving early
		if (currentStep < wizardSteps.length) {
			nextStep();
			return;
		}
		for (let s = 1; s <= wizardSteps.length; s++) {
			const stepErrors = errorsForStep(s);
			if (Object.keys(stepErrors).length) {
				currentStep = s;
				modalError = '';
				showErrors(stepErrors);
				return;
			}
		}
		try {
			isSubmitting = true;
			modalError = '';
			const payload = buildPayload();
			const res = isEdit
				? await api.program.update(formValues.id, payload)
				: await api.program.create(payload);
			if (!res.success) {
				// a taken name is shown on the name field, on the step that has it
				const nameError = /^name: (.+)$/.exec(String(res.error ?? ''));
				if (nameError) {
					currentStep = fieldStep.name;
					showErrors({ name: capitalize(`name ${nameError[1]}`) });
					return;
				}
				// a schedule error of the server is shown on its field
				const fieldError = /^(\w+): (.+)$/.exec(String(res.error ?? ''));
				const field = fieldError ? serverFields[fieldError[1]] : null;
				if (field) {
					if (field === 'blackout') blackoutErrorIndex = 0;
					if (field === 'webhooks') webhookErrorIndex = 0;
					currentStep = fieldStep[field];
					showErrors({ [field]: capitalize(fieldError[2]) });
					return;
				}
				modalError = res.error;
				return;
			}
			addToast(
				!preStart
					? 'Program updated'
					: formValues.startMode === 'draft'
						? 'Program saved as a draft'
						: formValues.startMode === 'date'
							? `Program starts ${formatDateTime(payload.startAt)}`
							: isEdit
								? 'Program saved and started'
								: 'Program created and started',
				'Success'
			);
			dispatch('saved', { id: isEdit ? formValues.id : res.data?.id, created: !isEdit });
			closeModal();
		} catch (e) {
			addToast('Failed to save program', 'Error');
			console.error('failed to save program', e);
		} finally {
			isSubmitting = false;
		}
	};

	// forecast rows, shown on the schedule and review steps

	// a rough length for the forecast, in weeks or months
	const roughDuration = (minutes) => {
		const days = minutes / MINUTES_PER_DAY;
		if (days < 14) {
			return plural(Math.max(1, Math.round(days)), 'day', 'days');
		}
		if (days < 60) {
			return plural(Math.max(1, Math.round(days / 7)), 'week', 'weeks');
		}
		return plural(Math.round(days / 30), 'month', 'months');
	};

	// the minutes from the start until the last send window of one round
	// closes, so one monthly round of three templates is about two months.
	// Without the windows it is the time between the first and last template.
	const roundMinutes = (v, from, interval, spread, n) => {
		const windows =
			spread > interval ? [] : sendSchedule(v, from, interval, spread, Math.min(n, TIMELINE_MAX));
		if (windows.length === Math.min(n, TIMELINE_MAX)) {
			return Math.max(0, (windows[windows.length - 1].by - from.getTime()) / 60000);
		}
		return (n - 1) * interval + (spread || 0);
	};

	// the moment sending can start, null for a draft or a missing date
	// the end of the form, the earliest anonymize date
	$: endMoment = hasEnd(formValues)
		? fromZoneLocal(formValues.endAtLocal, browserTimeZone())
		: null;
	// a saved Anonymize All date that an update without an end removes
	const anonymizeDateDropped = (v) =>
		isEdit && v !== originalValues && !!originalValues?.anonymizeAt && !hasEnd(v);
	// a date field value as an ISO time, null when empty
	const isoOrNull = (v) => (v ? new Date(v).toISOString() : null);
	// two date values on the same minute, both empty counts as the same
	const sameMinute = (a, b) =>
		(!a && !b) ||
		(!!a &&
			!!b &&
			Math.floor(new Date(a).getTime() / 60000) === Math.floor(new Date(b).getTime() / 60000));
	const startMoment = (v, beforeStart) => {
		if (!beforeStart) return v.startedAt ? new Date(v.startedAt) : new Date();
		if (v.startMode === 'draft') return null;
		if (v.startMode === 'date') return fromZoneLocal(v.startAtLocal, browserTimeZone());
		return new Date();
	};

	// the start as text, for the forecast and the review
	const startText = (v, beforeStart) => {
		if (!beforeStart) {
			return v.startedAt ? `Started ${formatDateTime(v.startedAt)}` : 'Started';
		}
		if (v.startMode === 'draft') return 'Draft, nothing is sent until you start it';
		if (v.startMode === 'date') {
			const at = fromZoneLocal(v.startAtLocal, browserTimeZone());
			return at ? formatDateTime(at) : 'Not set';
		}
		return 'Now';
	};

	// the end as text, for the forecast and the review
	const endText = (v) => {
		if (!hasEnd(v)) return 'Never, runs until you close it';
		const at = fromZoneLocal(v.endAtLocal, browserTimeZone());
		if (!at) return 'Not set';
		if (v.endMode === 'rounds') {
			return `${formatDateTime(at)}, after ${roundsLabel(roundsOf(v)).toLowerCase()}`;
		}
		return formatDateTime(at);
	};

	// a time span in hours under a day, else as roughDuration
	const gapText = (ms) => {
		const minutes = ms / 60000;
		if (minutes < MINUTES_PER_DAY) {
			return plural(Math.max(1, Math.round(minutes / MINUTES_PER_HOUR)), 'hour', 'hours');
		}
		return roughDuration(minutes);
	};
	// an hour of slack for a time change between the last send and the end
	const HOUR_MS = 3600000;
	const DAY_MS = 24 * HOUR_MS;
	// the most send windows the timeline draws
	const TIMELINE_MAX = 400;

	// what the end leaves of the schedule: a note on the time the last template
	// gets, warnings when the end cuts templates off or leaves the last one
	// less time than the others get, and the safe end to fit it to. fitRound
	// tells the fit is one round, for an end that cut off templates. After the
	// start the people in the program are part way through, so the check is for
	// a recipient starting now and says nothing of templates cut off.
	const endCheck = (v, from, end, interval, spread, loops, n, count, beforeStart) => {
		const out = { endNote: '', warnings: [], fitEnd: null, fitRound: false, lastBy: null };
		// after the start without a loop, the last template a new recipient gets
		// before the end is not the last of the program when templates are cut
		const checkGap = beforeStart || loops || count >= n;
		const got = count > 0 && checkGap ? safeEnd(v, from, interval, spread, count, loops) : null;
		// a last send window that closes after the end does not count as sent
		const fits = got && end.getTime() < got.by ? count - 1 : count;
		if (count < n && beforeStart) {
			out.warnings.push(
				fits === 0
					? 'No template is sure to go out before the end.'
					: `Only ${fits} of ${n} templates are sure to go out before the end.`
			);
		}
		if (got) {
			out.lastBy = got.by;
			const gap = end.getTime() - got.by;
			// the time the safe end gives the last send, at most the interval
			const needed = Math.min(interval * 60000, got.end.getTime() - got.by);
			if (gap + HOUR_MS < needed) {
				out.warnings.push(
					gap > 0
						? `The last template goes out about ${gapText(gap)} before the end. Its links stop working when the program closes.`
						: 'The last template may still be going out when the program ends. Its links stop working when the program closes.'
				);
			} else if (got.overlap) {
				// the spread lets the last template and the next one go out on the same
				// days, so no end fits both
				out.warnings.push(
					'With Start over and this spread, the last template can go out as late as the next one could start, so some recipients may not get it before the end. Pick Stop after the last template or a shorter spread.'
				);
			} else if (got.capped && gap < Math.min(DAY_MS, (interval * 60000) / 2)) {
				// starting over, the next template goes out right after the last one
				out.warnings.push(
					'The last template has less than a day before its links stop, as the next template would go out after that. Pick Stop after the last template or a shorter spread to give it more time.'
				);
			} else {
				// starting over, the end of the rounds is where the next round would start
				const why = loops && v.endMode === 'rounds' ? ', as the next round would start then' : '';
				out.endNote = `Recipients get ${gapText(gap)} to respond to the last template${why}.`;
			}
		}
		if (out.warnings.length) {
			// a custom end that cut off templates fits one round, else it keeps the
			// templates it has and gives the last one its time
			const rounds = v.endMode === 'rounds' ? roundsOf(v) : 1;
			const target =
				count < n || v.endMode !== 'date'
					? safeEnd(v, from, interval, spread, loops ? rounds * n : n, loops)
					: got;
			if (target && Math.abs(target.end.getTime() - end.getTime()) > 60000) {
				out.fitEnd = target.end;
				out.fitRound = count < n;
			}
		}
		return out;
	};

	// the timeline of the forecast from the start until the end, or a year
	// without one: a bar per send window, the time to respond after the last
	// send and the end. Windows after the end show as cut off. Positions are in
	// percent of the span.
	const timelineOf = (v, from, end, interval, spread, shown, sent, lastBy, fitEnd) => {
		const zone = v.timeZone || browserTimeZone();
		const windows = sendSchedule(v, from, interval, spread, Math.min(shown, TIMELINE_MAX));
		if (!windows.length) return null;
		const t0 = from.getTime();
		let t1 = end ? end.getTime() : t0 + 365 * DAY_MS;
		if (end && fitEnd) t1 = Math.max(t1, fitEnd.getTime());
		if (end && sent < windows.length) t1 = Math.max(t1, windows[windows.length - 1].by);
		const span = t1 - t0;
		if (!(span > 0)) return null;
		const pct = (t) => Math.min(100, Math.max(0, ((t - t0) / span) * 100));
		const bars = [];
		for (const [i, w] of windows.entries()) {
			const open = fromZoneLocal(`${w.first}T${v.startTime}`, zone);
			const at = Math.max(w.from, open ? open.getTime() : w.from);
			if (!end && at >= t1) break;
			bars.push({ left: pct(at), width: pct(w.by) - pct(at), cut: !!end && i >= sent });
		}
		const out = { bars, response: null, end: null, start: formatDate(from), stop: '', summary: '' };
		const kept = bars.filter((b) => !b.cut).length;
		if (end) {
			out.end = pct(end.getTime());
			out.stop = formatDate(end);
			if (lastBy != null && end.getTime() > lastBy) {
				out.response = { left: pct(lastBy), width: pct(end.getTime()) - pct(lastBy) };
			}
			const cut = bars.length - kept;
			out.summary = `Timeline from ${out.start} to ${formatDateTime(end)}: ${plural(kept, 'template goes', 'templates go')} out${lastBy == null ? '' : end.getTime() < lastBy ? ', the last may be cut off at the end' : `, the last by ${formatDate(lastBy)}`}${cut ? `, ${cut} more would go out after the end` : ''}.`;
		} else {
			out.stop = formatDate(t1);
			out.summary = `Timeline of the first 12 months, ${out.start} to ${out.stop}: ${plural(kept, 'template goes', 'templates go')} out, the program has no end.`;
		}
		return out;
	};

	// the minutes of the picked spread, also when it is longer than the interval
	const spreadOf = (v) =>
		minutesOf(withValue(spreadPresets, storedMinutes.spread), spreadLabel, v.spread);

	const ordinal = (n) => ['1st', '2nd', '3rd'][n - 1] || `${n}th`;
	const windowText = (w) =>
		w.first === w.last
			? formatDate(dayDate(w.first), 'UTC')
			: `${formatDate(dayDate(w.first), 'UTC')} to ${formatDate(dayDate(w.last), 'UTC')}`;

	const buildForecast = (v, people, beforeStart) => {
		const n = v.steps.filter((s) => s.template).length;
		if (n === 0) {
			return { ...noForecast };
		}
		const interval = minutesOf(intervalList, intervalLabel, v.interval);
		const spread = spreadOf(v) ?? 0;
		const loops = loopModes.byKey(v.loopMode) === 'loop';
		const start = startMoment(v, beforeStart);
		const end = hasEnd(v) ? fromZoneLocal(v.endAtLocal, browserTimeZone()) : null;
		let check = { endNote: '', warnings: [], fitEnd: null, fitRound: false, lastBy: null };
		let timeline = null;
		const rows = [
			{
				label: 'Recipients',
				text: people != null ? String(people) : audienceLoading ? 'Counting...' : 'Pick a group'
			},
			{ label: 'Start', text: startText(v, beforeStart) }
		];
		// when each recipient gets their 1st and 2nd template
		// no windows while the spread is longer than the interval, that is an error
		const windows =
			beforeStart && start && !(interval != null && spread > interval)
				? sendWindows(v, start, interval, spread, Math.min(2, loops ? 2 : n), end)
				: [];
		windows.forEach((w, i) =>
			rows.push({ label: `${ordinal(i + 1)} template`, text: windowText(w) })
		);
		const windowNote =
			windows.length && spread > 0 ? 'Each send is at a random time in its window.' : '';
		// the cadence, the yearly count and the skip dates, long lists stay in the form
		let note = '';
		if (interval != null) {
			rows.push({ label: 'How often', text: `A new template ${describeInterval(interval)}` });
			// templates per recipient over 12 months, or until the end when that is
			// set. A started program counts from now.
			const from = beforeStart ? (start ?? new Date()) : new Date();
			const until = end ?? new Date(from.getTime() + 365 * MINUTES_PER_DAY * 60000);
			// without a loop the count stops when every template is sent
			const count = countSends(v, from, until, interval, spread, loops ? Infinity : n);
			if (end && !(spread > interval)) {
				check = endCheck(v, from, end, interval, spread, loops, n, count, beforeStart);
			}
			// the last send window closes after the end, so that template may be
			// cut off and only the ones before it count
			const lastCut = check.lastBy != null && end != null && end.getTime() < check.lastBy;
			const fits = lastCut ? count - 1 : count;
			rows.push({
				label: 'Per recipient',
				text: `About ${plural(fits, 'template', 'templates')} ${end ? 'until the end' : 'a year'}${beforeStart ? '' : ' for a recipient starting now'}`
			});
			if (!(spread > interval)) {
				if (check.lastBy != null) {
					rows.push({
						label: 'Last template',
						text: lastCut
							? `May be cut off at the end, its window runs to ${formatDate(check.lastBy)}`
							: `By ${formatDate(check.lastBy)}`
					});
				}
				// the timeline also shows the templates an end cuts off, before the start
				const shown = end && count < n && beforeStart ? n : count;
				timeline = timelineOf(
					v,
					from,
					end,
					interval,
					spread,
					shown,
					count,
					check.lastBy,
					check.fitEnd
				);
				// a draft has no start, so its forecast is from today
				if (timeline && beforeStart && !start) {
					timeline.start = `${timeline.start}, if started today`;
				}
			}
			// the length of one round, left out when the end cuts the round as the
			// warnings say how many go out
			if (!end || count >= n) {
				const all =
					n === 1
						? 'The template takes'
						: n === 2
							? 'Both templates take'
							: `All ${n} templates take`;
				const then = !end
					? loops
						? 'then it starts over'
						: 'then sending stops'
					: loops && count > n
						? 'then it starts over'
						: 'then the program ends';
				note = `${all} about ${roughDuration(roundMinutes(v, from, interval, spread, n))}, ${then}.`;
			}
		}
		const skips = v.blackout.filter((r) => r.from && r.to).length;
		if (skips) {
			rows.push({ label: 'Skip dates', text: plural(skips, 'date range', 'date ranges') });
		}
		rows.push({ label: 'Ends', text: hasEnd(v) ? endText(v) : 'Never' });
		return { rows, note, windowNote, timeline, ...check };
	};

	// how many lines of a list the review shows
	const REVIEW_LIST_LIMIT = 5;
	// the forecast shows on the schedule and review steps and is built again
	// only when a field it reads changes, so typing a name or picking a
	// webhook does not redo the schedule maths
	const noForecast = {
		rows: [],
		note: '',
		windowNote: '',
		timeline: null,
		endNote: '',
		warnings: [],
		fitEnd: null,
		fitRound: false,
		lastBy: null
	};
	let forecast = noForecast;
	let forecastKey = '';
	const forecastKeyOf = (v, people, beforeStart, counting, stored, clock24) =>
		JSON.stringify([
			v.steps.filter((s) => s.template).length,
			v.interval,
			v.spread,
			stored.interval,
			stored.spread,
			v.loopMode,
			v.weekDays,
			v.blackout,
			v.startTime,
			v.endTime,
			v.timeZone,
			v.startMode,
			v.startAtLocal,
			v.endMode,
			v.endAtLocal,
			v.startedAt,
			people,
			beforeStart,
			counting,
			clock24
		]);
	$: updateForecast(
		currentStep >= 3
			? forecastKeyOf(
					formValues,
					audienceCount,
					preStart,
					audienceLoading,
					storedMinutes,
					$timeFormat
				)
			: ''
	);
	const updateForecast = (key) => {
		if (key === forecastKey) return;
		forecastKey = key;
		forecast = key ? buildForecast(formValues, audienceCount, preStart) : noForecast;
	};
	// review rows per section; text is what is shown and compared in edit mode.
	// Rows marked blackbox only show in blackbox mode.
	const reviewSections = (v) => {
		const sims = v.steps.filter((s) => s.template).map((s) => s.template);
		const zone = zoneNote(v.timeZone);
		const webhooks = v.webhooks.map(
			(wh) =>
				`${wh.name || 'Not set'}, ${labelOf(webhookDataLevelOptions, wh.includeData).toLowerCase()} data, ${
					wh.events.length === webhookEventOptions.length
						? 'all events'
						: plural(wh.events.length, 'event', 'events')
				}`
		);
		const skips = v.blackout.map((r) => describeBlackout(r));
		const row = (label, text, extra = {}) => ({ label, text, ...extra });
		return [
			{
				step: 1,
				title: 'Setup',
				rows: [
					row('Name', v.name || 'Not set'),
					row('Recipient groups', v.recipientGroups.join(', ') || 'None selected', {
						groups: true
					}),
					row('Anonymous', v.isAnonymous ? 'Yes, totals only' : 'No')
				]
			},
			{
				step: 2,
				title: 'Templates',
				rows: [
					row(`Templates (${sims.length})`, sims.join('\n') || 'None', {
						list: sims,
						ordered: true,
						key: 'Templates'
					}),
					...(sims.length >= 2 ? [row('Template order', v.orderMode)] : []),
					...(sims.length >= 2 && orderModes.byKey(v.orderMode) === 'shuffle'
						? [row('Balance per', v.templateMixBy || 'None')]
						: []),
					row('After the last template', v.loopMode)
				]
			},
			{
				step: 3,
				title: 'Schedule',
				rows: [
					row('Start', startText(v, preStart)),
					row('End', endText(v)),
					row('Time between templates', v.interval || 'Not set'),
					row('Spread sends over', v.spread || 'Not set'),
					row('Sending days', capitalize(describeDays(v.weekDays))),
					row(
						'Sending hours',
						`${formatTimeConstraint(v.startTime, $timeFormat)} to ${formatTimeConstraint(v.endTime, $timeFormat)}${zone ? `, ${zone}` : ''}`
					),
					row('Skip dates', skips.join('\n') || 'None', { list: skips }),
					row('First template to new members', v.newMemberDelay),
					...(hasEnd(v) && v.anonymizeAt
						? [row('Anonymize all data at', formatDateTime(v.anonymizeAt), { at: v.anonymizeAt })]
						: anonymizeDateDropped(v)
							? [row('Anonymize all data at', 'Removed')]
							: []),
					...(v.dataAnonymizeAt
						? [
								row('Anonymize data at', formatDateTime(v.dataAnonymizeAt), {
									at: v.dataAnonymizeAt
								})
							]
						: [])
				]
			},
			{
				step: 1,
				title: 'Advanced Options',
				rows: [
					row('Data saving', !v.isAnonymous && v.saveSubmittedData ? 'Enabled' : 'Disabled'),
					row('Automated report', reportText(v), { reportCheck: true }),
					row('Webhooks', webhooks.join('\n') || 'None', { list: webhooks }),
					row(
						'Filtering',
						v.filterMode === 'none'
							? 'Off'
							: `${v.filterMode === 'allow' ? 'Only allow' : 'Block'}: ${v.allowDeny.join(', ') || 'no lists'}`,
						{ blackbox: true }
					),
					row('Deny page', v.denyPage || 'None', { blackbox: true }),
					row('Evasion page', v.evasionPage || 'None', { blackbox: true })
				]
			}
		];
	};

	// the values the edit wizard opened with, to mark what changed
	let originalValues = null;
	// the stored start and zone of the program being edited
	let loadedProgram = { startAt: null, timeZone: null };
	// the hours follow the 12h or 24h choice
	$: sections = ($timeFormat, reviewSections(formValues));
	$: originalRows = (() => {
		if (!isEdit || !originalValues) return null;
		const map = {};
		for (const sec of reviewSections(originalValues)) {
			for (const r of sec.rows) map[`${sec.title}:${r.key || r.label}`] = r;
		}
		return map;
	})();
	const rowChange = (title, r, original) => {
		if (!original) return null;
		const before = original[`${title}:${r.key || r.label}`];
		if (before && before.text === r.text) return null;
		return { old: before && !before.list && before.text.length <= 40 ? before.text : '' };
	};
	$: audienceLine =
		formValues.recipientGroups.length === 0
			? ''
			: audienceLoading
				? 'Counting...'
				: audienceCount != null
					? plural(audienceCount, 'recipient')
					: '';
</script>

<Modal
	headerText={modalText}
	bind:visible={isModalVisible}
	onClose={closeModal}
	{isSubmitting}
	noAutoFocus
>
	<div
		class="w-full sm:w-[34rem] md:w-[40rem] lg:w-[56rem] max-w-full grid grid-rows-[auto_1fr] sm:h-[min(52rem,calc(100dvh-8rem))]"
	>
		<nav
			id="program-wizard"
			aria-label="Wizard progress"
			class="sticky top-0 z-20 -mx-4 sm:-mx-8 px-4 sm:px-8 pt-4 pb-3 bg-white dark:bg-gray-800 border-b border-gray-200 dark:border-gray-700 transition-colors duration-200"
		>
			<ol class="flex items-start">
				{#each wizardSteps as step, index}
					{@const stepNumber = index + 1}
					{@const isDone = currentStep > stepNumber}
					{@const isCurrent = currentStep === stepNumber}
					<li
						class="relative flex-1 flex flex-col items-center min-w-0"
						aria-current={isCurrent ? 'step' : undefined}
					>
						{#if index < wizardSteps.length - 1}
							<span
								aria-hidden="true"
								class="absolute top-4 left-[calc(50%+1.25rem)] right-[calc(-50%+1.25rem)] h-0.5 -translate-y-1/2 rounded-full transition-colors duration-200 {isDone
									? 'bg-cta-blue dark:bg-highlight-blue'
									: 'bg-gray-200 dark:bg-gray-600'}"
							/>
						{/if}
						<!-- earlier steps, and every step when editing, can be opened -->
						<button
							type="button"
							class="pc-focus relative z-10 w-8 h-8 rounded-full flex items-center justify-center text-sm font-semibold transition-colors duration-200 disabled:cursor-default {isDone
								? 'bg-blue-100 text-cta-blue dark:bg-highlight-blue/20 dark:text-blue-200'
								: isCurrent
									? 'bg-cta-blue text-white ring-4 ring-blue-100 dark:bg-highlight-blue dark:ring-highlight-blue/25'
									: 'bg-white text-gray-500 border-2 border-gray-300 dark:bg-gray-800 dark:text-gray-400 dark:border-gray-600'} {!isCurrent &&
							canJumpTo(index)
								? 'hover:ring-4 hover:ring-blue-100 dark:hover:ring-highlight-blue/25'
								: ''}"
							disabled={isCurrent || !canJumpTo(index)}
							on:click={() => goToStep(stepNumber)}
						>
							{#if isDone}
								<svg
									xmlns="http://www.w3.org/2000/svg"
									class="h-4 w-4"
									viewBox="0 0 20 20"
									fill="currentColor"
									aria-hidden="true"
								>
									<path
										fill-rule="evenodd"
										d="M16.707 5.293a1 1 0 010 1.414l-8 8a1 1 0 01-1.414 0l-4-4a1 1 0 011.414-1.414L8 12.586l7.293-7.293a1 1 0 011.414 0z"
										clip-rule="evenodd"
									/>
								</svg>
								<span class="sr-only">{stepNumber} of {wizardSteps.length}, {step.name}, done</span>
							{:else}
								<span aria-hidden="true">{stepNumber}</span>
								<span class="sr-only">{stepNumber} of {wizardSteps.length}, {step.name}</span>
							{/if}
						</button>
						<span
							class="mt-1.5 text-xs sm:text-sm text-center whitespace-nowrap transition-colors duration-200 {isCurrent
								? 'font-semibold text-cta-blue dark:text-blue-300'
								: isDone
									? 'text-slate-600 dark:text-gray-300'
									: 'text-gray-500 dark:text-gray-400'}"
							class:hidden={!isCurrent}
							class:sm:block={!isCurrent}
							aria-hidden="true"
						>
							{step.name}
						</span>
					</li>
				{/each}
			</ol>
		</nav>
		<FormGrid on:submit={onSubmit} bind:bindTo={form} {isSubmitting} novalidate>
			{#if isEdit && missingItems.length}
				<Note boxed id="program-missing-items" class="w-full mt-4 mb-2">
					Some saved settings could not be found: {missingItems.join(', ')}. They are marked "not
					found" and are kept unless you change them.
				</Note>
			{/if}
			{#if currentStep === 1}
				<FormColumns id={'step-1'}>
					<div class="w-full max-w-2xl min-w-0 flex flex-col items-start">
						<div class="w-full max-w-lg">
							<TextField
								id="program-name"
								width="full"
								minLength={1}
								maxLength={64}
								error={!!errors.name}
								ariaDescribedBy={errors.name ? errorId('name') : null}
								bind:value={formValues.name}
							>
								Name
							</TextField>
							{#if errors.name}
								<p id={errorId('name')} class={errorClass}>{errors.name}</p>
							{/if}
							<TextFieldMultiSelect
								id="program-groups"
								size="full"
								error={!!errors.groups}
								ariaDescribedBy={errors.groups ? errorId('groups') : null}
								toolTipText={isEdit
									? 'Removing a group stops sending to its members. Their results are kept.'
									: 'Everyone in these groups is enrolled, including recipients added later.'}
								bind:value={formValues.recipientGroups}
								options={groupMap.values()}>Recipient Groups</TextFieldMultiSelect
							>
							{#if errors.groups}
								<p id={errorId('groups')} class={errorClass}>{errors.groups}</p>
							{/if}
							{#if audienceLine}
								<p id="program-audience" class={noteClass} aria-live="polite">{audienceLine}</p>
							{/if}
							{#if dependenciesLoaded && groupMap.values().length === 0}
								<p class={noteClass}>
									No groups yet. <a
										href="/recipient/group"
										target="_blank"
										rel="noopener noreferrer"
										class={linkClass}>Create one</a
									>
								</p>
							{/if}
						</div>

						<div class="w-full mt-4">
							<SelectSquare
								disabled={anonymousLocked}
								toolTipText="Reports show totals only. Nobody can see who clicked. Cannot be changed after the program starts."
								label="Anonymous"
								options={anonymousOptions}
								bind:value={formValues.isAnonymous}
							/>
							{#if formValues.isAnonymous && audienceCount != null && audienceCount > 0 && audienceCount < ANON_SMALL_AUDIENCE}
								<p id="program-anonymous-small" class="{noteClass} mb-4">
									With {plural(audienceCount, 'recipient')}, most results will show as Hidden.
								</p>
							{/if}
							{#if anonymousLocked}
								<p class="flex items-start gap-1.5 mb-4 text-sm text-slate-600 dark:text-gray-300">
									<svg
										xmlns="http://www.w3.org/2000/svg"
										class="h-4 w-4 mt-0.5 flex-shrink-0"
										fill="none"
										viewBox="0 0 24 24"
										stroke="currentColor"
										aria-hidden="true"
									>
										<path
											stroke-linecap="round"
											stroke-linejoin="round"
											stroke-width="2"
											d="M12 15v2m-6 4h12a2 2 0 002-2v-6a2 2 0 00-2-2H6a2 2 0 00-2 2v6a2 2 0 002 2zm10-10V7a4 4 0 00-8 0v4h8z"
										/>
									</svg>
									Anonymous mode is fixed once the program has started.
								</p>
							{/if}
						</div>

						{#if !showAdvancedOptions}
							<div class="mt-4">
								<button
									id="program-show-advanced"
									type="button"
									class="pc-focus inline-flex items-center gap-1 rounded-sm text-sm font-medium text-cta-blue dark:text-highlight-blue hover:underline"
									aria-expanded={false}
									on:click={() => (showAdvancedOptions = true)}
								>
									Show advanced options
									<svg
										class="w-4 h-4"
										fill="none"
										stroke="currentColor"
										viewBox="0 0 24 24"
										aria-hidden="true"
										><path
											stroke-linecap="round"
											stroke-linejoin="round"
											stroke-width="2"
											d="M19 9l-7 7-7-7"
										/></svg
									>
								</button>
							</div>
						{:else}
							<div id="program-save-data" class="w-full pt-4">
								<SelectSquare
									disabled={formValues.isAnonymous}
									toolTipText="Consider privacy when saving data."
									label="Save submitted data?"
									options={saveSubmittedDataOptions}
									bind:value={formValues.saveSubmittedData}
								/>
								{#if errors.saveSubmittedData}
									<p id={errorId('saveSubmittedData')} class={errorClass}>
										{errors.saveSubmittedData}
									</p>
								{/if}
								{#if formValues.isAnonymous}
									<div
										class="mt-2 mb-4 bg-blue-50 dark:bg-blue-900/30 p-3 rounded-md text-sm text-blue-700 dark:text-blue-200 transition-colors duration-200"
									>
										<strong>Data collection disabled.</strong><br /> Submitted data is never stored for
										anonymous programs.
									</div>
								{/if}
							</div>
							<div id="program-report" class="w-full">
								<SelectSquare
									toolTipText="Email the program report PDF to the report recipients of the company, the same recipients a campaign report goes to. Needs PDF reports and report delivery to be set up."
									label="Automated Report"
									options={reportEnabledOptions}
									bind:value={formValues.reportEnabled}
								/>
								{#if formValues.reportEnabled}
									<SelectSquare
										toolTipText="How often the report is sent. It is sent when a calendar month, quarter or year ends, in the program time zone, and 3 days later for an anonymous program."
										label="Send Every"
										options={reportIntervalOptions}
										bind:value={formValues.reportInterval}
									/>
									<SelectSquare
										toolTipText="The period the report covers. Periods are whole calendar months, so the rules of an anonymous program hold."
										label="Report Covers"
										options={reportPeriodOptions}
										bind:value={formValues.reportPeriod}
									/>
									{#if !(isEdit && originalValues?.reportEnabled && originalValues?.reportInterval === formValues.reportInterval)}
										<p class={noteClass}>
											<!-- the backend counts from the period the program starts in, or the current one once it runs -->
											The first report is sent when {preStart ? 'the' : 'the current'}
											{formValues.reportInterval === 'monthly'
												? 'month'
												: formValues.reportInterval === 'quarterly'
													? 'quarter'
													: 'year'}{preStart ? ' the program starts in' : ''} ends{formValues.isAnonymous
												? ', 3 days later for an anonymous program, once the last month counts'
												: ''}.
										</p>
									{/if}
									{#if reportDeliveryBadge}
										<!-- the same status row as the report inbox on the campaign page -->
										<div
											id="program-report-delivery"
											class="mt-3 flex items-center justify-between gap-x-4 max-w-md text-sm"
										>
											<span class="text-gray-600 dark:text-gray-400">Report Delivery</span>
											<span class="flex shrink-0 items-center gap-x-3">
												<StatusBadge tone={reportDeliveryBadge.tone} title={reportDeliveryBadge.tip}
													>{reportDeliveryBadge.label}</StatusBadge
												>
												<a
													href={reportDeliveryHref(reportDelivery, reportDelivery.companyID)}
													target="_blank"
													rel="noopener"
													class="pc-focus rounded-sm text-xs text-cta-blue dark:text-highlight-blue hover:underline"
													>Settings</a
												>
											</span>
										</div>
									{/if}
								{/if}
							</div>
							<div id="program-webhooks" class="w-full mb-6 pt-4">
								<FieldLabel
									optional
									toolTipText="Send program events, such as clicks and reports, to another system. Each webhook can have its own data level and event filters."
									>Webhooks</FieldLabel
								>
								<div class="space-y-3 max-w-lg">
									{#each formValues.webhooks as webhook, index}
										{@const bad = !!errors.webhooks && index === webhookErrorIndex}
										<div
											class="flex flex-col gap-3 p-4 bg-gradient-to-br from-gray-50 to-gray-100 dark:from-gray-800/50 dark:to-gray-800/30 rounded-lg border shadow-sm hover:shadow-md transition-all duration-200 {bad
												? 'border-red-500 dark:border-red-400'
												: 'border-gray-300 dark:border-gray-600/50'}"
										>
											<div class="flex gap-2 items-start">
												<div class="flex-1 min-w-0">
													<TextFieldSelect
														id={`program-webhook-${index}`}
														size="full"
														optional
														error={bad}
														ariaDescribedBy={bad ? errorId('webhooks') : null}
														bind:value={webhook.name}
														options={webhookOptionsFor(index, formValues.webhooks, webhookMap)}
													>
														Endpoint
													</TextFieldSelect>
													{#if bad}
														<p id={errorId('webhooks')} class={errorClass}>{errors.webhooks}</p>
													{/if}
												</div>
												<div class="flex items-end pb-4 self-end">
													<button
														type="button"
														class={rowButtonClass}
														on:click={() => removeWebhook(index)}
														title="Remove this webhook"
														aria-label="Remove webhook"
													>
														<img class="w-4 flex-shrink-0" src="/delete2.svg" alt="" />
													</button>
												</div>
											</div>

											<div>
												<SelectSquare
													bind:value={webhook.includeData}
													options={webhookDataLevelOptions}
													label="Data Level"
												/>
											</div>

											<div class="pt-1">
												<div class="flex items-center gap-2 mb-2">
													<p class="text-xs font-semibold text-gray-700 dark:text-gray-300">
														Events
													</p>
													<span
														class="px-2 py-0.5 bg-blue-100 dark:bg-blue-900/40 text-blue-700 dark:text-blue-300 rounded-full text-xs font-medium"
													>
														{webhook.events.length === webhookEventOptions.length
															? 'All'
															: webhook.events.length} / {webhookEventOptions.length}
													</span>
												</div>
												<div
													class="flex flex-row flex-wrap gap-1.5 max-h-28 overflow-y-auto p-2 bg-white/50 dark:bg-gray-900/30 rounded border border-gray-200 dark:border-gray-700/50"
												>
													{#each webhookEventDisplayOptions as eventOption}
														{@const isSelected = webhook.events.includes(eventOption.value)}
														<button
															type="button"
															aria-pressed={isSelected}
															on:click={() => toggleWebhookEvent(index, eventOption.value)}
															class="pc-focus px-2.5 py-1 rounded-md text-xs font-medium transition-colors duration-200 {isSelected
																? webhookEventOnClass
																: webhookEventOffClass}"
														>
															{eventOption.label}
														</button>
													{/each}
												</div>
											</div>
										</div>
									{/each}
									{#if dependenciesLoaded && webhookMap.values().length === 0}
										<p class={noteClass}>
											No webhooks yet. <a
												href="/webhook"
												target="_blank"
												rel="noopener noreferrer"
												class={linkClass}>Create one</a
											>
										</p>
									{/if}
									<button
										id="program-add-webhook"
										type="button"
										class={addButtonClass}
										disabled={formValues.webhooks.length >=
											Math.min(webhookMax, webhookMap.values().length)}
										on:click={addWebhook}
									>
										+ Add Webhook
									</button>
								</div>
							</div>

							<ConditionalDisplay show="blackbox">
								<div class="w-full mb-6">
									<SelectSquare
										label="Security Configuration"
										options={securityOptions}
										bind:value={showSecurityOptions}
										onChange={() => {
											if (!showSecurityOptions) {
												formValues.denyPage = null;
												formValues.evasionPage = null;
												formValues.filterMode = 'none';
												formValues.allowDeny = [];
											}
										}}
									/>
								</div>

								{#if showSecurityOptions}
									<div class="w-full max-w-lg mb-6">
										<TextFieldSelect
											id="program-deny-page"
											size="full"
											bind:value={formValues.denyPage}
											optional
											error={!!errors.denyPage}
											ariaDescribedBy={errors.denyPage ? errorId('denyPage') : null}
											toolTipText="Page to show when access is denied. Required for evasion pages and filtering."
											options={pageMap.values().filter((n) => n !== formValues.evasionPage)}
											>Deny Page</TextFieldSelect
										>
										{#if errors.denyPage}
											<p id={errorId('denyPage')} class={errorClass}>{errors.denyPage}</p>
										{/if}
									</div>

									<div class="w-full max-w-lg mb-6">
										{#if formValues.denyPage}
											<TextFieldSelect
												id="program-evasion-page"
												size="full"
												bind:value={formValues.evasionPage}
												optional
												toolTipText="Shown before the first page. Bots get the deny page instead."
												options={pageMap.values().filter((n) => n !== formValues.denyPage)}
												>Evasion Page</TextFieldSelect
											>
										{:else}
											<div
												class="p-4 bg-gray-100 dark:bg-gray-800 rounded-lg border-2 border-dashed border-gray-300 dark:border-gray-600"
											>
												<p class="text-gray-600 dark:text-gray-400 text-sm">
													<strong>Evasion Page</strong><br />
													You must select a deny page first to use evasion pages.
												</p>
											</div>
										{/if}
									</div>

									<div class="w-full mb-6">
										{#if formValues.denyPage}
											<SelectSquare
												label="Filtering"
												toolTipText="Filter access based on allow / deny lists. Mail scanners that match get the deny page and are not counted as clicks."
												options={filteringOptions}
												width="small"
												bind:value={formValues.filterMode}
												onChange={() => (formValues.allowDeny = [])}
											/>

											{#if formValues.filterMode !== 'none'}
												<div class="mt-4 max-w-lg">
													<TextFieldMultiSelect
														id="program-allow-deny"
														size="full"
														error={!!errors.allowDeny}
														ariaDescribedBy={errors.allowDeny ? errorId('allowDeny') : null}
														toolTipText="Select the allow / deny filters"
														bind:value={formValues.allowDeny}
														options={allowDenyOptions}>Lists</TextFieldMultiSelect
													>
													{#if errors.allowDeny}
														<p id={errorId('allowDeny')} class={errorClass}>{errors.allowDeny}</p>
													{/if}
													{#if dependenciesLoaded && allowDenyOptions.length === 0}
														<p class={noteClass}>
															No {formValues.filterMode} lists yet.
															<a
																href="/filter"
																target="_blank"
																rel="noopener noreferrer"
																class={linkClass}>Create one</a
															>
														</p>
													{/if}
												</div>
											{/if}
										{:else}
											<div
												class="max-w-lg p-4 bg-gray-100 dark:bg-gray-800 rounded-lg border-2 border-dashed border-gray-300 dark:border-gray-600"
											>
												<p class="text-gray-600 dark:text-gray-400 text-sm">
													<strong>Filtering</strong><br />
													You must select a deny page first to use filtering.
												</p>
											</div>
										{/if}
									</div>
								{/if}
							</ConditionalDisplay>
						{/if}
					</div>
				</FormColumns>
			{:else if currentStep === 2}
				<FormColumns id={'step-2'}>
					<!-- centered and capped like the Setup step, the options under the list -->
					<div class="w-full max-w-2xl min-w-0 flex flex-col items-start gap-y-6">
						<div class="w-full max-w-lg min-w-0">
							<FieldLabel
								toolTipText={isEdit
									? 'Each recipient gets one template at a time, from this list. Removing a template skips it for recipients who have not had it yet.'
									: 'Each recipient gets one template at a time, from this list.'}
								>Templates</FieldLabel
							>
							<ol class="flex flex-col gap-1">
								{#each formValues.steps as step, i}
									<li class="flex items-center gap-1">
										<span
											class="w-6 flex-shrink-0 text-right pr-1 text-sm text-slate-500 dark:text-gray-400"
											>{i + 1}.</span
										>
										<div class="flex-1 min-w-0">
											<TextFieldSelect
												id={`program-simulation-template-${i}`}
												hideLabel
												size="full"
												placeholder="Select..."
												error={!!errors.simulations && !step.template}
												ariaDescribedBy={errors.simulations ? errorId('simulations') : null}
												bind:value={step.template}
												options={templateOptionsFor(i, formValues.steps, templateMap)}
											/>
										</div>
										<button
											type="button"
											class={rowButtonClass}
											title="Move up"
											aria-label={`Move template ${i + 1} up`}
											disabled={i === 0}
											on:click={() => moveStep(i, -1)}
										>
											<svg
												class="h-4 w-4"
												viewBox="0 0 20 20"
												fill="currentColor"
												aria-hidden="true"
											>
												<path
													fill-rule="evenodd"
													d="M14.77 12.79a.75.75 0 01-1.06-.02L10 8.832 6.29 12.77a.75.75 0 11-1.08-1.04l4.25-4.5a.75.75 0 011.08 0l4.25 4.5a.75.75 0 01-.02 1.06z"
													clip-rule="evenodd"
												/>
											</svg>
										</button>
										<button
											type="button"
											class={rowButtonClass}
											title="Move down"
											aria-label={`Move template ${i + 1} down`}
											disabled={i === formValues.steps.length - 1}
											on:click={() => moveStep(i, 1)}
										>
											<svg
												class="h-4 w-4"
												viewBox="0 0 20 20"
												fill="currentColor"
												aria-hidden="true"
											>
												<path
													fill-rule="evenodd"
													d="M5.23 7.21a.75.75 0 011.06.02L10 11.168l3.71-3.938a.75.75 0 111.08 1.04l-4.25 4.5a.75.75 0 01-1.08 0l-4.25-4.5a.75.75 0 01.02-1.06z"
													clip-rule="evenodd"
												/>
											</svg>
										</button>
										<button
											type="button"
											class={rowButtonClass}
											title="Remove this template"
											aria-label={`Remove template ${i + 1}`}
											disabled={formValues.steps.length === 1}
											on:click={() => removeStep(i)}
										>
											<img class="w-4 flex-shrink-0" src="/delete2.svg" alt="" />
										</button>
									</li>
								{/each}
							</ol>
							{#if errors.simulations}
								<p id={errorId('simulations')} class="{errorClass} pt-1">{errors.simulations}</p>
							{/if}
							{#if dependenciesLoaded && templateMap.values().length === 0}
								<p class={noteClass}>
									No templates yet. <a
										href="/campaign-template"
										target="_blank"
										rel="noopener noreferrer"
										class={linkClass}>Create one</a
									>
								</p>
							{/if}
							<div class="pt-2">
								<button
									id="program-add-simulation"
									type="button"
									class={addButtonClass}
									disabled={formValues.steps.length >=
										Math.min(stepMax, Math.max(1, templateMap.values().length))}
									title={formValues.steps.length >= stepMax
										? `A program can have at most ${stepMax} templates`
										: null}
									on:click={addStep}
								>
									+ Add Template
								</button>
							</div>
						</div>

						<!-- the select labels carry their own top padding, pulled up to line up with Templates -->
						<div id="program-template-options" class="w-full max-w-lg min-w-0">
							{#if formValues.steps.length > 1}
								<TextFieldSelect
									id="program-order"
									size="full"
									error={!!errors.orderMode}
									ariaDescribedBy={errors.orderMode ? errorId('orderMode') : null}
									bind:value={formValues.orderMode}
									toolTipText="Same order: everyone gets template 1, then 2, and so on. Random: each recipient gets all the templates, in their own shuffled order."
									options={orderModes.keys()}>Template Order</TextFieldSelect
								>
								{#if errors.orderMode}
									<p id={errorId('orderMode')} class={errorClass}>{errors.orderMode}</p>
								{/if}
								{#if orderModes.byKey(formValues.orderMode) === 'shuffle'}
									<TextFieldSelect
										id="program-mix"
										size="full"
										error={!!errors.templateMixBy}
										ariaDescribedBy={errors.templateMixBy ? errorId('templateMixBy') : null}
										bind:value={formValues.templateMixBy}
										toolTipText="Recipients who share this value get an even mix of the templates around the same time."
										options={templateMixes.keys()}>Balance per</TextFieldSelect
									>
									{#if errors.templateMixBy}
										<p id={errorId('templateMixBy')} class={errorClass}>{errors.templateMixBy}</p>
									{/if}
								{/if}
							{/if}
							<TextFieldSelect
								id="program-loop"
								size="full"
								error={!!errors.loopMode}
								ariaDescribedBy={errors.loopMode ? errorId('loopMode') : null}
								bind:value={formValues.loopMode}
								toolTipText="Start over from the first template, or send no more templates."
								options={loopModes.keys()}>After the Last Template</TextFieldSelect
							>
							{#if errors.loopMode}
								<p id={errorId('loopMode')} class={errorClass}>{errors.loopMode}</p>
							{/if}
						</div>
					</div>
				</FormColumns>
			{:else if currentStep === 3}
				<FormColumns id={'step-3'}>
					<!-- fields on the left, the forecast stays in view on the right -->
					<div
						class="w-full max-w-4xl min-w-0 grid grid-cols-1 lg:grid-cols-[minmax(0,1fr)_minmax(0,20rem)] gap-x-8 gap-y-6 items-start"
					>
						<div class="w-full min-w-0 flex flex-col items-start">
							{#if preStart}
								<div id="program-start-mode" class="w-full">
									<SelectSquare
										label="Start"
										options={startModes}
										bind:value={formValues.startMode}
										onChange={() => pickStartMode(formValues.startMode)}
									/>
									{#if formValues.startMode === 'date'}
										<div id="program-start-at" class="mb-2">
											<DateTimeField
												value={formValues.startAtLocal}
												min={new Date()}
												onChange={(v) => (formValues.startAtLocal = v)}>Start Date</DateTimeField
											>
											{#if errors.startAt}
												<p id={errorId('startAt')} class="{errorClass} pt-1">{errors.startAt}</p>
											{/if}
										</div>
									{/if}
								</div>
							{:else if formValues.startedAt}
								<p class="{noteClass} mb-2">Started {formatDateTime(formValues.startedAt)}</p>
							{/if}

							<div id="program-end-mode" class="w-full">
								<SelectSquare
									label="End"
									toolTipText={preStart
										? 'When the program closes: after some rounds, on a date, or never. In one round every template goes out once. With Start over the program ends before the next template, so the last one can get less time. The forecast shows how much.'
										: 'When the program closes: on a date, or never. With Start over the program ends before the next template, so the last one can get less time. The forecast shows how much.'}
									options={endModes}
									bind:value={formValues.endMode}
									onChange={() => pickEndMode(formValues.endMode)}
								/>
								{#if formValues.endMode === 'rounds' && loopsPicked}
									<div class="w-full max-w-lg">
										<TextFieldSelect
											id="program-end-rounds"
											size="full"
											error={!!errors.endRounds}
											ariaDescribedBy={errors.endRounds ? errorId('endRounds') : null}
											bind:value={formValues.endRounds}
											toolTipText="How many times every template goes out before the program ends."
											options={roundCounts.map(roundsLabel)}>Rounds</TextFieldSelect
										>
										{#if errors.endRounds}
											<p id={errorId('endRounds')} class={errorClass}>{errors.endRounds}</p>
										{:else if roundsEndUnknown}
											<Note role="status">{ROUNDS_END_UNKNOWN}</Note>
										{/if}
									</div>
								{/if}
								{#if hasEnd(formValues)}
									<div id="program-end-at" class="mb-2">
										<DateTimeField
											value={formValues.endAtLocal}
											min={new Date()}
											ariaDescribedBy={errors.endAt ? errorId('endAt') : null}
											toolTipText="The program closes itself at this time and its links stop working. Results are kept. Changing it makes the end a custom date."
											onChange={onEndDate}>End Date</DateTimeField
										>
										{#if errors.endAt}
											<p id={errorId('endAt')} class="{errorClass} pt-1">{errors.endAt}</p>
										{/if}
									</div>
									{#if forecast.warnings.length}
										<div id="program-end-warning" class="w-full max-w-lg mb-2 space-y-1">
											{#each forecast.warnings as warning}
												<Note role="status">{warning}</Note>
											{/each}
											{#if forecast.fitEnd && preStart}
												<button
													id="program-fit-end"
													type="button"
													class="mt-2 {addButtonClass}"
													title={formatDateTime(forecast.fitEnd)}
													on:click={fitTheEnd}>Move end to {formatDate(forecast.fitEnd)}</button
												>
											{/if}
										</div>
									{/if}
								{/if}
							</div>

							<!-- the anonymize dates, as in the campaign wizard -->
							<div id="program-anonymize" class="w-full max-w-lg">
								{#if formValues.isAnonymous}
									<div
										class="bg-blue-50 dark:bg-blue-900/30 p-3 mb-2 rounded-md text-sm text-blue-700 dark:text-blue-200 transition-colors duration-200"
									>
										<strong>Auto-anonymized at close.</strong>
									</div>
								{:else}
									{#if hasEnd(formValues)}
										<div id="program-anonymize-at">
											<DateTimeField
												bind:value={formValues.anonymizeAt}
												min={endMoment || new Date()}
												ariaDescribedBy={errors.anonymizeAt ? errorId('anonymizeAt') : null}
												optional
												toolTipText="When reached, all data of the closed program is anonymized, including the recipient relation."
												>Anonymize All Data</DateTimeField
											>
											{#if errors.anonymizeAt}
												<p id={errorId('anonymizeAt')} class="{errorClass} pt-1">
													{errors.anonymizeAt}
												</p>
											{/if}
										</div>
									{:else if anonymizeDateDropped(formValues)}
										<div class="w-full mb-2">
											<Note id="program-anonymize-dropped" role="status">
												Without an end the scheduled Anonymize All date is removed.
											</Note>
										</div>
									{:else}
										<p id="program-anonymize-needs-end" class="{noteClass} mb-2">
											Set an end to anonymize all data on a date.
										</p>
									{/if}
									<div id="program-data-anonymize-at">
										<DateTimeField
											bind:value={formValues.dataAnonymizeAt}
											min={endMoment || new Date()}
											ariaDescribedBy={errors.dataAnonymizeAt ? errorId('dataAnonymizeAt') : null}
											optional
											toolTipText="When reached, only the submitted data, user agent, ip and browser metadata are anonymized. The recipient relation is kept."
											>Anonymize Data</DateTimeField
										>
										{#if errors.dataAnonymizeAt}
											<p id={errorId('dataAnonymizeAt')} class="{errorClass} pt-1">
												{errors.dataAnonymizeAt}
											</p>
										{/if}
									</div>
								{/if}
							</div>

							<div class="w-full max-w-lg">
								<TextFieldSelect
									id="program-interval"
									size="full"
									error={!!errors.interval}
									ariaDescribedBy={errors.interval ? errorId('interval') : null}
									bind:value={formValues.interval}
									toolTipText="How long each recipient waits between two templates."
									options={intervalList.map(intervalLabel)}>Time Between Templates</TextFieldSelect
								>
								{#if errors.interval}
									<p id={errorId('interval')} class={errorClass}>{errors.interval}</p>
								{/if}
								<TextFieldSelect
									id="program-spread"
									size="full"
									error={spreadTooLong || !!errors.spread}
									ariaDescribedBy={spreadTooLong || errors.spread ? 'program-spread-error' : null}
									bind:value={formValues.spread}
									toolTipText="Each template goes out at a random time in this window, so recipients do not all get it at the same moment. At most the time between templates."
									options={spreadList.map(spreadLabel)}>Spread Sends Over</TextFieldSelect
								>
								{#if spreadTooLong || errors.spread}
									<p id="program-spread-error" class={errorClass}>
										{spreadTooLong ? SPREAD_TOO_LONG : errors.spread}
									</p>
								{:else if cadenceText}
									<p id="program-cadence" class={noteClass}>{cadenceText}</p>
								{/if}
							</div>

							<fieldset class="w-full mt-2">
								<legend class="contents">
									<FieldLabel>Sending Days</FieldLabel>
								</legend>
								<div
									class="grid grid-cols-2 sm:grid-cols-4 lg:grid-cols-7 gap-2 lg:gap-1.5 max-w-lg"
								>
									{#each weekDayOrder as d}
										<label
											class="flex items-center gap-2 lg:gap-1.5 min-h-[44px] px-3 lg:px-2 lg:justify-center rounded-md border text-sm cursor-pointer transition-colors duration-200 {formValues
												.weekDays[d]
												? 'border-cta-blue bg-blue-50 text-slate-700 dark:border-highlight-blue dark:bg-highlight-blue/20 dark:text-gray-200'
												: 'border-gray-300 text-slate-700 hover:border-cta-blue hover:bg-blue-50 dark:border-gray-700/60 dark:text-gray-200 dark:hover:border-highlight-blue dark:hover:bg-highlight-blue/10'} {errors.days
												? '!border-red-500 dark:!border-red-400'
												: ''}"
										>
											<input
												id={`program-day-${d}`}
												type="checkbox"
												class="pc-checkbox"
												bind:checked={formValues.weekDays[d]}
											/>
											<span class="lg:sr-only">{dayNames[d]}</span>
											<span class="hidden lg:inline" aria-hidden="true"
												>{dayNames[d].slice(0, 3)}</span
											>
										</label>
									{/each}
								</div>
								{#if errors.days}
									<p id={errorId('days')} class="{errorClass} pt-1">{errors.days}</p>
								{/if}
							</fieldset>

							<div class="mt-4">
								<FieldLabel>Sending Hours</FieldLabel>
							</div>
							<div class="grid grid-cols-2 gap-3 sm:flex sm:flex-wrap sm:items-center sm:gap-y-2">
								<input
									id="program-start-time"
									type="time"
									aria-label="From"
									autocomplete="off"
									class="{timeInputClass} {errors.times ? timeInputErrorClass : timeInputOkClass}"
									bind:value={formValues.startTime}
								/>
								<span class="hidden sm:block"><ToIcon /></span>
								<input
									id="program-end-time"
									type="time"
									aria-label="To"
									autocomplete="off"
									aria-describedby={errors.times ? errorId('times') : null}
									class="{timeInputClass} {errors.times ? timeInputErrorClass : timeInputOkClass}"
									bind:value={formValues.endTime}
								/>
							</div>
							{#if errors.times}
								<p id={errorId('times')} class="{errorClass} pt-1">{errors.times}</p>
							{/if}
							{#if timeZoneLocked}
								<p
									id="program-time-zone-locked"
									class="flex items-start gap-1.5 text-sm text-slate-600 dark:text-gray-300 mt-2"
								>
									<svg
										xmlns="http://www.w3.org/2000/svg"
										class="h-4 w-4 mt-0.5 flex-shrink-0"
										fill="none"
										viewBox="0 0 24 24"
										stroke="currentColor"
										aria-hidden="true"
									>
										<path
											stroke-linecap="round"
											stroke-linejoin="round"
											stroke-width="2"
											d="M12 15v2m-6 4h12a2 2 0 002-2v-6a2 2 0 00-2-2H6a2 2 0 00-2 2v6a2 2 0 002 2zm10-10V7a4 4 0 00-8 0v4h8z"
										/>
									</svg>
									<span
										>Days and hours are in {timeZoneLabel(formValues.timeZone)}. Locked once an
										anonymous program has started.</span
									>
								</p>
							{:else if zoneNote(formValues.timeZone)}
								<!-- only when the program was saved from a browser in another zone -->
								<p class="text-sm text-slate-600 dark:text-gray-300 mt-2">
									Days and hours are in {zoneNote(formValues.timeZone)}
								</p>
							{/if}

							<div class="w-full mt-4">
								<FieldLabel
									optional
									toolTipText="Nothing is sent on these days, for example 24 Dec to 1 Jan."
									>Skip Dates</FieldLabel
								>
								{#each formValues.blackout as range, i}
									<div
										class="grid grid-cols-[minmax(0,1fr)_minmax(0,1fr)_auto] items-center gap-2 pb-2 sm:flex sm:flex-wrap sm:gap-x-3 sm:gap-y-0 [&_input]:w-full sm:[&_input]:w-44"
									>
										<DateField
											id={`program-blackout-from-${i}`}
											noLabel
											inputWidth="small"
											bind:value={range.from}
											onChange={() => onBlackoutFrom(i)}>First day</DateField
										>
										<span class="hidden sm:block"><ToIcon /></span>
										<DateField
											id={`program-blackout-to-${i}`}
											noLabel
											inputWidth="small"
											min={range.from || ''}
											bind:value={range.to}>Last day</DateField
										>
										<button
											type="button"
											class={rowButtonClass}
											title="Remove these skip dates"
											aria-label={`Remove skip dates ${i + 1}`}
											on:click={() => removeBlackout(i)}
										>
											<img class="w-4 flex-shrink-0" src="/delete2.svg" alt="" />
										</button>
										<!-- the yearly box sits on its own line, next to the dates its label wraps -->
										<div class="col-span-3 sm:basis-full [&>label]:w-auto [&>label]:py-0">
											<CheckboxField
												id={`program-blackout-yearly-${i}`}
												inline
												bind:value={range.yearly}
												toolTipText="Skip these days every year. A range may run over the new year, for example 24 Dec to 1 Jan."
												>Every Year</CheckboxField
											>
										</div>
									</div>
									{#if range.yearly && range.from && range.to && range.to >= range.from}
										<p class="text-sm text-slate-600 dark:text-gray-300 pb-2">
											{describeBlackout(range)}
										</p>
									{/if}
								{/each}
								{#if errors.blackout}
									<p id={errorId('blackout')} class="{errorClass} pb-2">{errors.blackout}</p>
								{/if}
								<button
									id="program-add-blackout"
									type="button"
									class={addButtonClass}
									disabled={formValues.blackout.length >= blackoutMax}
									on:click={addBlackout}
								>
									+ Add Skip Dates
								</button>
								<div class="mt-3">
									<FieldLabel toolTipText="Add a holiday as a skip day that repeats every year."
										>Holidays</FieldLabel
									>
									<div class="flex flex-wrap gap-2 mt-1">
										{#each HOLIDAY_PRESETS as holiday}
											<button
												id={`program-holiday-${holiday.day}`}
												type="button"
												class={addButtonClass}
												disabled={hasHoliday(formValues.blackout, holiday) ||
													formValues.blackout.length >= blackoutMax}
												on:click={() => addHoliday(holiday)}
											>
												+ {holiday.name}
											</button>
										{/each}
									</div>
								</div>
							</div>

							<div class="w-full max-w-lg mt-2">
								<TextFieldSelect
									id="program-new-member-delay"
									size="full"
									error={!!errors.newMemberDelay}
									ariaDescribedBy={errors.newMemberDelay ? errorId('newMemberDelay') : null}
									bind:value={formValues.newMemberDelay}
									toolTipText="For recipients who join a group after the start. After this wait their first template goes out at a random time within the spread. With no wait the spread starts when they join. Everyone in the groups at the start gets the first template when the program starts."
									options={delayList.map(delayLabel)}>First Template to New Members</TextFieldSelect
								>
								{#if errors.newMemberDelay}
									<p id={errorId('newMemberDelay')} class={errorClass}>{errors.newMemberDelay}</p>
								{/if}
							</div>
						</div>

						<!-- stays in view while the form scrolls and never grows taller than the
						     space between the step bar and the sticky footer -->
						<div
							id="program-forecast"
							class="w-full max-w-lg lg:sticky lg:top-[6.5rem] lg:max-h-[calc(min(52rem,100dvh_-_8rem)_-_12.5rem)] lg:overflow-y-auto {reviewCardClass}"
							aria-live="polite"
						>
							<h3 class={reviewTitleClass}>Forecast</h3>
							{#if forecast.rows.length}
								<div class={reviewGridClass}>
									{#each forecast.rows as row}
										<span class="text-slate-500 dark:text-gray-400 font-medium">{row.label}:</span>
										<span class="text-pc-darkblue dark:text-white">{row.text}</span>
									{/each}
								</div>
								{#if forecast.windowNote}
									<p class="mt-3 {noteClass}">{forecast.windowNote}</p>
								{/if}
								{#if forecast.note}
									<p id="program-forecast-note" class="mt-3 {noteClass}">
										{forecast.note}
									</p>
								{/if}
								{#if forecast.endNote}
									<p id="program-forecast-end-note" class="mt-3 {noteClass}">
										{forecast.endNote}
									</p>
								{/if}
								{#if forecast.timeline}
									<ProgramEndTimeline id="program-forecast-timeline" timeline={forecast.timeline} />
								{/if}
							{:else}
								<p class={noteClass}>Add a template to see the forecast.</p>
							{/if}
						</div>
					</div>
				</FormColumns>
			{:else if currentStep === 4}
				<FormColumns id={'step-4'}>
					<div class="w-full max-w-2xl min-w-0">
						<div class="space-y-4 w-full">
							{#if forecast.rows.length}
								<div class={reviewCardClass}>
									<h3 class={reviewTitleClass}>Forecast</h3>
									<div class={reviewGridClass}>
										{#each forecast.rows as row}
											<span class="text-slate-500 dark:text-gray-400 font-medium">{row.label}:</span
											>
											<span class="text-pc-darkblue dark:text-white">{row.text}</span>
										{/each}
									</div>
									{#if forecast.windowNote}
										<p class="mt-3 {noteClass}">{forecast.windowNote}</p>
									{/if}
									{#if forecast.note}
										<p id="program-forecast-note" class="mt-3 {noteClass}">
											{forecast.note}
										</p>
									{/if}
									{#if forecast.endNote}
										<p id="program-forecast-end-note" class="mt-3 {noteClass}">
											{forecast.endNote}
										</p>
									{/if}
									{#if forecast.warnings.length}
										<div id="program-review-end-warning" class="mt-3 space-y-1">
											{#each forecast.warnings as warning}
												<Note boxed>{warning}</Note>
											{/each}
											{#if forecast.fitEnd && preStart}
												<button
													id="program-review-fit-end"
													type="button"
													class="mt-2 {addButtonClass}"
													title={formatDateTime(forecast.fitEnd)}
													on:click={fitTheEnd}>Move end to {formatDate(forecast.fitEnd)}</button
												>
											{/if}
										</div>
									{/if}
									{#if forecast.timeline}
										<ProgramEndTimeline timeline={forecast.timeline} />
									{/if}
								</div>
							{/if}

							<div class="grid grid-cols-1 md:grid-cols-2 gap-4">
								{#each sections as section}
									<div class={reviewCardClass}>
										<h3 class={reviewTitleClass}>{section.title}</h3>
										<div class={reviewGridClass}>
											{#each section.rows.filter((r) => !r.blackbox || isBlackbox) as row}
												{@const change = rowChange(section.title, row, originalRows)}
												<span class="text-slate-500 dark:text-gray-400 font-medium"
													>{row.label}:</span
												>
												<div class="text-pc-darkblue dark:text-white">
													{#if change?.old}
														<s class="mr-1 text-gray-500 dark:text-gray-400">{change.old}</s>
													{/if}
													{#if row.list && row.list.length}
														<!-- a long list shows its first lines, the step itself lists all -->
														{#each row.list.slice(0, REVIEW_LIST_LIMIT) as item, n}
															<div>{row.ordered ? `${n + 1}. ` : ''}{item}</div>
														{/each}
														{#if row.list.length > REVIEW_LIST_LIMIT}
															<div class="text-gray-500 dark:text-gray-400">
																+{row.list.length - REVIEW_LIST_LIMIT} more
															</div>
														{/if}
													{:else if row.at}
														<!-- a date as on the campaign review -->
														<Datetime value={row.at} />
														<span class="block text-xs"><RelativeTime value={row.at} /></span>
													{:else}
														{row.text}
													{/if}
													{#if row.reportCheck && reportDeliveryBadge}
														<span class="mt-1 flex items-center gap-x-3">
															<StatusBadge
																tone={reportDeliveryBadge.tone}
																title={reportDeliveryBadge.tip}
																>{reportDeliveryBadge.label}</StatusBadge
															>
															<a
																href={reportDeliveryHref(reportDelivery, reportDelivery.companyID)}
																target="_blank"
																rel="noopener"
																class="pc-focus rounded-sm text-xs text-cta-blue dark:text-highlight-blue hover:underline"
																>Settings</a
															>
														</span>
													{/if}
													{#if row.groups && audienceLine}
														<span class="block text-xs text-gray-500 dark:text-gray-400"
															>{audienceLine}</span
														>
													{/if}
													{#if change}
														<StatusBadge tone="warning" class="mt-1">Changed</StatusBadge>
													{/if}
												</div>
											{/each}
										</div>
									</div>
								{/each}
							</div>
						</div>
					</div>
				</FormColumns>
			{/if}
			<FormError message={modalError} />
			<div
				class="sticky bottom-0 z-20 -mx-4 sm:-mx-8 px-4 sm:px-8 py-4 mt-2 flex flex-wrap items-center justify-between gap-2 border-t-2 border-gray-200 dark:border-gray-700 bg-white dark:bg-gray-800 transition-colors duration-200"
			>
				{#if currentStep > 1}
					<button
						type="button"
						class="{wizardSecondaryButtonClass} max-sm:min-w-0 max-sm:px-3"
						data-modal-nav
						on:click={previousStep}
					>
						<svg
							class="h-4 w-4"
							xmlns="http://www.w3.org/2000/svg"
							fill="none"
							viewBox="0 0 24 24"
							stroke="currentColor"
							aria-hidden="true"
						>
							<path
								stroke-linecap="round"
								stroke-linejoin="round"
								stroke-width="2"
								d="M15 19l-7-7 7-7"
							/>
						</svg>
						Previous
					</button>
				{:else}
					<div></div>
				{/if}

				<div class="flex items-center gap-2 ml-auto">
					<button
						type="button"
						class="{wizardSecondaryButtonClass} max-sm:min-w-0 max-sm:px-3"
						disabled={isSubmitting}
						on:click={closeModal}
					>
						Cancel
					</button>
					{#if currentStep < wizardSteps.length}
						<FormButton type="button" size="large" on:click={nextStep}>
							Next
							<svg
								class="ml-1 h-4 w-4"
								xmlns="http://www.w3.org/2000/svg"
								fill="none"
								viewBox="0 0 24 24"
								stroke="currentColor"
								aria-hidden="true"
							>
								<path
									stroke-linecap="round"
									stroke-linejoin="round"
									stroke-width="2"
									d="M9 5l7 7-7 7"
								/>
							</svg>
						</FormButton>
					{:else}
						<FormButton size="large" {isSubmitting}>{submitText}</FormButton>
					{/if}
				</div>
			</div>
		</FormGrid>
	</div>
</Modal>
