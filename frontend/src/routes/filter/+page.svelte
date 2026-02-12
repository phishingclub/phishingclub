<script>
	import { page } from '$app/stores';
	import { api } from '$lib/api/apiProxy.js';
	import { onMount } from 'svelte';
	import { newTableURLParams } from '$lib/service/tableURLParams.js';
	import { globalButtonDisabledAttributes } from '$lib/utils/form.js';
	import Headline from '$lib/components/Headline.svelte';
	import TextField from '$lib/components/TextField.svelte';
	import ToolTip from '$lib/components/ToolTip.svelte';
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
	import TextFieldMultiSelect from '$lib/components/TextFieldMultiSelect.svelte';

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
		headers: [],
		allowed: null
	};
	let allowDenyList = [];
	let allowDenyListHasNextPage = true;
	let formError = '';
	let contextCompanyID = null;
	const tableURLParams = newTableURLParams();
	let isModalVisible = false;
	let isSubmitting = false;
	let isTableLoading = false;
	let modalMode = null;
	let modalText = '';
	let availableCountryCodes = [];

	let isDeleteAlertVisible = false;
	let deleteValues = {
		id: null,
		name: null
	};

	$: {
		modalText = getModalText('Filter', modalMode);
	}

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
			}
		} catch (e) {
			console.error('failed to load geoip metadata', e);
		}
	};

	// component logic
	const refreshAllowDenies = async () => {
		try {
			isTableLoading = true;
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
		// validate that at least one of cidrs, ja4Fingerprints, countryCodes, or headers is provided
		const hasCidrs = formValues.cidrs && formValues.cidrs.trim().length > 0;
		const hasJA4 = formValues.ja4Fingerprints && formValues.ja4Fingerprints.trim().length > 0;
		const hasCountryCodes = formValues.countryCodes && formValues.countryCodes.length > 0;
		const hasHeaders = formValues.headers && formValues.headers.length > 0;

		if (!hasCidrs && !hasJA4 && !hasCountryCodes && !hasHeaders) {
			formError =
				'At least one of CIDRs, JA4 fingerprints, Country Codes, or Headers must be provided';
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
				console.log(r.title);
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
	<Headline>Filters</Headline>
	<BigButton on:click={openCreateModal}>New filter</BigButton>
	<Table
		columns={[
			{ column: 'Name', size: 'large' },
			{ column: 'Allowed', size: 'small', alignText: 'center' },
			...(contextCompanyID ? [{ column: 'Scope', size: 'small' }] : [])
		]}
		sortable={['Name', 'Allowed', ...(contextCompanyID ? ['scope'] : [])]}
		hasData={!!allowDenyList.length}
		hasNextPage={allowDenyListHasNextPage}
		plural="Allow deny entries"
		pagination={tableURLParams}
		isGhost={isTableLoading}
	>
		{#each allowDenyList as entry}
			<TableRow>
				<TableCell>
					<button
						on:click={() => {
							openUpdateModal(entry.id);
						}}
						{...globalButtonDisabledAttributes(entry, contextCompanyID)}
						title={entry.name}
					>
						{entry.name}
					</button>
				</TableCell>
				<TableCellCheck value={entry.allowed} />
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

	<Modal headerText={modalText} visible={isModalVisible} onClose={closeModal} {isSubmitting}>
		<FormGrid on:submit={onClickSubmit} bind:bindTo={form} {isSubmitting}>
			<FormColumns>
				<FormColumn>
					<TextField
						required
						minLength={1}
						maxLength={127}
						bind:value={formValues.name}
						placeholder="Company allow range">Name</TextField
					>
					{#if modalMode === 'create' || modalMode === 'copy'}
						<SelectSquare
							label="Filter Type"
							options={[
								{ value: true, label: 'Allow' },
								{ value: false, label: 'Deny' }
							]}
							bind:value={formValues.allowed}
						/>
					{/if}
					<div class="mb-6 pt-4">
						<label class="flex flex-col">
							<div class="flex items-center py-2">
								<p class="font-semibold text-slate-600 dark:text-gray-400">Header Rules</p>
								<ToolTip>
									Add header key/value regex patterns to match. Both key and value must match for
									the rule to trigger.
								</ToolTip>
								<div
									class="bg-gray-100 dark:bg-gray-800/60 ml-2 px-2 rounded-md transition-colors duration-200 h-6 flex items-center"
								>
									<p
										class="text-slate-600 dark:text-gray-400 text-xs transition-colors duration-200"
									>
										optional
									</p>
								</div>
							</div>
							<div class="space-y-3 min-w-[700px]">
								{#each formValues.headers as header, index}
									<div class="flex gap-2">
										<div class="flex-1">
											<TextField
												bind:value={header.keyRegex}
												placeholder="user-agent"
												width="full"
												required={false}>Key Regex</TextField
											>
										</div>
										<div class="flex-1">
											<TextField
												bind:value={header.valueRegex}
												placeholder=".*bot.*"
												width="full"
												required={false}>Value Regex</TextField
											>
										</div>
										<div class="flex items-end pb-4">
											<button
												type="button"
												class="p-1 hover:bg-gray-200 dark:hover:bg-gray-700/80 rounded-md transition-colors duration-200"
												on:click={() => removeHeaderRule(index)}
												title="Remove this header rule"
												aria-label="Remove header rule"
											>
												<img class="w-4 flex-shrink-0" src="/delete2.svg" alt="" />
											</button>
										</div>
									</div>
								{/each}
								<button
									type="button"
									class="px-4 py-2 bg-gradient-to-b from-blue-500 to-indigo-400 dark:from-blue-600 dark:to-indigo-500 hover:from-blue-400 hover:to-indigo-400 dark:hover:from-blue-500 dark:hover:to-indigo-400 text-white font-semibold rounded-md transition-all duration-200"
									on:click={addHeaderRule}
								>
									+ Add Header Rule
								</button>
							</div>
						</label>
					</div>
					<TextareaField
						optional
						bind:value={formValues.ja4Fingerprints}
						placeholder="t13d1715h2_8daaf6152771_02713d6af862"
						toolTipText="Newlines separated JA4 fingerprints (does not work behind Reverse Proxy)"
						fullWidth>JA4 Fingerprints</TextareaField
					>
					<TextareaField
						optional
						bind:value={formValues.cidrs}
						placeholder="192.168.1.0/24"
						toolTipText="Newlines seperated CIDRs"
						fullWidth>CIDRs</TextareaField
					>
					<TextFieldMultiSelect
						id="country-codes"
						optional
						bind:value={formValues.countryCodes}
						options={availableCountryCodes}
						placeholder="Select countries..."
						toolTipText="Filter based on GeoIP country code lookup"
					>
						GeoIP Country Codes
					</TextFieldMultiSelect>
				</FormColumn>
			</FormColumns>
			<FormError message={formError} />
			<FormFooter {closeModal} {isSubmitting} />
		</FormGrid>
	</Modal>
	<DeleteAlert
		name={deleteValues.name}
		onClick={() => onClickDelete(deleteValues.id)}
		bind:isVisible={isDeleteAlertVisible}
	></DeleteAlert>
</main>
