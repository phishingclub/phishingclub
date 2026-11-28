<script>
	import { api } from '$lib/api/apiProxy.js';
	import { onMount, tick } from 'svelte';
	import { newTableURLParams } from '$lib/service/tableURLParams.js';
	import { globalButtonDisabledAttributes } from '$lib/utils/form.js';
	import { safeExternalHref } from '$lib/utils/url.js';
	import Headline from '$lib/components/Headline.svelte';
	import TextField from '$lib/components/TextField.svelte';
	import TableRow from '$lib/components/table/TableRow.svelte';
	import TableCell from '$lib/components/table/TableCell.svelte';
	import TableUpdateButton from '$lib/components/table/TableUpdateButton.svelte';
	import TableDeleteButton from '$lib/components/table/TableDeleteButton2.svelte';
	import TableCellAction from '$lib/components/table/TableCellAction.svelte';
	import { addToast } from '$lib/store/toast';
	import FormError from '$lib/components/FormError.svelte';
	import { AppStateService } from '$lib/service/appState';
	import { fetchAllRows } from '$lib/utils/api-utils';
	import { BiMap } from '$lib/utils/maps';
	import TextFieldSelect from '$lib/components/TextFieldSelect.svelte';
	import Modal from '$lib/components/Modal.svelte';
	import InstalledResourceBadge from '$lib/components/InstalledResourceBadge.svelte';
	import ProxySvgIcon from '$lib/components/ProxySvgIcon.svelte';
	import PageFlow from '$lib/components/PageFlow.svelte';
	import FormGrid from '$lib/components/FormGrid.svelte';
	import TableCellEmpty from '$lib/components/table/TableCellEmpty.svelte';
	import BigButton from '$lib/components/BigButton.svelte';
	import FormFooter from '$lib/components/FormFooter.svelte';
	import TableCellScope from '$lib/components/table/TableCellScope.svelte';
	import Table from '$lib/components/table/Table.svelte';
	import HeadTitle from '$lib/components/HeadTitle.svelte';
	import { getModalText } from '$lib/utils/common';
	import TableCopyButton from '$lib/components/table/TableCopyButton.svelte';
	import { showIsLoading, hideIsLoading } from '$lib/store/loading.js';
	import TableCellCheck from '$lib/components/table/TableCellCheck.svelte';
	import TableDropDownEllipsis from '$lib/components/table/TableDropDownEllipsis.svelte';
	import DeleteAlert from '$lib/components/modal/DeleteAlert.svelte';
	import { page } from '$app/stores';
	import TableCellCheckbox from '$lib/components/table/TableCellCheckbox.svelte';
	import CheckboxField from '$lib/components/CheckboxField.svelte';
	import Tag from '$lib/components/Tag.svelte';
	import BulkActionBar from '$lib/components/table/BulkActionBar.svelte';
	import {
		createTableSelection,
		headerSelectionState,
		runBulkDelete
	} from '$lib/service/tableSelection.js';
	import SelectSquare from '$lib/components/SelectSquare.svelte';
	import TableDropDownButton from '$lib/components/table/TableDropDownButton.svelte';
	import AllowListingModal from '$lib/components/AllowListingModal.svelte';
	import TextFieldSelectWithType from '$lib/components/form/TextFieldSelectWithType.svelte';
	import ConditionalDisplay from '$lib/components/ConditionalDisplay.svelte';
	import { resourceContext } from '$lib/store/resourceContext';
	import { displayMode, isShown } from '$lib/store/displayMode';
	import Alert from '$lib/components/Alert.svelte';
	import FieldLabel from '$lib/components/form/FieldLabel.svelte';
	import Note from '$lib/components/Note.svelte';
	import { LANGUAGES, languageName } from '$lib/utils/languages.js';
	import { addButtonClass, rowButtonClass } from '$lib/utils/rowButton.js';

	// services
	const appStateService = AppStateService.instance;

	// data
	let form = null;
	let formValues = {
		id: null,
		templateType: 'Email',
		name: null,
		domain: null,
		landingPage: null,
		landingPageType: 'page', // 'page' or 'proxy'
		beforeLandingPage: null,
		afterLandingPage: null,
		afterLandingPageRedirectURL: '',
		email: null,
		smtpConfiguration: null,
		apiSender: null,
		urlIdentifier: 'id',
		stateIdentifier: 'session',
		urlPath: '',
		lureURLMode: 'query',
		lureCodeAlgo: 'crockford32',
		lureCodeLength: 12,
		isTraining: false,
		language: '',
		languages: [],
		multiLanguage: false,
		beforeLandingKind: 'none',
		afterLandingKind: 'none'
	};

	// codes are matched exactly, so the choice is about which glyphs appear and
	// how many characters are needed, not about how forgiving matching is
	const LURE_CODE_ALGOS = {
		crockford32: {
			label: 'Crockford base32',
			symbols: 32,
			sample: '4H7K9QM2XR3T',
			note: 'Upper case only, without I L O U.'
		},
		base58: {
			label: 'Base58',
			symbols: 58,
			sample: 'zQ8fRt2mKp9x',
			note: 'Mixed case, without 0 O I l.'
		}
	};

	const LURE_CODE_MIN_LENGTH = 6;
	const LURE_CODE_MAX_LENGTH = 16;

	// the backend rejects anything outside these bounds, so correct it here
	// rather than bouncing the whole form back with a validation error
	/** @param {string|number} value */
	const clampLureCodeLength = (value) => {
		const n = Number(value);
		if (!Number.isFinite(n)) {
			return 12;
		}
		return Math.min(LURE_CODE_MAX_LENGTH, Math.max(LURE_CODE_MIN_LENGTH, Math.round(n)));
	};

	/** @param {string} algo @param {number} length */
	const lureCodeKeyspace = (algo, length) => {
		const spec = LURE_CODE_ALGOS[algo];
		if (!spec || !length || length < LURE_CODE_MIN_LENGTH || length > LURE_CODE_MAX_LENGTH) {
			return '';
		}
		return `${(spec.symbols ** length).toLocaleString('en-US', { maximumFractionDigits: 0 })} combinations`;
	};

	// mirrors the server, which stores the path with the slash added
	/** @param {string} path */
	const withLeadingSlash = (path) => (path && !path.startsWith('/') ? `/${path}` : path);

	// shows the delivered link so the choice is concrete
	/** @param {Object} values */
	const lureURLExample = (values) => {
		const path = withLeadingSlash(values.urlPath || '');
		if (values.lureURLMode === 'path') {
			const sample = LURE_CODE_ALGOS[values.lureCodeAlgo]?.sample ?? '4H7K9QM2XR3T';
			return `https://domain${path.replace(/\/$/, '')}/${sample}`;
		}
		return `https://domain${path}?${values.urlIdentifier || 'id'}=6ba7b810-9dad-11d1-80b4-00c04fd430c8`;
	};

	let contextCompanyID = null;
	let domainMap = new BiMap({});
	let domainObjectMap = new Map(); // stores full domain objects
	let beforeLandingPageMap = new BiMap({});
	let landingPageMap = new BiMap({});
	let afterLandingPageMap = new BiMap({});
	let beforeLandingProxyMap = new BiMap({});
	let landingProxyMap = new BiMap({});
	let afterLandingProxyMap = new BiMap({});
	let emailMap = new BiMap({});
	let smtpConfigurationMap = new BiMap({});
	let apiSenderMap = new BiMap({});
	let identifierMap = new BiMap({});
	let templates = [];
	let templatesHasNextPage = true;

	// multi select
	const selection = createTableSelection();
	let isBulkDeleteAlertVisible = false;
	const isRowDisabled = (t) => globalButtonDisabledAttributes(t, contextCompanyID).disabled;
	$: selectablePageIds = templates
		.filter((t) => !globalButtonDisabledAttributes(t, contextCompanyID).disabled)
		.map((t) => t.id);
	$: headerState = headerSelectionState($selection, selectablePageIds);
	$: showMultiSelect = templates.length > 0;
	const onClickBulkDelete = async () => {
		await runBulkDelete({
			ids: [...$selection],
			deleteFn: api.campaignTemplate.delete,
			noun: 'template'
		});
		await refreshCampaignTemplates();
		return { success: true };
	};
	let modalError = '';
	// a server refusal of the training flag, shown under its checkbox
	let trainingError = '';
	$: formValues.isTraining, (trainingError = '');
	// a server refusal of the languages, shown at the end of the Languages section
	let languagesError = '';

	// Multiple languages: every language is a row of formValues.languages, the
	// first row is the main language. On save the main row is the template's
	// own email, pages and language, the other rows are its language rows.
	// Without it the editor has no language fields and the template keeps the
	// language it was loaded with.
	// the rows taken away by unchecking, given back when it is checked again
	let removedLanguageRows = [];
	// the rows taken away with the remove button, given back when the server
	// refuses to remove them
	let droppedLanguageRows = [];
	// true when the template was loaded with language rows
	let loadedWithLanguages = false;
	let isRemoveLanguagesAlertVisible = false;

	// language picks: the fixed list as code and name
	const languageOptions = LANGUAGES.map((l) => ({ value: l.code, label: l.name }));
	// codes a row can take: not the code of another row
	$: rowLanguageOptions = (i) =>
		languageOptions.filter(
			(o) => !formValues.languages.some((row, j) => j !== i && row.language === o.value)
		);
	// what each page flow step is with Multiple languages, the pages are picked
	// per language in the rows
	const stepNone = { value: 'none', label: 'None' };
	const stepPage = { value: 'page', label: 'Page' };
	const stepProxy = { value: 'proxy', label: 'Proxy' };
	$: landingKindOptions =
		isShown('blackbox', $displayMode) || formValues.landingPageType === 'proxy'
			? [stepPage, stepProxy]
			: [stepPage];
	// typed text that is not an option keeps the last pick, so a step is
	// never dropped by a half typed word
	/** @param {'beforeLandingKind'|'afterLandingKind'} field @param {string} kind */
	const setStepKind = (field, kind) => {
		if (kind !== 'none' && kind !== 'page') return;
		formValues[field] = kind;
	};
	/** @param {string} kind */
	const setLandingKind = (kind) => {
		if (kind !== 'page' && kind !== 'proxy') return;
		if (kind !== formValues.landingPageType) {
			formValues.landingPageType = kind;
			// the proxy is picked on the step, a page per language
			formValues.landingPage = '';
		}
	};
	// the page flow stages a language row has, rows fill the pages and share
	// the proxy
	$: languageStages = [
		...(formValues.beforeLandingKind === 'page'
			? [{ key: 'beforeLandingPage', label: 'Before Landing', map: beforeLandingPageMap }]
			: []),
		formValues.landingPageType === 'proxy'
			? { key: 'landingPage', label: 'Landing', proxy: true }
			: { key: 'landingPage', label: 'Landing', map: landingPageMap },
		...(formValues.afterLandingKind === 'page'
			? [{ key: 'afterLandingPage', label: 'After Landing', map: afterLandingPageMap }]
			: [])
	];
	// rows are keyed, so making a row the main one moves its fields with it
	let lastRowKey = 0;
	const nextRowKey = () => ++lastRowKey;
	const emptyLanguageRow = () => ({
		key: nextRowKey(),
		language: '',
		email: null,
		beforeLandingPage: null,
		landingPage: null,
		afterLandingPage: null
	});
	// the main row made from the template's own email and pages
	const mainRowFromTemplate = () => ({
		key: nextRowKey(),
		language: formValues.language || '',
		email: formValues.email || null,
		beforeLandingPage: formValues.beforeLandingPage || null,
		landingPage: formValues.landingPageType === 'page' ? formValues.landingPage || null : null,
		afterLandingPage: formValues.afterLandingPage || null
	});
	// turns Multiple languages on: the template's email and pages become the
	// main row, with a second row or the rows taken away earlier
	const startLanguages = () => {
		// a step is kept when the main or a language set aside has a page for it
		formValues.beforeLandingKind =
			formValues.beforeLandingPage || removedLanguageRows.some((row) => row.beforeLandingPage)
				? 'page'
				: 'none';
		formValues.afterLandingKind =
			formValues.afterLandingPage || removedLanguageRows.some((row) => row.afterLandingPage)
				? 'page'
				: 'none';
		const main = mainRowFromTemplate();
		if (!main.language && removedLanguageRows[0]?.language) {
			main.language = removedLanguageRows[0].language;
		}
		const others = removedLanguageRows.slice(1).filter((row) => row.language !== main.language);
		formValues.languages = [main, ...(others.length ? others : [emptyLanguageRow()])];
		removedLanguageRows = [];
		languagesError = '';
		if (formValues.landingPageType === 'page') {
			formValues.landingPage = '';
		}
		formValues.multiLanguage = true;
	};
	// turns Multiple languages off: the main row becomes the template's email
	// and pages and keeps its language, the other rows are kept aside
	const stopLanguages = () => {
		const [main, ...others] = formValues.languages;
		if (main) {
			formValues.language = main.language || formValues.language || '';
			formValues.email = main.email;
			formValues.beforeLandingPage =
				formValues.beforeLandingKind === 'page' ? main.beforeLandingPage : null;
			if (formValues.landingPageType === 'page') {
				formValues.landingPage = main.landingPage;
			}
			formValues.afterLandingPage =
				formValues.afterLandingKind === 'page' ? main.afterLandingPage : null;
		}
		removedLanguageRows = main ? [main, ...others] : [];
		formValues.languages = [];
		formValues.multiLanguage = false;
		languagesError = '';
	};
	// a row with nothing picked yet
	const isBlankRow = (row) =>
		!row.language &&
		!row.email &&
		!row.beforeLandingPage &&
		!row.landingPage &&
		!row.afterLandingPage;
	// the other languages that unchecking takes away
	$: removedLanguageNames = formValues.languages
		.slice(1)
		.filter((row) => row.language)
		.map((row) => languageName(row.language));
	// the main language that unchecking keeps
	$: keptLanguageName = formValues.languages[0]?.language
		? languageName(formValues.languages[0].language)
		: '';
	/** @param {string[]} names */
	const joinNames = (names) =>
		names.length > 1
			? `${names.slice(0, -1).join(', ')} and ${names[names.length - 1]}`
			: (names[0] ?? '');
	const onMultiLanguageChange = async (e) => {
		const checked = e.target.checked;
		// the binding writes the new value first, it is set again below
		await tick();
		if (checked) {
			startLanguages();
			return;
		}
		if (loadedWithLanguages || formValues.languages.slice(1).some((row) => !isBlankRow(row))) {
			formValues.multiLanguage = true;
			isRemoveLanguagesAlertVisible = true;
			return;
		}
		stopLanguages();
	};
	const addLanguage = () => {
		formValues.languages = [...formValues.languages, emptyLanguageRow()];
	};
	/** @param {number} i */
	const removeLanguage = (i) => {
		if (i === 0 || formValues.languages.length <= 2) return;
		const row = formValues.languages[i];
		if (!isBlankRow(row)) {
			droppedLanguageRows = [...droppedLanguageRows, row];
		}
		formValues.languages = formValues.languages.filter((_, j) => j !== i);
	};
	// puts back the removed rows a refusal names, or all of them when it
	// names none of them. Unchecked rows come back by checking again.
	/** @param {string} refusal */
	const restoreRemovedLanguages = (refusal) => {
		const named = new Set(
			(refusal.split(' can not be removed')[0] ?? '').split(',').map((name) => name.trim())
		);
		const pick = (rows) => {
			const hit = rows.filter((row) => named.has(languageName(row.language)));
			return hit.length ? hit : rows;
		};
		if (!formValues.multiLanguage && removedLanguageRows.length) {
			startLanguages();
		}
		const present = new Set(formValues.languages.map((row) => row.language));
		const back = pick(droppedLanguageRows).filter((row) => !present.has(row.language));
		if (back.length) {
			formValues.languages = [...formValues.languages, ...back];
			droppedLanguageRows = droppedLanguageRows.filter((row) => !back.includes(row));
			if (back.some((row) => row.beforeLandingPage)) formValues.beforeLandingKind = 'page';
			if (back.some((row) => row.afterLandingPage)) formValues.afterLandingKind = 'page';
		}
	};
	// the row becomes the main language and moves to the top
	/** @param {number} i */
	const makeMainLanguage = (i) => {
		if (i <= 0) return;
		const row = formValues.languages[i];
		formValues.languages = [row, ...formValues.languages.filter((_, j) => j !== i)];
		languagesError = '';
	};
	// a stage that is not a page sends no id
	const stagePageID = (row, key) => {
		const stage = languageStages.find((st) => st.key === key);
		if (!stage || stage.proxy) return null;
		return stage.map.byValueOrNull(row[key]);
	};
	const languagesPayload = () =>
		formValues.languages.slice(1).map((row) => ({
			language: row.language,
			emailID: emailMap.byValueOrNull(row.email),
			beforeLandingPageID: stagePageID(row, 'beforeLandingPage'),
			landingPageID: stagePageID(row, 'landingPage'),
			afterLandingPageID: stagePageID(row, 'afterLandingPage')
		}));
	// the email, pages and language fields of a create or update. With
	// Multiple languages they come from the rows. Without it a new template
	// sends no language, a loaded one keeps its language, and the language
	// rows are only sent, empty, when the template was loaded with rows.
	const languageFieldsPayload = () => {
		if (formValues.multiLanguage) {
			const main = formValues.languages[0] ?? emptyLanguageRow();
			return {
				emailID: emailMap.byValueOrNull(main.email),
				landingPageID:
					formValues.landingPageType === 'page'
						? landingPageMap.byValueOrNull(main.landingPage)
						: null,
				landingProxyID:
					formValues.landingPageType === 'proxy'
						? landingProxyMap.byValueOrNull(formValues.landingPage)
						: null,
				beforeLandingPageID: stagePageID(main, 'beforeLandingPage'),
				afterLandingPageID: stagePageID(main, 'afterLandingPage'),
				language: main.language || '',
				languages: languagesPayload()
			};
		}
		return {
			emailID: emailMap.byValueOrNull(formValues.email),
			landingPageID:
				formValues.landingPageType === 'page'
					? landingPageMap.byValueOrNull(formValues.landingPage)
					: null,
			landingProxyID:
				formValues.landingPageType === 'proxy'
					? landingProxyMap.byValueOrNull(formValues.landingPage)
					: null,
			beforeLandingPageID: beforeLandingPageMap.byValueOrNull(formValues.beforeLandingPage),
			afterLandingPageID: afterLandingPageMap.byValueOrNull(formValues.afterLandingPage),
			...(formValues.language ? { language: formValues.language } : {}),
			...(loadedWithLanguages ? { languages: [] } : {})
		};
	};
	// the stage names for the request fields a language refusal names
	const stageFieldLabels = {
		emailID: 'Email',
		beforeLandingPageID: 'Before Landing',
		landingPageID: 'Landing',
		afterLandingPageID: 'After Landing'
	};
	const withStageLabels = (text) =>
		text.replace(
			/\((emailID|beforeLandingPageID|landingPageID|afterLandingPageID)\)/g,
			(_, field) => `(${stageFieldLabels[field]})`
		);
	// a server refusal of the languages is shown in the Languages section, true
	// when it was one
	const showLanguageRefusal = async (error) => {
		const text = String(error ?? '');
		const refusal = /^languages?: (.+)$/s.exec(text);
		if (!refusal) return false;
		if (/ can not be removed /.test(refusal[1])) {
			restoreRemovedLanguages(refusal[1]);
		}
		languagesError = capitalizeFirst(withStageLabels(refusal[1]));
		await tick();
		document
			.getElementById('template-languages')
			?.scrollIntoView({ block: 'center', behavior: 'smooth' });
		return true;
	};

	// the language names of a listed template: its main language, then its
	// rows. A row that can not be sent yet is marked like in Template Details.
	// A template without rows has one language and shows none, its stored code
	// stays as sent recipients may carry it.
	const listLanguageNames = (template) =>
		template.languages?.length
			? [
					...(template.language ? [languageName(template.language)] : []),
					...template.languages
						.filter((l) => l.language)
						.map((l) => `${languageName(l.language)}${l.isUsable ? '' : ' (incomplete)'}`)
				]
			: [];
	// the title of the list cell marks the main language
	const listLanguageTitle = (template) =>
		listLanguageNames(template)
			.map((name, i) => (i === 0 && template.language ? `${name} (main)` : name))
			.join(', ');
	const capitalizeFirst = (text) => (text ? text[0].toUpperCase() + text.slice(1) : text);
	const tableURLParams = newTableURLParams();
	let isModalVisible = false;
	let isSubmitting = false;
	let isTableLoading = false;
	let modalMode = null;
	let modalText = '';
	let allowListingModal;
	let isDeleteAlertVisible = false;
	let deleteValues = {
		id: null,
		name: null
	};
	let showAdvancedOptions = false;

	$: {
		modalText = getModalText('template', modalMode);
	}

	// clear smtp or api sender when switching template type
	$: if (formValues.templateType === 'Email') {
		formValues.apiSender = null;
	} else if (formValues.templateType === 'External API') {
		formValues.smtpConfiguration = null;
	}

	// hooks
	onMount(() => {
		const context = appStateService.getContext();
		if (context) {
			contextCompanyID = context.companyID;
		}

		(async () => {
			showIsLoading();
			await Promise.all([
				refreshDomains(),
				refreshEmails(),
				refreshSmtpConfigurations(),
				refreshApiSenders(),
				refreshPages(),
				refreshProxies(),
				refreshIdentifiers(),
				getCampaignTemplates()
			]);
			tableURLParams.onChange(refreshCampaignTemplates);
			const editID = $page.url.searchParams.get('edit');
			if (editID) {
				await openUpdateModal(editID);
			}
			hideIsLoading();
		})();

		// cleanup resource context when leaving page
		return () => {
			tableURLParams.unsubscribe();
			resourceContext.clear();
		};
	});

	const refreshDomains = async () => {
		const allDomains = await fetchAllRows((options) => {
			return api.domain.getAllSubsetWithoutProxies(options, contextCompanyID);
		});
		domainMap = BiMap.FromArrayOfObjects(allDomains);
		// store full domain objects for type access
		domainObjectMap = new Map();
		allDomains.forEach((domain) => {
			domainObjectMap.set(domain.id, domain);
		});
	};

	const refreshEmails = async () => {
		const emails = await fetchAllRows((options) => {
			return api.email.getOverviews(options, contextCompanyID);
		});
		emailMap = BiMap.FromArrayOfObjects(emails);
	};

	const refreshSmtpConfigurations = async () => {
		const smtpConfigurations = await fetchAllRows((options) => {
			return api.smtpConfiguration.getAll(options, contextCompanyID);
		});
		smtpConfigurationMap = BiMap.FromArrayOfObjects(smtpConfigurations);
	};

	const refreshApiSenders = async () => {
		const apiSenders = await fetchAllRows((options) => {
			return api.apiSender.getAll(options, contextCompanyID);
		});
		apiSenderMap = BiMap.FromArrayOfObjects(apiSenders);
	};

	const refreshPages = async () => {
		const pages = await fetchAllRows((options) => {
			return api.page.getOverviews(options, contextCompanyID);
		});
		landingPageMap = BiMap.FromArrayOfObjects(pages);
		beforeLandingPageMap = BiMap.FromArrayOfObjects(pages);
		afterLandingPageMap = BiMap.FromArrayOfObjects(pages);
	};

	const refreshProxies = async () => {
		const proxies = await fetchAllRows((options) => {
			return api.proxy.getAllSubset(options, contextCompanyID);
		});
		landingProxyMap = BiMap.FromArrayOfObjects(proxies);
		beforeLandingProxyMap = BiMap.FromArrayOfObjects(proxies);
		afterLandingProxyMap = BiMap.FromArrayOfObjects(proxies);
	};

	const refreshIdentifiers = async () => {
		const identifiers = await fetchAllRows((options) => {
			return api.identifier.getAll(options);
		});
		identifierMap = BiMap.FromArrayOfObjects(identifiers);
	};

	// component logic

	const refreshCampaignTemplates = async () => {
		try {
			isTableLoading = true;
			selection.clear();
			await getCampaignTemplates();
		} finally {
			isTableLoading = false;
		}
	};
	const getCampaignTemplates = async () => {
		try {
			const result = await getTemplates();
			templates = result.rows;
			templatesHasNextPage = result.hasNextPage;
		} catch (e) {
			addToast('Failed to load campaign templates', 'Error');
			console.error('Failed to load campaign templates', e);
		}
	};

	/** @param {string} id */
	const getTemplate = async (id) => {
		try {
			const res = await api.campaignTemplate.getByID(id);
			if (!res.success) {
				throw res.error;
			}
			return res.data;
		} catch (e) {
			addToast('Failed to load campaign template', 'Error');
			console.error('failed to load campaign template', e);
		}
	};

	const getTemplates = async () => {
		try {
			const res = await api.campaignTemplate.getAll(tableURLParams, contextCompanyID);
			if (!res.success) {
				throw res.error;
			}
			return res.data;
		} catch (e) {
			addToast('Failed to load campaign templates', 'Error');
			console.error('Failed to load campaign templates', e);
		}
		return [];
	};

	const onSubmit = async () => {
		try {
			isSubmitting = true;
			if (modalMode === 'create' || modalMode === 'copy') {
				await create();
				return;
			} else {
				await update();
				return;
			}
		} finally {
			isSubmitting = false;
		}
	};

	const create = async () => {
		try {
			const res = await api.campaignTemplate.create({
				name: formValues.name,
				domainID: domainMap.byValue(formValues.domain),
				smtpConfigurationID: smtpConfigurationMap.byValueOrNull(formValues.smtpConfiguration),
				apiSenderID: apiSenderMap.byValueOrNull(formValues.apiSender),
				beforeLandingProxyID: null,
				afterLandingProxyID: null,
				...languageFieldsPayload(),
				afterLandingPageRedirectURL: formValues.afterLandingPageRedirectURL || '',
				urlIdentifierID: identifierMap.byValueOrNull(formValues.urlIdentifier),
				stateIdentifierID: identifierMap.byValueOrNull(formValues.stateIdentifier),
				urlPath: formValues.urlPath || '',
				lureURLMode: formValues.lureURLMode,
				lureCodeAlgo: formValues.lureCodeAlgo,
				lureCodeLength: clampLureCodeLength(formValues.lureCodeLength),
				isTraining: formValues.isTraining,
				companyID: contextCompanyID
			});
			if (!res.success) {
				if (await showLanguageRefusal(res.error)) {
					return;
				}
				modalError = res.error;
				return;
			}
			addToast('Campaign template created', 'Success');
			closeModal();
			refreshCampaignTemplates();
		} catch (err) {
			addToast('Failed to create campaign template', 'Error');
			console.error('failed to create campaign template:', err);
		}
	};

	const update = async () => {
		try {
			const res = await api.campaignTemplate.update({
				id: formValues.id,
				name: formValues.name,
				domainID: domainMap.byValueOrNull(formValues.domain),
				smtpConfigurationID: smtpConfigurationMap.byValueOrNull(formValues.smtpConfiguration),
				apiSenderID: apiSenderMap.byValueOrNull(formValues.apiSender),
				beforeLandingProxyID: null,
				afterLandingProxyID: null,
				...languageFieldsPayload(),
				afterLandingPageRedirectURL: formValues.afterLandingPageRedirectURL || '',
				urlIdentifierID: identifierMap.byValueOrNull(formValues.urlIdentifier),
				stateIdentifierID: identifierMap.byValueOrNull(formValues.stateIdentifier),
				urlPath: formValues.urlPath || '',
				lureURLMode: formValues.lureURLMode,
				lureCodeAlgo: formValues.lureCodeAlgo,
				lureCodeLength: clampLureCodeLength(formValues.lureCodeLength),
				isTraining: formValues.isTraining
			});
			if (!res.success) {
				if (await showLanguageRefusal(res.error)) {
					return;
				}
				// a refused training change names the program, it shows on the checkbox
				const training = /^isTraining: (.+)$/.exec(String(res.error ?? ''));
				if (training) {
					trainingError = capitalizeFirst(training[1]);
					await tick();
					document
						.getElementById('isTraining')
						?.scrollIntoView({ block: 'center', behavior: 'smooth' });
					return;
				}
				modalError = res.error;
				return;
			}
			addToast('Campaign template updated', 'Success');
			closeModal();
			refreshCampaignTemplates();
		} catch (e) {
			addToast('Failed to update campaign template', 'Error');
			console.error('failed to update campaign template', e);
		}
	};

	const openDeleteAlert = async (domain) => {
		isDeleteAlertVisible = true;
		deleteValues.id = domain.id;
		deleteValues.name = domain.name;
	};

	/** @param {string} id */
	const onClickDelete = async (id) => {
		const action = api.campaignTemplate.delete(id);
		action
			.then((res) => {
				if (!res.success) {
					throw res.error;
				}
				refreshCampaignTemplates();
			})
			.catch((e) => {
				console.error('failed to delete campaign template:', e);
			});
		return action;
	};

	const openCreateModal = () => {
		modalMode = 'create';
		isModalVisible = true;
	};

	const closeModal = () => {
		isModalVisible = false;
		formValues = {
			id: null,
			templateType: 'Email',
			name: null,
			domain: null,
			landingPage: null,
			landingPageType: 'page', // 'page' or 'proxy'
			beforeLandingPage: null,
			afterLandingPage: null,
			afterLandingPageRedirectURL: '',
			email: null,
			smtpConfiguration: null,
			apiSender: null,
			urlIdentifier: 'id',
			stateIdentifier: 'session',
			urlPath: '',
			lureURLMode: 'query',
			lureCodeAlgo: 'crockford32',
			lureCodeLength: 12,
			isTraining: false,
			language: '',
			languages: [],
			multiLanguage: false,
			beforeLandingKind: 'none',
			afterLandingKind: 'none'
		};
		modalError = '';
		trainingError = '';
		languagesError = '';
		removedLanguageRows = [];
		droppedLanguageRows = [];
		loadedWithLanguages = false;
		isRemoveLanguagesAlertVisible = false;
		showAdvancedOptions = false;
		// clear resource context when closing modal
		resourceContext.clear();
	};

	/** @param {string} id */
	const openUpdateModal = async (id) => {
		modalMode = 'update';
		try {
			showIsLoading();
			const template = await getTemplate(id);
			const r = globalButtonDisabledAttributes(template, contextCompanyID);
			if (r.disabled) {
				hideIsLoading();
				resourceContext.clear();
				return;
			}

			assignTemplate(template);

			// if company exists but name is missing, fetch it
			if (template.companyID && !template.company?.name) {
				try {
					const companyRes = await api.company.getByID(template.companyID);
					if (companyRes.success && companyRes.data) {
						template.company = companyRes.data;
					}
				} catch (e) {
					console.error('failed to load company details', e);
				}
			}

			// set resource context for banner
			resourceContext.setResource('template', template.companyID, template.company?.name);
			isModalVisible = true;
		} catch (e) {
			addToast('Failed to load campaign template', 'Error');
			console.error('failed to load campaign template', e);
		} finally {
			hideIsLoading();
		}
	};

	const openCopyModal = async (id) => {
		modalMode = 'copy';
		try {
			showIsLoading();
			const template = await getTemplate(id);
			assignTemplate(template);
			formValues.id = null;

			// if company exists but name is missing, fetch it
			if (template.companyID && !template.company?.name) {
				try {
					const companyRes = await api.company.getByID(template.companyID);
					if (companyRes.success && companyRes.data) {
						template.company = companyRes.data;
					}
				} catch (e) {
					console.error('failed to load company details', e);
				}
			}

			// set resource context for banner
			resourceContext.setResource('template', template.companyID, template.company?.name);
			isModalVisible = true;
		} catch (e) {
			addToast('Failed to load campaign template', 'Error');
			console.error('failed to load campaign template', e);
		} finally {
			hideIsLoading();
		}
	};

	const assignTemplate = (template) => {
		formValues.id = template.id;
		formValues.name = template.name;
		formValues.smtpConfiguration = smtpConfigurationMap.byKey(template.smtpConfigurationID);
		formValues.apiSender = apiSenderMap.byKey(template.apiSenderID);
		if (template.smtpConfigurationID) {
			formValues.templateType = 'Email';
		} else {
			formValues.templateType = 'External API';
		}
		formValues.domain = domainMap.byKey(template.domainID);
		formValues.email = emailMap.byKey(template.emailID);

		// handle landing page (page or proxy)
		if (template.landingPageID) {
			formValues.landingPage = landingPageMap.byKey(template.landingPageID);
			formValues.landingPageType = 'page';
		} else if (template.landingProxyID) {
			formValues.landingPage = landingProxyMap.byKey(template.landingProxyID);
			formValues.landingPageType = 'proxy';
		}

		// handle before landing page (only pages, no proxy)
		if (template.beforeLandingPageID) {
			formValues.beforeLandingPage = beforeLandingPageMap.byKey(template.beforeLandingPageID);
		}

		// handle after landing page (only pages, no proxy)
		if (template.afterLandingPageID) {
			formValues.afterLandingPage = afterLandingPageMap.byKey(template.afterLandingPageID);
		}

		formValues.afterLandingPageRedirectURL = template.afterLandingPageRedirectURL || '';
		formValues.urlIdentifier = identifierMap.byKey(template.urlIdentifierID);
		formValues.stateIdentifier = identifierMap.byKey(template.stateIdentifierID);
		formValues.urlPath = template.urlPath || '';
		formValues.lureURLMode = template.lureURLMode || 'query';
		formValues.lureCodeAlgo = template.lureCodeAlgo || 'crockford32';
		formValues.lureCodeLength = template.lureCodeLength || 12;
		formValues.isTraining = !!template.isTraining;
		formValues.language = template.language || '';
		// a template with language rows opens with Multiple languages, its own
		// email and pages as the main row
		const rows = (template.languages ?? []).map((row) => ({
			key: nextRowKey(),
			language: row.language,
			email: emailMap.byKey(row.emailID) ?? null,
			beforeLandingPage: beforeLandingPageMap.byKey(row.beforeLandingPageID) ?? null,
			landingPage: landingPageMap.byKey(row.landingPageID) ?? null,
			afterLandingPage: afterLandingPageMap.byKey(row.afterLandingPageID) ?? null
		}));
		loadedWithLanguages = rows.length > 0;
		removedLanguageRows = [];
		droppedLanguageRows = [];
		formValues.languages = [];
		formValues.multiLanguage = false;
		if (rows.length) {
			startLanguages();
			formValues.languages = [formValues.languages[0], ...rows];
			// a step is kept when the main or any language has a page for it, so
			// the pages of the languages are not dropped on the next save. A
			// missing main page shows as an empty required pick.
			if (rows.some((row) => row.beforeLandingPage)) {
				formValues.beforeLandingKind = 'page';
			}
			if (rows.some((row) => row.afterLandingPage)) {
				formValues.afterLandingKind = 'page';
			}
		}

		// set advanced options visibility based on template configuration
		showAdvancedOptions = !!(
			(
				(template.urlPath && template.urlPath !== '') ||
				(template.afterLandingPageRedirectURL && template.afterLandingPageRedirectURL !== '') ||
				(template.urlIdentifierID && identifierMap.byKey(template.urlIdentifierID) !== 'id') ||
				(template.stateIdentifierID &&
					identifierMap.byKey(template.stateIdentifierID) !== 'session') ||
				(template.lureURLMode && template.lureURLMode !== 'query') ||
				template.apiSenderID
			) // Show advanced if using External API
		);
	};
