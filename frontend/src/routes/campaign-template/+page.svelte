<script>
	import { api } from '$lib/api/apiProxy.js';
	import { onMount } from 'svelte';
	import { newTableURLParams } from '$lib/service/tableURLParams.js';
	import { globalButtonDisabledAttributes } from '$lib/utils/form.js';
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
	import ProxySvgIcon from '$lib/components/ProxySvgIcon.svelte';
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
	import TrainingLabel from '$lib/components/TrainingLabel.svelte';
	import BulkActionBar from '$lib/components/table/BulkActionBar.svelte';
	import {
		createTableSelection,
		headerSelectionState,
		runBulkDelete
	} from '$lib/service/tableSelection.js';
	import SelectSquare from '$lib/components/SelectSquare.svelte';
	import TableDropDownButton from '$lib/components/table/TableDropDownButton.svelte';
	import CopyCell from '$lib/components/table/CopyCell.svelte';
	import TextFieldSelectWithType from '$lib/components/form/TextFieldSelectWithType.svelte';
	import ConditionalDisplay from '$lib/components/ConditionalDisplay.svelte';
	import { resourceContext } from '$lib/store/resourceContext';

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
		isTraining: false
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
	const tableURLParams = newTableURLParams();
	let isModalVisible = false;
	let isSubmitting = false;
	let isTableLoading = false;
	let modalMode = null;
	let modalText = '';
	let isAllowListingVisible = false;
	let allowListingLoading = false;
	let allowListingError = '';
	let allowListingData = {
		senderIP: '',
		smtpSenderDomain: '',
		simulationUrl: ''
	};
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

	/**
	 * Opens the allow-listing modal for a given campaign template ID.
	 * Fetches the template, SMTP config, and fills allowListingData.
	 */
	async function openAllowListingModal(templateId) {
		isAllowListingVisible = true;
		allowListingLoading = true;
		allowListingError = '';
		allowListingData = {
			senderIP: 'Add email sender IP here',
			smtpSenderDomain: '',
			simulationUrl: ''
		};

		try {
			const templateRes = await api.campaignTemplate.getByID(templateId);
			if (!templateRes.success) throw templateRes.error || 'Failed to fetch campaign template';
			const template = templateRes.data;

			const emailRes = await api.email.getByID(template.emailID);
			const domainRes = await api.domain.getByID(template.domainID);

			allowListingData = {
				...allowListingData,
				smtpSenderDomain: emailRes.data.mailEnvelopeFrom,
				simulationUrl: `${domainRes.data.name}/*`
			};
		} catch (e) {
			allowListingError =
				typeof e === 'string' ? e : e?.message || 'Failed to load allow-listing info';
		} finally {
			allowListingLoading = false;
		}
	}
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
				emailID: emailMap.byValueOrNull(formValues.email),
				smtpConfigurationID: smtpConfigurationMap.byValueOrNull(formValues.smtpConfiguration),
				apiSenderID: apiSenderMap.byValueOrNull(formValues.apiSender),
				landingPageID:
					formValues.landingPageType === 'page'
						? landingPageMap.byValueOrNull(formValues.landingPage)
						: null,
				landingProxyID:
					formValues.landingPageType === 'proxy'
						? landingProxyMap.byValueOrNull(formValues.landingPage)
						: null,
				beforeLandingPageID: beforeLandingPageMap.byValueOrNull(formValues.beforeLandingPage),
				beforeLandingProxyID: null,
				afterLandingPageID: afterLandingPageMap.byValueOrNull(formValues.afterLandingPage),
				afterLandingProxyID: null,
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
				emailID: emailMap.byValueOrNull(formValues.email),
				smtpConfigurationID: smtpConfigurationMap.byValueOrNull(formValues.smtpConfiguration),
				apiSenderID: apiSenderMap.byValueOrNull(formValues.apiSender),
				landingPageID:
					formValues.landingPageType === 'page'
						? landingPageMap.byValueOrNull(formValues.landingPage)
						: null,
				landingProxyID:
					formValues.landingPageType === 'proxy'
						? landingProxyMap.byValueOrNull(formValues.landingPage)
						: null,
				beforeLandingPageID: beforeLandingPageMap.byValueOrNull(formValues.beforeLandingPage),
				beforeLandingProxyID: null,
				afterLandingPageID: afterLandingPageMap.byValueOrNull(formValues.afterLandingPage),
				afterLandingProxyID: null,
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
			isTraining: false
		};
		modalError = '';
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

