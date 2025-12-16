<script>
	import { api } from '$lib/api/apiProxy.js';
	import { onMount, tick } from 'svelte';
	import { goto } from '$app/navigation';
	import { page } from '$app/stores';
	import { newTableURLParams } from '$lib/service/tableURLParams.js';
	import Headline from '$lib/components/Headline.svelte';
	import TextField from '$lib/components/TextField.svelte';
	import TableRow from '$lib/components/table/TableRow.svelte';
	import TableCell from '$lib/components/table/TableCell.svelte';
	import TableCellLink from '$lib/components/table/TableCellLink.svelte';
	import TableUpdateButton from '$lib/components/table/TableUpdateButton.svelte';
	import TableDeleteButton from '$lib/components/table/TableDeleteButton2.svelte';
	import { addToast } from '$lib/store/toast';
	import { AppStateService } from '$lib/service/appState';
	import { nextDay, previousDay, addDays, subDays } from 'date-fns';
	import { getModalText } from '$lib/utils/common';
	import { globalButtonDisabledAttributes } from '$lib/utils/form';
	import {
		fetchAllRows,
		isTimeLarger,
		local_yyyy_mm_dd,
		localTimeToUTC,
		utcTimeToLocal
	} from '$lib/utils/api-utils';
	import { BiMap } from '$lib/utils/maps';
	import TextFieldSelect from '$lib/components/TextFieldSelect.svelte';
	import SelectSquare from '$lib/components/SelectSquare.svelte';
	import DateTimeField from '$lib/components/DateTimeField.svelte';
	import TextFieldMultiSelect from '$lib/components/TextFieldMultiSelect.svelte';
	import FormGrid from '$lib/components/FormGrid.svelte';
	import Modal from '$lib/components/Modal.svelte';
	import TableViewButton from '$lib/components/table/TableViewButton.svelte';
	import TableCellEmpty from '$lib/components/table/TableCellEmpty.svelte';
	import TableCellAction from '$lib/components/table/TableCellAction.svelte';
	import FormColumn from '$lib/components/FormColumn.svelte';
	import FormColumns from '$lib/components/FormColumns.svelte';
	import Table from '$lib/components/table/Table.svelte';
	import DateField from '$lib/components/DateField.svelte';
	import FormError from '$lib/components/FormError.svelte';
	import HeadTitle from '$lib/components/HeadTitle.svelte';
	import TableCopyButton from '$lib/components/table/TableCopyButton.svelte';
	import { showIsLoading, hideIsLoading } from '$lib/store/loading.js';
	import { toEvent } from '$lib/utils/events';
	import TableDropDownEllipsis from '$lib/components/table/TableDropDownEllipsis.svelte';
	import DeleteAlert from '$lib/components/modal/DeleteAlert.svelte';
	import TestLabel from '$lib/components/TestLabel.svelte';
	import BigButton from '$lib/components/BigButton.svelte';
	import ToIcon from '$lib/components/ToIcon.svelte';
	import Datetime from '$lib/components/Datetime.svelte';
	import JitterSlider from '$lib/components/JitterSlider.svelte';
	import RelativeTime from '$lib/components/RelativeTime.svelte';
	import AutoRefresh from '$lib/components/AutoRefresh.svelte';
	import CheckboxField from '$lib/components/CheckboxField.svelte';
	import ConditionalDisplay from '$lib/components/ConditionalDisplay.svelte';

	let currentStep = 1;

	const campaignSteps = [
		{ name: 'Info' },
		{ name: 'Recipients' },
		{ name: 'Schedule' },
		{ name: 'Miscellaneous' },
		{ name: 'Review' }
	];

	const testOptions = [
		{
			label: 'Production',
			value: false,
			icon: ''
		},
		{
			label: 'Test',
			value: true,
			icon: ''
		}
	];

	const saveSubbmitedDataOptions = [
		{
			label: 'Yes',
			value: true,
			icon: ''
		},
		{
			label: 'No',
			value: false,
			icon: ''
		}
	];

	const ipFilterOptions = [
		{
			label: 'None',
			value: 'none',
			icon: ''
		},
		{
			label: 'Allow',
			value: 'allow',
			icon: ''
		},

		{
			label: 'Deny',
			value: 'deny',
			icon: ''
		}
	];

	const scheduleOptions = [
		{
			label: 'Time Box',
			value: 'basic',
			icon: '🕒',
			description: 'Send within defined time box'
		},
		{
			label: 'Daily Slots',
			value: 'schedule',
			icon: '📅',
			description: 'Schedule specific times each day'
		},
		{
			label: 'Self Managed',
			value: 'self-managed',
			icon: '🔧',
			description: 'Handle delivery manually'
		}
	];

	// webhook data level descriptions:
	// - none: only send event type and timestamp - no campaign info, emails, or data
	// - basic: send campaign name and event, but exclude emails and submission data
	// - full: include all information: emails, captured credentials, and submission data
	const webhookDataLevelOptions = [
		{
			label: 'Minimum',
			value: 'none'
		},
		{
			label: 'Basic',
			value: 'basic'
		},
		{
			label: 'Full',
			value: 'full'
		}
	];

	// webhook event options - 0 means all events will trigger webhook (default)
	const webhookEventOptions = [
		'campaign_recipient_message_sent',
		'campaign_recipient_message_failed',
		'campaign_recipient_message_read',
		'campaign_recipient_submitted_data',
		'campaign_recipient_page_visited',
		'campaign_recipient_before_page_visited',
		'campaign_recipient_after_page_visited',
		'campaign_recipient_evasion_page_visited',
		'campaign_recipient_deny_page_visited',
		'campaign_closed'
	];

	// human-readable display names for webhook events
	const webhookEventDisplayNames = {
		campaign_closed: 'Campaign Closed',
		campaign_recipient_message_sent: 'Message Sent',
		campaign_recipient_message_failed: 'Message Failed',
		campaign_recipient_message_read: 'Message Read',
		campaign_recipient_submitted_data: 'Submitted Data',
		campaign_recipient_evasion_page_visited: 'Evasion Page Visited',
		campaign_recipient_before_page_visited: 'Before Page Visited',
		campaign_recipient_page_visited: 'Page Visited',
		campaign_recipient_after_page_visited: 'After Page Visited',
		campaign_recipient_deny_page_visited: 'Deny Page Visited'
	};

	// create display options array with nice names
	const webhookEventDisplayOptions = webhookEventOptions.map((event) => ({
		value: event,
		label: webhookEventDisplayNames[event] || event
	}));

	// map event names to bit positions (must match backend data.WebhookEventToBit)
	const webhookEventToBit = {
		campaign_closed: 1 << 0, // 1
		campaign_recipient_message_sent: 1 << 1, // 2
		campaign_recipient_message_failed: 1 << 2, // 4
		campaign_recipient_message_read: 1 << 3, // 8
		campaign_recipient_submitted_data: 1 << 4, // 16
		campaign_recipient_evasion_page_visited: 1 << 5, // 32
		campaign_recipient_before_page_visited: 1 << 6, // 64
		campaign_recipient_page_visited: 1 << 7, // 128
		campaign_recipient_after_page_visited: 1 << 8, // 256
		campaign_recipient_deny_page_visited: 1 << 9 // 512
	};

	const WEBHOOK_EVENT_ALL_BITS = 1023; // 2^10 - 1, all 10 events selected

	// convert array of event names to bitwise int
	const webhookEventsToBinary = (events) => {
		if (!events?.length) return 0; // 0 means all events
		return events.reduce((binary, event) => binary | (webhookEventToBit[event] || 0), 0);
	};

	// convert bitwise int to array of event names (internal values, not display names)
	const webhookEventsFromBinary = (binary) => {
		if (binary === 0) return [...webhookEventOptions]; // 0 means all events, return all
		return webhookEventOptions.filter((event) => binary & webhookEventToBit[event]);
	};

	// helper to get display name for an event
	const getEventDisplayName = (eventValue) => {
		return webhookEventDisplayNames[eventValue] || eventValue;
	};

	let deleteValues = {
		id: null,
		name: null
	};

	let speedIndex = 0; // This will correspond to SPREAD_MANUAL initially

	const appStateService = AppStateService.instance;
	let contextCompanyID = null;
	let campaigns = [];
	let campaignsHasNextPage = false;
	let templateMap = new BiMap({});
	let recipientGroupsByID = {};
	let recipientGroupMap = new BiMap({});
	let recipientGroupRecipients = {}; // stores actual recipients for each group
	let isRecipientModalVisible = false;
	let denyPages = [];
	let denyPageMap = new BiMap({});
	let allowDenyMap = new BiMap({});
	let webhookMap = new BiMap({});
	let modalMode = null;
	let scheduleType = 'basic';
	let allowDenyType = 'none';
	let allAllowDeny = [];
	let showSecurityOptions = false;
	let showAdvancedOptionsStep3 = false;
	let showAdvancedOptionsStep4 = false;

	// reactive statement to enable security options when deny page is set
	$: if (formValues.denyPageValue && formValues.denyPageValue.trim() !== '') {
		showSecurityOptions = true;
	}

	// reactive statement to clear evasion page and IP filtering when deny page is cleared
	$: if (!formValues.denyPageValue) {
		if (formValues.evasionPageValue) {
			formValues.evasionPageValue = null;
		}
		if (allowDenyType !== 'none') {
			allowDenyType = 'none';
			formValues.allowDeny = [];
		}
	}

	const defaultSendField = 'Email';
	const defaultSendOrder = 'Random';
	const sortField = new BiMap({
		Email: 'email',
		Name: 'name',
		Phone: 'phone',
		Position: 'position',
		Department: 'department',
		City: 'city',
		Country: 'country',
		Misc: 'misc',
		'Extra ID': 'extraID'
	});

	const sortOrder = new BiMap({
		'A to Z (ascending)': 'asc',
		'Z to A (descending)': 'desc',
		Random: 'random'
	});

	const dayMap = {
		0: 'Sunday',
		1: 'Monday',
		2: 'Tuesday',
		3: 'Wednesday',
		4: 'Thursday',
		5: 'Friday',
		6: 'Saturday'
	};

	const SPREAD_MANUAL = 'manual';
	const SPREAD_IMMEDIATE = 'immediate';
	const SPREAD_1MIN = '1min';
	const SPREAD_2MIN = '2min';
	const SPREAD_5MIN = '5min';
	const SPREAD_10MIN = '10min';
	const SPREAD_20MIN = '20min';
	const SPREAD_30MIN = '30min';
	const SPREAD_1HOUR = '1hour';
	const SPREAD_2HOUR = '2hour';
	const SPREAD_5HOUR = '5hour';
	const SPREAD_12HOUR = '12hour';
	const SPREAD_24HOUR = '24hour';

	const spreadOptionMap = new BiMap({
		Manual: SPREAD_MANUAL,
		'1 minute': SPREAD_1MIN,
		'2 minutes': SPREAD_2MIN,
		'5 minutes': SPREAD_5MIN,
		'10 minutes': SPREAD_10MIN,
		'20 minutes': SPREAD_20MIN,
		'30 minutes': SPREAD_30MIN,
		'1 hour': SPREAD_1HOUR,
		'2 hours': SPREAD_2HOUR,
		'5 hours': SPREAD_5HOUR,
		'12 hours': SPREAD_12HOUR,
		'24 hours': SPREAD_24HOUR
	});

	let spreadOption = SPREAD_MANUAL;

	const getSpreadMilliseconds = (spreadOption) => {
		switch (spreadOption) {
			case SPREAD_IMMEDIATE:
				return 0;
			case SPREAD_1MIN:
				return 60000;
			case SPREAD_2MIN:
				return 120000;
			case SPREAD_5MIN:
				return 300000;
			case SPREAD_10MIN:
				return 600000;
			case SPREAD_20MIN:
				return 1200000;
			case SPREAD_30MIN:
				return 1800000;
			case SPREAD_1HOUR:
				return 3600000;
			case SPREAD_2HOUR:
				return 7200000;
			case SPREAD_5HOUR:
				return 18000000;
			case SPREAD_12HOUR:
				return 43200000;
			case SPREAD_24HOUR:
				return 86400000;
			default:
				return null;
		}
	};

	let form = null;
	let formValues = {
		name: null,
		sendStartAt: null,
		sendEndAt: null,
		scheduledStartAt: null,
		scheduledEndAt: null,
		closeAt: null,
		anonymizeAt: null,
		template: null,
		sortField: null,
		sortOrder: null,
		recipientGroups: [],
		allowDeny: [],
		denyPageValue: null,
		evasionPageValue: null,
		constraintWeekDays: [],
		contraintStartTime: null,
		contraintEndTime: null,
		saveSubmittedData: true,
		saveBrowserMetadata: false,
		isAnonymous: null,
		isTest: false,
		obfuscate: false,
		selectedCount: 0,
		webhookValue: null,
		webhookIncludeData: 'full',
		webhookEvents: [...webhookEventOptions], // all events selected by default
		jitterMin: 0,
		jitterMax: 0
	};

	let modalError = '';
	let isModalVisible = false;
	let isSubmitting = false;
	let isTableLoading = false;
	let modalText = '';
	let isValidatingName = false;
	let weekDaysAvailable = [];
	let isDeleteAlertVisible = false;

	$: {
		modalText = getModalText('campaign', modalMode);
	}

	const getTemplateDetails = async (templateName) => {
		if (!templateName) return null;

		try {
			const templateID = templateMap.byValue(templateName);
			if (!templateID) return null;

			// fetch template with full details including email and api sender
			const templateRes = await api.campaignTemplate.getByID(templateID, true);
			if (!templateRes.success) {
				console.error('Failed to fetch template details:', templateRes.error);
				return null;
			}

			return templateRes.data;
		} catch (error) {
			console.error('Error fetching template details:', error);
			return null;
		}
	};

	const tableURLParams = newTableURLParams();

	onMount(() => {
		const context = appStateService.getContext();
		if (context) {
			contextCompanyID = context.companyID;
		}

		(async () => {
			showIsLoading();
			isTableLoading = true;
			try {
				await refreshCampaigns();
				tableURLParams.onChange(refreshCampaigns);

				// Check if we should auto-open the update modal
				const updateId = $page.url.searchParams.get('update');
				if (updateId) {
					// Clear the URL parameter
					const url = new URL($page.url);
					url.searchParams.delete('update');
					goto(url.pathname + url.search, { replaceState: true });

					// Open the update modal
					try {
						await openUpdateModal(updateId);
					} catch (e) {
						addToast('Failed to open campaign for editing', 'Error');
						console.error('Failed to open update modal:', e);
					}
				}
			} catch (e) {
				addToast('Failed to load data', 'Error');
				console.error('failed to load data', e);
			} finally {
				hideIsLoading();
				isTableLoading = false;
			}
		})();
		return () => {
			tableURLParams.unsubscribe();
		};
	});

	const nextStep = async () => {
		if (await validateCurrentStep()) {
			currentStep = Math.min(currentStep + 1, campaignSteps.length);
			modalError = '';
			// reset tab focus after dom update - only for explicit step navigation
			await tick();
			// focus first element in current step
			setTimeout(() => {
				const currentStepContainer = document.querySelector(`#step-${currentStep}`);
				if (currentStepContainer) {
					const firstFocusable = currentStepContainer.querySelector(
						'button:not([disabled]), input:not([disabled]), select:not([disabled]), textarea:not([disabled]), [tabindex]:not([tabindex="-1"])'
					);
					if (firstFocusable && firstFocusable instanceof HTMLElement) {
						firstFocusable.focus();
					}
				}
			}, 0);
		}
	};

	const previousStep = async () => {
		currentStep = Math.max(currentStep - 1, 1);
		// reset tab focus after dom update - only for explicit step navigation
		await tick();
		// focus first element in current step
		setTimeout(() => {
			const currentStepContainer = document.querySelector(`#step-${currentStep}`);
			if (currentStepContainer) {
				const firstFocusable = currentStepContainer.querySelector(
					'button:not([disabled]), input:not([disabled]), select:not([disabled]), textarea:not([disabled]), [tabindex]:not([tabindex="-1"])'
				);
				if (firstFocusable && firstFocusable instanceof HTMLElement) {
					firstFocusable.focus();
				}
			}
		}, 0);
	};

	const validateCurrentStep = async () => {
		switch (currentStep) {
			case 1:
				return await validateBasicInfo();
			case 2:
				return validateRecipients();
			case 3:
				return validateSchedule();
			case 4:
				return validateMisc();
			default:
				return true;
		}
	};

	const checkCurrentStepValidity = () => {
		/** @type {NodeListOf<HTMLInputElement>} */
		const currentStepElements = document.querySelectorAll(
			`[id="step-${currentStep}"] input:not([type="hidden"]), [id="step-${currentStep}"] select`
		);
		for (let i = 0; i < currentStepElements.length; i++) {
			const element = currentStepElements[i];
			if (!element.checkValidity()) {
				element.reportValidity();
				return false;
			}
		}
		return true;
	};

	const validateBasicInfo = async () => {
		if (!checkCurrentStepValidity()) {
			return false;
		}

		// check if campaign name exists
		if (formValues.name?.length) {
			isValidatingName = true;
			try {
				const nameExists = await campaignNameExists(formValues.name);
				if (nameExists) {
					/** @type {HTMLInputElement} */
					const ele = document.querySelector('#campaignName');
					if (ele) {
						ele.setCustomValidity('Name is used by another campaign');
						ele.reportValidity();
					}
					return false;
				}
			} finally {
				isValidatingName = false;
			}
		}

		return true;
	};

	const validateRecipients = () => {
		return checkCurrentStepValidity();
	};

	const validateSchedule = () => {
		// select and clear all previous messages
		/** @type {NodeListOf<HTMLInputElement>} */
		const currentStepElements = document.querySelectorAll(
			`[id="step-${currentStep}"] input:not([type="hidden"]), [id="step-${currentStep}"] select`
		);
		for (let i = 0; i < currentStepElements.length; i++) {
			const element = currentStepElements[i];
			element.setCustomValidity('');
		}
		return checkCurrentStepValidity();
	};

	const validateMisc = () => {
		// validate that deny page is selected if evasion page or IP filtering is used
		if (formValues.evasionPageValue && !formValues.denyPageValue) {
			modalError = 'Deny page is required when using an evasion page';
			return false;
		}
		if (allowDenyType !== 'none' && !formValues.denyPageValue) {
			modalError = 'Deny page is required when using IP filtering';
			return false;
		}
		return checkCurrentStepValidity();
	};

	const refreshCampaignDependencyData = async () => {
		const templates = await fetchAllRows((options) => {
			return api.campaignTemplate.getAll(options, contextCompanyID, true);
		});
		templateMap = BiMap.FromArrayOfObjects(templates);

		let recipientGroups = await fetchAllRows((options) => {
			return api.recipient.getAllGroups(options, contextCompanyID);
		});
		recipientGroups = recipientGroups
			.filter((group) => group.recipientCount)
			.map((group) => {
				group.name = group.name + ` (${group.recipientCount})`;
				return group;
			});
		recipientGroupsByID = recipientGroups.reduce((acc, group) => {
			acc[group.id] = group;
			return acc;
		}, {});
		recipientGroupMap = BiMap.FromArrayOfObjects(recipientGroups);

		// All features now available - no edition restrictions
		allAllowDeny = await fetchAllRows((options) => {
			return api.allowDeny.getAllOverview(options, contextCompanyID);
		});

		denyPages = await fetchAllRows((options) => {
			return api.page.getAll(options, contextCompanyID);
		});
		denyPageMap = BiMap.FromArrayOfObjects(denyPages);
		setAllowDenyType(allAllowDeny);

		const webhooks = await fetchAllRows((options) => {
			return api.webhook.getAll(options, contextCompanyID);
		});
		webhookMap = BiMap.FromArrayOfObjects(webhooks);
	};

	const setScheduledAt = () => {
		if (
			formValues.scheduledStartAt &&
			formValues.scheduledEndAt &&
			formValues.constraintWeekDays.length > 0
		) {
			const startDateTime = new Date(formValues.scheduledStartAt);
			const endDateTime = new Date(formValues.scheduledEndAt);
			const startWeekday = startDateTime.getDay();
			const firstWeekDay =
				formValues.constraintWeekDays.find((d) => d >= startWeekday) ??
				formValues.constraintWeekDays[0];
			const firstDay = nextDay(subDays(startDateTime, 1), firstWeekDay);
			const endWeekday = endDateTime.getDay();
			const reverseContaintWeekDays = [...formValues.constraintWeekDays].reverse();
			const lastWeekDay =
				reverseContaintWeekDays.find((d) => d <= endWeekday) ?? reverseContaintWeekDays[0];
			const endDay = previousDay(addDays(endDateTime, 1), lastWeekDay);
			formValues.sendStartAt = firstDay.toString();
			formValues.sendEndAt = endDay.toString();
		}
	};

	const setAllowDenyType = (allowDenyEntries) => {
		let filteredEntries = [];
		switch (allowDenyType) {
			case 'allow':
				filteredEntries = allowDenyEntries.filter((entry) => entry.allowed);
				break;
			case 'deny':
				filteredEntries = allowDenyEntries.filter((entry) => !entry.allowed);
				break;
			case 'none':
				filteredEntries = allowDenyEntries;
				break;
		}
		allowDenyMap = BiMap.FromArrayOfObjects(filteredEntries);
	};

	const refreshCampaigns = async (useTableLoading = true) => {
		try {
			if (useTableLoading) {
				isTableLoading = true;
			}
			campaigns = await getCampaigns();
			await refreshCampaignDependencyData();
		} catch (e) {
			addToast('Failed to load campaigns', 'Error');
			console.error('Failed to load campaigns', e);
		} finally {
			if (useTableLoading) {
				isTableLoading = false;
			}
		}
	};

	const getCampaign = async (id) => {
		try {
			showIsLoading();
			const res = await api.campaign.getByID(id);
			if (!res.success) {
				throw res.error;
			}
			return res.data;
		} catch (e) {
			addToast('Failed to load campaign', 'Error');
			console.error('failed to load campaign', e);
		} finally {
			hideIsLoading();
		}
	};

	const getCampaigns = async () => {
		try {
			const res = await api.campaign.getAll(tableURLParams, contextCompanyID);
			if (!res.success) {
				throw res.error;
			}
			campaignsHasNextPage = res.data.hasNextPage;
			return res.data.rows;
		} catch (e) {
			addToast('Failed to load campaigns', 'Error');
			console.error('Failed to load campaigns', e);
		}
	};

	const save = async () => {
		setScheduledAt();
		const recipientGroupIDs = formValues.recipientGroups.map((name) =>
			recipientGroupMap.byValue(name)
		);
		const allowDenyIDs = formValues.allowDeny.map((name) => allowDenyMap.byValue(name));

		try {
			const sendStartAtUTC = formValues.sendStartAt
				? new Date(formValues.sendStartAt).toISOString()
				: null;
			const sendEndAtUTC = formValues.sendEndAt
				? new Date(formValues.sendEndAt).toISOString()
				: null;
			const closeAtUTC = formValues.closeAt ? new Date(formValues.closeAt).toISOString() : null;
			const anonymizeAtUTC = formValues.anonymizeAt
				? new Date(formValues.anonymizeAt).toISOString()
				: null;
			const contraintStartTimeUTC = formValues.contraintStartTime
				? localTimeToUTC(formValues.contraintStartTime)
				: null;
			const contraintEndTimeUTC = formValues.contraintEndTime
				? localTimeToUTC(formValues.contraintEndTime)
				: null;

			const res = await api.campaign.create({
				name: formValues.name,
				companyID: contextCompanyID,
				templateID: templateMap.byValue(formValues.template),
				sendStartAt: sendStartAtUTC,
				sendEndAt: sendEndAtUTC,
				sortField: sortField.byKey(formValues.sortField),
				sortOrder: sortOrder.byKey(formValues.sortOrder),
				closeAt: closeAtUTC,
				anonymizeAt: anonymizeAtUTC,
				saveSubmittedData: formValues.saveSubmittedData,
				saveBrowserMetadata: formValues.saveBrowserMetadata,
				isAnonymous: formValues.isAnonymous,
				isTest: formValues.isTest,
				obfuscate: formValues.obfuscate,
				recipientGroupIDs: recipientGroupIDs,
				allowDenyIDs: allowDenyIDs,
				denyPageID: denyPageMap.byValueOrNull(formValues.denyPageValue),
				evasionPageID: denyPageMap.byValueOrNull(formValues.evasionPageValue),
				constraintWeekDays: weekDaysAvailableToBinary(formValues.constraintWeekDays),
				constraintStartTime: contraintStartTimeUTC,
				constraintEndTime: contraintEndTimeUTC,
				webhookID: webhookMap.byValueOrNull(formValues.webhookValue),
				webhookIncludeData: formValues.webhookIncludeData,
				webhookEvents: webhookEventsToBinary(formValues.webhookEvents),
				jitterMin: formValues.jitterMin !== 0 ? formValues.jitterMin : null,
				jitterMax: formValues.jitterMax !== 0 ? formValues.jitterMax : null
			});

			if (!res.success) {
				modalError = res.error;
				return;
			}
			addToast('Campaign created', 'Success');
			closeModal();
			refreshCampaigns();
		} catch (err) {
			addToast('Failed to create campaign', 'Error');
			console.error('failed to create campaign:', err);
		}
	};

	const update = async () => {
		setScheduledAt();
		const recipientGroupIDs = formValues.recipientGroups.map((name) =>
			recipientGroupMap.byValue(name)
		);
		const allowDenyIDs = formValues.allowDeny.map((name) => allowDenyMap.byValue(name));

		try {
			const sendStartAtUTC = formValues.sendStartAt
				? new Date(formValues.sendStartAt).toISOString()
				: null;
			const sendEndAtUTC = formValues.sendEndAt
				? new Date(formValues.sendEndAt).toISOString()
				: null;
			const closeAtUTC = formValues.closeAt ? new Date(formValues.closeAt).toISOString() : null;
			const anonymizeAtUTC = formValues.anonymizeAt
				? new Date(formValues.anonymizeAt).toISOString()
				: null;
			const contraintStartTimeUTC = formValues.contraintStartTime
				? localTimeToUTC(formValues.contraintStartTime)
				: null;
			const contraintEndTimeUTC = formValues.contraintEndTime
				? localTimeToUTC(formValues.contraintEndTime)
				: null;

			const res = await api.campaign.update({
				id: formValues.id,
				name: formValues.name,
				templateID: templateMap.byValue(formValues.template),
				sortField: sortField.byKey(formValues.sortField),
				sortOrder: sortOrder.byKey(formValues.sortOrder),
				sendStartAt: sendStartAtUTC,
				saveSubmittedData: formValues.saveSubmittedData,
				saveBrowserMetadata: formValues.saveBrowserMetadata,
				isAnonymous: formValues.isAnonymous,
				isTest: formValues.isTest,
				obfuscate: formValues.obfuscate,
				constraintWeekDays: weekDaysAvailableToBinary(formValues.constraintWeekDays),
				constraintStartTime: contraintStartTimeUTC,
				constraintEndTime: contraintEndTimeUTC,
				sendEndAt: sendEndAtUTC,
				closeAt: closeAtUTC,
				anonymizeAt: anonymizeAtUTC,
				recipientGroupIDs: recipientGroupIDs,
				allowDenyIDs: allowDenyIDs,
				denyPageID: denyPageMap.byValueOrNull(formValues.denyPageValue),
				evasionPageID: denyPageMap.byValueOrNull(formValues.evasionPageValue),
				webhookID: webhookMap.byValueOrNull(formValues.webhookValue),
				webhookIncludeData: formValues.webhookIncludeData,
				webhookEvents: webhookEventsToBinary(formValues.webhookEvents),
				jitterMin: formValues.jitterMin !== 0 ? formValues.jitterMin : null,
				jitterMax: formValues.jitterMax !== 0 ? formValues.jitterMax : null
			});

			if (!res.success) {
				modalError = res.error;
				return;
			}
			addToast('Campaign updated', 'Success');
			closeModal();
			refreshCampaigns();
		} catch (e) {
			addToast('Failed to update campaign', 'Error');
			console.error('failed to update campaign', e);
		}
	};

	const onSubmit = async () => {
		if (!validateAllSteps()) {
			addToast('Please review all sections before submitting', 'Error');
			return;
		}

		try {
			isSubmitting = true;
			if (modalMode === 'create' || modalMode === 'copy') {
				await save();
			} else if (modalMode === 'update') {
				await update();
			} else {
				throw new Error('Invalid modal mode', modalMode);
			}
		} finally {
			isSubmitting = false;
		}
	};

	const validateAllSteps = () => {
		return validateBasicInfo() && validateRecipients() && validateSchedule() && validateMisc();
	};

	const onClickViewCampaign = (id) => {
		goto(`/campaign/${id}`);
	};

	const openDeleteAlert = (campaign) => {
		isDeleteAlertVisible = true;
		deleteValues.id = campaign.id;
		deleteValues.name = campaign.name;
	};

	const onClickDelete = async (id) => {
		const action = api.campaign.delete(id);
		console.log(action);
		action
			.then((res) => {
				if (res.success) {
					refreshCampaigns();
					return;
				}
				throw res.error;
			})
			.catch((e) => {
				console.error('failed to delete campaign:', e);
			});
		return action;
	};

	/** @param {string} name */
	const campaignNameExists = async (name) => {
		if (!name?.length) return false;

		try {
			const res = await api.campaign.getByName(name, contextCompanyID);
			if (
				res.data &&
				(modalMode === 'create' || modalMode === 'copy' || res.data.id !== formValues.id)
			) {
				return true;
			}
			return false;
		} catch (e) {
			addToast('Failed to check if campaign name is used', 'Error');
			console.error('Failed to check if campaign name is used', e);
			return false;
		}
	};

	const openCreateModal = async () => {
		try {
			showIsLoading();
			modalMode = 'create';
			currentStep = 1;
			await refreshCampaignDependencyData();
			resetFormValues();
			isModalVisible = true;
		} finally {
			hideIsLoading();
		}
	};

	const resetFormValues = () => {
		formValues = {
			name: '',
			sendStartAt: null,
			sendEndAt: null,
			scheduledStartAt: null,
			scheduledEndAt: null,
			closeAt: null,
			anonymizeAt: null,
			template: null,
			sortField: defaultSendField,
			sortOrder: defaultSendOrder,
			recipientGroups: [],
			allowDeny: [],
			denyPageValue: null,
			evasionPageValue: null,
			constraintWeekDays: [],
			contraintStartTime: null,
			contraintEndTime: null,
			saveSubmittedData: true,
			saveBrowserMetadata: false,
			isAnonymous: null,
			isTest: false,
			obfuscate: false,
			selectedCount: 0,
			webhookValue: null,
			webhookIncludeData: 'full',
			webhookEvents: [...webhookEventOptions], // all events selected by default
			jitterMin: 0,
			jitterMax: 0
		};
		scheduleType = 'basic';
		allowDenyType = 'none';
		spreadOption = SPREAD_MANUAL;
		modalError = '';
		showAdvancedOptionsStep3 = false;
		showAdvancedOptionsStep4 = false;
	};

	const onChangeScheduleType = () => {
		formValues.scheduledStartAt = null;
		formValues.scheduledEndAt = null;
		formValues.constraintWeekDays = [];
		formValues.contraintStartTime = null;
		formValues.contraintEndTime = null;
		formValues.sendStartAt = null;
		formValues.sendEndAt = null;
	};

	const onChangeAllowDenyType = () => {
		formValues.allowDeny = [];
		setAllowDenyType(allAllowDeny);
	};

	const closeModal = () => {
		modalMode = null;
		isModalVisible = false;
		currentStep = 1;
		isValidatingName = false;
		if (form) form.reset();
		resetFormValues();
	};

	const openUpdateModal = async (id) => {
		modalMode = 'update';
		currentStep = 1;
		try {
			showIsLoading();
			const campaign = await getCampaign(id);
			const jit = campaignUpdateDisabledAndTitle(campaign);
			if (jit.disabled) {
				addToast('Campaign can not be edited', 'Info');
				refreshCampaigns();
				return;
			}
			await refreshCampaignDependencyData();
			assignCampaign(campaign);
			isModalVisible = true;
		} catch (e) {
			addToast('Failed to load campaign', 'Error');
			console.error('failed to load campaign', e);
		} finally {
			hideIsLoading();
		}
	};

	const openCopyModal = async (id) => {
		modalMode = 'copy';
		currentStep = 1;
		try {
			showIsLoading();
			const campaign = await getCampaign(id);
			await refreshCampaignDependencyData();
			assignCampaign(campaign, true);
			isModalVisible = true;
		} finally {
			hideIsLoading();
		}
	};

	const assignCampaign = (campaign, copyMode = false) => {
		if (campaign.constraintWeekDays) {
			scheduleType = 'schedule';
		}

		formValues = {
			...formValues,
			id: campaign.id,
			name: copyMode ? `${campaign.name} (Copy)` : campaign.name,
			sortField: sortField.byValue(campaign.sortField),
			sortOrder: sortOrder.byValue(campaign.sortOrder),
			sendStartAt: copyMode ? null : campaign.sendStartAt,
			sendEndAt: copyMode ? null : campaign.sendEndAt,
			scheduledStartAt: copyMode
				? null
				: campaign.sendStartAt
					? local_yyyy_mm_dd(new Date(campaign.sendStartAt))
					: null,
			scheduledEndAt: copyMode
				? null
				: campaign.sendEndAt
					? local_yyyy_mm_dd(new Date(campaign.sendEndAt))
					: null,
			constraintWeekDays: copyMode ? [] : weekDayBinaryToAvailable(campaign.constraintWeekDays),
			contraintStartTime: copyMode ? null : utcTimeToLocal(campaign.constraintStartTime),
			contraintEndTime: copyMode ? null : utcTimeToLocal(campaign.constraintEndTime),
			closeAt: copyMode ? null : campaign.closeAt,
			anonymizeAt: copyMode ? null : campaign.anonymizeAt,
			saveSubmittedData: campaign.saveSubmittedData,
			saveBrowserMetadata: campaign.saveBrowserMetadata ?? false,
			isAnonymous: campaign.isAnonymous,
			isTest: campaign.isTest,
			obfuscate: campaign.obfuscate || false,
			template: templateMap.byKey(campaign.templateID),
			webhookValue: webhookMap.byKey(campaign.webhookID),
			webhookIncludeData: campaign.webhookIncludeData || 'full',
			webhookEvents: webhookEventsFromBinary(campaign.webhookEvents || 0)
		};

		if (copyMode) {
			// reset recipient groups when copying
			formValues.recipientGroups = [];
			formValues.selectedCount = 0;
		} else {
			formValues.recipientGroups = campaign.recipientGroupIDs.map((id) =>
				recipientGroupMap.byKey(id)
			);
			formValues.selectedCount = formValues.recipientGroups.reduce((acc, label) => {
				const id = recipientGroupMap.byValue(label);
				const group = recipientGroupsByID[id];
				return acc + group.recipientCount;
			}, 0);
		}

		if (!formValues.sendStartAt && !formValues.sendEndAt) {
			scheduleType = 'self-managed';
		}

		// reset schedule type to basic when copying since delivery times are cleared
		if (copyMode) {
			scheduleType = 'basic';
		}

		if (campaign.allowDeny.length > 0) {
			allowDenyType = campaign.allowDeny[0].allowed ? 'allow' : 'deny';
		} else {
			allowDenyType = 'none';
		}
		setAllowDenyType(allAllowDeny);
		formValues.allowDeny = campaign.allowDeny.map((allowDeny) => allowDenyMap.byKey(allowDeny.id));

		if (campaign.denyPage) {
			formValues.denyPageValue = campaign.denyPage.name;
		}

		// set advanced options visibility based on campaign configuration
		showAdvancedOptionsStep3 = !!(campaign.closeAt || campaign.anonymizeAt);

		showAdvancedOptionsStep4 = !!(
			campaign.webhookID ||
			campaign.denyPage ||
			campaign.evasionPage ||
			campaign.allowDeny?.length ||
			campaign.obfuscate
		);

		if (campaign.evasionPage) {
			formValues.evasionPageValue = campaign.evasionPage.name;
		}
	};

	/*
	const onClickSendImmediately = () => {
		formValues.sendStartAt = new Date().toISOString();
		formValues.sendEndAt = new Date().toISOString();
		spreadOption = SPREAD_IMMEDIATE;
	};
	 */

	const onAddReceipientGroup = async (group) => {
		const groupLabel = recipientGroupMap.byValue(group);
		const groupData = recipientGroupsByID[groupLabel];
		formValues.selectedCount += groupData.recipientCount;
		refreshEndTimeBySendSpread();
		// load recipients for preview
		await loadRecipientsForGroup(groupLabel);
	};

	const onRemoveReceipientGroup = (group) => {
		const groupLabel = recipientGroupMap.byValue(group);
		const groupData = recipientGroupsByID[groupLabel];
		formValues.selectedCount -= groupData.recipientCount;
		refreshEndTimeBySendSpread();
		// remove recipients from cache (groupLabel is actually the ID)
		delete recipientGroupRecipients[groupLabel];
	};

	const loadRecipientsForGroup = async (groupID) => {
		console.log('loadRecipientsForGroup called with groupID:', groupID);

		// skip if already loaded
		if (recipientGroupRecipients[groupID]) {
			console.log('recipients already loaded for group:', groupID);
			return;
		}

		try {
			console.log('fetching recipients for group:', groupID);
			const res = await api.recipient.getAllByGroupID(groupID, { perPage: 1000 });
			console.log('api response:', res);

			if (res.success && res.data?.rows) {
				console.log('successfully loaded', res.data.rows.length, 'recipients for group:', groupID);
				recipientGroupRecipients[groupID] = res.data.rows;
				// trigger reactivity
				recipientGroupRecipients = recipientGroupRecipients;
			} else {
				console.warn('api call succeeded but no data returned for group:', groupID);
				recipientGroupRecipients[groupID] = [];
			}
		} catch (error) {
			console.error('failed to load recipients for group:', groupID, error);
			recipientGroupRecipients[groupID] = [];
		}
	};

	const loadAllSelectedRecipients = async () => {
		console.log('loadAllSelectedRecipients called for groups:', formValues.recipientGroups);

		// load recipients for all selected groups
		const promises = formValues.recipientGroups.map((groupName) => {
			const groupID = recipientGroupMap.byValue(groupName);
			console.log('mapping groupName to groupID:', groupName, '=>', groupID);
			return loadRecipientsForGroup(groupID);
		});
		await Promise.all(promises);

		console.log('all recipients loaded:', recipientGroupRecipients);
	};

	const refreshEndTimeBySendSpread = (milliseconds) => {
		if (formValues.selectedCount === 0 || !formValues.sendStartAt) return;

		const startDate = new Date(formValues.sendStartAt);
		if (milliseconds === 0) {
			formValues.sendEndAt = formValues.sendStartAt;
		} else {
			formValues.sendEndAt = new Date(
				startDate.getTime() + (formValues.selectedCount - 1) * milliseconds
			).toISOString();
		}
	};

	const weekDaysAvailableBetween = (start, end) => {
		if (!start || !end) return [];

		const startDate = new Date(start);
		const endDate = new Date(end);
		const daysInRange = new Set();

		for (let date = new Date(startDate); date <= endDate; date.setDate(date.getDate() + 1)) {
			daysInRange.add(date.getDay());
		}

		return Array.from(daysInRange).sort((a, b) => a - b);
	};

	const weekDaysAvailableToBinary = (weekDays) => {
		if (!weekDays?.length) return null;
		return weekDays.reduce((binary, day) => binary | (1 << day), 0);
	};

	const weekDayBinaryToAvailable = (binary) => {
		if (!binary) return [];
		return Array.from({ length: 7 }, (_, i) => i).filter((day) => binary & (1 << day));
	};

	const validateConstraintTimes = (element) => {
		if (!formValues.contraintStartTime || !formValues.contraintEndTime) return;

		if (isTimeLarger(formValues.contraintStartTime, formValues.contraintEndTime)) {
			element.setCustomValidity('Start time must be before end time');
		} else {
			element.setCustomValidity('');
		}
		element.reportValidity();
	};

	const campaignUpdateDisabledAndTitle = (campaign) => {
		const c = globalButtonDisabledAttributes(campaign, contextCompanyID);
		if (c?.disabled) {
			return c;
		}

		const now = new Date();
		const fiveMinutesFromNow = new Date(now.getTime() + 5 * 60 * 1000);

		// Check if campaign is closed
		const isClosed = campaign.closedAt != null;

		// Check if less than 5 minutes to start
		const isNearStart =
			campaign.sendStartAt != null && new Date(campaign.sendStartAt) <= fiveMinutesFromNow;

		if (isClosed) {
			return { disabled: true, title: 'Campaign is closed' };
		}

		if (isNearStart) {
			return { disabled: true, title: 'Campaign starts in less than 5 minutes' };
		}

		return { disabled: false, title: '' };
	};

	$: {
		if (formValues.scheduledStartAt && formValues.scheduledEndAt) {
			weekDaysAvailable = weekDaysAvailableBetween(
				formValues.scheduledStartAt,
				formValues.scheduledEndAt
			);
		} else {
			weekDaysAvailable = [];
		}
	}