</script>

<HeadTitle title="Templates" />

<main>
	<Headline docSlug="templates">Templates</Headline>
	<BigButton on:click={openCreateModal}>New template</BigButton>

	<BulkActionBar
		count={$selection.size}
		noun="template"
		on:delete={() => (isBulkDeleteAlertVisible = true)}
		on:clear={() => selection.clear()}
	/>
	<Table
		selectable
		{headerState}
		on:toggleAll={(e) => selection.setPageSelection(selectablePageIds, e.detail)}
		columns={[
			{ column: 'Name', size: 'large' },
			{ column: 'Domain', size: 'medium' },
			{ column: 'Email', size: 'medium' },
			{ column: 'Languages', size: 'medium' },
			{ column: 'Landing Page', title: 'Landing', size: 'medium' },
			{ column: 'SMTP', size: 'small' },
			{ column: 'API Sender', size: 'small' },
			{ column: 'Before Landing Page', title: 'Before page', size: 'small' },
			{ column: 'After Landing Page', title: 'After page', size: 'small' },
			{ column: 'After landing page redirect URL', title: 'Redirect URL', size: 'small' },
			{ column: 'Is complete', size: 'small', alignText: 'center' },
			...(contextCompanyID ? [{ column: 'Scope', size: 'small' }] : [])
		]}
		sortable={[
			'Name',
			'Domain',
			'Email',
			'Landing Page',
			'SMTP',
			'API Sender',
			'Before Landing Page',
			'After Landing Page',
			'After landing page redirect URL',
			'Is complete',
			...(contextCompanyID ? ['scope'] : [])
		]}
		hasData={!!templates.length}
		hasNextPage={templatesHasNextPage}
		plural="templates"
		pagination={tableURLParams}
		isGhost={isTableLoading}
	>
		{#each templates as template}
			<TableRow>
				{#if showMultiSelect}
					<TableCellCheckbox
						checked={$selection.has(template.id)}
						disabled={isRowDisabled(template)}
						on:change={() => selection.toggle(template.id)}
					/>
				{/if}
				<TableCell>
					<button
						on:click={() => openUpdateModal(template.id)}
						{...globalButtonDisabledAttributes(template, contextCompanyID)}
						title={template.name}
						class="block w-full py-1 text-left"
					>
						<span class="flex items-center gap-2 min-w-0">
							<span class="truncate">{template.name}</span>
							{#if template.isTraining}
								<Tag title="Awareness training">Training</Tag>
							{/if}
						</span>
					</button>
				</TableCell>
				<TableCell>
					{#if template.domainID}
						<a
							href={`/domain/?edit=${template.domainID}`}
							class="block w-full py-1"
							title={domainMap.byKey(template.domainID)}
						>
							{domainMap.byKey(template.domainID)}
						</a>
					{:else}
						<span class="text-slate-400 dark:text-gray-500">-</span>
					{/if}
				</TableCell>
				<TableCell>
					{#if template.emailID}
						<a
							href={`/email/?edit=${template.emailID}`}
							class="block w-full py-1"
							title={emailMap.byKey(template.emailID)}
						>
							{emailMap.byKey(template.emailID)}
						</a>
					{:else}
						<span class="text-slate-400 dark:text-gray-500">-</span>
					{/if}
				</TableCell>
				<TableCell>
					{#if listLanguageNames(template).length}
						<span class="block w-full py-1" title={listLanguageTitle(template)}
							>{listLanguageNames(template).join(', ')}</span
						>
					{:else}
						<span class="text-slate-400 dark:text-gray-500">-</span>
					{/if}
				</TableCell>
				<TableCell>
					{#if template.landingPageID}
						<a
							href={`/page/?edit=${template.landingPageID}`}
							class="block w-full py-1"
							title={landingPageMap.byKey(template.landingPageID)}
						>
							{landingPageMap.byKey(template.landingPageID)}
						</a>
					{:else if template.landingProxyID}
						<a
							href={`/proxy/?edit=${template.landingProxyID}`}
							class="block w-full py-1"
							title={landingProxyMap.byKey(template.landingProxyID)}
						>
							<span class="flex items-center gap-1 min-w-0">
								<span class="shrink-0"><ProxySvgIcon size="w-4 h-4" /></span>
								<span class="truncate">{landingProxyMap.byKey(template.landingProxyID)}</span>
							</span>
						</a>
					{:else}
						<span class="text-slate-400 dark:text-gray-500">-</span>
					{/if}
				</TableCell>
				<TableCell>
					{#if template.smtpConfigurationID}
						<a
							href={`/smtp-configuration/?edit=${template.smtpConfigurationID}`}
							class="block w-full py-1"
							title={smtpConfigurationMap.byKey(template.smtpConfigurationID)}
						>
							{smtpConfigurationMap.byKey(template.smtpConfigurationID)}
						</a>
					{:else}
						<span class="text-slate-400 dark:text-gray-500">-</span>
					{/if}
				</TableCell>
				<TableCell>
					{#if template.apiSenderID}
						<a
							href={`/api-sender/?edit=${template.apiSenderID}`}
							class="block w-full py-1"
							title={apiSenderMap.byKey(template.apiSenderID)}
						>
							{apiSenderMap.byKey(template.apiSenderID)}
						</a>
					{:else}
						<span class="text-slate-400 dark:text-gray-500">-</span>
					{/if}
				</TableCell>
				<TableCell>
					{#if template.beforeLandingPageID}
						<a
							href={`/page/?edit=${template.beforeLandingPageID}`}
							class="block w-full py-1"
							title={beforeLandingPageMap.byKey(template.beforeLandingPageID)}
						>
							{beforeLandingPageMap.byKey(template.beforeLandingPageID)}
						</a>
					{:else if template.beforeLandingProxyID}
						<a
							href={`/proxy/?edit=${template.beforeLandingProxyID}`}
							class="block w-full py-1"
							title={beforeLandingProxyMap.byKey(template.beforeLandingProxyID)}
						>
							<span class="flex items-center gap-1 min-w-0">
								<span class="shrink-0"><ProxySvgIcon size="w-4 h-4" /></span>
								<span class="truncate"
									>{beforeLandingProxyMap.byKey(template.beforeLandingProxyID)}</span
								>
							</span>
						</a>
					{:else}
						<span class="text-slate-400 dark:text-gray-500">-</span>
					{/if}
				</TableCell>
				<TableCell>
					{#if template.afterLandingPageID}
						<a
							href={`/page/?edit=${template.afterLandingPageID}`}
							class="block w-full py-1"
							title={afterLandingPageMap.byKey(template.afterLandingPageID)}
						>
							{afterLandingPageMap.byKey(template.afterLandingPageID)}
						</a>
					{:else if template.afterLandingProxyID}
						<a
							href={`/proxy/?edit=${template.afterLandingProxyID}`}
							class="block w-full py-1"
							title={afterLandingProxyMap.byKey(template.afterLandingProxyID)}
						>
							<span class="flex items-center gap-1 min-w-0">
								<span class="shrink-0"><ProxySvgIcon size="w-4 h-4" /></span>
								<span class="truncate"
									>{afterLandingProxyMap.byKey(template.afterLandingProxyID)}</span
								>
							</span>
						</a>
					{:else}
						<span class="text-slate-400 dark:text-gray-500">-</span>
					{/if}
				</TableCell>
				<TableCell>
					{#if template.afterLandingPageRedirectURL}
						{@const redirectHref = safeExternalHref(template.afterLandingPageRedirectURL)}
						{#if redirectHref}
							<a
								href={redirectHref}
								target="_blank"
								rel="noopener noreferrer"
								class="block w-full py-1"
								title={template.afterLandingPageRedirectURL}
							>
								{template.afterLandingPageRedirectURL}
							</a>
						{:else}
							<span class="block w-full py-1" title={template.afterLandingPageRedirectURL}>
								{template.afterLandingPageRedirectURL}
							</span>
						{/if}
					{:else}
						<span class="text-slate-400 dark:text-gray-500">-</span>
					{/if}
				</TableCell>
				<TableCellCheck value={template.isUsable} />
				{#if contextCompanyID}
					<TableCellScope companyID={template.companyID} />
				{/if}
				<TableCellEmpty />
				<TableCellAction>
					<TableDropDownEllipsis>
						<TableUpdateButton
							on:click={() => openUpdateModal(template.id)}
							{...globalButtonDisabledAttributes(template, contextCompanyID)}
						/>
						<TableCopyButton title={'Copy'} on:click={() => openCopyModal(template.id)} />
						<TableDropDownButton
							name="Allow listing"
							on:click={() => allowListingModal.open(template.id, template.name)}
						/>
						<TableDeleteButton
							on:click={() => openDeleteAlert(template)}
							{...globalButtonDisabledAttributes(template, contextCompanyID)}
						></TableDeleteButton>
					</TableDropDownEllipsis>
				</TableCellAction>
			</TableRow>
		{/each}
	</Table>

	<AllowListingModal
		bind:this={allowListingModal}
		kind="template"
		onEditTemplate={(id) => openUpdateModal(id)}
	/>

	<Modal headerText={modalText} visible={isModalVisible} onClose={closeModal} {isSubmitting}>
		{#if modalMode === 'update'}
			<InstalledResourceBadge
				resourceType="campaigntemplate"
				resourceId={formValues.id}
				variant="notice"
			/>
		{/if}
		<FormGrid on:submit={onSubmit} bind:bindTo={form} {isSubmitting}>
			<div class="w-full lg:w-[56rem] max-w-full py-6 space-y-8">
				<!-- Basic Information Section -->
				<div class="w-full">
					<h3
						class="text-base font-semibold text-pc-darkblue dark:text-white mb-3 pb-2 border-b border-gray-200 dark:border-gray-700 transition-colors duration-200"
					>
						Basic Information
					</h3>
					<div class="grid grid-cols-1 md:grid-cols-2 gap-x-6">
						<div class="md:max-w-xs">
							<TextField
								required
								minLength={1}
								maxLength={64}
								width="full"
								bind:value={formValues.name}>Name</TextField
							>
						</div>
						<div class="md:max-w-xs">
							<TextFieldSelect
								required
								id="domain"
								size="full"
								bind:value={formValues.domain}
								options={domainMap.values()}>Domain</TextFieldSelect
							>
						</div>
					</div>
					<CheckboxField
						id="isTraining"
						inline
						bind:value={formValues.isTraining}
						toolTipText="Tracks training events instead of phishing events."
					>
						Awareness training
					</CheckboxField>
					{#if trainingError}
						<div class="text-red-600 dark:text-red-400 text-sm" role="alert">{trainingError}</div>
					{/if}
				</div>

				<!-- Delivery Configuration Section -->
				<!-- Email/API Configuration -->
				<div class="w-full">
					<h3
						class="text-base font-semibold text-pc-darkblue dark:text-white mb-3 pb-2 border-b border-gray-200 dark:border-gray-700 transition-colors duration-200"
					>
						Email Configuration
					</h3>
					<div class="grid grid-cols-1 md:grid-cols-2 gap-x-6">
						{#if formValues.templateType === 'Email' || !formValues.templateType}
							<div class="md:max-w-xs">
								<TextFieldSelect
									required
									id="smtpConfiguration"
									size="full"
									bind:value={formValues.smtpConfiguration}
									options={smtpConfigurationMap.values()}>SMTP Configuration</TextFieldSelect
								>
							</div>
						{:else if formValues.templateType === 'External API'}
							<div class="md:max-w-xs">
								<TextFieldSelect
									required
									id="apiSender"
									size="full"
									bind:value={formValues.apiSender}
									options={apiSenderMap.values()}>API Sender</TextFieldSelect
								>
							</div>
						{/if}
						{#if !formValues.multiLanguage}
							<div class="md:max-w-xs">
								<TextFieldSelect
									id="email"
									size="full"
									bind:value={formValues.email}
									options={emailMap.values()}>Email</TextFieldSelect
								>
							</div>
						{:else}
							<!-- in the place of the Email field, on its label line -->
							<div class="md:max-w-xs py-2" data-email-per-language>
								<Note class="py-2">Email is picked per language under Languages.</Note>
							</div>
						{/if}
					</div>
				</div>

				<!-- Page Flow Section -->
				<div class="w-full">
					<h3
						class="text-base font-semibold text-pc-darkblue dark:text-white mb-3 pb-2 border-b border-gray-200 dark:border-gray-700 transition-colors duration-200"
					>
						Page Flow
					</h3>
					<div class="grid grid-cols-1 md:grid-cols-5 gap-6">
						<div class="md:col-span-2 flex flex-col w-full md:max-w-xs">
							{#if formValues.multiLanguage}
								<!-- the steps are picked once, the pages per language below -->
								<TextFieldSelect
									id="beforeLandingKind"
									size="full"
									required
									toolTipText="Pick the page of each language under Languages."
									value={formValues.beforeLandingKind}
									onSelect={(kind) => setStepKind('beforeLandingKind', kind)}
									options={[stepNone, stepPage]}>Before Landing</TextFieldSelect
								>
								<TextFieldSelect
									id="landingKind"
									size="full"
									required
									toolTipText="A proxy is the same in every language."
									value={formValues.landingPageType}
									onSelect={setLandingKind}
									options={landingKindOptions}>Landing</TextFieldSelect
								>
								{#if formValues.landingPageType === 'proxy'}
									<TextFieldSelect
										id="landingProxy"
										size="full"
										required
										bind:value={formValues.landingPage}
										options={landingProxyMap.values()}>Landing Proxy</TextFieldSelect
									>
								{/if}
								<TextFieldSelect
									id="afterLandingKind"
									size="full"
									required
									toolTipText="Pick the page of each language under Languages."
									value={formValues.afterLandingKind}
									onSelect={(kind) => setStepKind('afterLandingKind', kind)}
									options={[stepNone, stepPage]}>After Landing</TextFieldSelect
								>
							{:else}
								<!-- Before Landing Page -->
								<TextFieldSelect
									id="beforeLandingPage"
									bind:value={formValues.beforeLandingPage}
									options={beforeLandingPageMap.values()}
									size="full"
									optional>Before Landing</TextFieldSelect
								>

								<ConditionalDisplay show="blackbox">
									<!-- Landing Page -->
									<TextFieldSelectWithType
										id="landingPage"
										bind:value={formValues.landingPage}
										bind:type={formValues.landingPageType}
										pageOptions={landingPageMap.values()}
										proxyOptions={landingProxyMap.values()}
										required>Landing</TextFieldSelectWithType
									>
								</ConditionalDisplay>
								<ConditionalDisplay show="whitebox">
									<TextFieldSelect
										id="landingPage"
										bind:value={formValues.landingPage}
										options={landingPageMap.values()}
										size="full"
										required>Landing</TextFieldSelect
									>
								</ConditionalDisplay>

								<!-- After Landing Page -->
								<TextFieldSelect
									id="afterLandingPage"
									bind:value={formValues.afterLandingPage}
									options={afterLandingPageMap.values()}
									size="full"
									optional>After Landing</TextFieldSelect
								>
							{/if}
						</div>

						<div
							class="md:col-span-3 md:border-l md:border-gray-200 md:dark:border-gray-700 md:pl-8 flex items-center"
						>
							{#if formValues.multiLanguage}
								<PageFlow
									direction="vertical"
									deliveryType={formValues.templateType === 'External API' ? 'api' : 'email'}
									deliveryName={formValues.templateType === 'External API'
										? formValues.apiSender
										: 'Per language'}
									beforeName={formValues.beforeLandingKind === 'page' ? 'Per language' : ''}
									landingName={formValues.landingPageType === 'proxy'
										? formValues.landingPage
										: 'Per language'}
									landingIsProxy={formValues.landingPageType === 'proxy'}
									afterName={formValues.afterLandingKind === 'page' ? 'Per language' : ''}
									redirectURL={formValues.afterLandingPageRedirectURL}
								/>
							{:else}
								<PageFlow
									direction="vertical"
									deliveryType={formValues.templateType === 'External API' ? 'api' : 'email'}
									deliveryName={formValues.templateType === 'External API'
										? formValues.apiSender
										: formValues.email}
									beforeName={formValues.beforeLandingPage}
									landingName={formValues.landingPage}
									landingIsProxy={formValues.landingPageType === 'proxy'}
									afterName={formValues.afterLandingPage}
									redirectURL={formValues.afterLandingPageRedirectURL}
								/>
							{/if}
						</div>
					</div>
				</div>

				<!-- Languages Section: every language is a row, the first is the main language -->
				<div class="w-full" id="template-languages">
					<h3
						class="text-base font-semibold text-pc-darkblue dark:text-white mb-3 pb-2 border-b border-gray-200 dark:border-gray-700 transition-colors duration-200"
					>
						Languages
					</h3>
					<CheckboxField
						id="templateMultiLanguage"
						inline
						bind:value={formValues.multiLanguage}
						on:change={onMultiLanguageChange}
						toolTipText="Send the email and pages in more than one language. Each campaign decides who gets which language, using a recipient field such as Language or Country."
					>
						Multiple languages
					</CheckboxField>
					{#if formValues.multiLanguage}
						<!-- one block per language: the language and its actions on the first
						line, its email and pages under it with full width selects, so long
						names stay readable -->
						<ol class="flex flex-col pt-2">
							{#each formValues.languages as row, i (row.key)}
								<li
									class="flex flex-col gap-1 border-gray-200 dark:border-gray-700"
									class:pt-4={i > 0}
									class:mt-4={i > 0}
									class:border-t={i > 0}
								>
									<div class="flex items-center gap-2">
										<div class="w-40 sm:w-48 flex-shrink-0">
											<TextFieldSelect
												id={`template-language-${row.key}`}
												hideLabel
												size="full"
												required
												placeholder="Language"
												bind:value={row.language}
												onSelect={() => (languagesError = '')}
												options={rowLanguageOptions(i)}
											/>
										</div>
										<div class="flex-1 min-w-0">
											{#if i === 0}
												<span
													id="template-main-language"
													class="block py-2 text-sm text-slate-500 dark:text-gray-400"
													title="Recipients scheduled from now on get the main language when a campaign does not use Language by. Recipients already scheduled keep theirs."
													>Main language</span
												>
											{:else}
												<button
													type="button"
													class="pc-focus rounded-sm text-sm font-medium text-cta-blue dark:text-highlight-blue hover:underline"
													title="Recipients scheduled from now on get the main language when a campaign does not use Language by. Recipients already scheduled keep theirs."
													aria-label={`Make ${row.language ? languageName(row.language) : `language ${i + 1}`} the main language`}
													on:click={() => makeMainLanguage(i)}>Make main</button
												>
											{/if}
										</div>
										<button
											type="button"
											class={rowButtonClass}
											title={i === 0
												? 'Make another language the main one to remove this one'
												: formValues.languages.length <= 2
													? 'A template with Multiple languages has at least two'
													: 'Remove this language'}
											aria-label={`Remove ${row.language ? languageName(row.language) : `language ${i + 1}`}`}
											disabled={i === 0 || formValues.languages.length <= 2}
											on:click={() => removeLanguage(i)}
										>
											<img class="w-4 flex-shrink-0" src="/delete2.svg" alt="" />
										</button>
									</div>
									<div class="grid grid-cols-1 md:grid-cols-2 gap-x-6">
										<div class="min-w-0">
											<TextFieldSelect
												id={`template-language-${row.key}-email`}
												size="full"
												required
												bind:value={row.email}
												onSelect={() => (languagesError = '')}
												options={emailMap.values()}>Email</TextFieldSelect
											>
										</div>
										{#each languageStages as stage (stage.key)}
											<div class="min-w-0">
												{#if stage.proxy}
													<FieldLabel>{stage.label}</FieldLabel>
													<span
														class="block py-2 text-sm text-slate-500 dark:text-gray-400"
														title="Proxy stages are the same in every language">Shared proxy</span
													>
												{:else}
													<TextFieldSelect
														id={`template-language-${row.key}-${stage.key}`}
														size="full"
														required
														bind:value={row[stage.key]}
														onSelect={() => (languagesError = '')}
														options={stage.map.values()}>{stage.label}</TextFieldSelect
													>
												{/if}
											</div>
										{/each}
									</div>
								</li>
							{/each}
						</ol>
						<div class="pt-3">
							<button
								id="template-add-language"
								type="button"
								class={addButtonClass}
								disabled={formValues.languages.length >= LANGUAGES.length}
								on:click={addLanguage}
							>
								+ Add Language
							</button>
						</div>
					{/if}
					{#if languagesError}
						<div class="pt-1 text-red-600 dark:text-red-400 text-sm" role="alert">
							{languagesError}
						</div>
					{/if}
				</div>

				<FormError message={modalError} />

				<!-- Advanced Options Section -->
				{#if !showAdvancedOptions}
					<div>
						<button
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
				{/if}

				{#if showAdvancedOptions}
					<div class="w-full">
						<h3
							class="text-base font-semibold text-pc-darkblue dark:text-white mb-3 pb-2 border-b border-gray-200 dark:border-gray-700 transition-colors duration-200"
						>
							Advanced Options
						</h3>
						<div class="grid grid-cols-1 md:grid-cols-2 gap-6">
							<div>
								<SelectSquare
									label="Delivery Type"
									width="small"
									center={false}
									options={[
										{ value: 'Email', label: 'Email' },
										{ value: 'External API', label: 'External API' }
									]}
									bind:value={formValues.templateType}
								/>
							</div>
							<div>
								<TextField
									toolTipText="Path after the domain name."
									optional
									minLength={1}
									maxLength={1024}
									bind:value={formValues.urlPath}
									placeholder="/employee/login">URL Path</TextField
								>
							</div>
							<div>
								<SelectSquare
									label="Lure URL format"
									width="small"
									center={false}
									options={[
										{ value: 'query', label: 'Query parameter' },
										{ value: 'path', label: 'Path code' }
									]}
									bind:value={formValues.lureURLMode}
								/>
								<p class="mt-2 text-sm text-gray-600 dark:text-gray-300 break-all">
									{lureURLExample(formValues)}
								</p>
							</div>
							{#if formValues.lureURLMode === 'path'}
								<div>
									<TextFieldSelect
										id="lureCodeAlgo"
										toolTipText="Symbol set the generated code is drawn from."
										required
										bind:value={formValues.lureCodeAlgo}
										options={Object.entries(LURE_CODE_ALGOS).map(([value, spec]) => ({
											value,
											label: spec.label
										}))}>Code format</TextFieldSelect
									>
									{#if LURE_CODE_ALGOS[formValues.lureCodeAlgo]}
										<p class="mt-1 text-sm text-gray-600 dark:text-gray-300">
											{LURE_CODE_ALGOS[formValues.lureCodeAlgo].note}
										</p>
									{/if}
								</div>
								<div>
									<TextField
										type="number"
										min={6}
										max={16}
										toolTipText="Number of characters in the generated code, between 6 and 16."
										bind:value={formValues.lureCodeLength}
										placeholder="12">Code length (6 to 16)</TextField
									>
									{#if lureCodeKeyspace(formValues.lureCodeAlgo, Number(formValues.lureCodeLength))}
										<p class="mt-1 text-sm text-gray-600 dark:text-gray-300">
											{lureCodeKeyspace(formValues.lureCodeAlgo, Number(formValues.lureCodeLength))}
										</p>
									{/if}
								</div>
							{/if}
							<div>
								<TextFieldSelect
									id="urlIdentifier"
									toolTipText="Query param key carrying the recipient. With the path code format the delivered link does not use it, but the pages after the first click still do."
									required
									bind:value={formValues.urlIdentifier}
									options={identifierMap.values()}>Query param key</TextFieldSelect
								>
							</div>
							<div>
								<TextFieldSelect
									id="stateIdentifier"
									toolTipText="Query param key carrying page flow state. Always a query param, including with the path code format."
									required
									bind:value={formValues.stateIdentifier}
									options={identifierMap.values()}>State param key</TextFieldSelect
								>
							</div>
							<div>
								<TextField
									bind:value={formValues.afterLandingPageRedirectURL}
									type="url"
									minLength={1}
									maxLength={255}
									placeholder="https://example.com/u-been-phished">POST redirect URL</TextField
								>
							</div>
						</div>
					</div>
				{/if}
			</div>
			<FormFooter {closeModal} {isSubmitting} />
		</FormGrid>
	</Modal>

	<DeleteAlert
		list={[
			'Scheduled or active campaigns using this template will be closed',
			'A template used by an open program cannot be deleted'
		]}
		name={deleteValues.name}
		onClick={() => onClickDelete(deleteValues.id)}
		confirm
		bind:isVisible={isDeleteAlertVisible}
	></DeleteAlert>
	<Alert
		headline="Remove languages"
		ok="Remove"
		bind:visible={isRemoveLanguagesAlertVisible}
		onConfirm={async () => {
			stopLanguages();
			return { success: true };
		}}
	>
		<p>
			{#if removedLanguageNames.length}
				{joinNames(removedLanguageNames)}
				{removedLanguageNames.length === 1 ? 'is' : 'are'} removed when you save.
			{:else}
				The other languages are removed when you save.
			{/if}
			{keptLanguageName
				? `${keptLanguageName} stays as the template's language.`
				: "The main language stays as the template's language."}
		</p>
		{#if modalMode === 'update'}
			<p class="mt-2">Saving fails while campaigns or programs use a removed language.</p>
		{/if}
	</Alert>
	<DeleteAlert
		title="Delete templates"
		list={[
			'Scheduled or active campaigns using these templates will be closed',
			'Templates used by an open program cannot be deleted'
		]}
		name={`${$selection.size} template${$selection.size === 1 ? '' : 's'}`}
		onClick={onClickBulkDelete}
		confirm
		bind:isVisible={isBulkDeleteAlertVisible}
	></DeleteAlert>
</main>
