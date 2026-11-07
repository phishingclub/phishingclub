<script>
	// The create and edit wizard of a program, shown in a modal.
	// Call loadDependencies once, then openCreateModal or openUpdateModal.
	// Fires "saved" after a program is created or updated.
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
	import { timeZoneLabel } from '$lib/utils/dailySlots.js';
	import DateTimeField from '$lib/components/DateTimeField.svelte';
	import FormButton from '$lib/components/FormButton.svelte';
	import FieldLabel from '$lib/components/form/FieldLabel.svelte';
	import StatusBadge from '$lib/components/StatusBadge.svelte';
	import ConditionalDisplay from '$lib/components/ConditionalDisplay.svelte';
	import { displayMode, DISPLAY_MODE } from '$lib/store/displayMode';
	import { addToast } from '$lib/store/toast';
	import {
		DEFAULT_INTERVAL_MINUTES,
		MINUTES_PER_DAY,
		MINUTES_PER_MONTH,
		MINUTES_PER_WEEK,
		browserTimeZone,
		capitalize,
		describeBlackout,
		describeBlackoutShort,
		describeDays,
		describeDuration,
		describeInterval,
		formatDateTime,
		formatWeekDay,
		fromZoneLocal,
		plural,
		toZoneLocal,
		weekDayOrder
	} from '$lib/utils/programText.js';
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
		loopMode: 2,
		startAt: 3,
		endAt: 3,
		interval: 3,
		spread: 3,
		days: 3,
		times: 3,
		blackout: 3,
		newMemberDelay: 3
	};
	// fields shown only under "Show advanced options"
	const advancedFields = ['webhooks', 'denyPage', 'allowDeny'];
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
		loopMode: 'program-loop',
		startAt: () => document.querySelector('#program-start-at input[type="date"]'),
		endAt: () => document.querySelector('#program-end-at input[type="date"]'),
		interval: 'program-interval',
		spread: 'program-spread',
		days: 'program-day-1',
		times: 'program-end-time',
		blackout: () => `program-blackout-from-${blackoutErrorIndex}`,
		newMemberDelay: 'program-new-member-delay'
	};
	// the first skip dates row or webhook with an error, for focus
	let blackoutErrorIndex = 0;
	let webhookErrorIndex = 0;

	// the selects show and bind the label, like the campaign wizard, and the
	// payload maps it back to the API value
	const orderModes = new BiMap({
		'Same order for everyone': 'sequential',
		'Random order per person': 'shuffle'
	});
	const loopModes = new BiMap({
		'Start over': 'loop',
		Stop: 'complete'
	});
	// minutes for each preset
	const intervalPresets = [
		MINUTES_PER_WEEK,
		2 * MINUTES_PER_WEEK,
		MINUTES_PER_MONTH,
		3 * MINUTES_PER_MONTH
	];
	const spreadPresets = [
		0,
		MINUTES_PER_DAY,
		3 * MINUTES_PER_DAY,
		MINUTES_PER_WEEK,
		2 * MINUTES_PER_WEEK,
		MINUTES_PER_MONTH,
		2 * MINUTES_PER_MONTH,
		3 * MINUTES_PER_MONTH
	];
	const delayPresets = [0, MINUTES_PER_WEEK, 2 * MINUTES_PER_WEEK, MINUTES_PER_MONTH];
	const intervalLabel = (m) => describeDuration(m);
	const spreadLabel = (m) => (m > 0 ? describeDuration(m) : 'All at once');
	const delayLabel = (m) => (m > 0 ? `After ${describeDuration(m)}` : 'Right away');
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
	const endModes = [
		{
			value: 'never',
			label: 'Never',
			description: 'Runs until you close it',
			id: 'program-end-mode-never'
		},
		{
			value: 'date',
			label: 'On a date',
			description: 'Closes itself then',
			id: 'program-end-mode-date'
		}
	];
	const filteringOptions = [
		{ value: 'none', label: 'Off', id: 'program-filter-none' },
		{ value: 'allow', label: 'Only allow', id: 'program-filter-allow' },
		{ value: 'deny', label: 'Block', id: 'program-filter-deny' }
	];
	const securityOptions = [
		{ value: false, label: 'Disabled', id: 'program-security-off' },
		{ value: true, label: 'Enabled', id: 'program-security-on' }
	];
	const blackoutMax = 50;
	const webhookMax = 20;
	const SPREAD_TOO_LONG = 'The spread can not be longer than the time between emails';
	const labelOf = (list, value) => list.find((p) => p.value === String(value))?.label || '';
	// shows the browser zone and the stored one when they differ
	const zoneNote = (zone) => (zone && zone !== browserTimeZone ? timeZoneLabel(zone) : '');

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
	let depsPromise = null;
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
	$: isBlackbox = $displayMode === DISPLAY_MODE.BLACKBOX;

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
		orderMode: orderModes.byValue('shuffle'),
		loopMode: loopModes.byValue('loop'),
		interval: intervalLabel(DEFAULT_INTERVAL_MINUTES),
		spread: spreadLabel(MINUTES_PER_WEEK),
		newMemberDelay: delayLabel(2 * MINUTES_PER_WEEK),
		// sending hours and skip dates are in this zone, it is set from the browser
		// on create and kept on update
		timeZone: browserTimeZone,
		useWindow: true,
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
		steps: [{ id: null, template: null }],
		recipientGroups: []
	});

	let formValues = emptyForm();

	$: isEdit = modalMode === 'update';
	$: modalText = isEdit ? 'Update program' : 'New program';
	$: anonymousLocked = isEdit && !!formValues.startedAt;
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
	// allow and deny lists can not be mixed, only offer the chosen kind
	$: allowDenyOptions = allAllowDeny
		.filter((e) => (formValues.filterMode === 'allow' ? e.allowed : !e.allowed))
		.map((e) => e.name);
	// the stored minutes are kept in the lists when they are not a preset
	let storedMinutes = { interval: null, spread: null, delay: null };
	$: intervalList = withValue(intervalPresets, storedMinutes.interval);
	$: intervalMinutes = minutesOf(intervalList, intervalLabel, formValues.interval);
	// the spread can be as long as the time between emails, not longer
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
	const uniqueNames = (rows, nameOf = (r) => r.name) => {
		const count = {};
		for (const r of rows) count[nameOf(r)] = (count[nameOf(r)] || 0) + 1;
		const used = new Set();
		return rows.map((r) => {
			let name = nameOf(r);
			if (count[name] > 1 && !r.companyID) name = `${name} (shared)`;
			let label = name;
			for (let n = 2; used.has(label); n++) label = `${name} (${n})`;
			used.add(label);
			return { ...r, name: label };
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

	const fetchDependencies = async (companyID) => {
		const templates = await fetchAllRows((options) =>
			api.campaignTemplate.getAll(options, companyID, true)
		);
		const groups = await fetchAllRows((options) => api.recipient.getAllGroups(options, companyID));
		const allowDeny = await fetchAllRows((options) =>
			api.allowDeny.getAllOverview(options, companyID)
		);
		const pages = await fetchAllRows((options) => api.page.getAll(options, companyID));
		const webhooks = await fetchAllRows((options) => api.webhook.getAll(options, companyID));
		loaded = {
			templates: uniqueNames(templates),
			groups: uniqueNames(groups).map((g) => ({ ...g, name: groupLabel(g) })),
			pages: uniqueNames(pages),
			allowDeny: uniqueNames(allowDeny),
			webhooks: uniqueNames(webhooks)
		};
		buildMaps();
		dependenciesLoaded = true;
	};

	// templates, groups, lists, pages and webhooks the wizard offers, for one
	// company. Calls for the same company share one load.
	export const loadDependencies = (companyID = contextCompanyID) => {
		const id = companyID || '';
		if (depsPromise && depsCompanyID === id) {
			return depsPromise;
		}
		depsCompanyID = id;
		const promise = fetchDependencies(id)
			.then(() => true)
			.catch((e) => {
				addToast('Failed to load templates, groups or pages', 'Error');
				console.error('failed to load program dependencies', e);
				if (depsPromise === promise) {
					depsPromise = null;
				}
				return false;
			});
		depsPromise = promise;
		return promise;
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
	const addMissingItems = async (p) => {
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
		// a label that is not in use yet, so the mapping back stays unique
		const freeLabel = (base, used) => {
			let label = base;
			for (let n = 2; used.has(label); n++) label = `${base} (${n})`;
			used.add(label);
			return label;
		};
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
		await add(p.recipientGroupIDs || [], groupMap, extra.groups, api.recipient.getGroupByID, 'group');
		await add(
			[p.denyPageID, p.evasionPageID],
			pageMap,
			extra.pages,
			api.page.getByID,
			'page'
		);
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
		missingItems = names;
		buildMaps(extra);
	};

	export const openCreateModal = async () => {
		formCompanyID = contextCompanyID || '';
		missingItems = [];
		const ok = await loadDependencies(formCompanyID);
		if (!ok) return;
		buildMaps();
		modalMode = 'create';
		formValues = { ...emptyForm(), companyID: formCompanyID || null };
		storedMinutes = { interval: null, spread: null, delay: null };
		originalValues = null;
		resetModalState();
		isModalVisible = true;
		focusFirstField();
	};

	export const openUpdateModal = async (id) => {
		try {
			const res = await api.program.getByID(id);
			if (!res.success) {
				throw res.error;
			}
			const p = res.data;
			if (p.closedAt) {
				addToast('A closed program can not be edited', 'Error');
				return;
			}
			// the options come from the company of the program, which can differ
			// from the company picked in the menu
			formCompanyID = p.companyID || '';
			const ok = await loadDependencies(formCompanyID);
			if (!ok) return;
			await addMissingItems(p);
			const weekDays = [false, false, false, false, false, false, false];
			const mask = p.constraintWeekDays;
			if (mask != null) {
				for (let i = 0; i < 7; i++) {
					weekDays[i] = (mask & (1 << i)) !== 0;
				}
			}
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
					p.status === 'scheduled' && p.startAt ? toZoneLocal(p.startAt, browserTimeZone) : '',
				endMode: p.endAt ? 'date' : 'never',
				endAtLocal: p.endAt ? toZoneLocal(p.endAt, browserTimeZone) : '',
				orderMode: orderModes.byValue(p.orderMode || 'shuffle'),
				loopMode: loopModes.byValue(p.loopMode || 'complete'),
				interval: intervalLabel(interval),
				spread: spreadLabel(spread),
				newMemberDelay: delayLabel(delay),
				timeZone: p.timeZone || browserTimeZone,
				useWindow: mask != null,
				weekDays: mask != null ? weekDays : [false, true, true, true, true, true, false],
				startTime: p.constraintStartTime || '09:00',
				endTime: p.constraintEndTime || '17:00',
				blackout: (p.blackoutDates || []).map((r) => ({ from: r.from, to: r.to })),
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
				steps: scheduled.length ? scheduled : [{ id: null, template: null }],
				recipientGroups: (p.recipientGroupIDs || [])
					.map((gid) => groupMap.byKey(gid))
					.filter(Boolean)
			};
			originalValues = JSON.parse(JSON.stringify(formValues));
			modalMode = 'update';
			resetModalState();
			// open the advanced options when the program uses them, like the campaign wizard
			showSecurityOptions = !!(formValues.denyPage || formValues.evasionPage || chosenLists.length);
			showAdvancedOptions = !!(formValues.webhooks.length || showSecurityOptions);
			isModalVisible = true;
			focusFirstField();
		} catch (e) {
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

	// the first sending day on or after a day, as "YYYY-MM-DD", skipping days
	// outside the sending days and skip dates. Empty when none is found in a year.
	const nextSendingDay = (v, fromKey, skipToday = () => false) => {
		const [y, m, d] = fromKey.split('-').map(Number);
		for (let i = 0; i < 400; i++) {
			const day = new Date(Date.UTC(y, m - 1, d + i));
			const key = day.toISOString().slice(0, 10);
			if (i === 0 && skipToday()) continue;
			if (v.useWindow && !v.weekDays[day.getUTCDay()]) continue;
			if (v.blackout.some((r) => r.from && r.to && key >= r.from && key <= r.to)) continue;
			return key;
		}
		return '';
	};

	// a wall clock time in the program zone as a browser local value for the
	// date and time field
	const zoneToBrowserLocal = (local, zone) => {
		const at = fromZoneLocal(local, zone);
		return at ? toZoneLocal(at, browserTimeZone) : '';
	};

	// a start date defaults to the next sending day at the start of the sending
	// hours, both in the program zone
	const pickStartMode = (mode) => {
		formValues.startMode = mode;
		if (mode === 'date' && !formValues.startAtLocal) {
			const zone = formValues.timeZone || browserTimeZone;
			const now = toZoneLocal(new Date(), zone);
			const clock = formValues.useWindow ? formValues.startTime : '09:00';
			const day = nextSendingDay(formValues, now.slice(0, 10), () => now.slice(11, 16) >= clock);
			if (day) formValues.startAtLocal = zoneToBrowserLocal(`${day}T${clock}`, zone);
		}
	};

	// an end date defaults to a year after the start, at the end of the sending
	// hours in the program zone
	const pickEndMode = (mode) => {
		formValues.endMode = mode;
		if (mode === 'date' && !formValues.endAtLocal) {
			const zone = formValues.timeZone || browserTimeZone;
			const start =
				formValues.startMode === 'date' && formValues.startAtLocal
					? fromZoneLocal(formValues.startAtLocal, browserTimeZone)
					: null;
			const from = toZoneLocal(start || new Date(), zone);
			const [y, m, d] = from.slice(0, 10).split('-').map(Number);
			const day = new Date(Date.UTC(y + 1, m - 1, d)).toISOString().slice(0, 10);
			const clock = formValues.useWindow ? formValues.endTime : '17:00';
			formValues.endAtLocal = zoneToBrowserLocal(`${day}T${clock}`, zone);
		}
	};

	// skip dates rows

	const addBlackout = async () => {
		const last = formValues.blackout[formValues.blackout.length - 1];
		formValues.blackout = [...formValues.blackout, { from: last?.to || '', to: last?.to || '' }];
		await tick();
		document.getElementById(`program-blackout-from-${formValues.blackout.length - 1}`)?.focus();
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
		}
		if (!orderModes.byKey(formValues.orderMode)) {
			e.orderMode = 'Pick a template order from the list';
		}
		if (!loopModes.byKey(formValues.loopMode)) {
			e.loopMode = 'Pick what happens after the last template';
		}
		if (preStart && formValues.startMode === 'date') {
			const at = fromZoneLocal(formValues.startAtLocal, browserTimeZone);
			if (!at) {
				e.startAt = 'Set the start date and time';
			} else if (at.getTime() <= Date.now()) {
				e.startAt = 'The start must be in the future';
			}
		}
		if (formValues.endMode === 'date') {
			const end = fromZoneLocal(formValues.endAtLocal, browserTimeZone);
			const start = startMoment(formValues, preStart);
			if (!end) {
				e.endAt = 'Set the end date and time';
			} else if (end.getTime() <= Date.now()) {
				e.endAt = 'The end must be in the future';
			} else if (start && end.getTime() <= start.getTime()) {
				e.endAt = 'The end must be after the start';
			}
		}
		const interval = minutesOf(intervalList, intervalLabel, formValues.interval);
		const spread = minutesOf(
			withValue(spreadPresets, storedMinutes.spread),
			spreadLabel,
			formValues.spread
		);
		if (interval == null) {
			e.interval = 'Pick the time between emails';
		}
		if (spread == null) {
			e.spread = 'Pick a spread from the list';
		} else if (interval != null && spread > interval) {
			e.spread = SPREAD_TOO_LONG;
		}
		if (formValues.useWindow) {
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
		}
		const badRange = formValues.blackout.findIndex((r) => !r.from || !r.to || r.to < r.from);
		if (badRange >= 0) {
			const r = formValues.blackout[badRange];
			e.blackout =
				!r.from || !r.to
					? 'Set both dates or remove the row'
					: 'The last day must be on or after the first day';
			blackoutErrorIndex = badRange;
		} else if (formValues.blackout.length > blackoutMax) {
			e.blackout = `At most ${blackoutMax} skip date ranges`;
			blackoutErrorIndex = blackoutMax;
		}
		if (formValues.filterMode !== 'none' && formValues.allowDeny.length === 0) {
			e.allowDeny = 'Pick at least one list or turn filtering off';
		}
		if ((formValues.filterMode !== 'none' || formValues.evasionPage) && !formValues.denyPage) {
			e.denyPage = 'Pick a deny page';
		} else if (formValues.denyPage && !pageMap.values().includes(formValues.denyPage)) {
			e.denyPage = 'Pick a page from the list';
		}
		if (minutesOf(delayList, delayLabel, formValues.newMemberDelay) == null) {
			e.newMemberDelay = 'Pick when new members get their first email';
		}
		const badHook = formValues.webhooks.findIndex(
			(w) => !w.name || !webhookMap.byValue(w.name)
		);
		if (badHook >= 0) {
			e.webhooks = 'Pick an endpoint for each webhook or remove it';
			webhookErrorIndex = badHook;
		}
		return e;
	};

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
			loopMode: loopModes.byKey(formValues.loopMode),
			stepIntervalMinutes: intervalMinutes,
			spreadMinutes: spreadMinutes,
			newMemberDelayMinutes: minutesOf(delayList, delayLabel, formValues.newMemberDelay),
			timeZone: formValues.timeZone || browserTimeZone,
			isAnonymous: formValues.isAnonymous,
			steps,
			recipientGroupIDs: formValues.recipientGroups.map((name) => groupMap.byValue(name)),
			blackoutDates: formValues.blackout.map((r) => ({ from: r.from, to: r.to })),
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
				payload.startAt = fromZoneLocal(formValues.startAtLocal, browserTimeZone).toISOString();
			} else {
				payload.draft = false;
				payload.startAt = null;
			}
		}
		payload.endAt =
			formValues.endMode === 'date'
				? fromZoneLocal(formValues.endAtLocal, browserTimeZone).toISOString()
				: null;
		if (formValues.useWindow) {
			let mask = 0;
			for (let i = 0; i < 7; i++) {
				if (formValues.weekDays[i]) {
					mask |= 1 << i;
				}
			}
			payload.constraintWeekDays = mask;
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
		if (days < 60) {
			return plural(Math.max(1, Math.round(days / 7)), 'week', 'weeks');
		}
		return plural(Math.round(days / 30), 'month', 'months');
	};

	// the moment sending can start, null for a draft or a missing date
	const startMoment = (v, beforeStart) => {
		if (!beforeStart) return v.startedAt ? new Date(v.startedAt) : new Date();
		if (v.startMode === 'draft') return null;
		if (v.startMode === 'date') return fromZoneLocal(v.startAtLocal, browserTimeZone);
		return new Date();
	};

	// the start as text, for the forecast and the review
	const startText = (v, beforeStart) => {
		if (!beforeStart) {
			return v.startedAt ? `Started ${formatDateTime(v.startedAt)}` : 'Started';
		}
		if (v.startMode === 'draft') return 'Draft, nothing is sent until you start it';
		if (v.startMode === 'date') {
			const at = fromZoneLocal(v.startAtLocal, browserTimeZone);
			return at ? formatDateTime(at) : 'Not set';
		}
		return 'Now';
	};

	// the end as text, for the forecast and the review
	const endText = (v) => {
		if (v.endMode !== 'date') return 'Never, runs until you close it';
		const at = fromZoneLocal(v.endAtLocal, browserTimeZone);
		return at ? formatDateTime(at) : 'Not set';
	};

	// the minutes of the picked spread, also when it is longer than the interval
	const spreadOf = (v) =>
		minutesOf(withValue(spreadPresets, storedMinutes.spread), spreadLabel, v.spread);

	const dayDate = (key) => {
		const [y, m, d] = key.split('-').map(Number);
		return new Date(Date.UTC(y, m - 1, d));
	};
	const addDays = (key, n) => {
		const day = dayDate(key);
		day.setUTCDate(day.getUTCDate() + n);
		return day.toISOString().slice(0, 10);
	};

	// the first and the last day of the first round, sending hours and skip
	// dates are in the program zone
	const firstRound = (v, start, spread) => {
		const zone = v.timeZone || browserTimeZone;
		const local = toZoneLocal(start, zone);
		if (!local) return null;
		const clock = local.slice(11, 16);
		const first = nextSendingDay(v, local.slice(0, 10), () => v.useWindow && clock >= v.endTime);
		if (!first) return null;
		// the spread starts on the first day, so a spread of 7 days ends on day 6
		let last = first;
		const spreadDays = Math.ceil((spread || 0) / MINUTES_PER_DAY) - 1;
		for (let i = spreadDays; i > 0; i--) {
			const key = addDays(first, i);
			if (nextSendingDay(v, key) === key) {
				last = key;
				break;
			}
		}
		return { first, last };
	};

	const buildForecast = (v, people, beforeStart) => {
		const n = v.steps.filter((s) => s.template).length;
		if (n === 0) {
			return { rows: [], note: '' };
		}
		const interval = minutesOf(intervalList, intervalLabel, v.interval);
		const spread = spreadOf(v) ?? 0;
		const loops = loopModes.byKey(v.loopMode) === 'loop';
		const start = startMoment(v, beforeStart);
		const end = v.endMode === 'date' ? fromZoneLocal(v.endAtLocal, browserTimeZone) : null;
		const rows = [
			{ label: 'People', text: people != null ? String(people) : audienceLoading ? 'Counting...' : 'Pick a group' },
			{ label: 'Start', text: startText(v, beforeStart) }
		];
		const round = beforeStart && start ? firstRound(v, start, spread) : null;
		if (round) {
			const who = people != null ? `${plural(people, 'person', 'people')}, ` : '';
			rows.push({
				label: 'First round',
				text:
					round.first === round.last
						? `${who}on ${formatWeekDay(dayDate(round.first))}`
						: `${who}between ${formatWeekDay(dayDate(round.first))} and ${formatWeekDay(dayDate(round.last))}`
			});
			if (interval != null && (loops || n > 1)) {
				const next = nextSendingDay(v, addDays(round.first, Math.round(interval / MINUTES_PER_DAY)));
				if (next && (!end || dayDate(next).getTime() < end.getTime())) {
					rows.push({ label: 'Next round', text: `From ${formatWeekDay(dayDate(next))}` });
				}
			}
		}
		let note = '';
		if (interval != null) {
			rows.push({ label: 'Each person', text: `A new template ${describeInterval(interval)}` });
			rows.push({
				label: 'Full round',
				text: `About ${roughDuration(n * interval)}, then ${loops ? 'starts over' : 'stops'}`
			});
			// emails per person over 12 months, or until the end when that is set
			const from = start ?? new Date();
			const span = end ? Math.max(0, end.getTime() - from.getTime()) / 60000 : 365 * MINUTES_PER_DAY;
			const fit = Math.floor(span / interval) + 1;
			const emails = loops ? fit : Math.min(n, fit);
			rows.push({
				label: end ? 'Until the end' : 'Over 12 months',
				text: `About ${plural(emails, 'email', 'emails')} per person`
			});
			if (loops && n < fit) {
				const times = Math.round(fit / n);
				note = `${plural(n, 'template', 'templates')}, about ${plural(fit, 'email', 'emails')} ${
					end ? 'until the end' : 'a year'
				}: each person gets each template about ${plural(times, 'time', 'times')}. Add templates or choose Stop.`;
			}
		}
		const skips = v.blackout.filter((r) => r.from && r.to).map((r) => describeBlackoutShort(r));
		if (skips.length) {
			rows.push({ label: 'Skip dates', text: `No emails ${skips.join(', ')}` });
		}
		rows.push({ label: 'Ends', text: endText(v) });
		return { rows, note };
	};

	$: forecast = buildForecast(formValues, audienceCount, preStart, audienceLoading);
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
					row('Recipient Groups', v.recipientGroups.join(', ') || 'None selected', { groups: true }),
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
					...(sims.length >= 2 ? [row('Template Order', v.orderMode)] : []),
					row('After the Last Template', v.loopMode)
				]
			},
			{
				step: 3,
				title: 'Schedule',
				rows: [
					row('Start', startText(v, preStart)),
					row('End', endText(v)),
					row('Time Between Emails', v.interval || 'Not set'),
					row('Spread Emails Over', v.spread || 'Not set'),
					row('Sending Days', v.useWindow ? capitalize(describeDays(v.weekDays)) : 'Any day'),
					row(
						'Sending Hours',
						v.useWindow ? `${v.startTime} to ${v.endTime}${zone ? `, ${zone}` : ''}` : 'Any time'
					),
					row('Skip Dates', skips.join('\n') || 'None', { list: skips }),
					row('First Email to New Members', v.newMemberDelay)
				]
			},
			{
				step: 1,
				title: 'Advanced options',
				rows: [
					row('Webhooks', webhooks.join('\n') || 'None', { list: webhooks }),
					row(
						'Filtering',
						v.filterMode === 'none'
							? 'Off'
							: `${v.filterMode === 'allow' ? 'Only allow' : 'Block'}: ${v.allowDeny.join(', ') || 'no lists'}`,
						{ blackbox: true }
					),
					row('Deny Page', v.denyPage || 'None', { blackbox: true }),
					row('Evasion Page', v.evasionPage || 'None', { blackbox: true })
				]
			}
		];
	};

	// the values the edit wizard opened with, to mark what changed
	let originalValues = null;
	$: sections = reviewSections(formValues);
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
					? plural(audienceCount, 'person', 'people')
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
				<p
					id="program-missing-items"
					role="status"
					class="w-full mt-4 mb-2 rounded-md bg-yellow-50 dark:bg-yellow-900/30 px-3 py-2 text-sm text-yellow-800 dark:text-yellow-200 transition-colors duration-200"
				>
					Some saved settings could not be found: {missingItems.join(', ')}. They are marked
					"not found" and are kept unless you change them.
				</p>
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
									? 'Removing a group stops emails to its members. Their results are kept.'
									: 'Everyone in these groups is enrolled, including people added later.'}
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
									No groups yet. <a href="/recipient/group" class={linkClass}>Create one</a>
								</p>
							{/if}
						</div>

						<div class="w-full mt-4">
							<SelectSquare
								disabled={anonymousLocked}
								toolTipText="Reports show totals only. Nobody can see who clicked. Can not be changed after the program starts."
								label="Anonymous"
								options={anonymousOptions}
								bind:value={formValues.isAnonymous}
							/>
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
									<svg class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24" aria-hidden="true"
										><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M19 9l-7 7-7-7" /></svg
									>
								</button>
							</div>
						{:else}
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
													<p class="text-xs font-semibold text-gray-700 dark:text-gray-300">Events</p>
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
											No webhooks yet. <a href="/webhook" class={linkClass}>Create one</a>
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
															<a href="/filter" class={linkClass}>Create one</a>
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
					<!-- the list grows down on the left, the options stay in place on the right -->
					<div
						class="w-full max-w-4xl min-w-0 grid grid-cols-1 lg:grid-cols-[minmax(0,1fr)_minmax(0,18rem)] gap-x-8 gap-y-4 items-start"
					>
						<div class="w-full max-w-lg min-w-0">
							<FieldLabel
								toolTipText={isEdit
									? 'Each person gets one template at a time, from this list. Removing a template skips it for people who have not had it yet.'
									: 'Each person gets one template at a time, from this list.'}
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
											<svg class="h-4 w-4" viewBox="0 0 20 20" fill="currentColor" aria-hidden="true">
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
											<svg class="h-4 w-4" viewBox="0 0 20 20" fill="currentColor" aria-hidden="true">
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
									No templates yet. <a href="/campaign-template" class={linkClass}>Create one</a>
								</p>
							{/if}
							<div class="pt-2">
								<button
									id="program-add-simulation"
									type="button"
									class={addButtonClass}
									on:click={addStep}
								>
									+ Add Template
								</button>
							</div>
						</div>

						<div id="program-template-options" class="w-full max-w-lg min-w-0">
							{#if formValues.steps.length > 1}
								<TextFieldSelect
									id="program-order"
									size="full"
									error={!!errors.orderMode}
									ariaDescribedBy={errors.orderMode ? errorId('orderMode') : null}
									bind:value={formValues.orderMode}
									toolTipText="Same order: everyone gets template 1, then 2, and so on. Random: each person gets all the templates, in their own shuffled order."
									options={orderModes.keys()}>Template Order</TextFieldSelect
								>
								{#if errors.orderMode}
									<p id={errorId('orderMode')} class={errorClass}>{errors.orderMode}</p>
								{/if}
							{/if}
							<TextFieldSelect
								id="program-loop"
								size="full"
								error={!!errors.loopMode}
								ariaDescribedBy={errors.loopMode ? errorId('loopMode') : null}
								bind:value={formValues.loopMode}
								toolTipText="Start over from the first template, or send no more emails."
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
									options={endModes}
									bind:value={formValues.endMode}
									onChange={() => pickEndMode(formValues.endMode)}
								/>
								{#if formValues.endMode === 'date'}
									<div id="program-end-at" class="mb-2">
										<DateTimeField
											value={formValues.endAtLocal}
											min={new Date()}
											toolTipText="The program closes itself at this time. Results are kept."
											onChange={(v) => (formValues.endAtLocal = v)}>End Date</DateTimeField
										>
										{#if errors.endAt}
											<p id={errorId('endAt')} class="{errorClass} pt-1">{errors.endAt}</p>
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
									toolTipText="Each person gets their next template after this time."
									options={intervalList.map(intervalLabel)}>Time Between Emails</TextFieldSelect
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
									toolTipText="Each email goes out at a random time in this period, so people do not all get it at once. At most the time between emails."
									options={spreadList.map(spreadLabel)}>Spread Emails Over</TextFieldSelect
								>
								{#if spreadTooLong || errors.spread}
									<p id="program-spread-error" class={errorClass}>
										{spreadTooLong ? SPREAD_TOO_LONG : errors.spread}
									</p>
								{/if}
							</div>

							<div class="w-full mt-2 [&>label]:w-auto">
								<CheckboxField
									id="program-working-hours"
									inline
									bind:value={formValues.useWindow}
									toolTipText="Only send on the chosen days and hours.">Sending Hours Only</CheckboxField
								>
							</div>

							{#if formValues.useWindow}
								<fieldset class="w-full">
									<legend class="contents">
										<FieldLabel>Sending Days</FieldLabel>
									</legend>
									<div class="grid grid-cols-2 sm:grid-cols-4 lg:grid-cols-7 gap-2 lg:gap-1.5 max-w-lg">
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
												<span class="hidden lg:inline" aria-hidden="true">{dayNames[d].slice(0, 3)}</span>
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
								{#if zoneNote(formValues.timeZone)}
									<!-- only when the program was saved from a browser in another zone -->
									<p class="text-sm text-slate-600 dark:text-gray-300 mt-2">
										Days and hours are in {zoneNote(formValues.timeZone)}
									</p>
								{/if}
							{/if}

							<div class="w-full mt-4">
								<FieldLabel
									optional
									toolTipText="No emails are sent on these days, for example 24 Dec to 1 Jan."
									>Skip Dates</FieldLabel
								>
								{#each formValues.blackout as range, i}
									<div
										class="grid grid-cols-[minmax(0,1fr)_minmax(0,1fr)_auto] items-center gap-2 pb-2 sm:flex sm:gap-3 [&_input]:w-full sm:[&_input]:w-44"
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
									</div>
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
							</div>

							<div class="w-full max-w-lg mt-2">
								<TextFieldSelect
									id="program-new-member-delay"
									size="full"
									error={!!errors.newMemberDelay}
									ariaDescribedBy={errors.newMemberDelay ? errorId('newMemberDelay') : null}
									bind:value={formValues.newMemberDelay}
									toolTipText="For people who join a group after the start. Everyone in the groups at the start gets their first email in the first round."
									options={delayList.map(delayLabel)}>First Email to New Members</TextFieldSelect
								>
								{#if errors.newMemberDelay}
									<p id={errorId('newMemberDelay')} class={errorClass}>{errors.newMemberDelay}</p>
								{/if}
							</div>
						</div>

						<div
							id="program-forecast"
							class="w-full max-w-lg lg:sticky lg:top-4 {reviewCardClass}"
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
								{#if forecast.note}
									<p
										id="program-forecast-note"
										class="mt-3 rounded-md bg-yellow-50 dark:bg-yellow-900/30 px-3 py-2 text-sm text-yellow-800 dark:text-yellow-200 transition-colors duration-200"
									>
										{forecast.note}
									</p>
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
											<span class="text-slate-500 dark:text-gray-400 font-medium">{row.label}:</span>
											<span class="text-pc-darkblue dark:text-white">{row.text}</span>
										{/each}
									</div>
									{#if forecast.note}
										<p
											id="program-forecast-note"
											class="mt-3 rounded-md bg-yellow-50 dark:bg-yellow-900/30 px-3 py-2 text-sm text-yellow-800 dark:text-yellow-200 transition-colors duration-200"
										>
											{forecast.note}
										</p>
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
												<span class="text-slate-500 dark:text-gray-400 font-medium">{row.label}:</span>
												<div class="text-pc-darkblue dark:text-white">
													{#if change?.old}
														<s class="mr-1 text-gray-500 dark:text-gray-400">{change.old}</s>
													{/if}
													{#if row.list && row.list.length}
														{#each row.list as item, n}
															<div>{row.ordered ? `${n + 1}. ` : ''}{item}</div>
														{/each}
													{:else}
														{row.text}
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
							<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M15 19l-7-7 7-7" />
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
								<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M9 5l7 7-7 7" />
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