</script>

<HeadTitle title="Campaigns" />

<main>
	<div class="relative">
		<Headline>Campaigns</Headline>
		<div class="absolute top-0 right-0">
			<AutoRefresh
				isLoading={false}
				onRefresh={() => {
					refreshCampaigns(false);
				}}
			/>
		</div>
	</div>
	<BigButton on:click={openCreateModal}>New Campaign</BigButton>
	<Table
		columns={[
			{ column: 'Name', size: 'large' },
			{ column: 'Status', size: 'small' },
			{ column: 'Template', size: 'medium' },
			{ column: 'Send start at', title: 'Delivery start', size: 'small' },
			{ column: 'Send end at', title: 'Delivery finish', size: 'small' },
			{ column: 'Close at', size: 'small' }
		]}
		sortable={['Name', 'Template', 'Send start at', 'Send end at', 'Close at']}
		hasData={!!campaigns?.length}
		plural="campaigns"
		hasNextPage={campaignsHasNextPage}
		pagination={tableURLParams}
		isGhost={isTableLoading}
	>
		{#each campaigns as campaign}
			<TableRow>
				<TableCellLink href={`/campaign/${campaign.id}`} title={campaign.name}>
					{#if campaign.isTest}
						<TestLabel />
					{/if}
					{campaign.name}
				</TableCellLink>
				<TableCell>
					{toEvent(campaign.notableEventName).name}
				</TableCell>
				<TableCellLink
					href={`/campaign-template/?edit=${campaign.templateID}`}
					title={templateMap.byKey(campaign.templateID)}
				>
					{templateMap.byKey(campaign.templateID)}
				</TableCellLink>
				<TableCell value={campaign.sendStartAt} isDate isRelative />
				<TableCell value={campaign.sendEndAt} isDate isRelative />
				<TableCell value={campaign.closeAt ?? ''} isDate isRelative />
				<TableCellEmpty />
				<TableCellAction>
					<TableDropDownEllipsis>
						<TableViewButton
							on:click={() => onClickViewCampaign(campaign.id)}
							{...globalButtonDisabledAttributes(campaign, contextCompanyID)}
						/>
						<TableUpdateButton
							on:click={() => openUpdateModal(campaign.id)}
							{...campaignUpdateDisabledAndTitle(campaign)}
						/>
						<TableCopyButton
							title="Copy"
							on:click={() => openCopyModal(campaign.id)}
							{...globalButtonDisabledAttributes(campaign, contextCompanyID)}
						/>
						<TableDeleteButton
							on:click={() => openDeleteAlert(campaign)}
							{...globalButtonDisabledAttributes(campaign, contextCompanyID)}
						/>
					</TableDropDownEllipsis>
				</TableCellAction>
			</TableRow>
		{/each}
	</Table>

	<Modal
		headerText={modalText}
		visible={isModalVisible}
		onClose={closeModal}
		isSubmitting={isSubmitting || isRecipientModalVisible}
	>
		<div class="relative flex justify-between items-center mb-8 w-full px-4">
			<!-- Connector Line -->
			<div
				class="absolute h-[2px] bg-gray-200 dark:bg-gray-600 top-1/2 left-0 right-0 -translate-y-1/2 -z-10 transition-colors duration-200"
			/>

			{#each campaignSteps as step, index}
				<div class="flex flex-col items-center w-20 sm:w-32">
					<!-- Step Circle -->
					<div
						class={`
          w-8 h-8 mt-8 rounded-full flex items-center justify-center text-sm font-medium
          transition-colors duration-200
          ${
						currentStep > index + 1
							? 'bg-blue-300 dark:bg-indigo-700 text-white'
							: currentStep === index + 1
								? 'bg-blue-600 dark:bg-indigo-600 text-white'
								: 'bg-white dark:bg-gray-700 text-gray-500 dark:text-gray-300 border-2 border-gray-300 dark:border-gray-600'
					}
        `}
						role="tab"
						aria-selected={currentStep === index + 1}
						aria-label={`Step ${index + 1}: ${step.name}`}
					>
						{#if currentStep > index + 1}
							<svg
								xmlns="http://www.w3.org/2000/svg"
								class="h-4 w-4"
								viewBox="0 0 20 20"
								fill="currentColor"
							>
								<path
									fill-rule="evenodd"
									d="M16.707 5.293a1 1 0 010 1.414l-8 8a1 1 0 01-1.414 0l-4-4a1 1 0 011.414-1.414L8 12.586l7.293-7.293a1 1 0 011.414 0z"
									clip-rule="evenodd"
								/>
							</svg>
						{:else}
							{index + 1}
						{/if}
					</div>

					<!-- Step Label -->
					<span
						class={`
          mt-2 text-xs sm:text-sm font-medium text-center
          ${
						currentStep > index + 1 || currentStep === index + 1
							? 'text-blue-600 dark:text-blue-400'
							: 'text-gray-500 dark:text-gray-400'
					}
        `}
					>
						<span>{step.name}</span>
					</span>
				</div>
			{/each}
		</div>
		<FormGrid on:submit={onSubmit} bind:bindTo={form} {isSubmitting}>
			{#if currentStep === 1}
				<!-- Basic Information Step -->
				<FormColumns id={'step-1'}>
					<FormColumn>
						<TextField
							id={'campaignName'}
							required
							minLength={1}
							maxLength={64}
							bind:value={formValues.name}
							on:keydown={(e) => {
								/** @type {HTMLInputElement}  */
								const ele = document.querySelector('#campaignName');
								ele.setCustomValidity('');
							}}
						>
							Name
						</TextField>
						<TextFieldSelect
							required
							id="template"
							bind:value={formValues.template}
							options={Array.from(templateMap.values())}>Template</TextFieldSelect
						>
					</FormColumn>
				</FormColumns>
			{:else if currentStep === 2}
				<!-- Recipients Step -->
				<FormColumns id={'step-2'}>
					<FormColumn>
						<div class="mb-6">
							<TextFieldMultiSelect
								id="recipientGroupIDs"
								bind:value={formValues.recipientGroups}
								required
								onSelect={onAddReceipientGroup}
								onRemove={onRemoveReceipientGroup}
								options={recipientGroupMap.values()}>Recipient Groups</TextFieldMultiSelect
							>
						</div>

						{#if formValues.recipientGroups.length > 0}
							<div>
								<button
									type="button"
									class="text-sm font-medium text-white dark:text-white hover:text-gray-200 dark:hover:text-gray-300 flex items-center gap-1"
									on:click={() => {
										loadAllSelectedRecipients();
										isRecipientModalVisible = true;
									}}
								>
									<svg class="w-3 h-3" fill="none" stroke="currentColor" viewBox="0 0 24 24">
										<path
											stroke-linecap="round"
											stroke-linejoin="round"
											stroke-width="2"
											d="M15 12a3 3 0 11-6 0 3 3 0 016 0z"
										/>
										<path
											stroke-linecap="round"
											stroke-linejoin="round"
											stroke-width="2"
											d="M2.458 12C3.732 7.943 7.523 5 12 5c4.478 0 8.268 2.943 9.542 7-1.274 4.057-5.064 7-9.542 7-4.477 0-8.268-2.943-9.542-7z"
										/>
									</svg>
									<span>View All Recipients</span>
								</button>
							</div>
						{/if}
					</FormColumn>
				</FormColumns>
			{:else if currentStep === 3}
				<FormColumns id={'step-3'}>
					<FormColumn>
						<div class="mb-6">
							<div class="mb-6">
								<SelectSquare
									label="Delivery Method"
									options={scheduleOptions}
									onChange={onChangeScheduleType}
									bind:value={scheduleType}
								/>
							</div>

							{#if scheduleType === 'basic'}
								<div class="flex items-center gap-2">
									<DateTimeField
										bind:value={formValues.sendStartAt}
										min={new Date()}
										labelWidth={'medium'}
										required
									>
										Delivery start
									</DateTimeField>
									<button
										class="text-cta-blue hover:text-blue-700 dark:text-white dark:hover:text-gray-200 text-sm transition-colors duration-200"
										on:click|preventDefault={() =>
											(formValues.sendStartAt = new Date().toISOString())}
									>
										set to now
									</button>
								</div>

								{#if formValues.sendStartAt}
									<div class="pt-4 pb-6">
										<div class="flex flex-col gap-2">
											<p
												class="font-semibold text-slate-600 dark:text-gray-400 py-1 transition-colors duration-200"
											>
												Distribution Speed

												<span class="italic font-normal">
													(
													{#if spreadOption === SPREAD_MANUAL}
														Manual timing
													{:else if spreadOption === SPREAD_1MIN}
														1 minute apart
													{:else if spreadOption === SPREAD_2MIN}
														2 minutes apart
													{:else if spreadOption === SPREAD_5MIN}
														5 minutes apart
													{:else if spreadOption === SPREAD_10MIN}
														10 minutes apart
													{:else if spreadOption === SPREAD_20MIN}
														20 minutes apart
													{:else if spreadOption === SPREAD_30MIN}
														30 minutes apart
													{:else if spreadOption === SPREAD_1HOUR}
														1 hour apart
													{:else if spreadOption === SPREAD_2HOUR}
														2 hours apart
													{:else if spreadOption === SPREAD_5HOUR}
														5 hours apart
													{:else if spreadOption === SPREAD_12HOUR}
														12 hours apart
													{:else if spreadOption === SPREAD_24HOUR}
														24 hours apart
													{/if}
													)
												</span>
											</p>
											<div class="flex items-center">
												<input
													type="range"
													min="0"
													max="11"
													bind:value={speedIndex}
													class="w-96 h-2 bg-gray-200 dark:bg-gray-700 rounded-lg appearance-none cursor-pointer [&::-webkit-slider-thumb]:appearance-none [&::-webkit-slider-thumb]:w-4 [&::-webkit-slider-thumb]:h-4 [&::-webkit-slider-thumb]:rounded-full [&::-webkit-slider-thumb]:bg-blue-600 [&::-webkit-slider-thumb]:cursor-pointer hover:[&::-webkit-slider-thumb]:bg-blue-700 [&::-moz-range-thumb]:w-4 [&::-moz-range-thumb]:h-4 [&::-moz-range-thumb]:rounded-full [&::-moz-range-thumb]:bg-blue-600 [&::-moz-range-thumb]:border-0 [&::-moz-range-thumb]:cursor-pointer hover:[&::-moz-range-thumb]:bg-blue-700 transition-colors duration-200"
													on:input={(event) => {
														const index = parseInt(event.currentTarget.value);
														const speeds = [
															SPREAD_MANUAL,
															SPREAD_1MIN,
															SPREAD_2MIN,
															SPREAD_5MIN,
															SPREAD_10MIN,
															SPREAD_20MIN,
															SPREAD_30MIN,
															SPREAD_1HOUR,
															SPREAD_2HOUR,
															SPREAD_5HOUR,
															SPREAD_12HOUR,
															SPREAD_24HOUR
														];
														spreadOption = speeds[index];
														const milliseconds = getSpreadMilliseconds(spreadOption);
														if (milliseconds !== null) {
															refreshEndTimeBySendSpread(milliseconds);
														}
													}}
												/>
											</div>
										</div>
									</div>
								{/if}

								<div class="flex items-center gap-2">
									<DateTimeField
										bind:value={formValues.sendEndAt}
										min={formValues.sendStartAt ? new Date(formValues.sendStartAt) : new Date()}
										labelWidth={'medium'}
										required
										disabled={spreadOption !== SPREAD_MANUAL}
									>
										Delivery end
									</DateTimeField>
									{#if spreadOption === SPREAD_MANUAL}
										<button
											class="text-cta-blue hover:text-blue-700 dark:text-white dark:hover:text-gray-200 text-sm transition-colors duration-200"
											on:click|preventDefault={() => {
												formValues.sendEndAt = new Date().toISOString();
											}}
										>
											set to now
										</button>
									{/if}
								</div>
							{:else if scheduleType === 'schedule'}
								<p class="font-semibold text-slate-600 py-4">Delivery start and end</p>
								<div class="flex">
									<DateField
										noLabel
										inputWidth="small"
										bind:value={formValues.scheduledStartAt}
										onChange={() => {
											formValues.constraintWeekDays = [];
											formValues.contraintStartTime = null;
										}}
										required>Start</DateField
									>
									<div class="self-center px-4">
										<ToIcon />
									</div>

									<DateField
										noLabel
										inputWidth="small"
										bind:value={formValues.scheduledEndAt}
										onChange={() => {
											formValues.constraintWeekDays = [];
											formValues.contraintStartTime = null;
										}}
										required>End</DateField
									>
								</div>

								<div class="mt-4">
									<p class="font-semibold text-slate-600 py-2">Delivery days</p>
									<div class="grid grid-cols-4 gap-2">
										{#each Object.entries(dayMap) as [dayNum, dayName]}
											{@const dayInt = parseInt(dayNum)}
											<label
												class:opacity-10={!weekDaysAvailable.includes(dayInt)}
												class="flex items-center gap-2 p-2 border border-gray-300 rounded"
											>
												<input
													type="checkbox"
													on:invalid={(e) => {
														const t = e.currentTarget;
														t.setCustomValidity('Please select atleast one day for delivery.');
													}}
													required={!formValues.sendEndAt && !formValues.constraintWeekDays.length}
													on:change={() => {
														if (formValues.constraintWeekDays.includes(dayInt)) {
															formValues.constraintWeekDays = formValues.constraintWeekDays.filter(
																(d) => d !== dayInt
															);
														} else {
															formValues.constraintWeekDays = [
																...formValues.constraintWeekDays,
																dayInt
															];
														}
														formValues.constraintWeekDays.sort();
													}}
													checked={formValues.constraintWeekDays.includes(dayInt)}
													disabled={!weekDaysAvailable.includes(dayInt)}
													title={!weekDaysAvailable.includes(dayInt)
														? 'Delivery start and end must be set first'
														: ''}
												/>
												<span>{dayName}</span>
											</label>
										{/each}
									</div>
								</div>

								<p class="font-semibold text-slate-600 py-4">Delivery hours</p>

								<div class="flex">
									<div>
										<input
											class="rounded-md py-2 text-gray-600 text-center border border-transparent focus:outline-none focus:border-solid focus:border-slate-400 focus:bg-gray-100 bg-grayblue-light font-normal"
											id="constraintStartTime"
											type="time"
											required
											autocomplete="off"
											on:change={() => {
												validateConstraintTimes(document.querySelector('#constraintStartTime'));
											}}
											bind:value={formValues.contraintStartTime}
										/>
									</div>
									<div class="self-center px-4">
										<ToIcon />
									</div>
									<div>
										<input
											class="rounded-md py-2 text-gray-600 text-center border border-transparent focus:outline-none focus:border-solid focus:border-slate-400 focus:bg-gray-100 bg-grayblue-light font-normal"
											id="constraintEndTime"
											type="time"
											required
											autocomplete="off"
											on:change={() => {
												validateConstraintTimes(document.querySelector('#constraintEndTime'));
											}}
											bind:value={formValues.contraintEndTime}
										/>
									</div>
								</div>
							{/if}

							<div class="mt-6">
								{#if !showAdvancedOptionsStep3}
									<div class="mt-4">
										<button
											type="button"
											class="text-cta-blue hover:text-blue-700 dark:text-white dark:hover:text-gray-200 text-sm transition-colors duration-200 underline"
											on:click={() => (showAdvancedOptionsStep3 = true)}
										>
											Show advanced options
										</button>
									</div>
								{/if}

								{#if showAdvancedOptionsStep3}
									<TextFieldSelect
										id="sortField"
										bind:value={formValues.sortField}
										required
										toolTipText="Choose which recipient field determines the delivery order"
										options={Array.from(sortField.keys())}>Delivery sort by</TextFieldSelect
									>

									<TextFieldSelect
										id="sortOrder"
										bind:value={formValues.sortOrder}
										toolTipText="Choose how recipients will be ordered for delivery"
										required
										options={Array.from(sortOrder.keys())}>Delivery order</TextFieldSelect
									>

									<JitterSlider
										id="jitter-slider"
										bind:valueMin={formValues.jitterMin}
										bind:valueMax={formValues.jitterMax}
									>
										Jitter
									</JitterSlider>

									<DateTimeField
										bind:value={formValues.closeAt}
										min={formValues.sendEndAt
											? new Date(formValues.sendEndAt)
											: formValues.sendStartAt
												? new Date(formValues.sendStartAt)
												: new Date()}
										optional
										toolTipText="After this time, no more events are saved."
										>Close Campaign</DateTimeField
									>

									<DateTimeField
										bind:value={formValues.anonymizeAt}
										min={formValues.closeAt
											? new Date(formValues.closeAt)
											: formValues.sendEndAt
												? new Date(formValues.sendEndAt)
												: new Date()}
										optional
										toolTipText="When reached, the campaign will close and a"
										>Anonymize Data</DateTimeField
									>
								{/if}
							</div>
						</div>
					</FormColumn>
				</FormColumns>
			{:else if currentStep === 4}
				<FormColumns id={'step-4'}>
					<FormColumn>
						<div class="mb-6">
							<SelectSquare
								label="Type"
								width="small"
								toolTipText={'Tests are not included in statistics'}
								options={testOptions}
								bind:value={formValues.isTest}
							/>
						</div>

						<div class="mb-6">
							<SelectSquare
								optional
								toolTipText="Consider privacy when saving data."
								label="Save submitted data?"
								options={saveSubbmitedDataOptions}
								bind:value={formValues.saveSubmittedData}
							/>
						</div>

						<ConditionalDisplay show="blackbox">
							<div class="mb-6">
								<SelectSquare
									optional
									toolTipText="Saves JA4 fingerprint, Sec-CH-UA-Platform header, and Accept-Language header."
									label="Save browser metadata?"
									options={saveSubbmitedDataOptions}
									bind:value={formValues.saveBrowserMetadata}
								/>
							</div>
						</ConditionalDisplay>

						{#if !showAdvancedOptionsStep4}
							<div class="mt-4">
								<button
									type="button"
									class="text-cta-blue hover:text-blue-700 dark:text-white dark:hover:text-gray-200 text-sm transition-colors duration-200 underline"
									on:click={() => (showAdvancedOptionsStep4 = true)}
								>
									Show advanced options
								</button>
							</div>
						{/if}

						{#if showAdvancedOptionsStep4}
							<div class="mb-6">
								<TextFieldSelect
									id="webhook"
									bind:value={formValues.webhookValue}
									optional
									options={Array.from(webhookMap.values())}>Webhook</TextFieldSelect
								>
							</div>
							{#if formValues.webhookValue}
								<div class="mb-6">
									<SelectSquare
										bind:value={formValues.webhookIncludeData}
										options={webhookDataLevelOptions}
										label="Webhook Data Level"
										toolTipText="Control what campaign and recipient information is sent to the webhook endpoint"
									/>
								</div>
								<div class="mb-6 max-w-xl">
									<label class="flex flex-col">
										<div class="flex items-center py-2">
											<p class="font-semibold text-slate-600 dark:text-gray-400">Webhook Events</p>
											<div class="ml-2 text-sm text-gray-600 dark:text-gray-400">
												<span class="text-xs"
													>({formValues.webhookEvents.length === webhookEventOptions.length
														? 'All'
														: formValues.webhookEvents.length} selected)</span
												>
											</div>
										</div>
										<div class="flex flex-row flex-wrap gap-2 max-h-48 overflow-y-auto">
											{#each webhookEventDisplayOptions as eventOption}
												{@const isSelected = formValues.webhookEvents.includes(eventOption.value)}
												<button
													type="button"
													on:click={() => {
														if (isSelected) {
															// prevent unselecting the last item
															if (formValues.webhookEvents.length > 1) {
																formValues.webhookEvents = formValues.webhookEvents.filter(
																	(e) => e !== eventOption.value
																);
															}
														} else {
															formValues.webhookEvents = [
																...formValues.webhookEvents,
																eventOption.value
															];
														}
													}}
													class="flex flex-row items-center px-3 py-2 rounded-md text-xs transition-colors duration-200 flex-shrink-0 {isSelected
														? 'bg-green-50 dark:bg-green-900/30 text-green-600 dark:text-green-400 border-2 border-green-400 dark:border-green-500 hover:bg-green-100 dark:hover:bg-green-900/40'
														: 'bg-white dark:bg-gray-900/60 text-gray-700 dark:text-gray-300 border-2 border-gray-200 dark:border-gray-700/60 hover:border-blue-300 dark:hover:border-highlight-blue/80 hover:bg-blue-50 dark:hover:bg-highlight-blue/20'}"
													style="max-width: 280px;"
												>
													<span class="truncate block" style="max-width: 250px;">
														{eventOption.label}
													</span>
												</button>
											{/each}
										</div>
									</label>
								</div>
							{/if}

							<ConditionalDisplay show="blackbox">
								<div class="mb-6">
									<SelectSquare
										optional
										label="Security Configuration"
										options={[
											{ value: false, label: 'Disabled' },
											{ value: true, label: 'Enabled' }
										]}
										bind:value={showSecurityOptions}
										onChange={() => {
											if (!showSecurityOptions) {
												formValues.denyPageValue = '';
												formValues.evasionPageValue = '';
												allowDenyType = 'none';
												formValues.allowDeny = [];
											}
										}}
									/>
								</div>
							</ConditionalDisplay>
						{/if}

						{#if showAdvancedOptionsStep4 && showSecurityOptions}
							<div class="mb-6">
								<SelectSquare
									optional
									label="Obfuscation"
									toolTipText="Obfuscate html pages to avoid fingerprinting of static content."
									options={[
										{ value: false, label: 'Disabled' },
										{ value: true, label: 'Enabled' }
									]}
									bind:value={formValues.obfuscate}
								/>
							</div>

							<div class="mb-6">
								<TextFieldSelect
									id="deny-page"
									bind:value={formValues.denyPageValue}
									optional
									toolTipText="Page to show when access is denied. Required for evasion pages and IP filtering."
									onSelect={(page) => {
										formValues.denyPageValue = page;
									}}
									options={Array.from(denyPageMap.values())}>Deny Page</TextFieldSelect
								>
							</div>

							<div class="mb-6">
								{#if formValues.denyPageValue}
									<TextFieldSelect
										id="evasion-page"
										bind:value={formValues.evasionPageValue}
										optional
										toolTipText="Select an anti-bot/evasion page to be served before the first real page. If evasion fails, the deny page will be shown instead."
										onSelect={(page) => {
											formValues.evasionPageValue = page;
										}}
										options={Array.from(denyPageMap.values())}
										>Anti-bot / Evasion Page</TextFieldSelect
									>
								{:else}
									<div
										class="p-4 bg-gray-100 dark:bg-gray-800 rounded-lg border-2 border-dashed border-gray-300 dark:border-gray-600"
									>
										<p class="text-gray-600 dark:text-gray-400 text-sm">
											<strong>Anti-bot / Evasion Page</strong><br />
											You must select a deny page first to use evasion pages.
										</p>
									</div>
								{/if}
							</div>

							<div class="mb-6">
								{#if formValues.denyPageValue}
									<SelectSquare
										label="IP filtering"
										toolTipText="Filter access based on IP address lists"
										options={ipFilterOptions}
										width="small"
										bind:value={allowDenyType}
										onChange={() => {
											onChangeAllowDenyType();
										}}
									/>

									{#if allowDenyType !== 'none'}
										<div class="mt-4">
											<TextFieldMultiSelect
												id="allowDenyIDs"
												toolTipText="Select the IP groups to allow or block"
												bind:value={formValues.allowDeny}
												options={Array.from(allowDenyMap.values())}>Lists</TextFieldMultiSelect
											>
										</div>
									{/if}
								{:else}
									<div
										class="p-4 bg-gray-100 dark:bg-gray-800 rounded-lg border-2 border-dashed border-gray-300 dark:border-gray-600"
									>
										<p class="text-gray-600 dark:text-gray-400 text-sm">
											<strong>IP filtering mode</strong><br />
											You must select a deny page first to use IP filtering.
										</p>
									</div>
								{/if}
							</div>
						{/if}
					</FormColumn>
				</FormColumns>
			{:else if currentStep === 5}
				<!-- Review Step -->
				<FormColumns id={'step-5'}>
					<FormColumn>
						<div class="space-y-6 w-full">
							<!-- First Row: Basic Info and Recipients -->
							<div class="grid grid-cols-2 gap-6">
								<!-- Basic Information -->
								<div
									class="bg-white dark:bg-gray-800 p-6 rounded-lg shadow-sm transition-colors duration-200"
								>
									<h3
										class="text-xl font-semibold text-pc-darkblue dark:text-white mb-4 border-b dark:border-gray-600 pb-2 transition-colors duration-200"
									>
										Basic Information
									</h3>
									<div class="grid grid-cols-[120px_1fr] gap-y-3">
										<span class="text-grayblue-dark font-medium">Name:</span>
										<span class="text-pc-darkblue dark:text-white"
											>{formValues.name || 'Not set'}</span
										>

										<span class="text-grayblue-dark font-medium">Template:</span>
										<span class="text-pc-darkblue dark:text-white"
											>{formValues.template || 'Not set'}</span
										>

										<span class="text-grayblue-dark font-medium">Type:</span>
										<span class="text-pc-darkblue dark:text-white"
											>{formValues.isTest ? 'Test' : 'Production'}</span
										>
									</div>
								</div>

								<!-- Recipients -->
								<div
									class="bg-white dark:bg-gray-800 p-6 rounded-lg shadow-sm transition-colors duration-200"
								>
									<h3
										class="text-xl font-semibold text-pc-darkblue dark:text-white mb-4 border-b dark:border-gray-600 pb-2 transition-colors duration-200"
									>
										Recipients
									</h3>
									<div class="space-y-4">
										<div class="grid grid-cols-[120px_1fr] gap-y-3">
											<span class="text-grayblue-dark font-medium">Groups:</span>
											<span
												class="text-pc-darkblue dark:text-gray-100 transition-colors duration-200"
											>
												{formValues.recipientGroups.length
													? formValues.recipientGroups.join(', ')
													: 'None selected'}
											</span>

											<span class="text-grayblue-dark font-medium">Total:</span>
											<span class="text-pc-darkblue dark:text-white"
												>{formValues.selectedCount} recipients</span
											>
										</div>

										{#if formValues.recipientGroups.length > 0}
											<button
												type="button"
												class="text-xs font-medium text-white dark:text-white hover:text-gray-200 dark:hover:text-gray-300 flex items-center gap-1"
												on:click={() => {
													loadAllSelectedRecipients();
													isRecipientModalVisible = true;
												}}
											>
												<svg class="w-3 h-3" fill="none" stroke="currentColor" viewBox="0 0 24 24">
													<path
														stroke-linecap="round"
														stroke-linejoin="round"
														stroke-width="2"
														d="M15 12a3 3 0 11-6 0 3 3 0 016 0z"
													/>
													<path
														stroke-linecap="round"
														stroke-linejoin="round"
														stroke-width="2"
														d="M2.458 12C3.732 7.943 7.523 5 12 5c4.478 0 8.268 2.943 9.542 7-1.274 4.057-5.064 7-9.542 7-4.477 0-8.268-2.943-9.542-7z"
													/>
												</svg>
												<span>View All Recipients</span>
											</button>
										{/if}
									</div>
								</div>
							</div>

							<!-- Second Row: Email and Schedule -->
							<div class="grid grid-cols-2 gap-6">
								<!-- Email/API Sender Information -->
								<div
									class="bg-white dark:bg-gray-800 p-6 rounded-lg shadow-sm transition-colors duration-200"
								>
									<h3
										class="text-xl font-semibold text-pc-darkblue dark:text-white mb-4 border-b dark:border-gray-600 pb-2 transition-colors duration-200"
									>
										Delivery Details
									</h3>
									{#await getTemplateDetails(formValues.template)}
										<div class="text-gray-500">Loading delivery details...</div>
									{:then template}
										{#if template?.apiSender}
											<div class="grid grid-cols-[120px_1fr] gap-y-3">
												<span class="text-grayblue-dark font-medium">Type:</span>
												<span class="text-pc-darkblue dark:text-white font-semibold"
													>API Sender</span
												>

												<span class="text-grayblue-dark font-medium">Name:</span>
												<span class="text-pc-darkblue dark:text-white"
													>{template.apiSender.name || 'Not set'}</span
												>

												{#if template?.email}
													<span class="text-grayblue-dark font-medium">Email:</span>
													<span class="text-pc-darkblue dark:text-white"
														>{template.email.name || 'Not set'}</span
													>
												{/if}
											</div>
										{:else if template?.email}
											<div class="grid grid-cols-[120px_1fr] gap-y-3">
												<span class="text-grayblue-dark font-medium">Type:</span>
												<span class="text-pc-darkblue dark:text-white font-semibold">SMTP</span>

												<span class="text-grayblue-dark font-medium">Name:</span>
												<span class="text-pc-darkblue dark:text-white"
													>{template.email.name || 'Not set'}</span
												>

												<span class="text-grayblue-dark font-medium">From:</span>
												<span class="text-pc-darkblue dark:text-white"
													>{template.email.mailHeaderFrom || 'Not set'}</span
												>

												<span class="text-grayblue-dark font-medium">Mail from:</span>
												<span class="text-pc-darkblue dark:text-white"
													>{template.email.mailEnvelopeFrom || 'Not set'}</span
												>

												<span class="text-grayblue-dark font-medium">Subject:</span>
												<span class="text-pc-darkblue dark:text-white"
													>{template.email.mailHeaderSubject || 'Not set'}</span
												>
											</div>
										{:else}
											<div class="text-gray-500">
												No email or API sender configured for this template
											</div>
										{/if}
									{:catch error}
										<div class="text-red-500">Failed to load delivery details</div>
									{/await}
								</div>

								<!-- Schedule -->
								<div
									class="bg-white dark:bg-gray-800 p-6 rounded-lg shadow-sm transition-colors duration-200"
								>
									<h3
										class="text-xl font-semibold text-pc-darkblue dark:text-white mb-4 border-b dark:border-gray-600 pb-2 transition-colors duration-200"
									>
										Schedule
									</h3>
									<div class="grid grid-cols-[120px_1fr] gap-y-3">
										<span class="text-grayblue-dark font-medium">Type:</span>
										<span class="text-pc-darkblue dark:text-white capitalize">{scheduleType}</span>

										{#if scheduleType === 'basic'}
											<span class="text-grayblue-dark font-medium">Start:</span>
											<span
												class="text-pc-darkblue dark:text-gray-100 transition-colors duration-200"
											>
												<Datetime value={formValues.sendStartAt} />
												<RelativeTime value={formValues.sendStartAt} />
											</span>

											<span
												class="text-grayblue-dark dark:text-gray-300 font-medium transition-colors duration-200"
												>End:</span
											>
											<span
												class="text-pc-darkblue dark:text-gray-100 transition-colors duration-200"
											>
												<Datetime value={formValues.sendEndAt} />
												<RelativeTime value={formValues.sendEndAt} />
											</span>

											{#if spreadOption && spreadOption !== SPREAD_MANUAL}
												<span class="text-grayblue-dark font-medium">Spread:</span>
												<span
													class="text-pc-darkblue dark:text-gray-100 transition-colors duration-200"
												>
													{spreadOptionMap.byValue(spreadOption)}
												</span>
											{/if}

											{#if formValues.jitterMin !== 0 || formValues.jitterMax !== 0}
												<span class="text-grayblue-dark font-medium">Jitter:</span>
												<span
													class="text-pc-darkblue dark:text-gray-100 transition-colors duration-200"
												>
													{formValues.jitterMin} to {formValues.jitterMax} minutes
												</span>
											{/if}
										{:else if scheduleType === 'schedule'}
											<span class="text-grayblue-dark font-medium">Active days:</span>
											<span
												class="text-pc-darkblue dark:text-gray-100 transition-colors duration-200"
											>
												{formValues.constraintWeekDays.map((d) => dayMap[d]).join(', ') ||
													'None selected'}
											</span>

											{#if formValues.contraintStartTime && formValues.contraintEndTime}
												<span class="text-grayblue-dark font-medium">Hours:</span>
												<span class="text-pc-darkblue dark:text-white">
													{formValues.contraintStartTime} - {formValues.contraintEndTime}
												</span>
											{/if}

											{#if formValues.jitterMin !== 0 || formValues.jitterMax !== 0}
												<span class="text-grayblue-dark font-medium">Jitter:</span>
												<span
													class="text-pc-darkblue dark:text-gray-100 transition-colors duration-200"
												>
													{formValues.jitterMin} to {formValues.jitterMax} minutes
												</span>
											{/if}
										{/if}

										{#if formValues.closeAt}
											<span class="text-grayblue-dark font-medium">Close at:</span>
											<span class="text-pc-darkblue dark:text-white">
												<Datetime value={formValues.closeAt} />
												<RelativeTime value={formValues.closeAt} />
											</span>
										{/if}
									</div>
								</div>
							</div>

							<!-- Third Row: Security & Privacy -->
							<div class="grid grid-cols-1 gap-6">
								<div
									class="bg-white dark:bg-gray-800 p-6 rounded-lg shadow-sm transition-colors duration-200"
								>
									<h3
										class="text-xl font-semibold text-pc-darkblue dark:text-white mb-4 border-b dark:border-gray-600 pb-2 transition-colors duration-200"
									>
										Security & Privacy
									</h3>
									<div class="grid grid-cols-[120px_1fr] gap-y-3">
										<ConditionalDisplay show="blackbox">
											<span class="text-grayblue-dark font-medium">IP Filtering:</span>
											<span class="text-pc-darkblue dark:text-white">
												{#if allowDenyType === 'none'}
													None
												{:else}
													{allowDenyType === 'allow' ? 'Allow-list' : 'Deny-list'}:
													{formValues.allowDeny.length
														? formValues.allowDeny.join(', ')
														: 'No groups selected'}
												{/if}
											</span>
										</ConditionalDisplay>

										<span class="text-grayblue-dark font-medium">Save Data:</span>
										<span class="text-pc-darkblue dark:text-white"
											>{formValues.saveSubmittedData ? 'Enabled' : 'Disabled'}</span
										>

										<ConditionalDisplay show="blackbox">
											<span class="text-grayblue-dark font-medium">Save Metadata:</span>
											<span class="text-pc-darkblue dark:text-white"
												>{formValues.saveBrowserMetadata ? 'Enabled' : 'Disabled'}</span
											>
										</ConditionalDisplay>

										<!--
										<span class="text-grayblue-dark font-medium">Anonymization:</span>
										<span class="text-pc-darkblue"
											>{formValues.isAnonymous ? 'Enabled' : 'Disabled'}</span
										>
										 -->

										{#if formValues.webhookValue}
											<span class="text-grayblue-dark font-medium">Webhook:</span>
											<span class="text-pc-darkblue dark:text-white">{formValues.webhookValue}</span
											>
											<span class="text-grayblue-dark font-medium">Data Level:</span>
											<span class="text-pc-darkblue dark:text-white capitalize"
												>{formValues.webhookIncludeData}</span
											>
											<span class="text-grayblue-dark font-medium">Webhook Events:</span>
											<span class="text-pc-darkblue dark:text-white">
												{formValues.webhookEvents.length === webhookEventOptions.length
													? 'All Events'
													: formValues.webhookEvents.length + ' selected'}
											</span>
										{/if}

										{#if formValues.denyPageValue}
											<span class="text-grayblue-dark font-medium">Deny Page:</span>
											<span class="text-pc-darkblue dark:text-white"
												>{formValues.denyPageValue}</span
											>
										{/if}

										{#if formValues.evasionPageValue}
											<span class="text-grayblue-dark font-medium">Evasion Page:</span>
											<span class="text-pc-darkblue dark:text-white"
												>{formValues.evasionPageValue}</span
											>
										{/if}

										{#if formValues.anonymizeAt}
											<span class="text-grayblue-dark font-medium">Anonymize at:</span>
											<span class="text-pc-darkblue dark:text-white">
												<Datetime value={formValues.anonymizeAt} />
												<RelativeTime value={formValues.anonymizeAt} />
											</span>
										{/if}
									</div>
								</div>
							</div>
						</div>
					</FormColumn>
				</FormColumns>
			{/if}
			<FormError message={modalError} />
			<div
				class="col-span-3 flex justify-between items-center w-full mt-2 border-t dark:border-gray-600 py-6 transition-colors duration-200"
			>
				{#if currentStep > 1}
					<button
						type="button"
						class="inline-flex items-center px-4 py-2 border border-gray-300 dark:border-gray-600 rounded-md shadow-sm text-sm font-medium text-gray-700 dark:text-gray-200 bg-white dark:bg-gray-700 hover:bg-gray-50 dark:hover:bg-gray-600 focus:outline-none focus:ring-2 focus:ring-offset-2 focus:ring-blue-500"
						on:click={previousStep}
					>
						<svg
							class="mr-2 h-4 w-4"
							xmlns="http://www.w3.org/2000/svg"
							fill="none"
							viewBox="0 0 24 24"
							stroke="currentColor"
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

				{#if currentStep < 5}
					<button
						type="button"
						class="inline-flex items-center px-4 py-2 border border-transparent rounded-md shadow-sm text-sm font-medium text-white bg-blue-600 hover:bg-blue-700 focus:outline-none focus:ring-2 focus:ring-offset-2 focus:ring-blue-500 disabled:opacity-50 disabled:cursor-not-allowed"
						disabled={isValidatingName}
						on:click={nextStep}
					>
						{#if isValidatingName}
							Checking...
						{:else}
							Next
						{/if}
						<svg
							class="ml-2 h-4 w-4"
							xmlns="http://www.w3.org/2000/svg"
							fill="none"
							viewBox="0 0 24 24"
							stroke="currentColor"
						>
							<path
								stroke-linecap="round"
								stroke-linejoin="round"
								stroke-width="2"
								d="M9 5l7 7-7 7"
							/>
						</svg>
					</button>
				{:else}
					<button
						type="submit"
						class="inline-flex items-center px-4 py-2 border border-transparent rounded-md shadow-sm text-sm font-medium text-white bg-blue-600 hover:bg-blue-700 focus:outline-none focus:ring-2 focus:ring-offset-2 focus:ring-blue-500"
						disabled={isSubmitting}
					>
						{modalMode === 'create' || modalMode === 'copy' ? 'Create' : 'Update'}
						{#if !isSubmitting}
							<svg
								class="ml-2 h-4 w-4"
								xmlns="http://www.w3.org/2000/svg"
								fill="none"
								viewBox="0 0 24 24"
								stroke="currentColor"
							>
								<path
									stroke-linecap="round"
									stroke-linejoin="round"
									stroke-width="2"
									d="M5 13l4 4L19 7"
								/>
							</svg>
						{/if}
					</button>
				{/if}
			</div>
		</FormGrid>
	</Modal>

	<!-- Recipient Preview Modal -->
	<Modal
		headerText="Recipients"
		visible={isRecipientModalVisible}
		onClose={() => (isRecipientModalVisible = false)}
	>
		<div class="p-4">
			<div class="mb-4">
				<p class="text-sm text-gray-600 dark:text-gray-400">
					Total: <span class="font-semibold text-pc-darkblue dark:text-white"
						>{formValues.selectedCount} recipients</span
					>
				</p>
			</div>

			<div class="space-y-4">
				{#each formValues.recipientGroups as groupName}
					{@const groupID = recipientGroupMap.byValue(groupName)}
					{@const recipients = recipientGroupRecipients[groupID] || []}
					<div
						class="bg-gray-50 dark:bg-gray-800 rounded-lg border border-gray-200 dark:border-gray-700"
					>
						<details class="group">
							<summary
								class="cursor-pointer p-4 font-semibold text-base text-pc-darkblue dark:text-white hover:bg-gray-100 dark:hover:bg-gray-700/50 rounded-lg transition-colors list-none flex items-center gap-2"
							>
								<svg
									class="w-4 h-4 transition-transform group-open:rotate-90"
									fill="none"
									stroke="currentColor"
									viewBox="0 0 24 24"
								>
									<path
										stroke-linecap="round"
										stroke-linejoin="round"
										stroke-width="2"
										d="M9 5l7 7-7 7"
									/>
								</svg>
								<span>{groupName}</span>
							</summary>
							<div class="px-4 pb-4">
								{#if recipients.length > 0}
									<div class="space-y-1">
										{#each recipients as recipient}
											<div
												class="flex items-center justify-between py-2 px-3 rounded hover:bg-white dark:hover:bg-gray-700/50 transition-colors"
											>
												<span
													class="text-sm text-gray-900 dark:text-gray-100 font-medium truncate flex-1"
												>
													{recipient.email}
												</span>
												{#if recipient.firstName || recipient.lastName}
													<span
														class="text-sm text-gray-500 dark:text-gray-400 ml-4 whitespace-nowrap"
													>
														{recipient.firstName || ''}
														{recipient.lastName || ''}
													</span>
												{/if}
											</div>
										{/each}
									</div>
								{:else}
									<p class="text-sm text-gray-500 dark:text-gray-400 italic">
										Loading recipients...
									</p>
								{/if}
							</div>
						</details>
					</div>
				{/each}
			</div>
		</div>
	</Modal>

	<DeleteAlert
		list={['This will remove statistics related to the campaign and recipients']}
		name={deleteValues.name}
		onClick={() => onClickDelete(deleteValues.id)}
		confirm
		bind:isVisible={isDeleteAlertVisible}
	/>
</main>
