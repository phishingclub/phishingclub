<script>
	import StatusBadge from '$lib/components/StatusBadge.svelte';
	import { page } from '$app/stores';
	import { api } from '$lib/api/apiProxy.js';
	import { onMount } from 'svelte';
	import { newTableURLParams } from '$lib/service/tableURLParams.js';
	import { globalButtonDisabledAttributes } from '$lib/utils/form.js';
	import Headline from '$lib/components/Headline.svelte';
	import TextField from '$lib/components/TextField.svelte';
	import ToolTip from '$lib/components/ToolTip.svelte';
	import FieldLabel from '$lib/components/form/FieldLabel.svelte';
	import Button from '$lib/components/Button.svelte';
	import TableRow from '$lib/components/table/TableRow.svelte';
	import TableCell from '$lib/components/table/TableCell.svelte';
	import TableUpdateButton from '$lib/components/table/TableUpdateButton.svelte';
	import TableDeleteButton from '$lib/components/table/TableDeleteButton2.svelte';
	import FormError from '$lib/components/FormError.svelte';
	import { addToast } from '$lib/store/toast';
	import { AppStateService } from '$lib/service/appState';
	import TableCellAction from '$lib/components/table/TableCellAction.svelte';
	import TableCellEmpty from '$lib/components/table/TableCellEmpty.svelte';
	import FormGrid from '$lib/components/FormGrid.svelte';
	import Modal from '$lib/components/Modal.svelte';
	import InstalledResourceBadge from '$lib/components/InstalledResourceBadge.svelte';
	import BigButton from '$lib/components/BigButton.svelte';
	import FormColumns from '$lib/components/FormColumns.svelte';
	import FormColumn from '$lib/components/FormColumn.svelte';
	import FormFooter from '$lib/components/FormFooter.svelte';
	import Table from '$lib/components/table/Table.svelte';
	import HeadTitle from '$lib/components/HeadTitle.svelte';
	import TableCellCheck from '$lib/components/table/TableCellCheck.svelte';
	import { getModalText } from '$lib/utils/common';
	import TableCopyButton from '$lib/components/table/TableCopyButton.svelte';
	import { showIsLoading, hideIsLoading } from '$lib/store/loading.js';
	import TextareaField from '$lib/components/TextareaField.svelte';
	import FileField from '$lib/components/FileField.svelte';
	import TableDropDownEllipsis from '$lib/components/table/TableDropDownEllipsis.svelte';
	import DeleteAlert from '$lib/components/modal/DeleteAlert.svelte';
	import SelectSquare from '$lib/components/SelectSquare.svelte';
	import TableCellScope from '$lib/components/table/TableCellScope.svelte';
	import TableCellCheckbox from '$lib/components/table/TableCellCheckbox.svelte';
	import BulkActionBar from '$lib/components/table/BulkActionBar.svelte';
	import {
		createTableSelection,
		headerSelectionState,
		runBulkDelete
	} from '$lib/service/tableSelection.js';
	import TextFieldMultiSelect from '$lib/components/TextFieldMultiSelect.svelte';
	import AsnSelect from '$lib/components/AsnSelect.svelte';

	// services
	const appStateService = AppStateService.instance;

	// data
	let form = null;
	let formValues = {
		id: null,
		name: null,
		cidrs: null,
		ja4Fingerprints: null,
		countryCodes: [],
		asns: [],
		headers: [],
		allowed: null
	};
	let allowDenyList = [];
	let allowDenyListHasNextPage = true;

	// multi select
	const selection = createTableSelection();
	let isBulkDeleteAlertVisible = false;
	const isRowDisabled = (entry) => globalButtonDisabledAttributes(entry, contextCompanyID).disabled;
	$: selectablePageIds = allowDenyList
		.filter((entry) => !globalButtonDisabledAttributes(entry, contextCompanyID).disabled)
		.map((entry) => entry.id);
	$: headerState = headerSelectionState($selection, selectablePageIds);
	$: showMultiSelect = allowDenyList.length > 0;
	const onClickBulkDelete = async () => {
		await runBulkDelete({ ids: [...$selection], deleteFn: api.allowDeny.delete, noun: 'filter' });
		await refreshAllowDenies();
		return { success: true };
	};
	let formError = '';
	let contextCompanyID = null;
	const tableURLParams = newTableURLParams();
	let isModalVisible = false;
	let isSubmitting = false;
	let isTableLoading = false;
	let modalMode = null;
	let modalText = '';
	let availableCountryCodes = [];
	let asnAvailable = false;

	let isDeleteAlertVisible = false;
	let deleteValues = {
		id: null,
		name: null
	};

	$: {
		modalText = getModalText('Filter', modalMode);
	}

	// a visitor matches a filter when it matches any of its rules. While no ASN
	// data is downloaded no visitor IP resolves to an ASN, so an ASN rule never
	// matches. An allow filter whose only rule is ASNs then blocks every visitor,
	// otherwise the ASN rule simply has no effect.
	const hasNonASNRules = (f) =>
		!!(f.cidrs && f.cidrs.trim()) ||
		!!(f.ja4Fingerprints && f.ja4Fingerprints.trim()) ||
		(Array.isArray(f.countryCodes)
			? f.countryCodes.length > 0
			: !!(f.countryCodes && f.countryCodes.trim())) ||
		(Array.isArray(f.headers)
			? f.headers.length > 0
			: !!(f.headers && f.headers.trim() && f.headers.trim() !== '[]'));
	const asnOnlyAllow = (f) => f.allowed === true && !hasNonASNRules(f);

	$: formListsASNs = !!(formValues.asns && formValues.asns.length > 0) && !asnAvailable;
	$: asnAllowBlocksAll = formListsASNs && asnOnlyAllow(formValues);
	$: asnRuleNoEffect = formListsASNs && !asnAllowBlocksAll;

	// hooks
	onMount(() => {
		if (appStateService.getContext()) {
			contextCompanyID = appStateService.getContext().companyID;
		}
		refreshAllowDenies();
		tableURLParams.onChange(refreshAllowDenies);
		loadGeoIPMetadata();

		(async () => {
			const editID = $page.url.searchParams.get('edit');
			if (editID) {
				await openUpdateModal(editID);
			}
		})();

		return () => {
			tableURLParams.unsubscribe();
		};
	});

	// load geoip metadata to get country codes
	const loadGeoIPMetadata = async () => {
		try {
			const res = await api.geoip.getMetadata();
			if (res.success && res.data) {
				availableCountryCodes = res.data.country_codes || [];
				asnAvailable = res.data.asn_available || false;
			}
		} catch (e) {
			console.error('failed to load geoip metadata', e);
		}
	};

	// component logic
	const refreshAllowDenies = async () => {
		try {
			isTableLoading = true;
			selection.clear();
			const data = await getAllAllowDenyEntries();
			allowDenyList = data.rows;
			allowDenyListHasNextPage = data.hasNextPage;
		} catch (e) {
			addToast('Failed to get filters', 'Error');
			console.error(e);
		} finally {
			isTableLoading = false;
		}
	};

	/**
	 * @param {string} id
	 */
	const getAllowDenyListEntry = async (id) => {
		try {
			const res = await api.allowDeny.getByID(id);
			if (res.success) {
				return res.data;
			} else {
				throw res.error;
			}
		} catch (e) {
			addToast('Failed to get filter', 'Error');
			console.error('failed to get filter', e);
		}
	};

	const getAllAllowDenyEntries = async () => {
		try {
			const res = await api.allowDeny.getAllOverview(tableURLParams, contextCompanyID);
			if (!res.success) {
				throw res.error;
			}
			return res.data;
		} catch (e) {
			addToast('Failed to get filters', 'Error');
			console.error('failed to get filters', e);
		}
		return [];
	};

	const onClickSubmit = async () => {
		// validate that at least one of cidrs, ja4Fingerprints, countryCodes, asns, or headers is provided
		const hasCidrs = formValues.cidrs && formValues.cidrs.trim().length > 0;
		const hasJA4 = formValues.ja4Fingerprints && formValues.ja4Fingerprints.trim().length > 0;
		const hasCountryCodes = formValues.countryCodes && formValues.countryCodes.length > 0;
		const hasAsns = formValues.asns && formValues.asns.length > 0;
		const hasHeaders = formValues.headers && formValues.headers.length > 0;

		if (!hasCidrs && !hasJA4 && !hasCountryCodes && !hasAsns && !hasHeaders) {
			formError =
				'At least one of CIDRs, JA4 fingerprints, Country Codes, ASNs, or Headers must be provided';
			return;
		}

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
		formError = '';

		// process cidrs only if provided
		if (formValues.cidrs) {
			formValues.cidrs = formValues.cidrs
				.split('\n')
				.map((line) => singleIPToCIDR(line))
				.filter((line) => line.length > 0)
				.join('\n');
		} else {
			formValues.cidrs = '';
		}

		try {
			// convert headers array to json string
			const headersStr = JSON.stringify(
				formValues.headers.filter((h) => h.keyRegex && h.valueRegex)
			);

			const res = await api.allowDeny.create({
				name: formValues.name,
				cidrs: formValues.cidrs,
				ja4Fingerprints: formValues.ja4Fingerprints || '',
				countryCodes: formValues.countryCodes.join('\n'),
				asns: formValues.asns.join('\n'),
				headers: headersStr,
				allowed: formValues.allowed,
				companyID: contextCompanyID
			});
			if (!res.success) {
				formError = res.error;
				return;
			}
			addToast('Created filter', 'Success');
			closeModal();
		} catch (err) {
			addToast('Failed to create filter', 'Error');
			console.error('failed to create filter:', err);
		}
		refreshAllowDenies();
	};

	const update = async () => {
		formError = '';

		// process cidrs only if provided
		if (formValues.cidrs) {
			formValues.cidrs = formValues.cidrs
				.split('\n')
				.map((line) => singleIPToCIDR(line))
				.filter((line) => line.length > 0)
				.join('\n');
		} else {
			formValues.cidrs = '';
		}

		try {
			// convert headers array to json string
			const headersStr = JSON.stringify(
				formValues.headers.filter((h) => h.keyRegex && h.valueRegex)
			);

			const res = await api.allowDeny.update({
				id: formValues.id,
				name: formValues.name,
				cidrs: formValues.cidrs,
				ja4Fingerprints: formValues.ja4Fingerprints || '',
				countryCodes: formValues.countryCodes.join('\n'),
				asns: formValues.asns.join('\n'),
				headers: headersStr,
				companyID: formValues.companyID
			});
			if (res.success) {
				addToast('Updated filter', 'Success');
				closeModal();
			} else {
				formError = res.error;
			}
		} catch (e) {
			addToast('Failed to update filter', 'Error');
			console.error('failed to update filter', e);
		}
		refreshAllowDenies();
	};

	const openDeleteAlert = async (domain) => {
		isDeleteAlertVisible = true;
		deleteValues.id = domain.id;
		deleteValues.name = domain.name;
	};

	/**
	 * @param {string} id
	 */
	const onClickDelete = async (id) => {
		const action = api.allowDeny.delete(id);
		action
			.then((res) => {
				if (res.success) {
					refreshAllowDenies();
					return;
				}
				throw res.error;
			})
			.catch((e) => {
				console.error('failed to delete filter:', e);
			});
		return action;
	};

	const openCreateModal = () => {
		modalMode = 'create';
		formValues = {
			id: null,
			name: null,
			cidrs: null,
			ja4Fingerprints: null,
			countryCodes: [],
			asns: [],
			headers: [],
			allowed: null
		};
		isModalVisible = true;
	};

	const closeModal = () => {
		formError = '';
		isModalVisible = false;
		form.reset();
	};

	/**
	 * Opens the update modal
	 * @param {string} id
	 */
	const openUpdateModal = async (id) => {
		modalMode = 'update';
		try {
			showIsLoading();
			const allowDeny = await getAllowDenyListEntry(id);
			const r = globalButtonDisabledAttributes(allowDeny, contextCompanyID);
			if (r.disabled) {
				hideIsLoading();
				addToast(r.title, 'Info');
				return;
			}
			assignAllowDeny(allowDeny);
			isModalVisible = true;
		} catch (e) {
			addToast('Failed to get filter', 'Error');
			console.error('failed to get filter', e);
		} finally {
			hideIsLoading();
		}
	};

	const openCopyModal = async (id) => {
		modalMode = 'copy';

		try {
			showIsLoading();
			const allowDeny = await getAllowDenyListEntry(id);
			assignAllowDeny(allowDeny);
			allowDeny.id = null;
			isModalVisible = true;
		} catch (e) {
			addToast('Failed to get filter', 'Error');
			console.error('failed to get filter', e);
		} finally {
			hideIsLoading();
		}
	};

	const assignAllowDeny = (allowDeny) => {
		// parse country codes from newline-separated string to array
		let countryCodesArray = [];
		if (allowDeny.countryCodes) {
			countryCodesArray = allowDeny.countryCodes
				.split('\n')
				.map((code) => code.trim())
				.filter((code) => code.length > 0);
		}

		// parse asns from newline-separated string to array
		let asnsArray = [];
		if (allowDeny.asns) {
			asnsArray = allowDeny.asns
				.split('\n')
				.map((a) => a.trim())
				.filter((a) => a.length > 0);
		}

		// parse headers from json string to array
		let headersArray = [];
		if (allowDeny.headers) {
			try {
				headersArray = JSON.parse(allowDeny.headers);
			} catch (e) {
				console.error('failed to parse headers json', e);
				headersArray = [];
			}
		}

		formValues = {
			id: allowDeny.id,
			name: allowDeny.name,
			cidrs: allowDeny.cidrs,
			ja4Fingerprints: allowDeny.ja4Fingerprints || '',
			countryCodes: countryCodesArray,
			asns: asnsArray,
			headers: headersArray,
			allowed: allowDeny.allowed,
			companyID: allowDeny.companyID
		};
	};

	const addHeaderRule = () => {
		formValues.headers = [...formValues.headers, { keyRegex: '', valueRegex: '' }];
	};

	const removeHeaderRule = (index) => {
		formValues.headers = formValues.headers.filter((_, i) => i !== index);
	};

	/** @param {string} ip */
	const singleIPToCIDR = (ip) => {
		if (ip.trim() == '') {
			return '';
		}
		if (ip.includes('/')) {
			return ip;
		}
		if (ip.includes(':')) {
			return ip + '/128';
		}
		return ip + '/32';
	};
