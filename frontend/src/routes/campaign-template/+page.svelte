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
	import SelectSquare from '$lib/components/SelectSquare.svelte';
	import TableDropDownButton from '$lib/components/table/TableDropDownButton.svelte';
	import CopyCell from '$lib/components/table/CopyCell.svelte';
	import TextFieldSelectWithType from '$lib/components/form/TextFieldSelectWithType.svelte';

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
		urlPath: ''
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
				getCampaignTemplates(),
				refreshIdentifiers()
			]);
			tableURLParams.onChange(refreshCampaignTemplates);
			const editID = $page.url.searchParams.get('edit');
			if (editID) {
				await openUpdateModal(editID);
			}
			hideIsLoading();
		})();
		return () => {
			tableURLParams.unsubscribe();
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
			await getCampaignTemplates();
		} finally {
			isTableLoading = false;
		}
	};
	const getCampaignTemplates = async () => {
		try {
			const result = await getTemplates();
			templates = result.rows;
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
				urlPath: formValues.urlPath || ''
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
		form.reset();
		formValues = {
			id: null,
			templateType: 'Email',
			name: null,
			domain: null,
			landingPage: null,
			landingPageType: 'page',
			beforeLandingPage: null,
			afterLandingPage: null,
			afterLandingPageRedirectURL: '',
			email: null,
			smtpConfiguration: null,
			apiSender: null,
			urlIdentifier: 'id',
			stateIdentifier: 'session',
			urlPath: ''
		};
		modalError = '';
		showAdvancedOptions = false;
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
				return;
			}

			assignTemplate(template);
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
			formValues.templateType = 'API Sender';
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

		// set advanced options visibility based on template configuration
		showAdvancedOptions = !!(
			(
				(template.urlPath && template.urlPath !== '') ||
				(template.afterLandingPageRedirectURL && template.afterLandingPageRedirectURL !== '') ||
				(template.urlIdentifierID && identifierMap.byKey(template.urlIdentifierID) !== 'id') ||
				(template.stateIdentifierID &&
					identifierMap.byKey(template.stateIdentifierID) !== 'session') ||
				template.apiSenderID
			) // Show advanced if using External API
		);
	};
</script>

<HeadTitle title="Campaigns templates" />
<main>
	<Headline>Campaign templates</Headline>
	<BigButton on:click={openCreateModal}>New template</BigButton>

	<Table
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
		plural="templates"
		pagination={tableURLParams}
		isGhost={isTableLoading}
	>
		{#each templates as template}
			<TableRow>
				<TableCell>
					<button
						on:click={() => openUpdateModal(template.id)}
						{...globalButtonDisabledAttributes(template, contextCompanyID)}
						title={template.name}
						class="block w-full py-1 text-left"
					>
						{template.name}
					</button>
				</TableCell>
				<TableCell>
					{#if template.domainID}
						<a href={`/domain/?edit=${template.domainID}`} class="block w-full py-1">
							{domainMap.byKey(template.domainID)}
						</a>
					{/if}
				</TableCell>
				<TableCell>
					{#if template.smtpConfigurationID}
						<a
							href={`/smtp-configuration/?edit=${template.smtpConfigurationID}`}
							class="block w-full py-1"
						>
							{smtpConfigurationMap.byKey(template.smtpConfigurationID)}
						</a>
					{/if}
				</TableCell>
				<TableCell>
					{#if template.apiSenderID}
						<a href={`/api-sender/?edit=${template.apiSenderID}`} class="block w-full py-1">
							{apiSenderMap.byKey(template.apiSenderID)}
						</a>
					{/if}
				</TableCell>
				<TableCell>
					{#if template.emailID}
						<a href={`/email/?edit=${template.emailID}`} class="block w-full py-1">
							{emailMap.byKey(template.emailID)}
						</a>
					{/if}
				</TableCell>
				<TableCell>
					{#if template.beforeLandingPageID}
						<a href={`/page/?edit=${template.beforeLandingPageID}`} class="block w-full py-1">
							{beforeLandingPageMap.byKey(template.beforeLandingPageID)}
						</a>
					{:else if template.beforeLandingProxyID}
						<a href={`/proxy/?edit=${template.beforeLandingProxyID}`} class="block w-full py-1">
							<span class="flex items-center gap-1">
								<ProxySvgIcon size="w-4 h-4" />
								{beforeLandingProxyMap.byKey(template.beforeLandingProxyID)}
							</span>
						</a>
					{/if}
				</TableCell>
				<TableCell>
					{#if template.landingPageID}
						<a href={`/page/?edit=${template.landingPageID}`} class="block w-full py-1">
							{landingPageMap.byKey(template.landingPageID)}
						</a>
					{:else if template.landingProxyID}
						<a href={`/proxy/?edit=${template.landingProxyID}`} class="block w-full py-1">
							<span class="flex items-center gap-1">
								<ProxySvgIcon size="w-4 h-4" />
								{landingProxyMap.byKey(template.landingProxyID)}
							</span>
						</a>
					{/if}
				</TableCell>
				<TableCell>
					{#if template.afterLandingPageID}
						<a href={`/page/?edit=${template.afterLandingPageID}`} class="block w-full py-1">
							{afterLandingPageMap.byKey(template.afterLandingPageID)}
						</a>
					{:else if template.afterLandingProxyID}
						<a href={`/proxy/?edit=${template.afterLandingProxyID}`} class="block w-full py-1">
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
						<TableCopyButton
							title={'Copy'}
							on:click={() => openCopyModal(template.id)}
							{...globalButtonDisabledAttributes(template, contextCompanyID)}
						/>
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
							<TextFieldSelect
								id="email"
								bind:value={formValues.email}
								optional
								options={emailMap.values()}>Email</TextFieldSelect
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

							<!-- Landing Page -->
							<TextFieldSelectWithType
								id="landingPage"
								bind:value={formValues.landingPage}
								bind:type={formValues.landingPageType}
								pageOptions={landingPageMap.values()}
								proxyOptions={landingProxyMap.values()}
								required>Landing</TextFieldSelectWithType
							>

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
								<TextFieldSelect
									id="urlIdentifier"
									toolTipText="This is the query param key used in the phishing URL."
									required
									bind:value={formValues.urlIdentifier}
									options={identifierMap.values()}>Query param key</TextFieldSelect
								>
							</div>
							<div>
								<TextFieldSelect
									id="stateIdentifier"
									toolTipText="This is the query param key used for state."
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
</main>
