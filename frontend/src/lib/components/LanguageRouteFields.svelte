<script>
	// Language by for the campaign and program wizards: the recipient field
	// that picks each recipient's language, the default language and the
	// language of each value the picked groups hold. Routes are keyed by the
	// normalized value, the same key the server matches on.
	import { api } from '$lib/api/apiProxy.js';
	import TextFieldSelect from '$lib/components/TextFieldSelect.svelte';
	import FieldLabel from '$lib/components/form/FieldLabel.svelte';
	import Note from '$lib/components/Note.svelte';
	import Pagination from '$lib/components/Pagination.svelte';
	import { newTableParams } from '$lib/service/tableParams.js';
	import { languageName } from '$lib/utils/languages.js';
	import { suggestedCountryLanguage } from '$lib/utils/countryLanguages.js';
	import {
		LANGUAGE_FIELD,
		hasLanguageRows,
		languageCountsOf,
		languageRoutingErrors,
		sendableCodes,
		sharedMainLanguage,
		valueMap,
		valueOf
	} from '$lib/utils/languageRouting.js';
	import { plural } from '$lib/utils/programText.js';

	// prefix of the element ids, so both wizards can focus a field on error
	export let idPrefix = 'language';
	// '' is off, else language, country, city, department, position or misc.
	// language gives each recipient their own Language field and takes no
	// routes.
	export let routeBy = '';
	export let defaultLanguage = '';
	// normalized value to language code
	export let routes = valueMap();
	// the language codes every template can send, the only codes to pick
	export let languages = [];
	// the templates that send, as { name, language, languages }. Empty while
	// they are not all known, then nothing is checked here.
	export let templates = [];
	export let groupIDs = [];
	export let companyID = null;
	// a short line under the field, for an edit where planned people keep
	// their language
	export let keepNote = '';
	// error texts by field: routeBy, default, routes
	export let errors = {};
	// the routes of each field left and the default before Language by was
	// set to None, so picking a field again gives them back. The wizard keeps
	// it while it is open, as this part is made again on each step.
	export let kept = {};
	// true refuses routing to more than one language, for an anonymous self
	// managed campaign
	export let oneLanguageOnly = false;
	// how many recipients of the picked groups get each language, code to
	// count, null when not known. For the wizard Review.
	export let languageCounts = null;

	const routeFields = [
		{ value: '', label: 'None' },
		{ value: LANGUAGE_FIELD, label: 'Language' },
		{ value: 'country', label: 'Country' },
		{ value: 'misc', label: 'Misc' },
		{ value: 'city', label: 'City' },
		{ value: 'department', label: 'Department' },
		{ value: 'position', label: 'Position' }
	];
	// Language by Language: each recipient gets their own language when every
	// template has it, else the default
	$: direct = routeBy === LANGUAGE_FIELD;
	const errorClass = 'text-red-600 dark:text-red-400 text-sm';
	const noteClass = 'text-sm text-slate-500 dark:text-gray-400 transition-colors duration-200';
	const errorId = (field) => `${idPrefix}-${field}-error`;

	// the codes to pick, and a picked code that the templates do not have,
	// so the field shows its name and not the code
	$: pickedCodes = [defaultLanguage, ...Object.values(routes)].filter(Boolean);
	$: optionCodes = [...new Set([...languages, ...pickedCodes])];
	// while a template has one language there is nothing to pick, the field
	// shows empty and keeps its value for when the templates change
	$: nothingToPick = !!routeBy && templates.length > 0 && !languages.length;
	$: languageOptions = optionCodes.map((code) => ({ value: code, label: languageName(code) }));
	$: defaultOptions = nothingToPick ? [] : languageOptions;
	// a value row can go back to the default language
	$: valueOptions = [{ value: '', label: 'Default' }, ...languageOptions];
	$: isAllowed = (code) => !code || languages.includes(code);

	// errors seen while picking: a template with one language, a picked code a
	// template does not have. Errors given by the wizard come first.
	$: liveErrors = languageRoutingErrors({
		routeBy,
		defaultLanguage,
		routes,
		templates,
		requireDefault: false,
		oneLanguageOnly
	});
	$: shownErrors = {
		routeBy: errors.routeBy || liveErrors.routeBy,
		default: errors.default || liveErrors.default,
		routes: errors.routes || liveErrors.routes
	};
	// a row is marked when its language is not on every template. A route
	// error with no such row marks every mapped row.
	$: anyRouteNotAllowed = Object.values(routes).some((code) => !isAllowed(code));
	const rowError = (code, _errors, _anyNotAllowed, _isAllowed) =>
		!!code && !!shownErrors.routes && (anyRouteNotAllowed ? !isAllowed(code) : true);

	// Language by None on templates with languages: everyone gets the main
	// language of their template
	$: multiTemplates = templates.filter(hasLanguageRows);
	$: noneMain = sharedMainLanguage(templates);
	$: noneNote =
		!routeBy && multiTemplates.length
			? noneMain
				? `Everyone gets ${languageName(noneMain)}, the main language of the ${
						templates.length > 1 ? 'templates' : 'template'
					}.`
				: "Everyone gets each template's main language."
			: '';
	// why Default language has nothing to pick
	$: defaultEmptyText =
		routeBy && templates.length && !languages.length && !shownErrors.routeBy
			? templates.some((info) => !sendableCodes(info).length)
				? 'No language to pick while a template has one language.'
				: 'No language is on every template.'
			: '';

	const fieldLabel = (value) => routeFields.find((f) => f.value === value)?.label ?? 'Value';

	// picking another field sets the routes aside, their keys are values of
	// the old field, and gives back the routes kept for the new one. None
	// sets the default aside too. Turning it on gives back the kept default,
	// else fills it with the main language the templates share.
	const onPickRouteBy = (value) => {
		if (value === routeBy) return;
		const wasOff = !routeBy;
		const keptRoutes = valueMap(kept.routes);
		if (routeBy) {
			keptRoutes[routeBy] = routes;
		}
		kept = { ...kept, routes: keptRoutes };
		routeBy = value;
		routes = valueMap(value && value !== LANGUAGE_FIELD ? valueOf(keptRoutes, value) : null);
		// a fresh pick of Country fills in the language of each country with a
		// clear main language, the operator checks and changes the rows
		suggest = value === 'country' && Object.keys(routes).length === 0;
		suggestedKeys = new Set();
		decidedKeys = new Set();
		if (!value) {
			kept = { ...kept, defaultLanguage };
			defaultLanguage = '';
			return;
		}
		if (wasOff && !defaultLanguage && kept.defaultLanguage) {
			defaultLanguage = kept.defaultLanguage;
			return;
		}
		const main = sharedMainLanguage(templates);
		if (wasOff && !defaultLanguage && main && languages.includes(main)) {
			defaultLanguage = main;
		}
	};

	// country suggestions of this pick: the keys already looked at, and the
	// keys given a suggested language
	let suggest = false;
	let decidedKeys = new Set();
	let suggestedKeys = new Set();
	const applySuggestions = (values, codes) => {
		if (!codes.length) return;
		const next = valueMap(routes);
		const filled = new Set(suggestedKeys);
		for (const v of values) {
			if (decidedKeys.has(v.key)) continue;
			decidedKeys.add(v.key);
			if (valueOf(next, v.key)) continue;
			const code = suggestedCountryLanguage(v.key);
			if (code && codes.includes(code)) {
				next[v.key] = code;
				filled.add(v.key);
			}
		}
		if (filled.size !== suggestedKeys.size) {
			suggestedKeys = filled;
			routes = next;
		}
	};
	$: if (suggest && routeBy === 'country' && valuesField === 'country' && !valuesLoading) {
		applySuggestions(fieldValues?.values ?? [], languages);
	}
	// a row still on its suggested language
	const isSuggested = (key, _routes, _keys) =>
		suggestedKeys.has(key) && valueOf(routes, key) === suggestedCountryLanguage(key);
	$: suggestedCount = [...suggestedKeys].filter((key) =>
		isSuggested(key, routes, suggestedKeys)
	).length;

	const setRoute = (key, code) => {
		const next = valueMap(routes);
		if (code) {
			next[key] = code;
		} else {
			delete next[key];
		}
		routes = next;
	};

	// the values of the field over the picked groups
	let fieldValues = null;
	// the field fieldValues belongs to
	let valuesField = '';
	let valuesLoading = false;
	let valuesFailed = false;
	let valuesSeq = 0;
	const loadValues = async (field, ids, company) => {
		const seq = ++valuesSeq;
		if (!field || !ids.length) {
			fieldValues = null;
			valuesField = '';
			valuesLoading = false;
			valuesFailed = false;
			return;
		}
		// values of another field are never shown under this one, so no route
		// is set on them
		if (field !== valuesField) {
			fieldValues = null;
			valuesField = field;
		}
		valuesLoading = true;
		try {
			const res = await api.recipient.getGroupFieldValues(field, ids, company);
			if (seq !== valuesSeq) return;
			if (!res.success) throw res.error;
			fieldValues = res.data;
			valuesFailed = false;
		} catch (e) {
			if (seq !== valuesSeq) return;
			console.error('failed to load recipient field values', e);
			fieldValues = null;
			valuesFailed = true;
		} finally {
			if (seq === valuesSeq) valuesLoading = false;
		}
	};
	$: groupKey = (groupIDs || []).filter(Boolean).join(',');
	$: loadValues(routeBy, groupKey ? groupKey.split(',') : [], companyID);

	// the spelling seen for each key, kept while the wizard is open, so a
	// mapped value that left the picked groups keeps the spelling it had
	let seenLabels = valueMap();
	$: if (fieldValues?.values?.length) {
		const next = valueMap(seenLabels);
		for (const v of fieldValues.values) {
			if (v.values?.[0]) next[v.key] = v.values[0];
		}
		seenLabels = next;
	}
	const notInGroups = 'No recipient in the picked groups has this value';
	// the observed values first, then mapped values no picked recipient has,
	// so every route stays in view and can be removed
	$: rows = (() => {
		const observed = (fieldValues?.values ?? []).map((v) => ({
			key: v.key,
			label: direct ? languageName(v.key) : (v.values?.[0] ?? v.key),
			title: direct ? languageName(v.key) : (v.values ?? []).join(', '),
			count: v.count
		}));
		const seen = new Set(observed.map((r) => r.key));
		const mapped = Object.keys(routes)
			.filter((key) => !seen.has(key))
			.sort()
			.map((key) => ({
				key,
				label: valueOf(seenLabels, key) ?? key,
				title: notInGroups,
				count: 0,
				absent: !valueOf(seenLabels, key)
			}));
		return [...observed, ...mapped];
	})();

	const params = newTableParams({ perPage: 10, sortBy: '', sortOrder: 'asc' });
	let pageTick = 0;
	params.onChange(() => pageTick++);
	const pageOf = (list, p, _tick) => {
		const start = (p.currentPage - 1) * p.perPage;
		return list.slice(start, start + p.perPage);
	};
	const morePages = (list, p, _tick) => list.length > p.currentPage * p.perPage;
	// a new list of values starts on the first page
	$: fieldValues, (params.perPage = params.perPage);
	// a page left empty, after its last row went away, moves back a page
	$: if (
		pageTick >= 0 &&
		params.currentPage > 1 &&
		rows.length <= (params.currentPage - 1) * params.perPage
	) {
		params.previous();
	}
	$: pageRows = pageOf(rows, params, pageTick);
	$: hasNextPage = morePages(rows, params, pageTick);
	$: moreValues = fieldValues?.truncated
		? Math.max(0, (fieldValues.total ?? 0) - (fieldValues.values?.length ?? 0))
		: 0;

	// recipients whose value has no language, empty values included.
	// unlistedCount holds the recipients of values the server did not list.
	// They are counted as unmatched unless a value with a language is one of
	// them, then only a lower bound is known.
	$: listedKeys = new Set((fieldValues?.values ?? []).map((v) => v.key));
	$: unmatchedListed =
		(fieldValues?.values ?? [])
			.filter((v) => !valueOf(routes, v.key))
			.reduce((sum, v) => sum + (v.count ?? 0), 0) + (fieldValues?.emptyCount ?? 0);
	$: unlistedKnown =
		typeof fieldValues?.unlistedCount === 'number' &&
		(fieldValues.unlistedCount === 0 || Object.keys(routes).every((key) => listedKeys.has(key)));
	$: unmatched = unmatchedListed + (unlistedKnown ? fieldValues.unlistedCount : 0);
	// the count is a lower bound when values were cut and their recipients
	// can not be told apart
	$: unmatchedAtLeast = !!fieldValues?.truncated && !unlistedKnown;
	$: languageCounts =
		routeBy && valuesField === routeBy && !valuesLoading
			? languageCountsOf(fieldValues, defaultLanguage, routes, direct ? languages : null)
			: null;

	// with Language by Language, one line per language a template does not
	// have, naming the templates and the language those recipients get
	const quotedNames = (names) =>
		names.length === 1
			? `'${names[0]}'`
			: `${names
					.slice(0, -1)
					.map((n) => `'${n}'`)
					.join(', ')} and '${names[names.length - 1]}'`;
	$: lackingNotes =
		direct && defaultLanguage && !nothingToPick && templates.length && fieldValues
			? (fieldValues.values ?? [])
					.filter((v) => !languages.includes(v.key))
					.map((v) => {
						const missing = templates
							.filter((info) => !sendableCodes(info).includes(v.key))
							.map((info) => info.name);
						const one = v.count === 1;
						return `${plural(v.count, 'recipient')} ${one ? 'speaks' : 'speak'} ${languageName(
							v.key
						)}, which ${missing.length === 1 ? 'template' : 'templates'} ${quotedNames(
							missing.length ? missing : templates.map((info) => info.name)
						)} ${missing.length === 1 ? 'does' : 'do'} not have; they get ${languageName(
							defaultLanguage
						)}.`;
					})
			: [];
	$: directEmpty = direct && fieldValues ? (fieldValues.emptyCount ?? 0) : 0;