</script>

<HeadTitle title="Filter" />
<main>
	<Headline docSlug="filtering">Filters</Headline>
	<BigButton on:click={openCreateModal}>New filter</BigButton>
	<BulkActionBar
		count={$selection.size}
		noun="filter"
		on:delete={() => (isBulkDeleteAlertVisible = true)}
		on:clear={() => selection.clear()}
	/>
	<Table
		selectable
		{headerState}
		on:toggleAll={(e) => selection.setPageSelection(selectablePageIds, e.detail)}
		columns={[
			{ column: 'Name', size: 'large' },
			{ column: 'Allowed', title: 'Type', size: 'small', alignText: 'center' },
			...(contextCompanyID ? [{ column: 'Scope', size: 'small' }] : [])
		]}
		sortable={['Name', 'Allowed', ...(contextCompanyID ? ['scope'] : [])]}
		hasData={!!allowDenyList.length}
		hasNextPage={allowDenyListHasNextPage}
		plural="filters"
		pagination={tableURLParams}
		isGhost={isTableLoading}
	>
		{#each allowDenyList as entry}
			<TableRow>
				{#if showMultiSelect}
					<TableCellCheckbox
						checked={$selection.has(entry.id)}
						disabled={isRowDisabled(entry)}
						on:change={() => selection.toggle(entry.id)}
					/>
				{/if}
				<TableCell wrap>
					<button
						on:click={() => {
							openUpdateModal(entry.id);
						}}
						{...globalButtonDisabledAttributes(entry, contextCompanyID)}
						title={entry.name}
						class="pc-cell-link py-1 text-left max-w-full truncate align-middle"
					>
						{entry.name}
					</button>
					{#if entry.asns && entry.asns.trim() && !asnAvailable}
						<StatusBadge
							tone="warning"
							class="ml-2 align-middle"
							title={asnOnlyAllow(entry)
								? 'This filter lists ASNs but ASN data is not downloaded, so it blocks every visitor. Download it under Settings, IP Data.'
								: 'This filter lists ASNs but ASN data is not downloaded, so its ASN rule has no effect. Download it under Settings, IP Data.'}
							>ASN data missing</StatusBadge
						>
					{/if}
					<!-- the Type column is off screen on phones -->
					<span class="flex sm:hidden pb-1">
						<span
							class="inline-flex items-center rounded px-2 py-0.5 text-xs font-semibold {entry.allowed
								? 'bg-cta-light-blue text-active-blue dark:bg-highlight-blue/20 dark:text-pc-dusty-light-blue'
								: 'bg-gray-200 text-slate-700 dark:bg-gray-700 dark:text-gray-200'}"
						>
							{entry.allowed ? 'Allow' : 'Deny'}
						</span>
					</span>
				</TableCell>
				<TableCell alignText="center">
					<span class="leading-none">
						<span
							class="inline-flex items-center rounded px-2 py-0.5 text-xs font-semibold {entry.allowed
								? 'bg-cta-light-blue text-active-blue dark:bg-highlight-blue/20 dark:text-pc-dusty-light-blue'
								: 'bg-gray-200 text-slate-700 dark:bg-gray-700 dark:text-gray-200'}"
						>
							{entry.allowed ? 'Allow' : 'Deny'}
						</span>
					</span>
				</TableCell>
				{#if contextCompanyID}
					<TableCellScope companyID={entry.companyID} />
				{/if}
				<TableCellEmpty />
				<TableCellAction>
					<TableDropDownEllipsis>
						<TableUpdateButton
							on:click={() => openUpdateModal(entry.id)}
							{...globalButtonDisabledAttributes(entry, contextCompanyID)}
						/>
						<TableCopyButton
							title={'Copy'}
							on:click={() => openCopyModal(entry.id)}
							{...globalButtonDisabledAttributes(entry, contextCompanyID)}
						/>

						<TableDeleteButton
							on:click={() => openDeleteAlert(entry)}
							{...globalButtonDisabledAttributes(entry, contextCompanyID)}
						></TableDeleteButton>
					</TableDropDownEllipsis>
				</TableCellAction>
			</TableRow>
		{/each}
	</Table>

	<Modal
		headerText={modalText}
		visible={isModalVisible}
		onClose={closeModal}
		{isSubmitting}
		width="lg"
	>
		<div class="w-full">
			<FormGrid on:submit={onClickSubmit} bind:bindTo={form} {isSubmitting}>
				<FormColumns>
					<FormColumn stretch>
						{#if modalMode === 'update'}
							<!-- zero width so the notice wraps to the form width instead of setting it -->
							<div class="w-0 min-w-full">
								<InstalledResourceBadge resourceType="allowdeny" resourceId={formValues.id} variant="notice" />
							</div>
						{/if}
						<div class="w-full">
							<TextField
								width="full"
								required
								minLength={1}
								maxLength={127}
								bind:value={formValues.name}
								placeholder="Company allow range">Name</TextField
							>
						</div>
						{#if modalMode === 'create' || modalMode === 'copy'}
							<SelectSquare
								label="Filter Type"
								options={[
									{ value: true, label: 'Allow' },
									{ value: false, label: 'Deny' }
								]}
								bind:value={formValues.allowed}
							/>
						{:else}
							<div class="flex flex-col py-2">
								<FieldLabel>Filter Type</FieldLabel>
								<span
									class="self-start inline-flex items-center rounded px-2 py-0.5 text-sm font-semibold {formValues.allowed
										? 'bg-cta-light-blue text-active-blue dark:bg-highlight-blue/20 dark:text-pc-dusty-light-blue'
										: 'bg-gray-200 text-slate-700 dark:bg-gray-700 dark:text-gray-200'}"
								>
									{formValues.allowed ? 'Allow' : 'Deny'}
								</span>
							</div>
						{/if}
						<p class="text-sm text-gray-600 dark:text-gray-400 mb-4 transition-colors duration-200">
							A visitor matches this filter when it matches any one of its rules.
							{#if formValues.allowed === true}
								Matching visitors are let in.
							{:else if formValues.allowed === false}
								Matching visitors are blocked.
							{:else}
								Pick Allow or Deny.
							{/if}
						</p>
						{#if asnAllowBlocksAll}
							<div
								role="note"
								class="mb-4 flex w-0 min-w-full items-start gap-2.5 rounded-md border border-amber-200 bg-amber-50 px-3 py-2.5 dark:border-amber-700/50 dark:bg-amber-900/20"
							>
								<svg
									class="mt-0.5 h-4 w-4 shrink-0 text-amber-600 dark:text-amber-400"
									viewBox="0 0 24 24"
									fill="none"
									stroke="currentColor"
									stroke-width="2"
									stroke-linecap="round"
									stroke-linejoin="round"
									aria-hidden="true"
								>
									<path d="M10.29 3.86 1.82 18a2 2 0 0 0 1.71 3h16.94a2 2 0 0 0 1.71-3L13.71 3.86a2 2 0 0 0-3.42 0z" />
									<line x1="12" y1="9" x2="12" y2="13" />
									<line x1="12" y1="17" x2="12.01" y2="17" />
								</svg>
								<p class="min-w-0 break-words text-sm leading-relaxed text-amber-800 dark:text-amber-200">
									This allow filter only lists ASNs and no ASN data is downloaded, so it will
									block every visitor. Download the ASN data under
									<a
										class="font-medium text-cta-blue dark:text-highlight-blue underline-offset-2 hover:underline"
										href="/settings#ipdata">Settings, IP Data</a
									>
									or remove the ASN rules.
								</p>
							</div>
						{/if}
						{#if asnRuleNoEffect}
							<div
								role="note"
								class="mb-4 flex w-0 min-w-full items-start gap-2.5 rounded-md border border-amber-200 bg-amber-50 px-3 py-2.5 dark:border-amber-700/50 dark:bg-amber-900/20"
							>
								<svg
									class="mt-0.5 h-4 w-4 shrink-0 text-amber-600 dark:text-amber-400"
									viewBox="0 0 24 24"
									fill="none"
									stroke="currentColor"
									stroke-width="2"
									stroke-linecap="round"
									stroke-linejoin="round"
									aria-hidden="true"
								>
									<path d="M10.29 3.86 1.82 18a2 2 0 0 0 1.71 3h16.94a2 2 0 0 0 1.71-3L13.71 3.86a2 2 0 0 0-3.42 0z" />
									<line x1="12" y1="9" x2="12" y2="13" />
									<line x1="12" y1="17" x2="12.01" y2="17" />
								</svg>
								<p class="min-w-0 break-words text-sm leading-relaxed text-amber-800 dark:text-amber-200">
									This filter lists ASNs but no ASN data is downloaded, so its ASN rule has no
									effect (no visitor can be matched by ASN). Download the ASN data under
									<a
										class="font-medium text-cta-blue dark:text-highlight-blue underline-offset-2 hover:underline"
										href="/settings#ipdata">Settings, IP Data</a
									>.
								</p>
							</div>
						{/if}
						<div class="mb-6 pt-4">
							<div class="flex flex-col" role="group" aria-labelledby="filter-header-rules-label">
								<FieldLabel
									id="filter-header-rules-label"
									optional
									toolTipText="Add header key/value regex patterns to match. Both key and value must match for the rule to trigger."
									>Header Rules</FieldLabel
								>
								<div class="space-y-3 w-full">
									{#each formValues.headers as header, index}
										<div
											class="flex flex-col sm:flex-row sm:gap-2 border-b border-gray-200 dark:border-gray-700 pb-2 sm:border-0 sm:pb-0"
										>
											<div class="flex-1 min-w-0">
												<TextField
													bind:value={header.keyRegex}
													placeholder="e.g. user-agent"
													width="full"
													required={false}>Key Regex</TextField
												>
											</div>
											<div class="flex-1 min-w-0">
												<TextField
													bind:value={header.valueRegex}
													placeholder="e.g. .*bot.*"
													width="full"
													required={false}>Value Regex</TextField
												>
											</div>
											<div class="flex items-end sm:pb-3">
												<button
													type="button"
													class="pc-focus inline-flex items-center gap-2 h-9 px-2 text-sm text-slate-600 dark:text-gray-300 hover:bg-gray-200 dark:hover:bg-gray-700/80 rounded-md transition-colors duration-200"
													on:click={() => removeHeaderRule(index)}
													title="Remove this header rule"
													aria-label="Remove header rule"
												>
													<img class="w-4 flex-shrink-0" src="/delete2.svg" alt="" />
													<span class="sm:hidden">Remove rule</span>
												</button>
											</div>
										</div>
									{/each}
									<Button type="button" variant="secondary" css="w-auto" size="" on:click={addHeaderRule}>
										+ Add Header Rule
									</Button>
								</div>
							</div>
						</div>
						<TextareaField
							optional
							bind:value={formValues.ja4Fingerprints}
							placeholder="e.g. t13d1715h2_8daaf6152771_..."
							toolTipText="One JA4 fingerprint per line. Does not work behind a reverse proxy."
							fullWidth>JA4 Fingerprints</TextareaField
						>
						<TextareaField
							optional
							bind:value={formValues.cidrs}
							placeholder="e.g. 192.168.1.0/24"
							toolTipText="One CIDR per line"
							fullWidth>CIDRs</TextareaField
						>
						<div class="w-full">
							<TextFieldMultiSelect
								id="country-codes"
								width="full"
								optional
								bind:value={formValues.countryCodes}
								options={availableCountryCodes}
								placeholder="Select countries..."
								toolTipText="Filter based on Geo IP country code lookup"
							>
								Geo IP Country Codes
							</TextFieldMultiSelect>
							<AsnSelect
								width="full"
								bind:value={formValues.asns}
								placeholder="Number or name..."
								installed={asnAvailable}
								toolTipText="Filter based on the autonomous system that announces the visitor IP. Requires the ASN data package under Settings."
							/>
						</div>
					</FormColumn>
				</FormColumns>
				<FormError message={formError} />
				<FormFooter {closeModal} {isSubmitting} />
			</FormGrid>
		</div>
	</Modal>
	<DeleteAlert
		list={[
			'It is removed from the campaigns and assets that use it',
			'Assets with no lists left are no longer filtered'
		]}
		name={deleteValues.name}
		onClick={() => onClickDelete(deleteValues.id)}
		bind:isVisible={isDeleteAlertVisible}
	></DeleteAlert>
	<DeleteAlert
		title="Delete filters"
		list={[
			'They are removed from the campaigns and assets that use them',
			'Assets with no lists left are no longer filtered'
		]}
		name={`${$selection.size} filter${$selection.size === 1 ? '' : 's'}`}
		onClick={onClickBulkDelete}
		confirm
		bind:isVisible={isBulkDeleteAlertVisible}
	></DeleteAlert>
</main>