<HeadTitle title="Campaigns templates" />

<main>
	<Headline>Campaigns templates</Headline>
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
			{ column: 'Domain', size: 'small' },
			{ column: 'SMTP', size: 'small' },
			{ column: 'API Sender', size: 'small' },
			{ column: 'Email', size: 'small' },
			{ column: 'Before Landing Page', size: 'small' },
			{ column: 'Landing Page', size: 'small' },
			{ column: 'After Landing Page', size: 'small' },
			{ column: 'After landing page redirect URL', size: 'small' },
			{ column: 'Is complete', size: 'small', alignText: 'center' },
			...(contextCompanyID ? [{ column: 'Scope', size: 'small' }] : [])
		]}
		sortable={[
			'Name',
			'Domain',
			'SMTP',
			'API Sender',
			'Email',
			'Before Landing Page',
			'Landing Page',
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
						<span class="flex items-center gap-2">
							{#if template.isTraining}
								<TrainingLabel />
							{/if}
							{template.name}
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
							<span class="flex items-center gap-1">
								<ProxySvgIcon size="w-4 h-4" />
								{beforeLandingProxyMap.byKey(template.beforeLandingProxyID)}
							</span>
						</a>
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
							<span class="flex items-center gap-1">
								<ProxySvgIcon size="w-4 h-4" />
								{landingProxyMap.byKey(template.landingProxyID)}
							</span>
						</a>
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
							<span class="flex items-center gap-1">
								<ProxySvgIcon size="w-4 h-4" />
								{afterLandingProxyMap.byKey(template.afterLandingProxyID)}
							</span>
						</a>
					{/if}
				</TableCell>
				<TableCell>
					{#if template.afterLandingPageRedirectURL}
						<a
							href={`${template.afterLandingPageRedirectURL}`}
							target="_blank"
							class="block w-full py-1"
							title={template.afterLandingPageRedirectURL}
						>
							{template.afterLandingPageRedirectURL}
						</a>
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
						{#if template.smtpConfigurationID}
							<TableDropDownButton
								name="Allow listing"
								on:click={() => openAllowListingModal(template.id)}
								{...globalButtonDisabledAttributes(template, contextCompanyID)}
							/>
						{/if}
						<TableDeleteButton
							on:click={() => openDeleteAlert(template)}
							{...globalButtonDisabledAttributes(template, contextCompanyID)}
						></TableDeleteButton>
					</TableDropDownEllipsis>
				</TableCellAction>
			</TableRow>
		{/each}
	</Table>

	<Modal
		headerText="Allow listing"
		visible={isAllowListingVisible}
		onClose={() => {
			isAllowListingVisible = false;
			allowListingData = { senderIP: '', smtpSenderDomain: '', simulationUrl: '' };
			allowListingError = '';
		}}
	>
		<div class="space-y-4 p-4 min-w-[350px] max-w-[600px]">
			{#if allowListingLoading}
				<div>Loading allow-listing information…</div>
			{:else if allowListingError}
				<div class="text-red-600">{allowListingError}</div>
			{:else}
				<h1>Microsoft Allow listing</h1>
				<p>
					To ensure your emails are delivered and not blocked by Microsoft Defender for Office 365,
					add the following information to the <b>Advanced Delivery Policy</b>
					as a third-party phishing simulation.
				</p>
				<div>
					<b>Domain (MAIL FROM/5321.MailFrom)</b>
					<CopyCell text={allowListingData.smtpSenderDomain}>
						{allowListingData.smtpSenderDomain}
					</CopyCell>
					<div class="text-xs text-gray-500 mt-1">
						{#if !allowListingData.smtpSenderDomain}
							Use the domain part of the sender address you use for this campaign (e.g. <code
								>example.com</code
							>
							if your sender is <code>user@example.com</code>).
						{/if}
					</div>
				</div>
				<div>
					<b>Sending IP</b>
					<CopyCell text={allowListingData.senderIP}>
						{allowListingData.senderIP}
					</CopyCell>
				</div>
				<div>
					<b>Simulation URLs to allow</b>
					<CopyCell text={allowListingData.simulationUrl}>
						{allowListingData.simulationUrl}
					</CopyCell>
				</div>
				<div class="text-sm text-gray-700">
					<b>Where to add these:</b>
					<ol class="list-decimal ml-6">
						<li>
							Go to <a
								href="https://security.microsoft.com/advanceddelivery"
								target="_blank"
								class="text-blue-600 underline">Microsoft Defender Advanced Delivery</a
							>
						</li>
						<li>Select the <b>Phishing simulation</b> tab</li>
						<li>
							Click <b>Add</b> or <b>Edit</b> to configure a third-party phishing simulation
						</li>
						<li>
							Enter the above values in the <b>Domain</b>, <b>Sending IP</b>, and
							<b>Simulation URLs to allow</b> fields
						</li>
					</ol>
					<div class="mt-2">
						For more details, see the <a
							href="https://learn.microsoft.com/en-us/defender-office-365/advanced-delivery-policy-configure?view=o365-worldwide"
							target="_blank"
							class="text-blue-600 underline">official Microsoft documentation</a
						>.
					</div>
				</div>
				<div class="mt-4">
					<button
						class="bg-blue-600 text-white px-4 py-2 rounded hover:bg-blue-700"
						on:click={() => {
							const text = `Domain:\n${allowListingData.smtpSenderDomain}\n
Sender IP:\n${allowListingData.senderIP}\n
Simulation URLs to allow:\n${allowListingData.simulationUrl}\n
`;
							navigator.clipboard.writeText(text);
							addToast('Copied allow listing', 'Info');
						}}>Copy All</button
					>
				</div>
			{/if}
		</div>
	</Modal>

	<Modal headerText={modalText} visible={isModalVisible} onClose={closeModal} {isSubmitting}>
		<FormGrid on:submit={onSubmit} bind:bindTo={form} {isSubmitting}>
			<div class="col-span-3 w-full overflow-y-auto px-6 py-4 space-y-8">
				<!-- Basic Information Section -->
				<div class="w-full">
					<h3 class="text-base font-medium text-pc-darkblue dark:text-white mb-3">
						Basic Information
					</h3>
					<div class="grid grid-cols-1 md:grid-cols-2 gap-6">
						<div>
							<TextField required minLength={1} maxLength={64} bind:value={formValues.name}
								>Name</TextField
							>
						</div>
						<div>
							<TextFieldSelect
								required
								id="domain"
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
				</div>

				<!-- Delivery Configuration Section -->
				<!-- Email/API Configuration -->
				<div class="w-full">
					<h3 class="text-base font-medium text-pc-darkblue dark:text-white mb-3">
						Email Configuration
					</h3>
					<div class="grid grid-cols-1 md:grid-cols-2 gap-6">
						{#if formValues.templateType === 'Email' || !formValues.templateType}
							<div>
								<TextFieldSelect
									required
									id="smtpConfiguration"
									bind:value={formValues.smtpConfiguration}
									options={smtpConfigurationMap.values()}>SMTP Configuration</TextFieldSelect
								>
							</div>
						{:else if formValues.templateType === 'External API'}
							<div>
								<TextFieldSelect
									required
									id="apiSender"
									bind:value={formValues.apiSender}
									options={apiSenderMap.values()}>API Sender</TextFieldSelect
								>
							</div>
						{/if}
						<div>
							<TextFieldSelect id="email" bind:value={formValues.email} options={emailMap.values()}
								>Email</TextFieldSelect
							>
						</div>
					</div>
				</div>

				<!-- Page Flow Section -->
				<div class="w-full">
					<h3 class="text-base font-medium text-pc-darkblue dark:text-white mb-3">Page Flow</h3>
					<div class="grid grid-cols-1 md:grid-cols-5 gap-6">
						<div class="md:col-span-2 flex flex-col space-y-6">
							<!-- Before Landing Page -->
							<TextFieldSelect
								id="beforeLandingPage"
								bind:value={formValues.beforeLandingPage}
								options={beforeLandingPageMap.values()}
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
									required>Landing</TextFieldSelect
								>
							</ConditionalDisplay>

							<!-- After Landing Page -->
							<TextFieldSelect
								id="afterLandingPage"
								bind:value={formValues.afterLandingPage}
								options={afterLandingPageMap.values()}
								optional>After Landing</TextFieldSelect
							>
						</div>

						<!-- Visualization - Takes 2 columns on larger screens -->
						<div class="md:col-span-2 pl-20 flex justify-center">
							<!-- Dynamic Page Flow Visualization -->
							<div class="flex flex-col space-y-3 w-full justify-center sm:hidden md:flex">
								<!-- Before Landing Page -->
								<div class="flex items-center">
									<div
										class={`w-10 h-10 rounded-lg flex items-center justify-center border mr-3 transition-colors duration-200
                                    ${formValues.beforeLandingPage ? 'bg-blue-50 dark:bg-blue-900/40 border-blue-300 dark:border-blue-500' : 'bg-gray-100 dark:bg-gray-800/60 border-gray-300 dark:border-gray-600'}`}
									>
										<span
											class={`text-xl transition-colors duration-200 ${formValues.beforeLandingPage ? 'text-blue-500 dark:text-blue-300' : 'text-gray-400 dark:text-gray-500'}`}
											>1</span
										>
									</div>
									<div class="flex-1">
										<p
											class="text-xs font-medium text-gray-900 dark:text-gray-300 transition-colors duration-200"
										>
											Before Landing Page
										</p>
										<p
											class="text-xs text-gray-500 dark:text-gray-400 truncate max-w-[180px] transition-colors duration-200"
										>
											{formValues.beforeLandingPage || 'Not selected'}
										</p>
									</div>
								</div>

								<!-- Down Arrow -->
								<div class="flex">
									<div
										class="ml-5 w-0.5 h-4 bg-gray-300 dark:bg-gray-600 transition-colors duration-200"
									></div>
								</div>

								<!-- Main Landing Page -->
								<div class="flex items-center">
									<div
										class="w-10 h-10 rounded-lg bg-blue-100 dark:bg-blue-900/40 flex items-center justify-center border border-blue-400 dark:border-blue-500 mr-3 transition-colors duration-200"
									>
										<span
											class="text-xl text-blue-600 dark:text-blue-300 transition-colors duration-200"
											>2</span
										>
									</div>
									<div class="flex-1">
										<p
											class="text-xs font-medium text-gray-900 dark:text-gray-300 transition-colors duration-200"
										>
											Landing {formValues.landingPageType === 'proxy' ? 'Proxy' : 'Page'}
										</p>
										<p
											class="text-xs text-gray-500 dark:text-gray-400 truncate max-w-[180px] transition-colors duration-200"
										>
											{formValues.landingPage || 'Required'}
										</p>
									</div>
								</div>

								<!-- Down Arrow -->
								<div class="flex">
									<div
										class="ml-5 w-0.5 h-4 bg-gray-300 dark:bg-gray-600 transition-colors duration-200"
									></div>
								</div>

								<!-- After Landing Page -->
								<div class="flex items-center">
									<div
										class={`w-10 h-10 rounded-lg flex items-center justify-center border mr-3 transition-colors duration-200
                                    ${formValues.afterLandingPage ? 'bg-blue-50 dark:bg-blue-900/40 border-blue-300 dark:border-blue-500' : 'bg-gray-100 dark:bg-gray-800/60 border-gray-300 dark:border-gray-600'}`}
									>
										<span
											class={`text-xl transition-colors duration-200 ${formValues.afterLandingPage ? 'text-blue-500 dark:text-blue-300' : 'text-gray-400 dark:text-gray-500'}`}
											>3</span
										>
									</div>
									<div class="flex-1">
										<p
											class="text-xs font-medium text-gray-900 dark:text-gray-300 transition-colors duration-200"
										>
											After Landing Page
										</p>
										<p
											class="text-xs text-gray-500 dark:text-gray-400 truncate max-w-[180px] transition-colors duration-200"
										>
											{formValues.afterLandingPage || 'Not selected'}
										</p>
									</div>
								</div>

								{#if showAdvancedOptions}
									<!-- Down Arrow -->
									<div class="flex">
										<div
											class="ml-5 w-0.5 h-4 bg-gray-300 dark:bg-gray-600 transition-colors duration-200"
										></div>
									</div>

									<!-- Final Redirect -->
									<div class="flex items-center">
										<div
											class={`w-10 h-10 rounded-lg flex items-center justify-center border mr-3 transition-colors duration-200
			                                    ${formValues.afterLandingPageRedirectURL ? 'bg-blue-50 dark:bg-blue-900/40 border-blue-300 dark:border-blue-500' : 'bg-gray-100 dark:bg-gray-800/60 border-gray-300 dark:border-gray-600'}`}
										>
											<span
												class={`text-xl transition-colors duration-200 ${formValues.afterLandingPageRedirectURL ? 'text-blue-500 dark:text-blue-300' : 'text-gray-400 dark:text-gray-500'}`}
												>4</span
											>
										</div>
										<div class="flex-1">
											<p
												class="text-xs font-medium text-gray-900 dark:text-gray-300 transition-colors duration-200"
											>
												POST Redirect URL
											</p>
											<p
												class="text-xs text-gray-500 dark:text-gray-400 truncate max-w-[180px] transition-colors duration-200"
											>
												{formValues.afterLandingPageRedirectURL || 'Not set'}
											</p>
										</div>
									</div>
								{/if}
							</div>
						</div>
					</div>
				</div>

				<FormError message={modalError} />

				<!-- Advanced Options Section -->
				{#if !showAdvancedOptions}
					<div class="mt-6 text-center">
						<button
							type="button"
							class="text-cta-blue hover:text-blue-700 dark:text-white dark:hover:text-gray-200 text-sm transition-colors duration-200 underline"
							on:click={() => (showAdvancedOptions = true)}
						>
							Show advanced options
						</button>
					</div>
				{/if}

				{#if showAdvancedOptions}
					<div class="w-full border-t pt-6 mt-6">
						<h3 class="text-base font-medium text-pc-darkblue dark:text-white mb-3">
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
											{lureCodeKeyspace(
												formValues.lureCodeAlgo,
												Number(formValues.lureCodeLength)
											)}
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
		list={['Scheduled or active campaigns using this template will be closed']}
		name={deleteValues.name}
		onClick={() => onClickDelete(deleteValues.id)}
		confirm
		bind:isVisible={isDeleteAlertVisible}
	></DeleteAlert>
	<DeleteAlert
		title="Delete templates"
		list={['Scheduled or active campaigns using these templates will be closed']}
		name={`${$selection.size} template${$selection.size === 1 ? '' : 's'}`}
		onClick={onClickBulkDelete}
		confirm
		bind:isVisible={isBulkDeleteAlertVisible}
	></DeleteAlert>
</main>