</script>

<div class="w-full max-w-lg min-w-0">
	<TextFieldSelect
		id={`${idPrefix}-language-by`}
		size="full"
		value={routeBy}
		onSelect={onPickRouteBy}
		error={!!shownErrors.routeBy}
		ariaDescribedBy={shownErrors.routeBy ? errorId('routeBy') : null}
		toolTipText="The recipient field that picks the language each recipient gets. Language uses each recipient's own Language field."
		options={routeFields}>Language by</TextFieldSelect
	>
	{#if shownErrors.routeBy}
		<p id={errorId('routeBy')} class={errorClass}>{shownErrors.routeBy}</p>
	{/if}
	{#if noneNote}
		<Note>{noneNote}</Note>
	{/if}
	{#if keepNote && routeBy}
		<Note>{keepNote}</Note>
	{/if}

	{#if routeBy}
		<TextFieldSelect
			id={`${idPrefix}-language-default`}
			size="full"
			required
			value={nothingToPick ? '' : defaultLanguage}
			onSelect={(code) => {
				if (!nothingToPick) defaultLanguage = code;
			}}
			error={!!shownErrors.default}
			ariaDescribedBy={shownErrors.default ? errorId('default') : null}
			toolTipText={direct
				? 'The language of recipients with no language, or with a language a template does not have.'
				: 'The language of recipients whose value has no language below.'}
			options={defaultOptions}>Default language</TextFieldSelect
		>
		{#if shownErrors.default}
			<p id={errorId('default')} class={errorClass}>{shownErrors.default}</p>
		{:else if defaultEmptyText}
			<Note>{defaultEmptyText}</Note>
		{/if}

		<div id={`${idPrefix}-language-values`} class="pt-2">
			{#if !groupKey}
				<p class={noteClass}>Pick recipient groups to see their values.</p>
			{:else if valuesFailed && !valuesLoading}
				<div class="flex items-baseline gap-x-2 text-sm text-slate-600 dark:text-gray-300">
					<span>Could not load values</span>
					<button
						type="button"
						class="text-sm font-medium text-cta-blue hover:underline dark:text-highlight-blue transition-colors duration-200"
						on:click={() => loadValues(routeBy, groupKey.split(','), companyID)}>Retry</button
					>
				</div>
			{:else if rows.length === 0}
				<p class={noteClass}>
					{valuesLoading ? 'Loading values...' : 'No recipient in these groups has a value.'}
				</p>
			{:else}
				{#if suggestedCount > 0}
					<Note class="mb-2"
						>{`A language is suggested for ${plural(
							suggestedCount,
							'country',
							'countries'
						)} from the country name. Check every row and change any that is wrong.`}</Note
					>
				{/if}
				<div class="flex items-center gap-2">
					<span class="flex-1 min-w-0"><FieldLabel>{fieldLabel(routeBy)}</FieldLabel></span>
					<span class="w-16 sm:w-24 flex-shrink-0 flex justify-end"
						><FieldLabel>Recipients</FieldLabel></span
					>
					{#if !direct}
						<span class="w-36 sm:w-44 flex-shrink-0"><FieldLabel>Language</FieldLabel></span>
					{/if}
				</div>
				<ol class="flex flex-col gap-1">
					{#each pageRows as row, i (row.key)}
						<li class="flex items-center gap-2">
							<span
								class="flex-1 min-w-0 truncate text-sm text-slate-700 dark:text-gray-200"
								title={row.title}
								>{row.label}{#if row.absent}<span class="text-slate-500 dark:text-gray-400">
										(not in the picked groups)</span
									>{:else if isSuggested(row.key, routes, suggestedKeys)}<span
										class="text-slate-500 dark:text-gray-400">{' (suggested)'}</span
									>{/if}</span
							>
							<span
								class="w-16 sm:w-24 flex-shrink-0 text-right text-sm text-slate-500 dark:text-gray-400 tabular-nums"
								>{row.count}</span
							>
							{#if !direct}
								<div class="w-36 sm:w-44 flex-shrink-0">
									<TextFieldSelect
										id={`${idPrefix}-language-value-${(params.currentPage - 1) * params.perPage + i}`}
										hideLabel
										optional
										size="full"
										placeholder="Default"
										error={rowError(
											valueOf(routes, row.key),
											shownErrors,
											anyRouteNotAllowed,
											isAllowed
										)}
										value={valueOf(routes, row.key) ?? ''}
										onSelect={(code) => setRoute(row.key, code)}
										options={valueOptions}
									/>
								</div>
							{/if}
						</li>
					{/each}
				</ol>
				{#if rows.length > params.perPage}
					<Pagination paginator={params} {hasNextPage} />
				{/if}
				{#if fieldValues?.truncated}
					<p class="{noteClass} pb-2">
						{moreValues > 0 ? `and ${moreValues} more` : 'and more values not listed'}
					</p>
				{/if}
			{/if}
			{#if shownErrors.routes}
				<p id={errorId('routes')} class={errorClass}>{shownErrors.routes}</p>
			{/if}
			{#if direct}
				{#each lackingNotes as text}
					<Note class="mt-2">{text}</Note>
				{/each}
				{#if defaultLanguage && !nothingToPick && directEmpty > 0}
					<Note class="mt-2">
						{`${plural(directEmpty, 'recipient')} ${directEmpty === 1 ? 'has' : 'have'} no language and ${
							directEmpty === 1 ? 'gets' : 'get'
						} ${languageName(defaultLanguage)}.`}
					</Note>
				{/if}
			{:else if defaultLanguage && !nothingToPick && fieldValues && unmatched > 0}
				<Note class="mt-2">
					{`${unmatchedAtLeast ? 'At least ' : ''}${plural(unmatched, 'recipient')} ${
						unmatched === 1 ? 'has' : 'have'
					} no language picked and ${unmatched === 1 ? 'gets' : 'get'} ${languageName(defaultLanguage)}.`}
				</Note>
			{/if}
		</div>
	{/if}
</div>
