<script>
	import { onMount } from 'svelte';
	import { api } from '$lib/api/apiProxy.js';
	import { API } from '$lib/api/api.js';
	import { newTableURLParams } from '$lib/service/tableURLParams.js';
	import { addToast } from '$lib/store/toast';
	import { showIsLoading, hideIsLoading } from '$lib/store/loading.js';
	import HeadTitle from '$lib/components/HeadTitle.svelte';
	import Headline from '$lib/components/Headline.svelte';
	import Button from '$lib/components/Button.svelte';
	import Search from '$lib/components/Search.svelte';
	import Select from '$lib/components/Select.svelte';
	import Pagination from '$lib/components/Pagination.svelte';
	import Modal from '$lib/components/Modal.svelte';
	import FormGrid from '$lib/components/FormGrid.svelte';
	import FormColumns from '$lib/components/FormColumns.svelte';
	import FormColumn from '$lib/components/FormColumn.svelte';
	import FormFooter from '$lib/components/FormFooter.svelte';
	import FormError from '$lib/components/FormError.svelte';
	import TextField from '$lib/components/TextField.svelte';
	import TextFieldSelect from '$lib/components/TextFieldSelect.svelte';
	import CheckboxField from '$lib/components/CheckboxField.svelte';
	import DeleteAlert from '$lib/components/modal/DeleteAlert.svelte';
	import Alert from '$lib/components/Alert.svelte';
	import SettingsLoading from '$lib/components/SettingsLoading.svelte';
	import TableDropDownEllipsis from '$lib/components/table/TableDropDownEllipsis.svelte';
	import TableDropDownButton from '$lib/components/table/TableDropDownButton.svelte';
	import TableDeleteButton from '$lib/components/table/TableDeleteButton2.svelte';
	import Table from '$lib/components/table/Table.svelte';
	import TableRow from '$lib/components/table/TableRow.svelte';
	import TableCell from '$lib/components/table/TableCell.svelte';
	import TableCellEmpty from '$lib/components/table/TableCellEmpty.svelte';
	import TableCellAction from '$lib/components/table/TableCellAction.svelte';
	import { menuIcons, packageTypeIconKey } from '$lib/consts/menuIcons.js';
	import {
		installedResources,
		loadInstalledResources,
		refreshInstalledResources
	} from '$lib/store/installedResources.js';
	import SimpleCodeEditor from '$lib/components/editor/SimpleCodeEditor.svelte';
	import { submitPreviewForm, PACKAGE_PREVIEW_ENDPOINT } from '$lib/utils/safePreview.js';
	import { tagChipClass } from '$lib/utils/tagColor.js';
	import { fetchAllRows } from '$lib/utils/api-utils';

	const rawApi = API.instance;
	const tableURLParams = newTableURLParams({ sortBy: 'name', sortOrder: 'asc' });

	let registries = [];
	let selectedRegistryId = '';
	// reachability per registry id:
	// { state: 'checking' | 'ok' | 'refused' | 'error' | 'incompatible', reason }.
	// the list loads without contacting any registry, then each one is probed on its
	// own so a registry that is down never holds up the page.
	let registryStatus = {};
	// set when the package browse call fails, so the grid shows an unreachable state
	// instead of the no matches empty state. cleared on a successful load.
	let browseError = '';
	let allPackages = [];
	let tagColors = {}; // tag name -> color token from the registry, for chip colors
	let installed = [];
	let installedLoading = false;
	// an update reports each resource that kept a local edit as a skipped entry
	// "<type> <name> kept (local edit...)". matches keptLocalEdit in the backend.
	const KEPT_LOCAL_EDIT = 'kept (local edit';
	// ends a kept entry for an edited resource the new version removed.
	const KEPT_DROPPED = '; dropped upstream)';
	function isKeptEdit(entry) {
		return entry.includes(KEPT_LOCAL_EDIT) && !entry.includes(KEPT_DROPPED);
	}
	function isKeptDropped(entry) {
		return entry.includes(KEPT_LOCAL_EDIT) && entry.includes(KEPT_DROPPED);
	}
	// a resource kept as it is although it has no local edit, such as an API sender
	// keeping its OAuth provider. matches keptCurrent in the backend.
	const KEPT_CURRENT = 'kept (current';
	function isKeptEntry(entry) {
		return entry.includes(KEPT_LOCAL_EDIT) || entry.includes(KEPT_CURRENT);
	}
	// the resource name of a kept entry.
	function keptName(entry) {
		const start = entry.indexOf(' ') + 1;
		let end = entry.indexOf(` ${KEPT_LOCAL_EDIT}`);
		if (end < 0) end = entry.indexOf(` ${KEPT_CURRENT}`);
		return end > start ? entry.slice(start, end) : entry;
	}
	// how many update notes one toast lists before it says how many more there are.
	const UPDATE_NOTES_SHOWN = 5;
	// the skipped entries of an update other than kept items, such as items the new
	// version removed or items that could not be removed.
	function updateNotes(skipped) {
		return (skipped || []).filter((m) => !isKeptEntry(m));
	}
	// shows update notes in one info toast.
	function toastUpdateNotes(label, notes) {
		if (!notes.length) return;
		const shown = notes.slice(0, UPDATE_NOTES_SHOWN);
		const more = notes.length - shown.length;
		addToast(`${label}: ${shown.join('; ')}${more > 0 ? ` and ${more} more` : ''}`, 'Info');
	}
	// top level view: browse the selected Bait Box, or list every install. the
	// installed view also lists installs whose Bait Box is gone or not reachable.
	let activeView = 'browse';
	let visibleRows = [];
	let hasNextPage = false;
	// toolbar filter: all, installed, available (not installed) or updates.
	let installFilter = 'all';
	// toolbar filter by package type: 'all' or a raw type key (page, email, ...).
	let typeFilter = 'all';
	// the distinct types present in the current registry, for the type filter options.
	$: availableTypes = [...new Set(allPackages.map((p) => p.type).filter(Boolean))].sort();
	// how many packages each type holds, shown in the type filter so an operator sees the
	// spread at a glance.
	$: typeCounts = allPackages.reduce((m, p) => {
		if (p.type) m[p.type] = (m[p.type] || 0) + 1;
		return m;
	}, {});
	$: typeOptions = ['All', ...availableTypes.map((t) => typeOptionLabel(t))];

	// add registry modal
	let showAddRegistry = false;
	let addSubmitting = false;
	let addForm = null;
	let regName = '';
	let regURL = '';
	let regToken = '';
	let regInsecure = 'false';
	let addError = '';

	// edit registry modal. the token field stays blank and only overwrites the
	// stored token when the operator types a new one.
	let showEditRegistry = false;
	let editSubmitting = false;
	let editForm = null;
	let editRegId = '';
	let editName = '';
	let editURL = '';
	let editToken = '';
	let editInsecure = 'false';
	let editClearToken = false;
	let editHadToken = false;
	let editError = '';

	// package detail modal
	let showDetail = false;
	let detail = null;
	let detailFull = null; // full inspect result (files, variables, dependencies)
	let detailLoading = false;
	let detailError = '';
	let openFiles = {}; // which file rows are expanded

	// required values modal
	let showVars = false;
	let varsSubmitting = false;
	let varsForm = null;
	let varDefs = []; // [{name,type,description,key,scope}]
	// values keyed as varKeyOf gives.
	let varValues = {};
	// the key the server reads a variable's value under. it is the name unless the
	// bundle keeps a value per package, where scope names that package.
	const varKeyOf = (d) => d.key || d.name;
	let varsError = '';
	let pending = null;

	// some required variables must be chosen from the operator's own existing
	// entities rather than typed. the backend marks these with a pick kind. the
	// names the operator already has, per kind, fill those selects. the chosen
	// value is always the entity name, which the install resolves by.
	let pickEntities = {};
	let pickLoading = false;
	// each pick kind maps to the list call that returns the operator's entities of
	// that kind. companyID is null here because Bait Box installs at the global
	// scope. a large page is fetched through fetchAllRows so every entity is listed.
	const PICK_LOADERS = {
		smtpConfiguration: (options) => api.smtpConfiguration.getAll(options, null),
		domain: (options) => api.domain.getAll(options, null),
		email: (options) => api.email.getAll(options, null),
		page: (options) => api.page.getAll(options, null),
		proxy: (options) => api.proxy.getAll(options, null),
		apiSender: (options) => api.apiSender.getAllOverview(options, null),
		oauthProvider: (options) => api.oauthProvider.getAll(options, null),
		allowDeny: (options) => api.allowDeny.getAllOverview(options, null)
	};
	// a list pick offers only global lists, of either kind; the install refuses a
	// company list.
	const PICK_FILTERS = {
		allowDeny: (r) => !r.companyID
	};
	// readable name for a pick kind, used in the field label and the empty note.
	const PICK_LABELS = {
		smtpConfiguration: 'SMTP configuration',
		domain: 'domain',
		email: 'email',
		page: 'page',
		proxy: 'proxy',
		apiSender: 'API sender',
		oauthProvider: 'OAuth provider',
		allowDeny: 'allow/deny list'
	};
	function pickLabel(kind) {
		return PICK_LABELS[kind] || kind;
	}
	// the admin route where the operator creates an entity of a pick kind, so a blocked
	// install can link straight to it. opened in a new tab so the install modal stays.
	const PICK_ROUTES = {
		smtpConfiguration: '/smtp-configuration',
		domain: '/domain',
		email: '/email',
		page: '/page',
		proxy: '/proxy',
		apiSender: '/api-sender',
		oauthProvider: '/oauth-provider',
		allowDeny: '/filter'
	};
	function pickRoute(kind) {
		return PICK_ROUTES[kind] || '';
	}
	// heading for a pick variable, matching the template editor slot labels rather
	// than the raw variable name. Bait Box authors these with a description of "Your <slot label>",
	// so strip that lead in; otherwise fall back to the kind label or the name.
	function pickHeading(d) {
		const m = String(d.description || '').match(/^your\s+(.+)$/i);
		const s = m ? m[1] : pickLabel(d.pick) || d.name;
		return s.charAt(0).toUpperCase() + s.slice(1);
	}
	// fetch the operator's entities once for every distinct pick kind the missing
	// variables need, keeping just the names. a failed load leaves that kind empty
	// so the field falls back to its empty state and submit stays blocked.
	async function loadPickEntities(defs) {
		const kinds = [...new Set((defs || []).map((d) => d.pick).filter(Boolean))];
		pickEntities = {};
		if (!kinds.length) return;
		pickLoading = true;
		try {
			const pairs = await Promise.all(
				kinds.map(async (kind) => {
					const loader = PICK_LOADERS[kind];
					if (!loader) return [kind, []];
					try {
						const rows = await fetchAllRows((options) => loader(options));
						const keep = PICK_FILTERS[kind] || (() => true);
						return [
							kind,
							rows
								.filter(keep)
								.map((r) => r.name)
								.filter(Boolean)
						];
					} catch (e) {
						return [kind, []];
					}
				})
			);
			const next = {};
			for (const [kind, names] of pairs) next[kind] = names;
			pickEntities = next;
		} finally {
			pickLoading = false;
		}
	}
	// pick variables the operator cannot satisfy because they own no entity of that
	// kind yet. every value asked for here is required, so while these exist the
	// install is blocked. drives the up front notice and the disabled submit.
	$: missingPickDefs = (varDefs || []).filter(
		(d) => d.pick && !pickLoading && !(pickEntities[d.pick] || []).length
	);
	$: installBlockedByMissing = missingPickDefs.length > 0;

	// delete alerts
	let removeInstallVisible = false;
	let removeInstallTarget = null;
	// what the uninstall would affect, read from the server when the confirm opens.
	let removePreview = null;
	let removePreviewLoading = false;
	let removePreviewError = '';
	let removeRegistryVisible = false;

	// update confirmation
	let confirmUpdateVisible = false;
	let confirmUpdateInst = null;

	function openUpdate(inst) {
		confirmUpdateInst = inst;
		confirmUpdateVisible = true;
	}
	async function confirmUpdate() {
		// the first submit sends no resolutions, so the backend reports any conflicts
		// (409) and update() opens the conflict modal. returning the result lets the
		// Alert close instead of showing a false error.
		if (confirmUpdateInst) return await update(confirmUpdateInst, {});
		return { success: true };
	}

	// update conflicts: when an update has items that need a choice, such as content
	// the user edited, the backend returns the conflicts and changes nothing. the
	// user picks a resolution per conflict, then the update is resubmitted with them.
	let conflictsVisible = false;
	let conflictInst = null;
	let conflictVariables = {};
	let conflictList = []; // [{resourceType, sourceName, currentName, reason}]
	let conflictChoices = {}; // "<resourceType>:<sourceName>" -> keep | overwrite | both
	const conflictKey = (c) => `${c.resourceType}:${c.sourceName}`;
	const CHOICE_LABELS = { keep: 'Keep mine', overwrite: 'Overwrite', both: 'Keep both' };
	const choiceLabel = (v) => CHOICE_LABELS[v] || 'Keep mine';
	const labelToChoice = (label) =>
		label === 'Overwrite' ? 'overwrite' : label === 'Keep both' ? 'both' : 'keep';
	// a domain or oauth provider cannot be renamed to a local copy, a report
	// template has one global slot per kind, and an asset is served at one path, so
	// the server rejects "both" for them and the choice is not offered.
	const NO_BOTH_TYPES = ['domain', 'oauthprovider', 'reporttemplate', 'asset'];
	// keepOnly marks a conflict the server applies only with keep, such as an API
	// sender the new version would bind to an OAuth provider it may not use.
	const conflictOptions = (c) =>
		c.keepOnly
			? ['Keep mine']
			: NO_BOTH_TYPES.includes(c.resourceType)
				? ['Keep mine', 'Overwrite']
				: ['Keep mine', 'Overwrite', 'Keep both'];
	// a referenced package the new bundle version drops while its content is edited
	// or used elsewhere: keep it installed on its own, or uninstall it.
	const MEMBER_LABELS = { keep: 'Keep installed', remove: 'Uninstall' };
	const isMemberConflict = (c) => c.resourceType === 'member';
	$: fileConflicts = conflictList.filter((c) => !isMemberConflict(c));
	// a conflict with a reason waits for a choice without any local edit.
	$: conflictsHaveReason = fileConflicts.some((c) => c.reason);
	// an asset conflict with filterLoss can wait for a choice without any local
	// edit: the new version takes allow/deny lists off the asset.
	$: conflictsHaveFilterLoss = fileConflicts.some((c) => c.filterLoss);
	$: memberConflicts = conflictList.filter(isMemberConflict);
	// the keep both note is only shown when one of the conflicts offers it.
	$: conflictsOfferBoth = fileConflicts.some((c) => conflictOptions(c).includes('Keep both'));
	function openConflicts(inst, variables, conflicts) {
		conflictInst = inst;
		conflictVariables = variables || {};
		conflictList = conflicts || [];
		const choices = {};
		for (const c of conflictList) choices[conflictKey(c)] = 'keep';
		conflictChoices = choices;
		conflictsVisible = true;
	}
	function setConflictChoice(key, label) {
		conflictChoices = { ...conflictChoices, [key]: labelToChoice(label) };
	}
	function setMemberChoice(key, label) {
		conflictChoices = {
			...conflictChoices,
			[key]: label === MEMBER_LABELS.remove ? 'remove' : 'keep'
		};
	}
	async function confirmConflicts() {
		if (!conflictInst) return { success: true };
		return await update(conflictInst, conflictVariables, { ...conflictChoices });
	}

	// report template overwrite: Phishing Club keeps one global report template per
	// kind (simulation and training), so a package that ships one overwrites the
	// current template of that kind. the backend answers 409 with the templates it
	// would overwrite and changes nothing; on confirm the request is resent with
	// those kinds plus the values and conflict choices already given.
	let rtOverwriteVisible = false;
	let rtOverwriteList = []; // [{kind, templateId, name, customized, sourceName, packageId}]
	// {kind: 'install', p, variables} or {kind: 'update', inst, variables, resolutions}
	let rtOverwritePending = null;
	const RT_KIND_LABELS = { simulation: 'simulation', training: 'training' };
	const rtKindLabel = (k) => RT_KIND_LABELS[k] || k;
	function openReportTemplateOverwrite(list, p) {
		rtOverwriteList = list || [];
		rtOverwritePending = p;
		// close whatever modal sent the request so dialogs never stack.
		showVars = false;
		conflictsVisible = false;
		confirmInstallVisible = false;
		rtOverwriteVisible = true;
	}
	async function confirmReportTemplateOverwrite() {
		const pend = rtOverwritePending;
		if (!pend) return { success: true };
		const kinds = [...new Set(rtOverwriteList.map((t) => t.kind))];
		if (pend.kind === 'install') return await doInstall(pend.p, pend.variables, kinds);
		return await update(pend.inst, pend.variables, pend.resolutions, kinds);
	}

	// bulk update: update every package that has an update available, keeping any
	// local edits and skipping packages that need variables or have conflicts with a
	// reason (those are done one by one). no per package prompts during the loop.
	let updateAllVisible = false;
	// updateAllRegistryId limits update all to one Bait Box; empty means every
	// connected Bait Box.
	let updateAllRegistryId = '';
	function openUpdateAll(registryId = '') {
		updateAllRegistryId = registryId;
		updateAllVisible = true;
	}
	async function confirmUpdateAll() {
		// allow/deny packages go first, since a page that uses a newer list is refused
		// until its list package is updated.
		const targets = installed
			.filter((i) => i.updateAvailable)
			.filter((i) => !updateAllRegistryId || i.registryId === updateAllRegistryId)
			.sort((a, b) => (b.packageType === 'allowdeny') - (a.packageType === 'allowdeny'));
		const updated = [];
		const kept = [];
		const keptDropped = [];
		const notes = [];
		const needVars = [];
		const needChoice = [];
		const needReportConfirm = [];
		const failed = [];
		showIsLoading();
		try {
			for (const inst of targets) {
				let res = await rawApi.registry.update(inst.id, {}, {});
				if (!res.success && res.statusCode === 409 && res.data?.conflicts) {
					// a conflict with a reason needs the operator's choice, so the package is
					// skipped. otherwise resubmit keeping every local edit (the bulk default).
					if (res.data.conflicts.some((c) => c.reason)) {
						needChoice.push(inst.packageId);
						continue;
					}
					const keepAll = {};
					for (const c of res.data.conflicts) keepAll[`${c.resourceType}:${c.sourceName}`] = 'keep';
					res = await rawApi.registry.update(inst.id, {}, keepAll);
				}
				if (res.success) {
					updated.push(inst.packageId);
					const skipped = res.data?.result?.skipped || [];
					if (skipped.some(isKeptEdit)) kept.push(inst.packageId);
					for (const m of skipped.filter(isKeptDropped)) {
						keptDropped.push(`${inst.packageId}: ${keptName(m)}`);
					}
					for (const n of updateNotes(skipped)) notes.push(`${inst.packageId}: ${n}`);
				} else if (res.statusCode === 409 && res.data?.missing) {
					needVars.push(inst.packageId);
				} else if (res.statusCode === 409 && res.data?.reportTemplates) {
					// overwriting a report template needs the user to confirm, so it is
					// never done in bulk.
					needReportConfirm.push(inst.packageId);
				} else {
					failed.push(inst.packageId);
				}
			}
		} finally {
			hideIsLoading();
		}
		await loadInstalled();
		if (updated.length) {
			refreshInstalledResources();
			const n = updated.length;
			addToast(`Updated ${n} package${n === 1 ? '' : 's'}`, 'Success');
		}
		if (kept.length) addToast(`Kept your local edits in: ${kept.join(', ')}`, 'Info');
		if (keptDropped.length) {
			addToast(
				`Kept your edited copies that the new version removed: ${keptDropped.join(', ')}`,
				'Info'
			);
		}
		if (needChoice.length) {
			addToast(
				`Skipped (needs a choice): ${needChoice.join(', ')}. Update these individually.`,
				'Info'
			);
		}
		toastUpdateNotes('Update notes', notes);
		if (needVars.length) {
			addToast(
				`Skipped (need variables): ${needVars.join(', ')}. Update these individually.`,
				'Info'
			);
		}
		if (needReportConfirm.length) {
			addToast(
				`Skipped (would overwrite your report template): ${needReportConfirm.join(', ')}. ` +
					'Update these individually to confirm.',
				'Info'
			);
		}
		if (failed.length) addToast(`Update failed: ${failed.join(', ')}`, 'Error');
		if (!targets.length) addToast('No packages to update', 'Info');
		return { success: true };
	}

	// install modal: preview the package and collect its variable inputs inline, then
	// install in one step. the required variables flow downstream (409 missing) stays as
	// a safety net.
	let confirmInstallVisible = false;
	let confirmInstallPkg = null;
	// the full inspect result for the package being installed, so the modal can show what
	// the package contains and render its variable inputs. kept apart from the detail
	// modal's detailFull so the two modals never overwrite each other.
	let confirmInstallFull = null;
	let confirmInstallLoading = false;
	let confirmInstallError = '';
	$: confirmBrokenDeps = confirmInstallFull?.brokenDependencies || [];
	// referenced packages the token may not read. they may exist, so they get the token
	// message rather than the missing package one, and they block the install too.
	$: confirmRefusedDeps = confirmInstallFull?.refusedDependencies || [];
	// the variable inputs collected in the install modal, keyed as varKeyOf gives and
	// filled from each variable default when the manifest loads, picks excepted.
	let installVars = {};
	let installVarsError = '';
	let installVarsSubmitting = false;
	let installForm = null;
	// the variables declared by the package being installed.
	$: installVarDefs = confirmInstallFull?.variables || [];
	// required pick variables the operator cannot satisfy because they own no entity of
	// that kind yet. derived from the install manifest so it stays separate from the
	// update required values modal, which derives its own missing set from varDefs.
	$: installMissingPickDefs = installVarDefs.filter(
		(d) => d.required && d.pick && !pickLoading && !(pickEntities[d.pick] || []).length
	);
	$: installBlocked = installMissingPickDefs.length > 0;
	// the one message for a package that references packages missing on the Bait Box.
	function brokenBundleMessage(deps) {
		const what =
			deps.length === 1 ? 'a package that no longer exists' : 'packages that no longer exist';
		return (
			`This package cannot be installed. It references ${what} on the Bait Box ` +
			`(${deps.join(', ')}). Ask the package author to republish it against the current ` +
			'package address.'
		);
	}
	// server messages start lower case and have no full stop.
	function capitalize(text) {
		const t = String(text || '');
		return t.charAt(0).toUpperCase() + t.slice(1);
	}
	function sentence(text) {
		const t = capitalize(text).trim();
		return /[.!?]$/.test(t) ? t : `${t}.`;
	}
	// the server error of a failed call as a sentence, or the fallback.
	function serverError(res, fallback) {
		return sentence(res?.error || fallback);
	}
	function openInstall(p) {
		confirmInstallPkg = p;
		confirmInstallFull = null;
		confirmInstallError = '';
		installVars = {};
		installVarsError = '';
		confirmInstallVisible = true;
		loadConfirmInstallFull(p);
	}
	// enrich the modal with the package manifest. the identity block renders from the
	// summary right away, so this fills the contents and variable inputs and the modal is
	// never blank while it loads. a failed load degrades to the summary only; the backend
	// revalidates on install.
	async function loadConfirmInstallFull(p) {
		confirmInstallLoading = true;
		const res = await api.registry.inspect(selectedRegistryId, p.namespace, p.name);
		confirmInstallLoading = false;
		// the operator may have cancelled or picked another package before this resolved.
		if (!confirmInstallVisible || confirmInstallPkg !== p) return;
		if (res.success) {
			confirmInstallFull = res.data?.package || null;
			const vals = {};
			// a pick starts empty so the operator chooses it, never takes the default.
			for (const d of confirmInstallFull?.variables || []) {
				vals[varKeyOf(d)] = d.pick ? '' : (d.default ?? '');
			}
			installVars = vals;
			loadPickEntities(confirmInstallFull?.variables || []);
		} else {
			confirmInstallError = serverError(res, 'Could not load the package contents');
		}
	}
	// validate the inline inputs and install in one step. a broken bundle or an
	// unsatisfiable required pick blocks the install, and every required value must be
	// filled before the backend is called.
	async function submitInstall() {
		if (!confirmInstallPkg) return;
		if (confirmBrokenDeps.length) {
			installVarsError = brokenBundleMessage(confirmBrokenDeps);
			return;
		}
		if (confirmRefusedDeps.length) {
			installVarsError = `This package cannot be installed. ${sentence(confirmInstallFull.refusedMessage)}`;
			return;
		}
		if (installBlocked) {
			const kinds = [...new Set(installMissingPickDefs.map((d) => pickLabel(d.pick)))];
			installVarsError =
				`Create the required ${kinds.join(', ')} in Phishing Club first, ` +
				'then reopen this install.';
			return;
		}
		const blank = installVarDefs.find(
			(d) => d.required && !String(installVars[varKeyOf(d)] ?? '').trim()
		);
		if (blank) {
			installVarsError = `${blank.name} is required.`;
			return;
		}
		installVarsError = '';
		// send every required value and any optional value the operator filled; optional
		// fields left blank are omitted so the backend keeps their defaults. a pick is
		// sent only when it names one of the operator's own entities from the picker.
		const values = {};
		for (const d of installVarDefs) {
			const v = installVars[varKeyOf(d)];
			if (d.pick) {
				if ((pickEntities[d.pick] || []).includes(v)) values[varKeyOf(d)] = v;
				continue;
			}
			if (d.required || String(v ?? '').trim()) values[varKeyOf(d)] = v;
		}
		installVarsSubmitting = true;
		await doInstall(confirmInstallPkg, values);
		installVarsSubmitting = false;
	}

	// the card uses the same icon the sidebar menu uses for that resource type, so an
	// empty preview reads as intentional and matches the rest of the app.
	function typeIconSvg(type) {
		return menuIcons[packageTypeIconKey(type)] || menuIcons.pages;
	}

	// the select shows readable names (not ids): options are names, the chosen name
	// resolves back to its id, matching how other pages drive TextFieldSelect.
	$: registryOptions = registries.map((r) => r.name);
	$: selectedRegistry = registries.find((r) => r.id === selectedRegistryId) || null;
	$: selectedRegistryName = selectedRegistry ? selectedRegistry.name : '';
	$: selectedStatus = selectedRegistry ? registryStatus[selectedRegistry.id] : null;
	$: installedByKey = (() => {
		const m = {};
		for (const i of installed) m[`${i.registryId}/${i.packageId}`] = i;
		return m;
	})();
	$: updatesCount = installed.filter((i) => i.updateAvailable).length;
	$: registryUpdatesCount = installed.filter(
		(i) => i.updateAvailable && i.registryId === selectedRegistryId
	).length;
	$: updateAllRegistryName = updateAllRegistryId
		? registries.find((r) => r.id === updateAllRegistryId)?.name || ''
		: '';
	$: installedRows = [...installed].sort((a, b) =>
		String(a.packageId || '').localeCompare(String(b.packageId || ''))
	);

	// the Bait Box an install came from, or null when it was removed.
	function installRegistryName(i) {
		return i.registryRemoved ? null : i.registryName || null;
	}
	// status of an install. update is only offered when the Bait Box answered with a
	// newer version; unknown means it did not answer or no longer lists the package.
	function installStatus(i) {
		const s = i.status;
		switch (s) {
			case 'updateAvailable':
				return {
					kind: s,
					label: i.currentVersion ? `Update to v${i.currentVersion}` : 'Update available',
					dot: 'bg-cta-blue dark:bg-highlight-blue',
					text: 'text-cta-blue dark:text-highlight-blue',
					title: ''
				};
			case 'upToDate':
				return {
					kind: s,
					label: 'Up to date',
					dot: 'bg-green-500',
					text: 'text-green-600 dark:text-green-400',
					title: ''
				};
			case 'tokenRefused':
				return {
					kind: s,
					label: 'Token refused',
					dot: 'bg-red-500',
					text: 'text-red-600 dark:text-red-400',
					title:
						`${sentence(i.statusMessage || 'the Bait Box refused the token')} ` +
						'Edit the Bait Box to enter a valid token. It can still be uninstalled.'
				};
			case 'registryRemoved':
				return {
					kind: s,
					label: 'Bait Box removed',
					dot: 'bg-gray-400',
					text: 'text-gray-500 dark:text-gray-400',
					title: 'The Bait Box this package came from was removed. It can still be uninstalled.'
				};
			default:
				return {
					kind: 'unknown',
					label: 'Unknown',
					dot: 'bg-gray-400',
					text: 'text-gray-500 dark:text-gray-400',
					title:
						'The Bait Box did not answer or no longer lists this package. ' +
						'It can still be uninstalled.'
				};
		}
	}

	const STATUS_LABELS = {
		ok: 'Connected',
		refused: 'Token refused',
		error: 'Not reachable',
		incompatible: 'Not compatible',
		checking: 'Checking'
	};

	function packageId(p) {
		return `${p.namespace}/${p.name}`;
	}
	function displayName(p) {
		return (p.title && p.title.trim()) || p.name;
	}
	function installFor(p) {
		return installedByKey[`${selectedRegistryId}/${packageId(p)}`];
	}
	// label is shown only for an update; the other kinds render fixed text.
	function statusOf(p) {
		const inst = installFor(p);
		if (inst && inst.updateAvailable) {
			return { label: `Update to v${inst.currentVersion}`, kind: 'update' };
		}
		if (inst) return { kind: 'installed' };
		return { kind: 'none' };
	}
	function previewSrcAt(p, i) {
		if (!selectedRegistryId || !p.previewImages || !p.previewImages[i]) return '';
		// proxy the image through the backend so it loads same origin; the browser CSP
		// (img-src 'self') blocks loading it cross origin directly from the registry.
		const url = encodeURIComponent(p.previewImages[i]);
		return rawApi.getPath(`/registry/${selectedRegistryId}/preview?url=${url}`);
	}
	function previewSrc(p) {
		return previewSrcAt(p, 0);
	}
	function varPlaceholder(type) {
		switch (type) {
			case 'domain':
				return 'example.com';
			case 'url':
				return 'https://example.com';
			case 'secret':
				return 'secret value';
			default:
				return '';
		}
	}
	// a readable label for a package type, so the UI shows "Page", not "page".
	const TYPE_LABELS = {
		page: 'Page',
		email: 'Email',
		domain: 'Domain',
		proxy: 'Proxy',
		script: 'Script',
		asset: 'Asset',
		attachment: 'Attachment',
		smtp: 'SMTP',
		webhook: 'Webhook',
		remotebrowser: 'Remote browser',
		template: 'Campaign template',
		reporttemplate: 'Report template',
		allowdeny: 'Allow / deny',
		oauthprovider: 'OAuth provider',
		apisender: 'API sender',
		identifier: 'Identifier',
		bundle: 'Bundle'
	};
	function typeLabel(t) {
		return TYPE_LABELS[t] || (t ? t.charAt(0).toUpperCase() + t.slice(1) : '');
	}
	// the All option carries no count and is handled by the caller.
	function typeOptionLabel(t) {
		return `${typeLabel(t)} (${typeCounts[t] || 0})`;
	}
	// map a bundled file to a monaco language so the viewer highlights it.
	function fileLang(path) {
		const ext = (path.split('.').pop() || '').toLowerCase();
		switch (ext) {
			case 'html':
			case 'htm':
				return 'html';
			case 'js':
				return 'javascript';
			case 'css':
				return 'css';
			case 'json':
				return 'json';
			case 'yaml':
			case 'yml':
				return 'yaml';
			case 'svg':
				return 'xml';
			case 'md':
				return 'markdown';
			default:
				return 'plaintext';
		}
	}
	// the meta file (data.yaml) describes the resource rather than being its content,
	// so it sorts last; real content (html, js, proxy.yaml) comes first.
	function isMetaFile(path) {
		return /(^|\/)data\.ya?ml$/i.test(path);
	}
	$: detailFiles = detailFull?.files
		? [...detailFull.files].sort((a, b) => Number(isMetaFile(a.path)) - Number(isMetaFile(b.path)))
		: [];
	// the first real content file (the html or js, not the meta file) opens by
	// default so the package content is visible without a click.
	$: primaryFilePath =
		detailFiles.find((f) => f.kind === 'text' && !isMetaFile(f.path))?.path || '';

	// preview gallery for the detail modal: every resolvable screenshot, and the
	// thumbnail the user is currently viewing. resets to the first image on open.
	let galleryIndex = 0;
	$: detailPreviews =
		detail && detail.previewImages
			? detail.previewImages.map((_, i) => previewSrcAt(detail, i)).filter(Boolean)
			: [];
	$: if (detail) galleryIndex = 0;
	// the installed record for the open detail package, so the modal can show the
	// installed version alongside the latest one.
	$: detailInst = detail ? installFor(detail) : null;
	// referenced packages that no longer resolve on the Bait Box; a bundle with any
	// of these cannot be installed, so the detail view refuses install with a reason.
	// uninstalling an existing install is unaffected (it uses local records), so this
	// only gates a fresh install.
	$: brokenDeps = detailFull?.brokenDependencies || [];
	$: refusedDeps = detailFull?.refusedDependencies || [];

	// the enlarged preview overlay shows one screenshot at full size. zoomIndex is the
	// index into detailPreviews being shown, or -1 when the overlay is closed.
	let zoomIndex = -1;
	$: zoomOpen = zoomIndex >= 0 && zoomIndex < detailPreviews.length;
	function openZoom(i) {
		zoomIndex = i;
	}
	function closeZoom() {
		zoomIndex = -1;
	}
	// step to the previous or next screenshot, wrapping around, so the overlay can be
	// paged without closing it. a no op when there is only one image.
	function zoomStep(delta) {
		const n = detailPreviews.length;
		if (n < 2 || zoomIndex < 0) return;
		zoomIndex = (zoomIndex + delta + n) % n;
	}

	// the manifest files describe the package rather than being its content.
	function isManifestFile(path) {
		return isMetaFile(path) || /(^|\/)baitbox\.json$/i.test(path);
	}
	// group the inspect file list so the detail modal can show one tab per
	// resource category. content is everything that is neither an asset nor a
	// manifest, so nothing is dropped.
	$: assetFiles = detailFiles.filter((f) => !isManifestFile(f.path) && /^assets\//i.test(f.path));
	$: manifestFiles = detailFiles.filter((f) => isManifestFile(f.path));
	// the settings a package sets per asset file, keyed by the file path in the
	// archive so the assets tab can show them on the matching file row.
	$: assetMetaByFile = Object.fromEntries((detailFull?.assets || []).map((a) => [a.file, a]));
	// the variable a value is when it is exactly one @@NAME@@ token, which the
	// installer fills, such as a filter chosen at install.
	function tokenVar(v) {
		const m = String(v || '').match(/^@@([A-Za-z0-9_]+)@@$/);
		return m ? m[1] : '';
	}
	// the paths of files declared as email attachments, so they are shown in the
	// attachments section rather than loose in the content tab.
	$: attachmentPaths = new Set((detailFull?.attachments || []).map((a) => a.file));
	$: contentFiles = detailFiles.filter(
		(f) => !isManifestFile(f.path) && !/^assets\//i.test(f.path) && !attachmentPaths.has(f.path)
	);
	// each attachment paired with its matching file so the details tab can show a
	// thumbnail, size and type alongside the declared name and flags.
	$: attachmentList = (detailFull?.attachments || []).map((a) => {
		const file = (detailFull?.files || []).find((f) => f.path === a.file);
		const basename = a.file.split('/').pop();
		const display = (a.name || '').trim() || basename;
		return {
			...a,
			basename,
			display,
			dataUri: file?.dataUri,
			size: file?.size,
			kind: file?.kind
		};
	});
	// show the owning email name per attachment only when more than one email
	// contributes attachments, otherwise it is noise.
	$: attachmentEmailNames = new Set(attachmentList.map((a) => a.emailName).filter(Boolean));
	$: showAttachmentEmailName = attachmentEmailNames.size > 1;
	$: detailTabs = [
		{ id: 'details', label: 'Details' },
		...(contentFiles.length
			? [{ id: 'content', label: (detail && typeLabel(detail.type)) || 'Content' }]
			: []),
		...(attachmentList.length ? [{ id: 'attachments', label: 'Attachments' }] : []),
		...(assetFiles.length ? [{ id: 'assets', label: 'Assets' }] : []),
		...(manifestFiles.length ? [{ id: 'manifest', label: 'Manifest' }] : [])
	];
	let activeDetailTab = 'details';
	$: if (detail) activeDetailTab = 'details';
	$: activeFiles =
		activeDetailTab === 'content'
			? contentFiles
			: activeDetailTab === 'assets'
				? assetFiles
				: activeDetailTab === 'manifest'
					? manifestFiles
					: [];

	// per file view mode in the content tab: source (monaco) or a rendered html
	// preview. defaults to source and clears when a different package opens.
	let fileView = {};
	$: if (detail) fileView = {};
	function setFileView(path, mode) {
		fileView = { ...fileView, [path]: mode };
	}

	// inline the package's bundled image assets as data uris so an html preview
	// shows its images. references of the form {{.BaseURL}}/<path> and the bare
	// assets/<path> are swapped, where path is the asset path without assets/.
	// other send time template tokens are left untouched.
	function inlineAssets(html) {
		if (!html) return '';
		let out = String(html);
		for (const f of detailFull?.files || []) {
			if (f.kind !== 'image' || !f.dataUri) continue;
			const rel = f.path.replace(/^assets\//i, '');
			const esc = rel.replace(/[.*+?^${}()|[\]\\]/g, '\\$&');
			out = out.replace(new RegExp('\\{\\{\\.BaseURL\\}\\}/' + esc, 'g'), () => f.dataUri);
			out = out.replace(new RegExp('assets/' + esc, 'g'), () => f.dataUri);
		}
		return out;
	}

	// post the processed html to the isolated preview endpoint, which returns it
	// under a sandbox policy with an opaque origin. the frame is also sandboxed
	// without allow-same-origin, so previewed scripts never reach the admin origin.
	function renderPreview(node, html) {
		submitPreviewForm(html, node.name, PACKAGE_PREVIEW_ENDPOINT);
		return {
			update(nextHtml) {
				submitPreviewForm(nextHtml, node.name, PACKAGE_PREVIEW_ENDPOINT);
			}
		};
	}

	function recompute() {
		const q = (tableURLParams.search || '').toLowerCase();
		let rows = allPackages.filter((p) => {
			if (!q) return true;
			return (
				(p.name || '').toLowerCase().includes(q) ||
				(p.title || '').toLowerCase().includes(q) ||
				(p.description || '').toLowerCase().includes(q) ||
				(p.type || '').toLowerCase().includes(q)
			);
		});
		if (installFilter !== 'all') {
			rows = rows.filter((p) => {
				const inst = installFor(p);
				if (installFilter === 'updates') return !!inst?.updateAvailable;
				return installFilter === 'installed' ? !!inst : !inst;
			});
		}
		if (typeFilter !== 'all') {
			rows = rows.filter((p) => p.type === typeFilter);
		}
		const sortBy = tableURLParams.sortBy || 'name';
		const desc = (tableURLParams.sortOrder || 'asc') === 'desc';
		const key = (p) =>
			sortBy === 'type' ? p.type || '' : sortBy === 'version' ? p.version || '' : p.name || '';
		rows.sort((a, b) => String(key(a)).localeCompare(String(key(b))) * (desc ? -1 : 1));
		const per = tableURLParams.perPage || 10;
		const pageNum = tableURLParams.currentPage || 1;
		const start = (pageNum - 1) * per;
		hasNextPage = start + per < rows.length;
		visibleRows = rows.slice(start, start + per);
	}

	// filter the grid whenever a toolbar filter changes. driving recompute from
	// the state (not only the select callback) makes the filtering apply reliably.
	$: installFilter, typeFilter, recompute();

	// the Updates option is offered only while this Bait Box has updates.
	$: hasUpdates = allPackages.some(
		(p) => installedByKey[`${selectedRegistryId}/${packageId(p)}`]?.updateAvailable
	);
	$: installFilterOptions = ['All', 'Installed', 'Available', ...(hasUpdates ? ['Updates'] : [])];
	$: if (installFilter === 'updates' && !hasUpdates) installFilter = 'all';

	async function loadRegistries() {
		const res = await api.registry.list();
		if (res.success) {
			registries = res.data?.registries || [];
			if (!selectedRegistryId && registries.length > 0) selectedRegistryId = registries[0].id;
			loadRegistryStatuses();
		} else {
			addToast(serverError(res, 'Could not load Bait Boxes'), 'Error');
		}
	}
	// probe every registry at once and fill in each status as its answer arrives.
	// each load gets a new generation number, so an answer from an older load that
	// arrives late is dropped instead of overwriting a newer status.
	let statusGeneration = 0;
	function loadRegistryStatuses() {
		const generation = ++statusGeneration;
		const next = {};
		for (const r of registries) next[r.id] = { state: 'checking', reason: '' };
		registryStatus = next;
		for (const r of registries) {
			api.registry.status(r.id).then((res) => {
				if (generation !== statusGeneration || !(r.id in registryStatus)) return;
				const st = res.data?.status;
				registryStatus = {
					...registryStatus,
					[r.id]: !res.success
						? { state: 'error', reason: res.error || 'could not check the connection' }
						: st?.tokenRefused
							? { state: 'refused', reason: st.connectError || '' }
							: !st?.connected
								? { state: 'error', reason: st?.connectError || 'not reachable' }
								: st?.incompatible
									? { state: 'incompatible', reason: st.incompatible }
									: { state: 'ok', reason: '' }
				};
			});
		}
	}
	async function loadInstalled() {
		installedLoading = true;
		const res = await api.registry.installed();
		installedLoading = false;
		if (res.success) installed = res.data?.installed || [];
		else addToast(serverError(res, 'Could not load installed packages'), 'Error');
		recompute();
	}
	async function loadBrowse() {
		if (!selectedRegistryId) {
			allPackages = [];
			tagColors = {};
			browseError = '';
			recompute();
			return;
		}
		const registryId = selectedRegistryId;
		showIsLoading();
		const res = await api.registry.browse(registryId);
		hideIsLoading();
		// another Bait Box may have been picked while this one loaded
		if (registryId !== selectedRegistryId) return;
		if (res.success) {
			allPackages = res.data?.packages || [];
			tagColors = res.data?.tagColors || {};
			browseError = '';
		} else {
			allPackages = [];
			tagColors = {};
			browseError = serverError(res, 'Could not reach this Bait Box or load its packages');
		}
		recompute();
	}
	// changing the installed filter resets to the first page so the new, shorter
	// result set is not viewed from a page that no longer exists. the select shows
	// the capitalized label, which maps back to the lowercase filter value.
	function onInstallFilterSelect(label) {
		installFilter = (label || 'All').toLowerCase();
		tableURLParams.currentPage = 1;
		recompute();
	}
	// the type select shows typeLabel strings, so map the chosen label back to its
	// raw type key ('all' for the All option).
	function onTypeFilterSelect(label) {
		typeFilter =
			!label || label === 'All'
				? 'all'
				: availableTypes.find((t) => typeOptionLabel(t) === label) || 'all';
		tableURLParams.currentPage = 1;
		recompute();
	}
	function onRegistryChange(name) {
		const r = registries.find((x) => x.name === name);
		if (!r) return;
		selectedRegistryId = r.id;
		loadBrowse();
	}

	function openAddRegistry() {
		regName = '';
		regURL = '';
		regToken = '';
		regInsecure = 'false';
		addError = '';
		showAddRegistry = true;
	}
	async function submitAddRegistry() {
		addSubmitting = true;
		const res = await api.registry.add(regName, regURL, regToken, regInsecure === 'true');
		addSubmitting = false;
		if (res.success) {
			addToast('Bait Box added', 'Success');
			showAddRegistry = false;
			await loadRegistries();
			selectedRegistryId = registries[0]?.id || selectedRegistryId;
			await loadBrowse();
		} else {
			addError = serverError(res, 'Could not add Bait Box');
		}
	}

	function openEditRegistry() {
		if (!selectedRegistry) return;
		editRegId = selectedRegistry.id;
		editName = selectedRegistry.name;
		editURL = selectedRegistry.url;
		editToken = '';
		editClearToken = false;
		editHadToken = !!selectedRegistry.hasToken;
		editInsecure = selectedRegistry.insecureTLS ? 'true' : 'false';
		editError = '';
		showEditRegistry = true;
	}
	async function submitEditRegistry() {
		editSubmitting = true;
		const res = await api.registry.updateRegistry(
			editRegId,
			editName,
			editURL,
			editClearToken ? '' : editToken,
			editInsecure === 'true',
			editClearToken
		);
		editSubmitting = false;
		if (res.success) {
			addToast('Bait Box updated', 'Success');
			showEditRegistry = false;
			const keepId = editRegId;
			await loadRegistries();
			selectedRegistryId = registries.some((r) => r.id === keepId) ? keepId : selectedRegistryId;
			await loadBrowse();
		} else {
			editError = serverError(res, 'Could not update Bait Box');
		}
	}

	async function openDetail(p) {
		detail = p;
		showDetail = true;
		await loadDetailFull();
	}
	// fetch the full inspect result for the open detail package. also the retry target,
	// so a failed load can be run again from the modal without reopening it.
	async function loadDetailFull() {
		if (!detail) return;
		detailFull = null;
		detailError = '';
		openFiles = {};
		detailLoading = true;
		const res = await api.registry.inspect(selectedRegistryId, detail.namespace, detail.name);
		detailLoading = false;
		if (res.success) {
			detailFull = res.data?.package || null;
			// open the primary content file so its toggle starts in a defined state and
			// the first click collapses it as expected. computed here from the response
			// rather than the derived, which has not recomputed yet in this tick.
			const primary = (detailFull?.files || []).find(
				(f) => f.kind === 'text' && !isMetaFile(f.path)
			);
			if (primary) openFiles = { [primary.path]: true };
		} else {
			detailError = serverError(res, 'Could not load full package details');
		}
	}
	function toggleFile(path) {
		openFiles = { ...openFiles, [path]: !openFiles[path] };
	}

	// returns the {success} shape the variables modal expects: a required values prompt
	// (409 missing) is success, since install continues in that modal. overwrite lists
	// the report template kinds the user confirmed the package may overwrite.
	async function doInstall(p, variables, overwrite) {
		showIsLoading();
		const res = await rawApi.registry.install(
			selectedRegistryId,
			p.namespace,
			p.name,
			variables,
			overwrite || []
		);
		hideIsLoading();
		if (res.success) {
			addToast(`Installed ${p.name}`, 'Success');
			showVars = false;
			showDetail = false;
			confirmInstallVisible = false;
			await loadInstalled();
			refreshInstalledResources();
			return { success: true };
		}
		if (res.statusCode === 409 && res.data?.missing) {
			// collects the values that only referenced packages declare. hand off to the
			// required values modal and close the install modal so the two never stack.
			// the values already given are kept and resent with the missing ones.
			confirmInstallVisible = false;
			openVars(res.data.missing, {
				kind: 'install',
				p,
				variables: { ...(variables || {}) },
				overwrite: [...(overwrite || [])]
			});
			return { success: true };
		}
		if (res.statusCode === 409 && res.data?.reportTemplates) {
			openReportTemplateOverwrite(res.data.reportTemplates, {
				kind: 'install',
				p,
				variables: { ...(variables || {}) }
			});
			return { success: true };
		}
		const installError = serverError(res, 'Install failed');
		addToast(installError, 'Error');
		return { success: false, error: installError };
	}
	// resolutions maps "<resourceType>:<sourceName>" to keep | overwrite | both,
	// sent when resolving conflicts. empty means none.
	// overwrite lists the report template kinds the user confirmed may be overwritten.
	async function update(inst, variables, resolutions, overwrite) {
		showIsLoading();
		const res = await rawApi.registry.update(
			inst.id,
			variables || {},
			resolutions || {},
			overwrite || []
		);
		hideIsLoading();
		if (res.success) {
			addToast(`Updated ${inst.packageId}`, 'Success');
			const skipped = res.data?.result?.skipped || [];
			const kept = skipped.filter(isKeptEdit).map(keptName);
			if (kept.length) addToast(`Kept your local edits: ${kept.join(', ')}`, 'Info');
			const keptDropped = skipped.filter(isKeptDropped).map(keptName);
			if (keptDropped.length) {
				addToast(
					`Kept your edited copies that the new version removed: ${keptDropped.join(', ')}`,
					'Info'
				);
			}
			const keptCurrent = skipped.filter((m) => m.includes(KEPT_CURRENT)).map(keptName);
			if (keptCurrent.length)
				addToast(`Kept the current settings of: ${keptCurrent.join(', ')}`, 'Info');
			toastUpdateNotes(`Update notes for ${inst.packageId}`, updateNotes(skipped).map(capitalize));
			showVars = false;
			showDetail = false;
			conflictsVisible = false;
			await loadInstalled();
			refreshInstalledResources();
			return { success: true };
		}
		if (res.statusCode === 409 && res.data?.missing) {
			// keep the values and conflict choices already given, so submitting the
			// missing values resends them instead of falling back to keep for every
			// conflict.
			conflictsVisible = false;
			openVars(res.data.missing, {
				kind: 'update',
				inst,
				variables: { ...(variables || {}) },
				resolutions: { ...(resolutions || {}) },
				overwrite: [...(overwrite || [])]
			});
			return { success: true };
		}
		if (res.statusCode === 409 && res.data?.conflicts) {
			openConflicts(inst, variables || {}, res.data.conflicts);
			return { success: true };
		}
		if (res.statusCode === 409 && res.data?.reportTemplates) {
			openReportTemplateOverwrite(res.data.reportTemplates, {
				kind: 'update',
				inst,
				variables: { ...(variables || {}) },
				resolutions: { ...(resolutions || {}) }
			});
			return { success: true };
		}
		const updateError = serverError(res, 'Update failed');
		addToast(updateError, 'Error');
		return { success: false, error: updateError };
	}

	function openVars(defs, p) {
		varDefs = defs || [];
		varValues = {};
		for (const d of varDefs) varValues[varKeyOf(d)] = '';
		varsError = '';
		pending = p;
		showVars = true;
		loadPickEntities(varDefs);
	}
	// what the required values modal finishes, for its text.
	$: varsAction = pending?.kind === 'update' ? 'update' : 'install';
	async function submitVars() {
		// a required pick whose entity kind the operator owns none of cannot be satisfied
		// here. say so plainly with the kinds to create, not a cryptic "<name> is required".
		if (installBlockedByMissing) {
			const kinds = [...new Set(missingPickDefs.map((d) => pickLabel(d.pick)))];
			varsError =
				`Create the required ${kinds.join(', ')} in Phishing Club first, ` +
				`then reopen this ${varsAction}.`;
			return;
		}
		// every variable in this modal is required (it is the missing required set), so
		// a blank one (including an unselected enum dropdown) must block the submit.
		const blank = varDefs.find((d) => !String(varValues[varKeyOf(d)] ?? '').trim());
		if (blank) {
			varsError = blank.scope
				? `${blank.name} for ${blank.scope} is required.`
				: `${blank.name} is required.`;
			return;
		}
		varsError = '';
		varsSubmitting = true;
		if (pending?.kind === 'install')
			await doInstall(
				pending.p,
				{ ...(pending.variables || {}), ...varValues },
				pending.overwrite || []
			);
		else if (pending?.kind === 'update')
			await update(
				pending.inst,
				{ ...(pending.variables || {}), ...varValues },
				pending.resolutions || {},
				pending.overwrite || []
			);
		varsSubmitting = false;
	}

	function openRemoveInstall(inst) {
		removeInstallTarget = inst;
		removeInstallVisible = true;
		loadRemovePreview(inst);
	}
	async function loadRemovePreview(inst) {
		removePreview = null;
		removePreviewError = '';
		if (!inst) return;
		removePreviewLoading = true;
		const res = await api.registry.uninstallPreview(inst.id);
		// a different package may have been opened while this one loaded.
		if (removeInstallTarget !== inst) return;
		removePreviewLoading = false;
		if (res.success) removePreview = res.data?.preview || null;
		else removePreviewError = serverError(res, 'Could not check which campaigns use this package');
	}
	$: removeCampaigns = removePreview?.campaigns || [];
	$: removeCampaignCount = removePreview?.campaignCount || 0;
	$: removeKeepRunning = removePreview?.keepRunning || [];
	$: removeKeepRunningCount = removePreview?.keepRunningCount || 0;
	$: removeChanged = removePreview?.changed || [];
	$: removeChangedCount = removePreview?.changedCount || 0;
	$: removeChangedOpen = removeChanged.some((c) => c.openToEveryone);
	$: removeUserAssetCount = removePreview?.userAssetCount || 0;
	$: removeChangedAssets = removePreview?.changedAssets || [];
	$: removeChangedAssetCount = removePreview?.changedAssetCount || 0;
	$: removeChangedAssetsOpen = removeChangedAssets.some((a) => a.openToEveryone);
	// where an asset is served, its domain and path, or the path for a global asset.
	function assetWhere(a) {
		return a.domainName ? `${a.domainName}/${a.path}` : a.path;
	}
	// the lines listed under an asset that loses filters. an asset left with no
	// filter at all says so when that is not already said by the first line.
	function assetLossLines(a) {
		const lines = [...(a.losses || [])];
		if (a.unfiltered && !a.openToEveryone) lines.push('No filter left on this asset');
		return lines;
	}
	// a close time from the preview, shown in the browser's local time.
	function formatCloseAt(v) {
		const d = new Date(v);
		return isNaN(d.getTime()) ? v : d.toLocaleString();
	}
	$: removeEdited = removePreview?.edited || [];
	$: removeMembers = removePreview?.members || [];
	$: removeUsedBy = removePreview?.usedBy || [];
	$: removeKeptLists = removePreview?.keptLists || [];
	// readable singular noun per resource type for the uninstall summary, so the confirm
	// names what is actually removed rather than a generic "pages and emails".
	const RESOURCE_NOUNS = {
		page: 'landing page',
		email: 'email',
		asset: 'asset',
		domain: 'domain',
		proxy: 'proxy',
		script: 'script',
		smtp: 'SMTP configuration',
		webhook: 'webhook',
		oauthprovider: 'OAuth provider',
		apisender: 'API sender',
		remotebrowser: 'remote browser',
		allowdeny: 'allow and deny filter',
		reporttemplate: 'report template',
		campaigntemplate: 'campaign template',
		identifier: 'identifier'
	};
	function resourceNoun(type, n) {
		const base = RESOURCE_NOUNS[type] || type;
		return n === 1 ? base : base + 's';
	}
	// the resources a given installed package provisioned, grouped and counted by type,
	// read from the provenance map (keyed `type:id`) and matched by package id and
	// registry, so it names exactly what uninstalling this package removes.
	function removeBreakdownFor(map, target) {
		const counts = {};
		if (!map || !target) return [];
		for (const [key, info] of Object.entries(map)) {
			if (info.packageId !== target.packageId || info.registryId !== target.registryId) continue;
			const type = key.slice(0, key.indexOf(':'));
			counts[type] = (counts[type] || 0) + 1;
		}
		return Object.entries(counts)
			.sort((a, b) => a[0].localeCompare(b[0]))
			.map(([type, n]) => ({ type, n }));
	}
	// the bullet list for the uninstall confirm. falls back to a generic line until
	// the provenance map has loaded.
	function buildRemoveList(map, target, members) {
		if (!target) return [];
		const breakdown = removeBreakdownFor(map, target);
		const bullets = [];
		// a report template is the global one of its kind, overwritten on install, so
		// uninstall writes its earlier content back instead of deleting it.
		const deleted = breakdown.filter((b) => b.type !== 'reporttemplate');
		const hasReportTemplate = deleted.length !== breakdown.length;
		if (breakdown.length) {
			if (deleted.length) {
				const phrases = deleted.map(({ type, n }) => `${n} ${resourceNoun(type, n)}`);
				const joined =
					phrases.length === 1
						? phrases[0]
						: phrases.slice(0, -1).join(', ') + ' and ' + phrases[phrases.length - 1];
				bullets.push(`Deletes ${joined}`);
			}
			if (breakdown.some((b) => b.type === 'asset')) {
				bullets.push('Assets that already existed are left in place');
			}
			if (hasReportTemplate) {
				bullets.push(
					'The report template this package overwrote gets back the content it had ' +
						'before the install. Edits made to it since then are lost'
				);
			}
		} else {
			bullets.push('The content this package installed is deleted');
		}
		// a bundle, or any package that depends on others (a page that uses an allow
		// / deny list package), pulls in the packages it references.
		const noun = target.packageType === 'bundle' ? 'bundle' : 'package';
		if (target.packageType === 'bundle' || members?.length) {
			if (members === null) {
				bullets.push(
					`The packages this ${noun} pulled in are also removed, unless another ` +
						'installed package still uses them'
				);
			} else if (members.length) {
				bullets.push(`Also removes the packages this ${noun} pulled in: ${members.join(', ')}`);
			} else {
				bullets.push(
					`The packages this ${noun} pulled in stay, since other installs still use them`
				);
			}
		}
		return bullets;
	}
	$: removeInstallList = buildRemoveList(
		$installedResources,
		removeInstallTarget,
		removePreview ? removeMembers : null
	);
	// the bullet list for the update confirm: the concrete content updated, and an honest
	// line about local edits. a local edit is kept unless the new version also changed that
	// same item, in which case the resolve step asks what to do, so the text must not
	// promise edits are never overwritten.
	function buildUpdateList(map, target) {
		if (!target) return [];
		const breakdown = removeBreakdownFor(map, target);
		const bullets = [];
		if (breakdown.length) {
			const phrases = breakdown.map(({ type, n }) => `${n} ${resourceNoun(type, n)}`);
			const joined =
				phrases.length === 1
					? phrases[0]
					: phrases.slice(0, -1).join(', ') + ' and ' + phrases[phrases.length - 1];
			bullets.push(`Updates ${joined} to the new version`);
		} else {
			bullets.push('The installed content is updated to the new version');
		}
		bullets.push(
			'Your local edits are kept. If the new version also changed something you ' +
				'edited, you choose how to resolve it'
		);
		return bullets;
	}
	$: updateInstallList = buildUpdateList($installedResources, confirmUpdateInst);
	async function confirmRemoveInstall() {
		if (!removeInstallTarget) return { success: false };
		const res = await api.registry.uninstall(removeInstallTarget.id);
		if (res.success) {
			addToast('Removed', 'Success');
			showDetail = false;
			await loadInstalled();
			refreshInstalledResources();
			return res;
		}
		return { ...res, error: serverError(res, 'Could not remove the package') };
	}
	async function confirmRemoveRegistry() {
		if (!selectedRegistry) return { success: false };
		const res = await api.registry.remove(selectedRegistry.id);
		if (res.success) {
			addToast('Bait Box removed', 'Success');
			selectedRegistryId = '';
			await loadRegistries();
			await loadBrowse();
			return res;
		}
		return { ...res, error: serverError(res, 'Could not remove the Bait Box') };
	}

	onMount(() => {
		tableURLParams.onChange(recompute);
		(async () => {
			await loadRegistries();
			// the installed list asks each registry for its latest version, which can be
			// slow when a registry is unreachable, so it loads alongside browse rather
			// than before it.
			loadInstalled();
			await loadBrowse();
			// provenance of installed entities, so the uninstall confirm can name what it removes
			loadInstalledResources();
		})();
		return () => tableURLParams.unsubscribe();
	});
</script>

<HeadTitle title="Bait Box" />
<main>
	<div class="flex flex-wrap items-start justify-between gap-4">
		<Headline>Bait Box</Headline>
		<div class="flex flex-wrap items-end gap-3">
			{#if registries.length && activeView === 'browse'}
				<div class="w-56">
					<TextFieldSelect
						id="registry"
						size="full"
						hideLabel={true}
						options={registryOptions}
						value={selectedRegistryName}
						onSelect={onRegistryChange}
						placeholder="Select Bait Box"
					/>
				</div>
				{#if selectedStatus}
					<!-- reachability of the selected Bait Box, filled in after the list shows -->
					<span
						id="registry-status"
						title={selectedStatus.reason || ''}
						class="flex h-10 shrink-0 items-center gap-1.5 text-sm {selectedStatus.state === 'ok' ||
						selectedStatus.state === 'checking'
							? 'text-gray-500 dark:text-gray-400'
							: 'text-red-600 dark:text-red-400'}"
					>
						<span
							class="inline-block h-2 w-2 rounded-full {selectedStatus.state === 'ok'
								? 'bg-green-500'
								: selectedStatus.state === 'checking'
									? 'animate-pulse bg-gray-400'
									: 'bg-red-500'}"
						></span>
						{STATUS_LABELS[selectedStatus.state] || 'Checking'}
					</span>
				{/if}
				<!-- adding a Bait Box stays a visible primary action so a second one is discoverable -->
				<Button
					size="small"
					css="!w-auto shrink-0 whitespace-nowrap px-5"
					on:click={openAddRegistry}>Add Bait Box</Button
				>
				<!-- edit and remove act on the selected Bait Box, so they stay in an overflow menu next to it -->
				{#if selectedRegistry}
					<div
						class="h-10 w-10 rounded-md transition-colors hover:bg-grayblue-light dark:hover:bg-gray-800/60"
					>
						<TableDropDownEllipsis>
							<TableDropDownButton name="Edit" on:click={openEditRegistry} />
							<TableDeleteButton name="Remove" on:click={() => (removeRegistryVisible = true)} />
						</TableDropDownEllipsis>
					</div>
				{/if}
			{/if}
		</div>
	</div>

	<nav class="mt-6 border-b border-gray-200 dark:border-gray-700">
		<div class="flex flex-wrap" role="tablist" aria-label="Bait Box views">
			{#each [{ id: 'browse', label: 'Browse' }, { id: 'installed', label: installed.length ? `Installed (${installed.length})` : 'Installed' }] as view (view.id)}
				<button
					type="button"
					role="tab"
					id={`bb-view-${view.id}`}
					aria-selected={activeView === view.id}
					aria-controls={`bb-view-panel-${view.id}`}
					on:click={() => (activeView = view.id)}
					class="px-6 py-3 text-sm font-medium border-b-2 transition-colors
						{activeView === view.id
						? 'border-cta-blue dark:border-highlight-blue text-cta-blue dark:text-highlight-blue'
						: 'border-transparent text-gray-500 dark:text-gray-400 hover:text-gray-700 dark:hover:text-gray-300 hover:border-gray-300 dark:hover:border-gray-600'}"
				>
					{view.label}
				</button>
			{/each}
		</div>
	</nav>

	<div role="tabpanel" id={`bb-view-panel-${activeView}`} aria-labelledby={`bb-view-${activeView}`}>
		{#if activeView === 'installed'}
			<!-- every install from local records, so installs whose Bait Box is removed,
		not reachable, or no longer lists the package can still be uninstalled -->
			<div class="mt-6">
				{#if updatesCount}
					<div class="mb-4 flex justify-end">
						<Button
							size="small"
							css="!w-auto shrink-0 whitespace-nowrap px-5"
							on:click={() => openUpdateAll()}>Update all ({updatesCount})</Button
						>
					</div>
				{/if}
				<Table
					columns={[
						{ column: 'Package', size: 'large' },
						{ column: 'Version', size: 'small' },
						{ column: 'Bait Box', size: 'medium' },
						{ column: 'Installed as', size: 'small' },
						{ column: 'Status', size: 'medium' }
					]}
					hasData={!!installedRows.length}
					hasNextPage={false}
					plural="installed packages"
					isGhost={installedLoading}
				>
					{#each installedRows as i (i.id)}
						{@const st = installStatus(i)}
						{@const regName = installRegistryName(i)}
						<TableRow>
							<TableCell value={i.packageId || ''} />
							<TableCell value={i.version ? `v${i.version}` : ''} />
							<TableCell>
								{#if regName}
									<span title={regName}>{regName}</span>
								{:else}
									<span class="italic text-gray-500 dark:text-gray-400">Bait Box removed</span>
								{/if}
							</TableCell>
							<TableCell value={i.installedAsDependency ? 'Dependency' : 'Direct'} />
							<TableCell>
								<span title={st.title || st.label} class="text-xs font-semibold {st.text}">
									<span class="mr-1.5 inline-block h-1.5 w-1.5 rounded-full align-middle {st.dot}"
									></span>{st.label}
								</span>
							</TableCell>
							<TableCellEmpty />
							<TableCellAction>
								<TableDropDownEllipsis>
									{#if st.kind === 'updateAvailable' && regName}
										<TableDropDownButton name="Update" on:click={() => openUpdate(i)} />
									{/if}
									<TableDeleteButton name="Uninstall" on:click={() => openRemoveInstall(i)} />
								</TableDropDownEllipsis>
							</TableCellAction>
						</TableRow>
					{/each}
				</Table>
			</div>
		{:else}
			<!-- a failed package load shows its reason in the card below instead -->
			{#if browseError}{:else if selectedStatus?.state === 'refused'}
				<p
					id="registry-status-reason"
					class="mt-2 text-right text-sm text-red-600 dark:text-red-400 break-words"
				>
					{selectedRegistry?.name}: {selectedStatus.reason}. Edit the Bait Box to enter a valid
					token.
				</p>
			{:else if selectedStatus?.state === 'error'}
				<p
					id="registry-status-reason"
					class="mt-2 text-right text-sm text-red-600 dark:text-red-400 break-words"
				>
					{selectedRegistry?.name} is not reachable: {selectedStatus.reason}
				</p>
			{:else if selectedStatus?.state === 'incompatible'}
				<p
					id="registry-status-reason"
					class="mt-2 text-right text-sm text-red-600 dark:text-red-400 break-words"
				>
					{sentence(selectedStatus.reason)}
				</p>
			{/if}

			{#if registries.length === 0}
				<div
					class="mt-8 flex flex-col items-center gap-4 rounded-md border-2 border-dashed border-gray-200 bg-grayblue-light/40 px-6 py-16 text-center dark:border-gray-700/60 dark:bg-gray-900/40"
				>
					<span
						class="flex h-14 w-14 items-center justify-center rounded-md bg-white text-cta-blue dark:bg-gray-800 dark:text-highlight-blue [&_svg]:h-8 [&_svg]:w-8"
						>{@html menuIcons.assets}</span
					>
					<div>
						<p class="text-lg font-semibold text-gray-700 dark:text-gray-200">
							No Bait Box connected
						</p>
						<p class="mt-1 text-sm text-gray-500 dark:text-gray-400">
							Add a Bait Box to browse and install ready made content.
						</p>
					</div>
					<Button on:click={openAddRegistry}>Add Bait Box</Button>
				</div>
			{:else}
				<div
					class="mt-8 flex flex-wrap items-center justify-between gap-4 border-b-2 border-gray-100 pb-4 dark:border-gray-800"
				>
					<div class="flex items-center gap-4">
						<Select pagination={tableURLParams}></Select>
						<div class="flex items-center gap-1.5">
							<label
								for="install-filter"
								class="text-gray-600 dark:text-gray-400 transition-colors duration-200"
								>Filter:</label
							>
							<TextFieldSelect
								id="install-filter"
								hideLabel={true}
								size="small"
								options={installFilterOptions}
								value={installFilter.charAt(0).toUpperCase() + installFilter.slice(1)}
								onSelect={onInstallFilterSelect}
							/>
						</div>
						{#if typeOptions.length > 1}
							<div class="flex items-center gap-1.5">
								<label
									for="type-filter"
									class="text-gray-600 dark:text-gray-400 transition-colors duration-200"
									>Type:</label
								>
								<TextFieldSelect
									id="type-filter"
									hideLabel={true}
									size="medium"
									options={typeOptions}
									value={typeFilter === 'all' ? 'All' : typeOptionLabel(typeFilter)}
									onSelect={onTypeFilterSelect}
								/>
							</div>
						{/if}
						{#if registryUpdatesCount}
							<Button
								size="small"
								css="!w-auto shrink-0 whitespace-nowrap px-5"
								on:click={() => openUpdateAll(selectedRegistryId)}
								>Update all ({registryUpdatesCount})</Button
							>
						{/if}
					</div>
					<Search pagination={tableURLParams}></Search>
				</div>

				{#if browseError}
					<!-- load failure, kept visually distinct from the no matches empty state so an
			unreachable Bait Box is not mistaken for an empty search result -->
					<div
						class="mt-8 flex flex-col items-center gap-4 rounded-md border-2 border-dashed border-red-200 bg-red-50/60 px-6 py-14 text-center dark:border-red-500/30 dark:bg-red-900/10"
					>
						<span
							class="flex h-12 w-12 items-center justify-center rounded-md bg-white text-red-600 shadow-sm dark:bg-gray-800 dark:text-red-400 [&_svg]:h-7 [&_svg]:w-7"
						>
							<svg
								viewBox="0 0 24 24"
								fill="none"
								stroke="currentColor"
								stroke-width="2"
								stroke-linecap="round"
								stroke-linejoin="round"
								><path
									d="M18.364 5.636a9 9 0 0 1 0 12.728M5.636 18.364a9 9 0 0 1 0-12.728M12 12h.01M8.5 8.5a5 5 0 0 0 0 7M15.5 8.5a5 5 0 0 1 0 7"
								/></svg
							>
						</span>
						<div>
							<p class="text-base font-semibold text-gray-700 dark:text-gray-200">
								Could not load packages
							</p>
							<p class="mt-1 text-sm text-gray-500 dark:text-gray-400">{browseError}</p>
							{#if selectedStatus?.state === 'refused'}
								<p class="mt-1 text-sm text-gray-500 dark:text-gray-400">
									Edit the Bait Box to enter a valid token.
								</p>
							{/if}
						</div>
						<Button size="small" on:click={loadBrowse}>Retry</Button>
					</div>
				{:else if visibleRows.length === 0}
					<div
						class="mt-8 rounded-md border-2 border-dashed border-gray-200 bg-grayblue-light/40 px-6 py-14 text-center text-sm text-gray-500 dark:border-gray-700/60 dark:bg-gray-900/40 dark:text-gray-400"
					>
						{allPackages.length
							? 'No packages match your search or filters.'
							: 'This Bait Box has no packages this token can read.'}
					</div>
				{:else}
					<div class="mt-6 grid grid-cols-1 gap-5 sm:grid-cols-2 lg:grid-cols-3 xl:grid-cols-4">
						{#each visibleRows as p (packageId(p))}
							{@const inst = installFor(p)}
							{@const st = statusOf(p)}
							<div
								class="group flex flex-col overflow-hidden rounded-md border-2 border-gray-200 bg-white transition-all duration-200 hover:border-gray-300 hover:shadow-theme dark:border-gray-700/60 dark:bg-gray-900/80 dark:hover:border-gray-600"
							>
								{#if previewSrc(p)}
									<!-- the preview mirrors the title action; the title button is the one
							tab stop per card, so the preview is removed from the tab and a11y tree -->
									<button
										type="button"
										tabindex="-1"
										aria-hidden="true"
										on:click={() => openDetail(p)}
										class="relative block h-32 w-full overflow-hidden border-b-2 border-gray-100 focus:outline-none dark:border-gray-800"
									>
										<img
											src={previewSrc(p)}
											alt={displayName(p)}
											class="h-full w-full object-cover transition-transform duration-200 group-hover:scale-[1.03]"
											on:error={(e) => (e.currentTarget.style.display = 'none')}
										/>
										<span
											class="absolute left-2 top-2 rounded bg-white/90 px-1.5 py-0.5 text-[10px] font-semibold uppercase tracking-wide text-gray-700 shadow-sm dark:bg-gray-900/80 dark:text-gray-200"
											>{typeLabel(p.type)}</span
										>
										<span
											class="absolute right-2 top-2 rounded bg-black/55 px-1.5 py-0.5 text-[10px] font-medium tabular-nums text-white"
											>v{p.version || '?'}</span
										>
										{#if p.previewImages && p.previewImages.length > 1}
											<span
												class="absolute bottom-2 right-2 inline-flex items-center gap-1 rounded bg-black/55 px-1.5 py-0.5 text-[10px] font-medium text-white"
											>
												<svg
													class="h-3 w-3"
													viewBox="0 0 24 24"
													fill="none"
													stroke="currentColor"
													stroke-width="2"
													stroke-linecap="round"
													stroke-linejoin="round"
													><rect x="3" y="7" width="12" height="11" rx="2" /><path
														d="M8 7V5a2 2 0 0 1 2-2h9a2 2 0 0 1 2 2v9a2 2 0 0 1-2 2h-2"
													/></svg
												>
												{p.previewImages.length}
											</span>
										{/if}
									</button>
								{:else}
									<button
										type="button"
										tabindex="-1"
										aria-hidden="true"
										on:click={() => openDetail(p)}
										class="flex h-32 w-full flex-col items-center justify-center gap-2 border-b-2 border-gray-100 bg-grayblue-light/50 px-4 text-center focus:outline-none dark:border-gray-800 dark:bg-gray-800/40"
									>
										<span
											class="flex h-12 w-12 items-center justify-center rounded-md bg-white text-cta-blue shadow-sm dark:bg-gray-900 dark:text-highlight-blue [&_svg]:h-7 [&_svg]:w-7"
											>{@html typeIconSvg(p.type)}</span
										>
										<span
											class="flex items-center gap-2 text-[10px] font-semibold uppercase tracking-wide text-gray-500 dark:text-gray-400"
										>
											<span>{typeLabel(p.type)}</span>
											<span class="tabular-nums text-gray-400 dark:text-gray-500"
												>v{p.version || '?'}</span
											>
										</span>
									</button>
								{/if}
								<div class="flex flex-1 flex-col p-4">
									<button
										type="button"
										on:click={() => openDetail(p)}
										class="truncate text-left text-base font-semibold text-gray-800 transition-colors duration-200 focus:outline-none focus-visible:rounded-sm focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-cta-blue group-hover:text-cta-blue dark:text-gray-100 dark:group-hover:text-highlight-blue dark:focus-visible:outline-highlight-blue"
										title={displayName(p)}
										aria-label={`View details of ${displayName(p)}`}>{displayName(p)}</button
									>
									{#if p.description}
										<p
											class="mt-1.5 line-clamp-2 text-xs leading-relaxed text-gray-500 dark:text-gray-400"
										>
											{p.description}
										</p>
									{/if}
									<!-- tags sit under the name with a comfortable gap, and mb-4 keeps a minimum
							gap to the footer below so they never touch the divider; the footer stays
							bottom aligned so the Install buttons line up across the row -->
									{#if p.tags && p.tags.length}
										<div class="mb-4 mt-3 flex flex-wrap gap-1">
											{#each p.tags as t}
												<span class="rounded px-1.5 py-0.5 text-[10px] {tagChipClass(tagColors[t])}"
													>{t}</span
												>
											{/each}
										</div>
									{/if}
									<div class="mt-auto border-t-2 border-gray-100 pt-3 dark:border-gray-800">
										{#if st.kind === 'update'}
											<div class="flex items-center gap-2">
												<Button size="full" css="flex-1" on:click={() => openUpdate(inst)}
													>Update</Button
												>
												<!-- remove stays quiet in an overflow menu so update is the only prominent action -->
												<TableDropDownEllipsis>
													<TableDeleteButton
														name="Uninstall"
														on:click={() => openRemoveInstall(inst)}
													/>
												</TableDropDownEllipsis>
											</div>
										{:else if st.kind === 'installed'}
											<div class="flex items-center justify-between gap-2">
												<span
													class="inline-flex min-w-0 items-center gap-1.5 text-xs font-semibold text-green-600 dark:text-green-400"
												>
													<span class="h-1.5 w-1.5 shrink-0 rounded-full bg-green-500"></span>
													Installed
												</span>
												<!-- remove stays quiet in an overflow menu rather than a loud red button -->
												<TableDropDownEllipsis>
													<TableDeleteButton
														name="Uninstall"
														on:click={() => openRemoveInstall(inst)}
													/>
												</TableDropDownEllipsis>
											</div>
										{:else}
											<!-- install is the card's primary action, sized to match Update on installed cards -->
											<div class="flex items-center gap-2">
												<Button size="full" css="flex-1" on:click={() => openInstall(p)}
													>Install</Button
												>
											</div>
										{/if}
									</div>
								</div>
							</div>
						{/each}
					</div>
					<Pagination paginator={tableURLParams} {hasNextPage} />
				{/if}
			{/if}
		{/if}
	</div>
</main>

<!-- package detail -->
{#if detail}
	<Modal
		headerText={displayName(detail)}
		visible={showDetail}
		onClose={() => (showDetail = false)}
		fullscreen={true}
	>
		<div class="mx-auto w-full max-w-6xl py-6">
			<nav class="mb-6 border-b border-gray-200 dark:border-gray-700">
				<div class="flex flex-wrap" role="tablist" aria-label="Package details">
					{#each detailTabs as tab}
						<button
							type="button"
							role="tab"
							id={`bb-tab-${tab.id}`}
							aria-selected={activeDetailTab === tab.id}
							aria-controls={`bb-panel-${tab.id}`}
							on:click={() => (activeDetailTab = tab.id)}
							class="px-6 py-3 text-sm font-medium border-b-2 transition-colors
								{activeDetailTab === tab.id
								? 'border-cta-blue dark:border-highlight-blue text-cta-blue dark:text-highlight-blue'
								: 'border-transparent text-gray-500 dark:text-gray-400 hover:text-gray-700 dark:hover:text-gray-300 hover:border-gray-300 dark:hover:border-gray-600'}"
						>
							{tab.label}
						</button>
					{/each}
				</div>
			</nav>

			<div
				role="tabpanel"
				id={`bb-panel-${activeDetailTab}`}
				aria-labelledby={`bb-tab-${activeDetailTab}`}
			>
				{#if activeDetailTab === 'details'}
					<div class="space-y-6">
						<div class="flex flex-col gap-5 sm:flex-row">
							{#if detailPreviews.length}
								<div class="sm:w-80 sm:shrink-0">
									<button
										type="button"
										on:click={() => openZoom(galleryIndex)}
										aria-label="Enlarge preview"
										class="block w-full cursor-zoom-in"
									>
										<img
											src={detailPreviews[galleryIndex] || detailPreviews[0]}
											alt={detail.name}
											class="max-h-72 w-full rounded-md border-2 border-gray-200 object-contain transition-colors duration-200 hover:border-cta-blue dark:border-gray-700/60 dark:hover:border-highlight-blue"
											on:error={(e) => (e.currentTarget.style.display = 'none')}
										/>
									</button>
									{#if detailPreviews.length > 1}
										<div class="mt-2 flex flex-wrap gap-2">
											{#each detailPreviews as src, i}
												<button
													type="button"
													on:click={() => {
														galleryIndex = i;
														openZoom(i);
													}}
													aria-label={`Enlarge preview ${i + 1}`}
													class="h-14 w-20 cursor-zoom-in overflow-hidden rounded border-2 transition-colors duration-200 {i ===
													galleryIndex
														? 'border-cta-blue dark:border-highlight-blue'
														: 'border-gray-200 hover:border-gray-300 dark:border-gray-700/60 dark:hover:border-gray-600'}"
												>
													<img
														{src}
														alt=""
														class="h-full w-full object-cover"
														on:error={(e) => (e.currentTarget.style.display = 'none')}
													/>
												</button>
											{/each}
										</div>
									{/if}
								</div>
							{:else}
								<div
									class="flex h-40 items-center justify-center rounded-md border-2 border-gray-200 bg-grayblue-light/50 dark:border-gray-700/60 dark:bg-gray-800/40 sm:w-72 sm:shrink-0"
								>
									<span
										class="flex h-16 w-16 items-center justify-center rounded-md bg-white text-cta-blue shadow-sm dark:bg-gray-900 dark:text-highlight-blue [&_svg]:h-9 [&_svg]:w-9"
										>{@html typeIconSvg(detail.type)}</span
									>
								</div>
							{/if}
							<div class="min-w-0 flex-1 space-y-4">
								<dl class="grid grid-cols-2 gap-x-6 gap-y-4">
									<div class="min-w-0">
										<dt
											class="text-[11px] font-semibold uppercase tracking-wide text-gray-500 dark:text-gray-400"
										>
											Type
										</dt>
										<dd class="mt-0.5 text-sm text-gray-800 dark:text-gray-100">
											{typeLabel(detail.type)}
										</dd>
									</div>
									<div class="min-w-0">
										<dt
											class="text-[11px] font-semibold uppercase tracking-wide text-gray-500 dark:text-gray-400"
										>
											Version
										</dt>
										<dd class="mt-0.5 text-sm tabular-nums text-gray-800 dark:text-gray-100">
											{#if detailInst && detailInst.updateAvailable}
												<span class="text-gray-400 line-through dark:text-gray-500"
													>v{detailInst.version}</span
												>
												<span class="mx-1 text-gray-400 dark:text-gray-500">&rarr;</span>
												<span class="font-medium text-cta-blue dark:text-highlight-blue"
													>v{detailInst.currentVersion}</span
												>
											{:else if detailInst}
												v{detailInst.version}
											{:else}
												{detail.version ? `v${detail.version}` : '-'}
											{/if}
										</dd>
									</div>
									<div class="min-w-0">
										<dt
											class="text-[11px] font-semibold uppercase tracking-wide text-gray-500 dark:text-gray-400"
										>
											Address
										</dt>
										<dd
											class="mt-0.5 truncate font-mono text-xs text-gray-800 dark:text-gray-100"
											title={packageId(detail)}
										>
											{packageId(detail)}
										</dd>
									</div>
									<div class="min-w-0">
										<dt
											class="text-[11px] font-semibold uppercase tracking-wide text-gray-500 dark:text-gray-400"
										>
											Status
										</dt>
										<dd class="mt-0.5">
											{#if statusOf(detail).kind === 'installed'}
												<span
													class="inline-flex items-center gap-1.5 text-sm font-medium text-green-600 dark:text-green-400"
													><span class="h-1.5 w-1.5 rounded-full bg-green-500"
													></span>Installed</span
												>
											{:else if statusOf(detail).kind === 'update'}
												<span
													class="inline-flex items-center gap-1.5 text-sm font-medium text-cta-blue dark:text-highlight-blue"
													><span class="h-1.5 w-1.5 rounded-full bg-cta-blue dark:bg-highlight-blue"
													></span>{statusOf(detail).label}</span
												>
											{:else}
												<span class="text-sm text-gray-800 dark:text-gray-100">Not installed</span>
											{/if}
										</dd>
									</div>
								</dl>
								{#if detail.description}
									<div>
										<div
											class="mb-1 text-[11px] font-semibold uppercase tracking-wide text-gray-500 dark:text-gray-400"
										>
											Description
										</div>
										<p
											class="whitespace-pre-wrap text-sm leading-relaxed text-gray-600 dark:text-gray-300"
										>
											{detail.description}
										</p>
									</div>
								{/if}
							</div>
						</div>

						{#if detailLoading}
							<SettingsLoading />
						{:else if detailError}
							<div
								class="flex flex-col items-start gap-3 rounded-md border-2 border-red-200 bg-red-50/60 p-4 dark:border-red-500/30 dark:bg-red-900/10"
							>
								<p class="text-sm text-red-600 dark:text-red-400">{detailError}</p>
								<Button size="small" on:click={loadDetailFull}>Retry</Button>
							</div>
						{:else if detailFull}
							{#if detailFull.variables && detailFull.variables.length}
								<section>
									<h3
										class="mb-3 border-b-2 border-gray-100 pb-2 text-base font-medium text-pc-darkblue dark:border-gray-800 dark:text-white"
									>
										Variables
									</h3>
									<div
										class="divide-y divide-gray-100 overflow-hidden rounded-md border-2 border-gray-200 dark:divide-gray-800 dark:border-gray-700/60"
									>
										{#each detailFull.variables as v}
											<div class="flex flex-wrap items-baseline gap-x-2 gap-y-1 px-4 py-3 text-sm">
												<span class="font-mono font-medium text-gray-800 dark:text-gray-100"
													>{v.name}</span
												>
												<span
													class="rounded bg-gray-100 px-1.5 py-0.5 text-[10px] uppercase text-gray-600 dark:bg-gray-800 dark:text-gray-300"
													>{v.type || 'text'}</span
												>
												<!-- the admin marks optional, not required: a field is required unless tagged -->
												{#if !v.required}<span class="text-xs text-gray-400 dark:text-gray-500"
														>optional</span
													>{/if}
												{#if v.description}<span
														class="w-full text-xs text-gray-500 dark:text-gray-400"
														>{v.description}</span
													>{/if}
											</div>
										{/each}
									</div>
								</section>
							{/if}

							{#if detailFull.dependencies && detailFull.dependencies.length}
								<section>
									<h3
										class="mb-3 border-b-2 border-gray-100 pb-2 text-base font-medium text-pc-darkblue dark:border-gray-800 dark:text-white"
									>
										Dependencies
									</h3>
									<div class="flex flex-wrap gap-2">
										{#each detailFull.dependencies as d}
											<span
												class="rounded-md bg-grayblue-light px-2.5 py-1 font-mono text-xs text-gray-700 dark:bg-gray-800 dark:text-gray-200"
												>{d.id}</span
											>
										{/each}
									</div>
								</section>
							{/if}
						{/if}
					</div>
				{:else if activeDetailTab === 'attachments'}
					<div class="space-y-2">
						{#each attachmentList as a}
							<div
								class="flex gap-3 rounded-md border-2 border-gray-200 bg-grayblue-light/50 p-3 dark:border-gray-700/60 dark:bg-gray-800/40"
							>
								{#if a.dataUri}
									<img
										src={a.dataUri}
										alt={a.display}
										class="h-16 w-16 shrink-0 rounded border-2 border-gray-200 bg-white object-contain p-0.5 dark:border-gray-700/60 dark:bg-gray-900"
									/>
								{:else}
									<span
										class="flex h-16 w-16 shrink-0 items-center justify-center rounded border-2 border-gray-200 bg-white text-gray-400 dark:border-gray-700/60 dark:bg-gray-900 dark:text-gray-500"
									>
										<svg class="h-7 w-7" viewBox="0 0 20 20" fill="currentColor"
											><path
												d="M4 3a2 2 0 0 0-2 2v10a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V5a2 2 0 0 0-2-2H4zm0 1.5h12a.5.5 0 0 1 .5.5v6.19l-2.72-2.72a.75.75 0 0 0-1.06 0l-3.47 3.47-1.72-1.72a.75.75 0 0 0-1.06 0L3.5 13.69V5a.5.5 0 0 1 .5-.5z"
											/></svg
										>
									</span>
								{/if}
								<div class="min-w-0 flex-1">
									<div class="flex flex-wrap items-center gap-x-2 gap-y-1">
										<span class="font-medium text-gray-800 dark:text-gray-100">{a.display}</span>
										{#if a.embedded}<span
												class="rounded bg-grayblue-light px-1.5 py-0.5 text-[10px] uppercase text-gray-600 dark:bg-gray-700 dark:text-gray-300"
												>Embedded</span
											>{/if}
										{#if a.inline}<span
												class="rounded bg-grayblue-light px-1.5 py-0.5 text-[10px] uppercase text-gray-600 dark:bg-gray-700 dark:text-gray-300"
												>Inline</span
											>{/if}
										{#if a.calendar}<span
												class="rounded bg-grayblue-light px-1.5 py-0.5 text-[10px] uppercase text-gray-600 dark:bg-gray-700 dark:text-gray-300"
												>Calendar</span
											>{/if}
									</div>
									<div
										class="mt-0.5 flex flex-wrap items-center gap-x-2 text-xs text-gray-500 dark:text-gray-400"
									>
										<span class="truncate font-mono">{a.basename}</span>
										{#if a.size !== undefined}<span class="shrink-0 tabular-nums"
												>{(a.size / 1024).toFixed(1)} KB</span
											>{/if}
									</div>
									{#if showAttachmentEmailName && a.emailName}
										<div class="mt-0.5 text-xs text-gray-500 dark:text-gray-400">{a.emailName}</div>
									{/if}
									{#if a.description}
										<p class="mt-1 text-xs text-gray-500 dark:text-gray-400">{a.description}</p>
									{/if}
									{#if a.inline}
										<p class="mt-1 text-xs text-gray-500 dark:text-gray-400">
											Reference it in the email HTML as <code
												class="rounded bg-grayblue-light px-1 py-0.5 font-mono text-gray-700 dark:bg-gray-700 dark:text-gray-200"
												>cid:{a.basename}</code
											>
										</p>
									{/if}
								</div>
							</div>
						{/each}
					</div>
				{:else}
					<div class="space-y-2">
						{#each activeFiles as f, i}
							{@const open =
								openFiles[f.path] !== undefined ? openFiles[f.path] : f.path === primaryFilePath}
							<div
								class="overflow-hidden rounded-md border-2 border-gray-200 dark:border-gray-700/60"
							>
								<button
									type="button"
									on:click={() => toggleFile(f.path)}
									aria-expanded={open}
									class="flex w-full items-center justify-between gap-3 bg-grayblue-light/50 px-4 py-2.5 text-left text-sm transition-colors duration-200 hover:bg-grayblue-light dark:bg-gray-800/50 dark:hover:bg-gray-800"
								>
									<span class="flex min-w-0 items-center gap-2">
										<svg
											class="h-3 w-3 shrink-0 text-gray-400 transition-transform duration-150 {open
												? 'rotate-90'
												: ''}"
											viewBox="0 0 20 20"
											fill="currentColor"><path d="M7 5l6 5-6 5V5z" /></svg
										>
										<span class="truncate font-mono text-gray-800 dark:text-gray-100">{f.path}</span
										>
										{#if isMetaFile(f.path)}<span
												class="shrink-0 rounded bg-gray-200 px-1.5 py-0.5 text-[10px] uppercase text-gray-500 dark:bg-gray-700 dark:text-gray-400"
												>meta</span
											>{/if}
									</span>
									<span class="shrink-0 text-xs tabular-nums text-gray-500 dark:text-gray-400"
										>{(f.size / 1024).toFixed(1)} KB</span
									>
								</button>
								{#if activeDetailTab === 'assets' && assetMetaByFile[f.path]}
									{@const meta = assetMetaByFile[f.path]}
									<dl
										class="grid grid-cols-[6.5rem_1fr] gap-x-4 gap-y-1 border-t-2 border-gray-200 px-4 py-2.5 text-xs dark:border-gray-700/60"
									>
										<dt class="text-gray-500 dark:text-gray-400">Name</dt>
										<dd class="min-w-0 break-words text-gray-800 dark:text-gray-100">
											{meta.name}
										</dd>
										{#if meta.description}
											<dt class="text-gray-500 dark:text-gray-400">Description</dt>
											<dd class="min-w-0 break-words text-gray-600 dark:text-gray-300">
												{meta.description}
											</dd>
										{/if}
										{#if meta.contentType}
											<dt class="text-gray-500 dark:text-gray-400">Content type</dt>
											<dd class="min-w-0 break-all font-mono text-gray-800 dark:text-gray-100">
												{meta.contentType}
											</dd>
										{/if}
										{#if (meta.headers || []).length}
											<dt class="text-gray-500 dark:text-gray-400">Headers</dt>
											<dd class="min-w-0 space-y-0.5 font-mono text-gray-800 dark:text-gray-100">
												{#each meta.headers as h}
													<div class="break-all">{h.key}: {h.value}</div>
												{/each}
											</dd>
										{/if}
										{#if (meta.allowDeny || []).length}
											<dt class="text-gray-500 dark:text-gray-400">Filters</dt>
											<dd class="flex min-w-0 flex-wrap items-center gap-1.5">
												{#each meta.allowDeny as n}
													{@const v = tokenVar(n)}
													{#if v}
														<span
															class="rounded bg-grayblue-light px-1.5 py-0.5 text-gray-700 dark:bg-gray-700 dark:text-gray-200"
															title={`Chosen at install through ${v}`}
															>{v}
															<span class="text-gray-500 dark:text-gray-400"
																>(chosen at install)</span
															></span
														>
													{:else}
														<span
															class="break-all rounded bg-grayblue-light px-1.5 py-0.5 text-gray-700 dark:bg-gray-700 dark:text-gray-200"
															>{n}</span
														>
													{/if}
												{/each}
											</dd>
										{/if}
									</dl>
								{/if}
								{#if open}
									<div class="border-t-2 border-gray-200 dark:border-gray-700/60">
										{#if f.kind === 'image' && f.dataUri}
											<div class="flex justify-center bg-grayblue-light/40 p-4 dark:bg-gray-900/40">
												<img src={f.dataUri} alt={f.path} class="max-h-64 rounded object-contain" />
											</div>
										{:else if f.kind === 'image'}
											<p class="p-4 text-xs text-gray-500 dark:text-gray-400">
												Image too large to preview inline.
											</p>
										{:else if f.kind === 'text' && fileLang(f.path) === 'html'}
											{@const view = fileView[f.path] || 'source'}
											<div
												class="flex items-center gap-1 border-b-2 border-gray-100 bg-grayblue-light/30 px-4 py-2 dark:border-gray-800 dark:bg-gray-900/30"
											>
												<button
													type="button"
													on:click={() => setFileView(f.path, 'source')}
													aria-pressed={view === 'source'}
													class="rounded px-3 py-1 text-xs font-medium transition-colors duration-200 {view ===
													'source'
														? 'bg-cta-blue text-white dark:bg-highlight-blue'
														: 'text-gray-600 hover:bg-grayblue-light dark:text-gray-300 dark:hover:bg-gray-800'}"
													>Source</button
												>
												<button
													type="button"
													on:click={() => setFileView(f.path, 'preview')}
													aria-pressed={view === 'preview'}
													class="rounded px-3 py-1 text-xs font-medium transition-colors duration-200 {view ===
													'preview'
														? 'bg-cta-blue text-white dark:bg-highlight-blue'
														: 'text-gray-600 hover:bg-grayblue-light dark:text-gray-300 dark:hover:bg-gray-800'}"
													>Preview</button
												>
											</div>
											{#if view === 'preview'}
												<iframe
													name={`bb-preview-${activeDetailTab}-${i}`}
													title={`Rendered preview of ${f.path}`}
													sandbox="allow-forms allow-popups allow-scripts"
													referrerpolicy="no-referrer"
													use:renderPreview={inlineAssets(f.text)}
													class="h-[60vh] w-full border-0"
													style="background: white;"
												></iframe>
											{:else}
												<SimpleCodeEditor
													value={f.text}
													language={fileLang(f.path)}
													height="large"
													readOnly={true}
													showVimToggle={false}
													showExpandButton={false}
													externalVimMode={false}
												/>
											{/if}
										{:else if f.kind === 'text'}
											<SimpleCodeEditor
												value={f.text}
												language={fileLang(f.path)}
												height="large"
												readOnly={true}
												showVimToggle={false}
												showExpandButton={false}
												externalVimMode={false}
											/>
										{:else}
											<p class="p-4 text-xs text-gray-500 dark:text-gray-400">
												Binary file, not shown.
											</p>
										{/if}
									</div>
								{/if}
							</div>
						{/each}
					</div>
				{/if}
			</div>

			<div
				class="mt-6 flex flex-wrap items-center gap-3 border-t-2 border-gray-100 pt-5 dark:border-gray-800"
			>
				{#if installFor(detail) && installFor(detail).updateAvailable}
					<Button on:click={() => openUpdate(installFor(detail))}>Update</Button>
					<!-- uninstall is a bordered secondary button, clearly a control but quieter than
					the primary action; the red DeleteAlert does the destructive confirm -->
					<button
						type="button"
						on:click={() => openRemoveInstall(installFor(detail))}
						class="rounded-md border-2 border-gray-200 px-6 py-2 text-sm font-semibold uppercase text-gray-600 transition-colors duration-200 hover:border-red-300 hover:bg-red-50 hover:text-red-600 dark:border-gray-700/60 dark:text-gray-300 dark:hover:border-red-500/40 dark:hover:bg-red-900/20 dark:hover:text-red-400"
						>Uninstall</button
					>
				{:else if installFor(detail)}
					<!-- the status is already shown in the details tab, so the footer is just the
					uninstall action: a bordered secondary button, not a bare text link -->
					<button
						type="button"
						on:click={() => openRemoveInstall(installFor(detail))}
						class="rounded-md border-2 border-gray-200 px-6 py-2 text-sm font-semibold uppercase text-gray-600 transition-colors duration-200 hover:border-red-300 hover:bg-red-50 hover:text-red-600 dark:border-gray-700/60 dark:text-gray-300 dark:hover:border-red-500/40 dark:hover:bg-red-900/20 dark:hover:text-red-400"
						>Uninstall</button
					>
				{:else if brokenDeps.length}
					<div class="w-full">
						<p class="mb-3 text-sm text-amber-600 dark:text-amber-400">
							{brokenBundleMessage(brokenDeps)}
						</p>
						<Button disabled>Install</Button>
					</div>
				{:else if refusedDeps.length}
					<div class="w-full">
						<p class="mb-3 text-sm text-amber-600 dark:text-amber-400">
							This package cannot be installed. {refusedDeps.length === 1
								? 'A referenced package'
								: 'Referenced packages'} ({refusedDeps.join(', ')}) could not be read: {detailFull.refusedMessage}.
						</p>
						<Button disabled>Install</Button>
					</div>
				{:else}
					<Button on:click={() => openInstall(detail)}>Install</Button>
				{/if}
			</div>
		</div>
	</Modal>
	{#if zoomOpen && showDetail}
		<!-- clicking the backdrop closes; the image and controls stop propagation so they
		do not. prev and next page through the screenshots, wrapping around. keyboard close
		and paging are handled by the window key handler below, so the lint is suppressed -->
		<!-- svelte-ignore a11y-click-events-have-key-events a11y-no-static-element-interactions -->
		<div
			role="presentation"
			on:click={closeZoom}
			class="fixed inset-0 z-[60] flex items-center justify-center bg-overlay/80 p-10"
		>
			<!-- svelte-ignore a11y-no-noninteractive-element-interactions -->
			<img
				src={detailPreviews[zoomIndex]}
				alt=""
				on:click|stopPropagation
				class="max-h-[90vh] max-w-[90vw] rounded-md border-2 border-highlight-blue object-contain shadow-theme-lg"
			/>
			{#if detailPreviews.length > 1}
				<button
					type="button"
					on:click|stopPropagation={() => zoomStep(-1)}
					aria-label="Previous preview"
					class="absolute left-4 top-1/2 flex h-11 w-11 -translate-y-1/2 items-center justify-center rounded-full bg-black/50 text-white transition-colors duration-200 hover:bg-black/70 focus:outline-none focus-visible:ring-2 focus-visible:ring-highlight-blue"
				>
					<svg
						class="h-6 w-6"
						viewBox="0 0 24 24"
						fill="none"
						stroke="currentColor"
						stroke-width="2"
						stroke-linecap="round"
						stroke-linejoin="round"><path d="M15 18l-6-6 6-6" /></svg
					>
				</button>
				<button
					type="button"
					on:click|stopPropagation={() => zoomStep(1)}
					aria-label="Next preview"
					class="absolute right-4 top-1/2 flex h-11 w-11 -translate-y-1/2 items-center justify-center rounded-full bg-black/50 text-white transition-colors duration-200 hover:bg-black/70 focus:outline-none focus-visible:ring-2 focus-visible:ring-highlight-blue"
				>
					<svg
						class="h-6 w-6"
						viewBox="0 0 24 24"
						fill="none"
						stroke="currentColor"
						stroke-width="2"
						stroke-linecap="round"
						stroke-linejoin="round"><path d="M9 18l6-6-6-6" /></svg
					>
				</button>
				<span
					class="absolute bottom-6 left-1/2 -translate-x-1/2 rounded-full bg-black/55 px-3 py-1 text-xs font-medium tabular-nums text-white"
					>{zoomIndex + 1} / {detailPreviews.length}</span
				>
			{/if}
			<button
				type="button"
				on:click|stopPropagation={closeZoom}
				aria-label="Close enlarged preview"
				class="absolute right-4 top-4 flex h-10 w-10 items-center justify-center rounded-full bg-black/50 text-white transition-colors duration-200 hover:bg-black/70 focus:outline-none focus-visible:ring-2 focus-visible:ring-highlight-blue"
			>
				<svg
					class="h-5 w-5"
					viewBox="0 0 24 24"
					fill="none"
					stroke="currentColor"
					stroke-width="2"
					stroke-linecap="round"
					stroke-linejoin="round"><path d="M18 6L6 18M6 6l12 12" /></svg
				>
			</button>
		</div>
	{/if}
{/if}

<!-- window keys for the enlarged preview; capture so they run before the modal.
Escape closes, the arrows page through the screenshots -->
<svelte:window
	on:keydown|capture={(e) => {
		if (!zoomOpen) return;
		if (e.key === 'Escape') {
			closeZoom();
			e.stopImmediatePropagation();
		} else if (e.key === 'ArrowLeft') {
			zoomStep(-1);
			e.stopImmediatePropagation();
		} else if (e.key === 'ArrowRight') {
			zoomStep(1);
			e.stopImmediatePropagation();
		}
	}}
/>

<!-- add bait box -->
<Modal
	headerText="Add Bait Box"
	visible={showAddRegistry}
	onClose={() => (showAddRegistry = false)}
	isSubmitting={addSubmitting}
>
	<FormGrid on:submit={submitAddRegistry} bind:bindTo={addForm} isSubmitting={addSubmitting}>
		<div class="col-span-3 mx-auto w-full max-w-md">
			<div class="space-y-3">
				<TextField
					required
					minLength={1}
					width="full"
					bind:value={regName}
					placeholder="Acme Bait Box">Name</TextField
				>
				<TextField
					required
					width="full"
					bind:value={regURL}
					placeholder="https://baitbox.example.com">URL</TextField
				>
				<TextField type="password" width="full" bind:value={regToken} placeholder="Read token"
					>Token</TextField
				>
				<p class="-mt-2 text-xs text-gray-500 dark:text-gray-400">
					Only leave this empty when the Bait Box runs in public mode.
				</p>
				<TextFieldSelect
					id="insecure"
					size="full"
					options={[
						{ value: 'false', label: 'No' },
						{ value: 'true', label: 'Yes' }
					]}
					bind:value={regInsecure}>Allow self signed TLS</TextFieldSelect
				>
			</div>
			<FormError message={addError} />
			<FormFooter
				closeModal={() => (showAddRegistry = false)}
				isSubmitting={addSubmitting}
				okText="Add"
			/>
		</div>
	</FormGrid>
</Modal>

<!-- edit bait box -->
<Modal
	headerText="Edit Bait Box"
	visible={showEditRegistry}
	onClose={() => (showEditRegistry = false)}
	isSubmitting={editSubmitting}
>
	<FormGrid on:submit={submitEditRegistry} bind:bindTo={editForm} isSubmitting={editSubmitting}>
		<div class="col-span-3 mx-auto w-full max-w-md">
			<div class="space-y-3">
				<TextField
					required
					minLength={1}
					width="full"
					bind:value={editName}
					placeholder="Acme Bait Box">Name</TextField
				>
				<TextField
					required
					width="full"
					bind:value={editURL}
					placeholder="https://baitbox.example.com">URL</TextField
				>
				{#if !editClearToken}
					<TextField
						type="password"
						width="full"
						bind:value={editToken}
						placeholder={editHadToken ? 'Leave blank to keep the stored token' : 'Read token'}
						>Token</TextField
					>
				{/if}
				{#if editHadToken}
					<CheckboxField id="editClearToken" inline bind:value={editClearToken}
						>Remove token</CheckboxField
					>
				{/if}
				<TextFieldSelect
					id="editInsecure"
					size="full"
					options={[
						{ value: 'false', label: 'No' },
						{ value: 'true', label: 'Yes' }
					]}
					bind:value={editInsecure}>Allow self signed TLS</TextFieldSelect
				>
			</div>
			<FormError message={editError} />
			<FormFooter
				closeModal={() => (showEditRegistry = false)}
				isSubmitting={editSubmitting}
				okText="Save"
			/>
		</div>
	</FormGrid>
</Modal>

<!-- required values -->
<Modal
	headerText="Required values"
	visible={showVars}
	onClose={() => (showVars = false)}
	isSubmitting={varsSubmitting}
>
	<FormGrid on:submit={submitVars} bind:bindTo={varsForm} isSubmitting={varsSubmitting}>
		<FormColumns>
			<FormColumn>
				{#if installBlockedByMissing}
					<!-- state up front that install cannot proceed, before the operator tries to
					submit, and name what to create. the per field links go to the create route -->
					<div
						class="mb-4 w-96 rounded-md border-2 border-amber-200 bg-amber-50/60 p-4 dark:border-amber-500/30 dark:bg-amber-900/10"
					>
						<p class="text-sm font-semibold text-amber-700 dark:text-amber-400">
							{varsAction === 'update' ? 'Update' : 'Install'} is blocked
						</p>
						<p class="mt-1 text-xs text-amber-700 dark:text-amber-400">
							This package needs {missingPickDefs.length === 1 ? 'an entity' : 'entities'} you have not
							created yet. Create {missingPickDefs.length === 1 ? 'it' : 'them'} in Phishing Club, then
							reopen this {varsAction}.
						</p>
					</div>
				{/if}
				{#each varDefs as d, i}
					{@const pickNames = pickEntities[d.pick] || []}
					<div class="mb-2">
						{#if d.pick}
							<div class="w-96">
								{#if pickNames.length}
									<TextFieldSelect
										id={`var-${i}`}
										containInModal
										size="full"
										options={pickNames}
										bind:value={varValues[varKeyOf(d)]}
									>
										{pickHeading(d)}{#if d.scope}<span
												class="ml-1.5 text-xs font-normal text-gray-400">{d.scope}</span
											>{/if}
									</TextFieldSelect>
								{:else}
									<p class="py-1 font-semibold text-slate-600 dark:text-gray-400">
										{pickHeading(d)}{#if d.scope}<span
												class="ml-1.5 text-xs font-normal text-gray-400">{d.scope}</span
											>{/if}
									</p>
									<p class="mt-1 text-xs text-amber-600 dark:text-amber-400">
										{#if pickLoading}
											Loading your {pickLabel(d.pick)} list...
										{:else}
											You have no {pickLabel(d.pick)} yet.
											{#if pickRoute(d.pick)}
												<a
													href={pickRoute(d.pick)}
													target="_blank"
													rel="noopener noreferrer"
													class="font-semibold underline hover:text-amber-700 dark:hover:text-amber-300"
													>Create one in a new tab</a
												>, then reopen this {varsAction}.
											{:else}
												Create one in Phishing Club first.
											{/if}
										{/if}
									</p>
								{/if}
							</div>
						{:else if d.type === 'enum'}
							<div class="w-96">
								<TextFieldSelect
									id={`var-${i}`}
									containInModal
									size="full"
									options={d.options || []}
									bind:value={varValues[varKeyOf(d)]}
								>
									{d.name}<span class="ml-1.5 text-xs font-normal text-gray-400">(enum)</span
									>{#if d.scope}<span class="ml-1.5 text-xs font-normal text-gray-400"
											>{d.scope}</span
										>{/if}
								</TextFieldSelect>
							</div>
						{:else}
							<TextField
								required
								width="large"
								bind:value={varValues[varKeyOf(d)]}
								placeholder={varPlaceholder(d.type)}
							>
								{d.name}{#if d.type}<span class="ml-1.5 text-xs font-normal text-gray-400"
										>({d.type})</span
									>{/if}{#if d.scope}<span class="ml-1.5 text-xs font-normal text-gray-400"
										>{d.scope}</span
									>{/if}
							</TextField>
						{/if}
						{#if d.description && !d.pick}
							<p class="mt-1 w-96 text-xs text-gray-500 dark:text-gray-400">{d.description}</p>
						{/if}
					</div>
				{/each}
			</FormColumn>
		</FormColumns>
		<FormError message={varsError} />
		<FormFooter
			closeModal={() => (showVars = false)}
			isSubmitting={varsSubmitting}
			disabled={installBlockedByMissing}
			okText="Continue"
		/>
	</FormGrid>
</Modal>

<!-- update confirmation: Alert in the affirmative tone, since update is a positive action -->
{#if confirmUpdateInst}
	<Alert
		tone="affirmative"
		bind:visible={confirmUpdateVisible}
		headline="Update package"
		ok="Update"
		onConfirm={confirmUpdate}
	>
		<div class="space-y-6">
			<p class="text-gray-600 dark:text-gray-400">
				Update
				<span class="font-medium text-gray-900 dark:text-gray-300"
					>{confirmUpdateInst.packageId}</span
				>
				to version
				<span class="font-medium text-gray-900 dark:text-gray-300"
					>v{confirmUpdateInst.currentVersion}</span
				>?
			</p>
			<div class="rounded-lg bg-gray-50 p-4 dark:bg-gray-800/40">
				<ul class="ml-4 list-disc space-y-2 text-gray-600 dark:text-gray-400">
					{#each updateInstallList as bullet}
						<li>{bullet}</li>
					{/each}
				</ul>
			</div>
		</div>
	</Alert>
{/if}

<!-- conflict resolution: shown when an update has items that need a choice -->
{#if conflictsVisible && conflictInst}
	<Alert
		bind:visible={conflictsVisible}
		headline={fileConflicts.length
			? conflictsHaveReason
				? 'Choose how to update'
				: conflictsHaveFilterLoss
					? 'Resolve edits and filters'
					: 'Resolve local edits'
			: 'Resolve dropped packages'}
		ok="Update"
		onConfirm={confirmConflicts}
	>
		<div class="space-y-6">
			{#if fileConflicts.length}
				<p class="text-gray-600 dark:text-gray-400">
					Updating
					<span class="font-medium text-gray-900 dark:text-gray-300">{conflictInst.packageId}</span>
					{#if conflictsHaveReason}
						changes items that need your choice. Choose what to do with each item.
					{:else if conflictsHaveFilterLoss}
						would overwrite content you edited locally or remove allow/deny lists from assets.
						Choose what to do with each item.
					{:else}
						would overwrite content you edited locally. Choose what to do with each edited item.
					{/if}
				</p>
				<div class="rounded-lg bg-gray-50 p-4 dark:bg-gray-800/40">
					<ul class="ml-4 list-disc space-y-2 text-sm text-gray-600 dark:text-gray-400">
						<li>
							<span class="font-medium text-gray-900 dark:text-gray-300">Keep mine</span>
							{#if conflictsHaveReason || conflictsHaveFilterLoss}
								keeps the item as it is now, and the package keeps using it instead of the new
								version.
							{:else}
								keeps your local edit, and the package keeps using it instead of the new version.
							{/if}
						</li>
						<li>
							<span class="font-medium text-gray-900 dark:text-gray-300">Overwrite</span>
							{#if conflictsHaveReason || conflictsHaveFilterLoss}
								applies the new version in place.
							{:else}
								replaces your edit with the new version in place.
							{/if}
						</li>
						{#if conflictsOfferBoth}
							<li>
								<span class="font-medium text-gray-900 dark:text-gray-300">Keep both</span> keeps the
								current copy as "name (local)", then installs the new version fresh and points the package
								and its templates at it. Not offered for domains, OAuth providers, report templates and
								assets.
							</li>
						{/if}
					</ul>
				</div>
				<div class="space-y-3">
					{#each fileConflicts as c}
						{@const key = conflictKey(c)}
						{@const loss = c.filterLoss}
						<div>
							<div class="flex items-center justify-between gap-3">
								<div class="min-w-0">
									<div class="truncate font-medium text-gray-900 dark:text-gray-300">
										{c.currentName || c.sourceName}
									</div>
									<div class="text-xs text-gray-500 dark:text-gray-400">
										{typeLabel(c.resourceType)}
										{#if c.resourceType === 'asset'}
											settings at <span class="break-all font-mono">{c.sourceName}</span>
										{/if}
									</div>
									{#if c.reason}
										<div class="mt-1 text-xs text-amber-700 dark:text-amber-400">
											{c.reason}
										</div>
									{/if}
									{#if c.note}
										<div class="mt-1 text-xs text-gray-500 dark:text-gray-400">
											{c.note}
										</div>
									{/if}
								</div>
								<div class="w-44 shrink-0">
									<TextFieldSelect
										id={`conflict-${key}`}
										hideLabel={true}
										size="full"
										options={conflictOptions(c)}
										value={choiceLabel(conflictChoices[key])}
										onSelect={(label) => setConflictChoice(key, label)}
									/>
								</div>
							</div>
							{#if loss}
								<div
									id={`conflict-loss-${key}`}
									class="mt-2 rounded-md border-2 p-3 {loss.openToEveryone
										? 'border-red-200 bg-red-50/60 dark:border-red-500/30 dark:bg-red-900/10'
										: 'border-amber-200 bg-amber-50/60 dark:border-amber-500/30 dark:bg-amber-900/10'}"
								>
									{#if loss.openToEveryone}
										<p class="text-sm font-medium text-red-700 dark:text-red-300">
											Overwrite makes this file reachable by everyone
										</p>
									{:else if loss.unfiltered}
										<p class="text-sm font-medium text-amber-800 dark:text-amber-300">
											Overwrite leaves this file with no allow/deny list
										</p>
									{:else}
										<p class="text-sm font-medium text-amber-800 dark:text-amber-300">
											Overwrite removes allow/deny lists from this file
										</p>
									{/if}
									{#if (loss.removedLists || []).length}
										<p class="mt-1 break-words text-sm text-gray-700 dark:text-gray-300">
											{loss.removedLists.length === 1 ? 'List removed:' : 'Lists removed:'}
											<span class="font-medium">{loss.removedLists.join(', ')}</span>
										</p>
									{/if}
								</div>
							{/if}
						</div>
					{/each}
				</div>
			{/if}
			{#if memberConflicts.length}
				<p class="text-gray-600 dark:text-gray-400">
					The new version of
					<span class="font-medium text-gray-900 dark:text-gray-300">{conflictInst.packageId}</span>
					no longer includes {memberConflicts.length === 1 ? 'a package' : 'packages'} whose content
					you edited or use elsewhere.
				</p>
				<div class="rounded-lg bg-gray-50 p-4 dark:bg-gray-800/40">
					<ul class="ml-4 list-disc space-y-2 text-sm text-gray-600 dark:text-gray-400">
						<li>
							<span class="font-medium text-gray-900 dark:text-gray-300">Keep installed</span>
							unlinks it from {conflictInst.packageId} but keeps it and its content installed. You can
							uninstall it later from Installed.
						</li>
						<li>
							<span class="font-medium text-gray-900 dark:text-gray-300">Uninstall</span> deletes it
							and its content, including your edits.
						</li>
					</ul>
				</div>
				<div class="space-y-4">
					{#each memberConflicts as c}
						{@const key = conflictKey(c)}
						{@const m = c.member || {}}
						<div class="rounded-lg border border-gray-200 p-4 dark:border-gray-700">
							<div class="flex items-center justify-between gap-3">
								<div class="min-w-0">
									<div class="truncate font-medium text-gray-900 dark:text-gray-300">
										{c.sourceName}
									</div>
									<div class="text-xs text-gray-500 dark:text-gray-400">
										No longer used by {conflictInst.packageId}
									</div>
								</div>
								<div class="w-44 shrink-0">
									<TextFieldSelect
										id={`conflict-${key}`}
										hideLabel={true}
										size="full"
										options={[MEMBER_LABELS.keep, MEMBER_LABELS.remove]}
										value={MEMBER_LABELS[conflictChoices[key]] || MEMBER_LABELS.keep}
										onSelect={(label) => setMemberChoice(key, label)}
									/>
								</div>
							</div>
							<ul class="ml-4 mt-3 list-disc space-y-1 text-sm text-gray-700 dark:text-gray-300">
								{#each m.edited || [] as e}
									<li class="break-words">
										Edited {resourceNoun(e.resourceType, 1)}
										<span class="font-medium">{e.name}</span>
										{#if e.packageId && e.packageId !== c.sourceName}
											<span class="text-gray-500 dark:text-gray-400">({e.packageId})</span>
										{/if}
									</li>
								{/each}
								{#each m.templates || [] as t}
									<li class="break-words">
										Used by campaign template <span class="font-medium">{t}</span>
									</li>
								{/each}
								{#if (m.templateCount || 0) > (m.templates || []).length}
									<li>and {m.templateCount - m.templates.length} more campaign templates</li>
								{/if}
								{#if (m.members || []).length}
									<li class="break-words">Also uninstalls {m.members.join(', ')}</li>
								{/if}
							</ul>
							{#if conflictChoices[key] === 'remove' && (m.campaignCount || m.keepRunningCount || m.changedCount || m.userAssetCount || m.changedAssetCount)}
								<div
									class="mt-3 rounded-md border-2 border-red-200 bg-red-50/60 p-3 dark:border-red-500/30 dark:bg-red-900/10"
								>
									<p class="text-sm font-medium text-red-700 dark:text-red-300">
										{m.campaignCount || m.keepRunningCount || m.changedCount
											? m.changedAssetCount
												? 'Uninstalling it affects active campaigns and assets'
												: 'Uninstalling it affects active campaigns'
											: 'Uninstalling it affects assets'}
									</p>
									<ul
										class="ml-4 mt-1 list-disc space-y-1 text-sm text-gray-700 dark:text-gray-300"
									>
										{#each m.campaigns || [] as camp (camp.id)}
											<li class="break-words">
												{camp.name} <span class="text-gray-500 dark:text-gray-400">(closed)</span>
											</li>
										{/each}
										{#each m.keepRunning || [] as camp (camp.id)}
											<li class="break-words">
												{camp.name}
												<span class="text-gray-500 dark:text-gray-400"
													>(keeps running without this content{camp.closeAt
														? ` until ${formatCloseAt(camp.closeAt)}`
														: ''})</span
												>
											</li>
										{/each}
										{#each m.changed || [] as camp (camp.id)}
											<li class="break-words">
												{camp.name}:
												<span
													class={camp.openToEveryone
														? 'font-medium text-red-700 dark:text-red-300'
														: ''}>{(camp.losses || []).join(', ')}</span
												>
											</li>
										{/each}
										{#each m.changedAssets || [] as a (a.id)}
											<li class="break-words">
												{a.name}{assetWhere(a) !== a.name ? ` (${assetWhere(a)})` : ''}:
												<span
													class={a.openToEveryone
														? 'font-medium text-red-700 dark:text-red-300'
														: ''}>{assetLossLines(a).join(', ')}</span
												>
											</li>
										{/each}
										{#if (m.campaignCount || 0) + (m.keepRunningCount || 0) + (m.changedCount || 0) + (m.changedAssetCount || 0) > (m.campaigns || []).length + (m.keepRunning || []).length + (m.changed || []).length + (m.changedAssets || []).length}
											<li>and more</li>
										{/if}
										{#if m.userAssetCount}
											<li>
												{m.userAssetCount === 1
													? '1 asset you added is deleted with its domain'
													: `${m.userAssetCount} assets you added are deleted with their domains`}
											</li>
										{/if}
									</ul>
								</div>
							{/if}
						</div>
					{/each}
				</div>
			{/if}
		</div>
	</Alert>
{/if}

<!-- report template overwrite: a package replaces the global report template of a kind -->
{#if rtOverwriteVisible}
	<Alert
		bind:visible={rtOverwriteVisible}
		headline="Overwrite report template"
		ok="Overwrite"
		onConfirm={confirmReportTemplateOverwrite}
	>
		<div class="space-y-6">
			<p class="text-gray-600 dark:text-gray-400">
				{rtOverwritePending?.kind === 'update' ? 'Updating' : 'Installing'}
				<span class="font-medium text-gray-900 dark:text-gray-300"
					>{rtOverwritePending?.kind === 'update'
						? rtOverwritePending?.inst?.packageId
						: packageId(rtOverwritePending?.p || {})}</span
				>
				replaces your current {rtOverwriteList.length === 1
					? 'report template'
					: 'report templates'}. Phishing Club keeps one report template per kind.
			</p>
			<div
				class="rounded-lg border-2 border-amber-200 bg-amber-50/60 p-4 dark:border-amber-500/30 dark:bg-amber-900/10"
			>
				<ul class="ml-4 list-disc space-y-2 text-sm text-amber-800 dark:text-amber-300">
					{#each rtOverwriteList as t}
						<li>
							This package replaces your current
							<span class="font-semibold">{rtKindLabel(t.kind)} report template</span>{t.customized
								? ', which you have customized'
								: ' (the built in default)'}. Your current template will be overwritten.
						</li>
					{/each}
				</ul>
			</div>
			<p class="text-sm text-gray-600 dark:text-gray-400">
				Uninstalling the package later restores the content your template has now.
			</p>
		</div>
	</Alert>
{/if}

<!-- update all confirmation: bulk update, keep local edits, no per package prompts -->
{#if updateAllVisible}
	<Alert
		tone="affirmative"
		bind:visible={updateAllVisible}
		headline="Update all packages"
		ok="Update all"
		onConfirm={confirmUpdateAll}
	>
		<div class="space-y-6">
			<p class="text-gray-600 dark:text-gray-400">
				{#if updateAllRegistryName}
					Update every package from {updateAllRegistryName} that has an available update?
				{:else}
					Update every package from every connected Bait Box that has an available update?
				{/if}
			</p>
			<div class="rounded-lg bg-gray-50 p-4 dark:bg-gray-800/40">
				<ul class="ml-4 list-disc space-y-2 text-gray-600 dark:text-gray-400">
					<li>Every package with an available update is updated to its new version.</li>
					<li>
						Where you edited content locally, your edit is kept, not overwritten, and reported
						after.
					</li>
					<li>Packages that need variables are skipped and must be updated individually.</li>
					<li>
						Packages with a change that needs your choice, such as a new OAuth binding, are skipped
						and must be updated individually.
					</li>
					<li>
						Packages that would overwrite your report template are skipped and must be updated
						individually.
					</li>
				</ul>
			</div>
		</div>
	</Alert>
{/if}

<!-- install: the package preview and its variable inputs in one modal -->
{#if confirmInstallPkg}
	<Modal
		headerText="Install package"
		visible={confirmInstallVisible}
		onClose={() => (confirmInstallVisible = false)}
		isSubmitting={installVarsSubmitting}
	>
		<FormGrid
			on:submit={submitInstall}
			bind:bindTo={installForm}
			isSubmitting={installVarsSubmitting}
		>
			<div class="col-span-3 mx-auto w-full max-w-2xl">
				<div class="space-y-5 py-2">
					<!-- identity: renders instantly from the summary so the modal is never blank -->
					<div class="flex gap-4">
						{#if previewSrcAt(confirmInstallPkg, 0)}
							<img
								src={previewSrcAt(confirmInstallPkg, 0)}
								alt=""
								class="h-16 w-16 shrink-0 rounded-md border-2 border-gray-200 object-cover dark:border-gray-700/60"
								on:error={(e) => (e.currentTarget.style.display = 'none')}
							/>
						{:else}
							<span
								class="flex h-16 w-16 shrink-0 items-center justify-center rounded-md bg-white text-cta-blue shadow-sm dark:bg-gray-900 dark:text-highlight-blue [&_svg]:h-8 [&_svg]:w-8"
								>{@html typeIconSvg(confirmInstallPkg.type)}</span
							>
						{/if}
						<div class="min-w-0 flex-1">
							<h2 class="text-lg font-semibold text-gray-800 dark:text-gray-100">
								{displayName(confirmInstallPkg)}
							</h2>
							<div class="mt-1 flex flex-wrap items-center gap-2">
								<span
									class="text-[11px] font-semibold uppercase tracking-wide text-gray-500 dark:text-gray-400"
									>{typeLabel(confirmInstallPkg.type)}</span
								>
								{#if confirmInstallPkg.version}
									<span
										class="font-mono text-xs font-medium tabular-nums text-cta-blue dark:text-highlight-blue"
										>v{confirmInstallPkg.version}</span
									>
								{/if}
							</div>
							<p
								class="mt-0.5 truncate font-mono text-xs text-gray-500 dark:text-gray-400"
								title={packageId(confirmInstallPkg)}
							>
								{packageId(confirmInstallPkg)}
							</p>
						</div>
					</div>

					{#if confirmInstallPkg.description}
						<p class="line-clamp-3 text-sm leading-relaxed text-gray-600 dark:text-gray-300">
							{confirmInstallPkg.description}
						</p>
					{/if}

					<!-- a broken bundle cannot install, so the warning stays above the scroll region
					where it is always visible and the Install button is disabled -->
					{#if confirmBrokenDeps.length}
						<div
							class="rounded-md border-2 border-amber-200 bg-amber-50/60 p-3 dark:border-amber-500/30 dark:bg-amber-900/10"
						>
							<p class="text-sm text-amber-700 dark:text-amber-400">
								{brokenBundleMessage(confirmBrokenDeps)}
							</p>
						</div>
					{/if}
					{#if confirmRefusedDeps.length}
						<div
							class="rounded-md border-2 border-amber-200 bg-amber-50/60 p-3 dark:border-amber-500/30 dark:bg-amber-900/10"
						>
							<p class="text-sm text-amber-700 dark:text-amber-400">
								This package cannot be installed. {confirmRefusedDeps.length === 1
									? 'A referenced package'
									: 'Referenced packages'} ({confirmRefusedDeps.join(', ')}) could not be read: {confirmInstallFull.refusedMessage}.
							</p>
						</div>
					{/if}

					<!-- a required pick the operator owns no entity of blocks the install, so the
					notice stays above the scroll region and the Install button is disabled -->
					{#if installBlocked}
						<div
							class="rounded-md border-2 border-amber-200 bg-amber-50/60 p-4 dark:border-amber-500/30 dark:bg-amber-900/10"
						>
							<p class="text-sm font-semibold text-amber-700 dark:text-amber-400">
								Install is blocked
							</p>
							<p class="mt-1 text-xs text-amber-700 dark:text-amber-400">
								This package needs {installMissingPickDefs.length === 1 ? 'an entity' : 'entities'} you
								have not created yet. Create {installMissingPickDefs.length === 1 ? 'it' : 'them'} in
								Phishing Club, then reopen this install.
							</p>
						</div>
					{/if}

					{#if confirmInstallLoading}
						<div class="min-h-24 border-t-2 border-gray-100 pt-5 dark:border-gray-800">
							<SettingsLoading />
						</div>
					{:else if confirmInstallError && !confirmInstallFull}
						<!-- the manifest is optional detail: install still works and the backend revalidates -->
						<div class="border-t-2 border-gray-100 pt-5 dark:border-gray-800">
							<p class="text-xs text-gray-500 dark:text-gray-400">
								{confirmInstallError}
								<button
									type="button"
									class="ml-1 font-medium text-cta-blue hover:underline dark:text-highlight-blue"
									on:click={() => loadConfirmInstallFull(confirmInstallPkg)}>Try again</button
								>
							</p>
						</div>
					{:else if confirmInstallFull}
						<!-- one scroll for the whole modal (the shared Modal body), so the field list
						is not boxed in a second nested scroll that slices a control at its edge -->
						<div class="space-y-6 border-t-2 border-gray-100 pt-5 dark:border-gray-800">
							{#if confirmInstallFull.dependencies && confirmInstallFull.dependencies.length}
								<div>
									<div
										class="mb-2 text-[11px] font-semibold uppercase tracking-wide text-gray-500 dark:text-gray-400"
									>
										{confirmInstallPkg?.type === 'bundle' ? 'Contains' : 'Also installs'}
										{confirmInstallFull.dependencies.length}
										{confirmInstallFull.dependencies.length === 1 ? 'package' : 'packages'}
									</div>
									<!-- one package per line so the list reads as scannable rows -->
									<ul class="space-y-1.5">
										{#each confirmInstallFull.dependencies.slice(0, 8) as d}
											<li class="font-mono text-sm text-gray-700 dark:text-gray-200">{d.id}</li>
										{/each}
										{#if confirmInstallFull.dependencies.length > 8}
											<li class="text-xs text-gray-500 dark:text-gray-400">
												and {confirmInstallFull.dependencies.length - 8} more
											</li>
										{/if}
									</ul>
								</div>
							{/if}

							{#if installVarDefs.length}
								{@const allRequired = installVarDefs.every((d) => d.required)}
								<div>
									<div
										class="mb-1 text-[11px] font-semibold uppercase tracking-wide text-gray-500 dark:text-gray-400"
									>
										Values
									</div>
									<!-- state the requiredness once up front, in the app convention where a field is
									required unless it is tagged optional -->
									<p class="mb-3 text-xs text-gray-500 dark:text-gray-400">
										{allRequired
											? 'All values are required.'
											: 'Values are required unless marked optional.'}
									</p>
									{#each installVarDefs as d, i}
										{@const pickNames = pickEntities[d.pick] || []}
										<div class="mb-2">
											{#if d.pick}
												<div class="w-full">
													{#if pickNames.length}
														<TextFieldSelect
															id={`ivar-${i}`}
															containInModal
															size="full"
															optional={!d.required}
															options={pickNames}
															bind:value={installVars[varKeyOf(d)]}
														>
															{pickHeading(d)}
														</TextFieldSelect>
													{:else}
														<p class="py-1 font-semibold text-slate-600 dark:text-gray-400">
															{pickHeading(d)}
														</p>
														<p class="mt-1 text-xs text-amber-600 dark:text-amber-400">
															{#if pickLoading}
																Loading your {pickLabel(d.pick)} list...
															{:else}
																You have no {pickLabel(d.pick)} yet.
																{#if pickRoute(d.pick)}
																	<a
																		href={pickRoute(d.pick)}
																		target="_blank"
																		rel="noopener noreferrer"
																		class="font-semibold underline hover:text-amber-700 dark:hover:text-amber-300"
																		>Create one in a new tab</a
																	>, then reopen this install.
																{:else}
																	Create one in Phishing Club first.
																{/if}
															{/if}
														</p>
													{/if}
												</div>
											{:else if d.type === 'enum'}
												<div class="w-full">
													<TextFieldSelect
														id={`ivar-${i}`}
														containInModal
														size="full"
														optional={!d.required}
														options={d.options || []}
														bind:value={installVars[varKeyOf(d)]}
													>
														{d.name}<span class="ml-1.5 text-xs font-normal text-gray-400"
															>(enum)</span
														>
													</TextFieldSelect>
												</div>
											{:else}
												<TextField
													required={d.required}
													optional={!d.required}
													width="full"
													bind:value={installVars[varKeyOf(d)]}
													placeholder={varPlaceholder(d.type)}
												>
													{d.name}{#if d.type}<span class="ml-1.5 text-xs font-normal text-gray-400"
															>({d.type})</span
														>{/if}
												</TextField>
											{/if}
											{#if d.description && !d.pick}
												<p class="mt-1 w-full text-xs text-gray-500 dark:text-gray-400">
													{d.description}
												</p>
											{/if}
										</div>
									{/each}
								</div>
							{/if}

							{#if !confirmBrokenDeps.length && !(confirmInstallFull.dependencies && confirmInstallFull.dependencies.length) && !installVarDefs.length}
								<p class="text-sm text-gray-600 dark:text-gray-400">
									This package installs directly and asks for no extra input.
								</p>
							{/if}
						</div>
					{/if}
				</div>
				<FormError message={installVarsError} />
				<FormFooter
					closeModal={() => (confirmInstallVisible = false)}
					isSubmitting={installVarsSubmitting}
					disabled={confirmBrokenDeps.length > 0 || confirmRefusedDeps.length > 0 || installBlocked}
					okText="Install"
				/>
			</div>
		</FormGrid>
	</Modal>
{/if}

<!-- delete confirmations -->
<DeleteAlert
	title="Uninstall package"
	ok="Uninstall"
	actionMessage="Uninstall"
	name={removeInstallTarget ? removeInstallTarget.packageId : ''}
	list={removeUsedBy.length ? [] : removeInstallList}
	okDisabled={removeUsedBy.length > 0}
	permanent={!removeUsedBy.length}
	onClick={confirmRemoveInstall}
	bind:isVisible={removeInstallVisible}
>
	{#if removeUsedBy.length}
		<div
			class="rounded-md border-2 border-red-200 bg-red-50/60 p-4 dark:border-red-500/30 dark:bg-red-900/10"
		>
			<p class="font-medium text-red-700 dark:text-red-300">
				{removeUsedBy.join(', ')}
				{removeUsedBy.length === 1 ? 'uses' : 'use'} this package, so it cannot be uninstalled
			</p>
			<p class="mt-1 text-sm text-gray-700 dark:text-gray-300">
				Uninstall or update {removeUsedBy.length === 1 ? 'that package' : 'those packages'} first.
			</p>
		</div>
	{:else}
		{#if removePreviewLoading || removePreviewError}
			<div
				class="rounded-md border-2 border-amber-200 bg-amber-50/60 p-4 dark:border-amber-500/30 dark:bg-amber-900/10"
			>
				<p class="font-medium text-amber-800 dark:text-amber-300">
					Active campaigns that use this content will be closed or lose the parts this uninstall
					deletes
				</p>
				{#if removePreviewLoading}
					<p class="mt-1 text-sm text-gray-600 dark:text-gray-400">
						Checking which campaigns use this package...
					</p>
				{:else}
					<p class="mt-1 text-sm text-gray-600 dark:text-gray-400">
						{removePreviewError}
					</p>
				{/if}
			</div>
		{/if}
		{#if removeCampaignCount}
			<div
				class="rounded-md border-2 border-red-200 bg-red-50/60 p-4 dark:border-red-500/30 dark:bg-red-900/10"
			>
				<p class="font-medium text-red-700 dark:text-red-300">
					{removeCampaignCount === 1
						? '1 active campaign uses this content and will be closed'
						: `${removeCampaignCount} active campaigns use this content and will be closed`}
				</p>
				<ul class="ml-4 mt-2 list-disc space-y-1 text-sm text-gray-700 dark:text-gray-300">
					{#each removeCampaigns as c (c.id)}
						<li class="break-words">{c.name}</li>
					{/each}
				</ul>
				{#if removeCampaignCount > removeCampaigns.length}
					<p class="mt-2 text-sm text-gray-600 dark:text-gray-400">
						and {removeCampaignCount - removeCampaigns.length} more
					</p>
				{/if}
			</div>
		{/if}
		{#if removeKeepRunningCount}
			<div
				class="rounded-md border-2 border-red-200 bg-red-50/60 p-4 dark:border-red-500/30 dark:bg-red-900/10"
			>
				<p class="font-medium text-red-700 dark:text-red-300">
					{removeKeepRunningCount === 1
						? '1 active campaign uses this content and already has a close time. It keeps running without this content until then and will not work correctly'
						: `${removeKeepRunningCount} active campaigns use this content and already have a close time. They keep running without this content until then and will not work correctly`}
				</p>
				<ul class="ml-4 mt-2 list-disc space-y-1 text-sm text-gray-700 dark:text-gray-300">
					{#each removeKeepRunning as c (c.id)}
						<li class="break-words">
							{c.name}
							{#if c.closeAt}
								<span class="text-gray-500 dark:text-gray-400"
									>(closes {formatCloseAt(c.closeAt)})</span
								>
							{/if}
						</li>
					{/each}
				</ul>
				{#if removeKeepRunningCount > removeKeepRunning.length}
					<p class="mt-2 text-sm text-gray-600 dark:text-gray-400">
						and {removeKeepRunningCount - removeKeepRunning.length} more
					</p>
				{/if}
			</div>
		{/if}
		{#if removeChangedCount}
			<div
				class="rounded-md border-2 p-4 {removeChangedOpen
					? 'border-red-200 bg-red-50/60 dark:border-red-500/30 dark:bg-red-900/10'
					: 'border-amber-200 bg-amber-50/60 dark:border-amber-500/30 dark:bg-amber-900/10'}"
			>
				<p
					class="font-medium {removeChangedOpen
						? 'text-red-700 dark:text-red-300'
						: 'text-amber-800 dark:text-amber-300'}"
				>
					{removeChangedCount === 1
						? '1 active campaign keeps running but loses a filter, deny page, webhook or script'
						: `${removeChangedCount} active campaigns keep running but lose a filter, deny page, webhook or script`}
				</p>
				<ul class="ml-4 mt-2 list-disc space-y-1 text-sm text-gray-700 dark:text-gray-300">
					{#each removeChanged as c (c.id)}
						<li class="break-words">
							<span class="font-medium">{c.name}</span>
							<ul class="ml-4 list-[circle]">
								{#each c.losses || [] as loss}
									<li
										class="break-words {c.openToEveryone && loss.startsWith('IP filter removed')
											? 'font-medium text-red-700 dark:text-red-300'
											: ''}"
									>
										{loss}
									</li>
								{/each}
							</ul>
						</li>
					{/each}
				</ul>
				{#if removeChangedCount > removeChanged.length}
					<p class="mt-2 text-sm text-gray-600 dark:text-gray-400">
						and {removeChangedCount - removeChanged.length} more
					</p>
				{/if}
			</div>
		{/if}
		{#if removeChangedAssetCount}
			<div
				class="rounded-md border-2 p-4 {removeChangedAssetsOpen
					? 'border-red-200 bg-red-50/60 dark:border-red-500/30 dark:bg-red-900/10'
					: 'border-amber-200 bg-amber-50/60 dark:border-amber-500/30 dark:bg-amber-900/10'}"
			>
				<p
					class="font-medium {removeChangedAssetsOpen
						? 'text-red-700 dark:text-red-300'
						: 'text-amber-800 dark:text-amber-300'}"
				>
					{removeChangedAssetCount === 1
						? '1 asset stays but loses a filter'
						: `${removeChangedAssetCount} assets stay but lose a filter`}
				</p>
				<ul class="ml-4 mt-2 list-disc space-y-1 text-sm text-gray-700 dark:text-gray-300">
					{#each removeChangedAssets as a (a.id)}
						<li class="break-words">
							<span class="font-medium">{a.name}</span>
							{#if assetWhere(a) !== a.name}
								<span class="text-gray-500 dark:text-gray-400">({assetWhere(a)})</span>
							{/if}
							<ul class="ml-4 list-[circle]">
								{#each assetLossLines(a) as loss}
									<li
										class="break-words {a.openToEveryone && loss.startsWith('Filter removed')
											? 'font-medium text-red-700 dark:text-red-300'
											: ''}"
									>
										{loss}
									</li>
								{/each}
							</ul>
						</li>
					{/each}
				</ul>
				{#if removeChangedAssetCount > removeChangedAssets.length}
					<p class="mt-2 text-sm text-gray-600 dark:text-gray-400">
						and {removeChangedAssetCount - removeChangedAssets.length} more
					</p>
				{/if}
			</div>
		{/if}
		{#if removeKeptLists.length}
			<div class="rounded-lg bg-gray-50 p-4 dark:bg-gray-800/40">
				<p class="font-medium text-gray-900 dark:text-gray-300">
					{removeKeptLists.length === 1
						? '1 allow/deny list stays because assets still use it'
						: `${removeKeptLists.length} allow/deny lists stay because assets still use them`}
				</p>
				<p class="mt-1 text-sm text-gray-600 dark:text-gray-400">
					Kept lists become your own and are not removed with the package.
				</p>
				<ul class="ml-4 mt-2 list-disc space-y-1 text-sm text-gray-700 dark:text-gray-300">
					{#each removeKeptLists as l}
						<li class="break-words">
							{l.allowed ? 'Allow list' : 'Deny list'} <span class="font-medium">{l.name}</span>
							<span class="text-gray-500 dark:text-gray-400"
								>(used by {(l.assets || []).join(', ')})</span
							>
						</li>
					{/each}
				</ul>
			</div>
		{/if}
		{#if removeUserAssetCount}
			<div
				class="rounded-md border-2 border-amber-200 bg-amber-50/60 p-4 dark:border-amber-500/30 dark:bg-amber-900/10"
			>
				<p class="font-medium text-amber-800 dark:text-amber-300">
					{removeUserAssetCount === 1
						? '1 asset that this package did not install is on a domain this uninstall deletes. It is deleted with the domain'
						: `${removeUserAssetCount} assets that this package did not install are on domains this uninstall deletes. They are deleted with the domains`}
				</p>
			</div>
		{/if}
		{#if removeEdited.length}
			<div
				class="rounded-md border-2 border-amber-200 bg-amber-50/60 p-4 dark:border-amber-500/30 dark:bg-amber-900/10"
			>
				<p class="font-medium text-amber-800 dark:text-amber-300">
					{removeEdited.length === 1
						? 'You edited 1 item after installing it. Your changes are lost'
						: `You edited ${removeEdited.length} items after installing them. Your changes are lost`}
				</p>
				<ul class="ml-4 mt-2 list-disc space-y-1 text-sm text-gray-700 dark:text-gray-300">
					{#each removeEdited as e}
						<li class="break-words">
							{resourceNoun(e.resourceType, 1)} <span class="font-medium">{e.name}</span>
							{#if e.resourceType === 'asset' && e.sourceName}
								<span class="text-gray-500 dark:text-gray-400">(at {e.sourceName})</span>
							{:else if e.sourceName && e.sourceName !== e.name}
								<span class="text-gray-500 dark:text-gray-400">(installed as {e.sourceName})</span>
							{/if}
							{#if removeInstallTarget && e.packageId !== removeInstallTarget.packageId}
								<span class="text-gray-500 dark:text-gray-400">({e.packageId})</span>
							{/if}
						</li>
					{/each}
				</ul>
			</div>
		{/if}
	{/if}
</DeleteAlert>

<DeleteAlert
	title="Remove Bait Box"
	name={selectedRegistry ? selectedRegistry.name : ''}
	list={['The connection is removed', 'Content already installed from it stays']}
	permanent={false}
	onClick={confirmRemoveRegistry}
	bind:isVisible={removeRegistryVisible}
/>
