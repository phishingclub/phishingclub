<script>
	import { api } from '$lib/api/apiProxy.js';
	import { onMount } from 'svelte';
	import { goto } from '$app/navigation';
	import { page } from '$app/stores';
	import { newTableURLParams } from '$lib/service/tableURLParams.js';
	import Headline from '$lib/components/Headline.svelte';
	import TextField from '$lib/components/TextField.svelte';
	import TableRow from '$lib/components/table/TableRow.svelte';
	import TableCell from '$lib/components/table/TableCell.svelte';
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
	import RelativeTime from '$lib/components/RelativeTime.svelte';
	import AutoRefresh from '$lib/components/AutoRefresh.svelte';

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
			label: 'Add allow-list',
			value: 'allow',
			icon: ''
		},

		{
			label: 'Add deny-list',
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

	let deleteValues = {
		id: null,
		name: null
	};

	let speedIndex = 0; // This will correspond to SPREAD_MANUAL initially

	const appStateService = AppStateService.instance;
	let contextCompanyID = null;
	let campaigns = [];
	let templateMap = new BiMap({});
	let recipientGroupsByID = {};
	let recipientGroupMap = new BiMap({});
	let denyPages = [];
	let denyPageMap = new BiMap({});
	let allowDenyMap = new BiMap({});
	let webhookMap = new BiMap({});
	let modalMode = null;
	let scheduleType = 'basic';
	let allowDenyType = 'none';
	let allAllowDeny = [];

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
	const SPREAD_5MIN = '5min';
	const SPREAD_20MIN = '20min';
	const SPREAD_1HOUR = '1hour';

	const spreadOptionMap = new BiMap({
		Manual: SPREAD_MANUAL,
		'1 minute': SPREAD_1MIN,
		'5 minutes': SPREAD_5MIN,
		'20 minutes': SPREAD_20MIN,
		'1 hour': SPREAD_1HOUR
	});

	let spreadOption = SPREAD_MANUAL;

	const getSpreadMilliseconds = (spreadOption) => {
		switch (spreadOption) {
			case SPREAD_IMMEDIATE:
				return 0;
			case SPREAD_1MIN:
				return 60000;
			case SPREAD_5MIN:
				return 300000;
			case SPREAD_20MIN:
				return 1200000;
			case SPREAD_1HOUR:
				return 3600000;
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
		constraintWeekDays: [],
		contraintStartTime: null,
		contraintEndTime: null,
		saveSubmittedData: null,
		isAnonymous: null,
		isTest: false,
		selectedCount: 0,
		webhookValue: null
	};

	let modalError = '';
	let isModalVisible = false;
	let isSubmitting = false;
	let isTableLoading = false;
	let modalText = '';
	let weekDaysAvailable = [];
	let isDeleteAlertVisible = false;

	$: {
		modalText = getModalText('campaign', modalMode);
	}

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

	const nextStep = () => {
		if (validateCurrentStep()) {
			currentStep = Math.min(currentStep + 1, campaignSteps.length);
			modalError = '';
		}
	};

	const previousStep = () => {
		currentStep = Math.max(currentStep - 1, 1);
	};

	const validateCurrentStep = () => {
		switch (currentStep) {
			case 1:
				return validateBasicInfo();
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

	const validateBasicInfo = () => {
		return checkCurrentStepValidity();
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
				isAnonymous: formValues.isAnonymous,
				isTest: formValues.isTest,
				recipientGroupIDs: recipientGroupIDs,
				allowDenyIDs: allowDenyIDs,
				denyPageID: denyPageMap.byValueOrNull(formValues.denyPageValue),
				constraintWeekDays: weekDaysAvailableToBinary(formValues.constraintWeekDays),
				constraintStartTime: contraintStartTimeUTC,
				constraintEndTime: contraintEndTimeUTC,
				webhookID: webhookMap.byValueOrNull(formValues.webhookValue)
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
				isAnonymous: formValues.isAnonymous,
				isTest: formValues.isTest,
				constraintWeekDays: weekDaysAvailableToBinary(formValues.constraintWeekDays),
				constraintStartTime: contraintStartTimeUTC,
				constraintEndTime: contraintEndTimeUTC,
				sendEndAt: sendEndAtUTC,
				closeAt: closeAtUTC,
				anonymizeAt: anonymizeAtUTC,
				recipientGroupIDs: recipientGroupIDs,
				allowDenyIDs: allowDenyIDs,
				denyPageID: denyPageMap.byValueOrNull(formValues.denyPageValue),
				webhookID: webhookMap.byValueOrNull(formValues.webhookValue)
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
	const campaignNameExits = async (name) => {
		try {
			const res = await api.campaign.getByName(name, contextCompanyID);
			/** @type {HTMLInputElement} */
			const ele = document.querySelector('#campaignName');
			if (
				res.data &&
				(modalMode === 'create' || modalMode === 'copy' || res.data.id !== formValues.id)
			) {
				ele.setCustomValidity('Name is used by another campaign');
				ele.reportValidity();
			} else {
				ele.setCustomValidity('');
			}
		} catch (e) {
			addToast('Failed to check if campaign name is used', 'Error');
			console.error('Failed to check if campaign name is used', e);
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
			name: null,
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
			constraintWeekDays: [],
			contraintStartTime: null,
			contraintEndTime: null,
			saveSubmittedData: false,
			isAnonymous: false,
			isTest: false,
			selectedCount: 0,
			webhookValue: null
		};
		scheduleType = 'basic';
		allowDenyType = 'none';
		spreadOption = SPREAD_MANUAL;
		modalError = '';
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
		formValues.denyPageValue = null;
		setAllowDenyType(allAllowDeny);
	};

	const closeModal = () => {
		modalMode = null;
		isModalVisible = false;
		currentStep = 1;
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
			sendStartAt: campaign.sendStartAt,
			sendEndAt: campaign.sendEndAt,
			scheduledStartAt: campaign.sendStartAt
				? local_yyyy_mm_dd(new Date(campaign.sendStartAt))
				: null,
			scheduledEndAt: campaign.sendEndAt ? local_yyyy_mm_dd(new Date(campaign.sendEndAt)) : null,
			constraintWeekDays: weekDayBinaryToAvailable(campaign.constraintWeekDays),
			contraintStartTime: utcTimeToLocal(campaign.constraintStartTime),
			contraintEndTime: utcTimeToLocal(campaign.constraintEndTime),
			closeAt: campaign.closeAt,
			anonymizeAt: campaign.anonymizeAt,
			saveSubmittedData: campaign.saveSubmittedData,
			isAnonymous: campaign.isAnonymous,
			isTest: campaign.isTest,
			template: templateMap.byKey(campaign.templateID),
			webhookValue: webhookMap.byKey(campaign.webhookID)
		};

		formValues.recipientGroups = campaign.recipientGroupIDs.map((id) =>
			recipientGroupMap.byKey(id)
		);
		formValues.selectedCount = formValues.recipientGroups.reduce((acc, label) => {
			const id = recipientGroupMap.byValue(label);
			const group = recipientGroupsByID[id];
			return acc + group.recipientCount;
		}, 0);

		if (!formValues.sendStartAt && !formValues.sendEndAt) {
			scheduleType = 'self-managed';
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
	};

	/*
	const onClickSendImmediately = () => {
		formValues.sendStartAt = new Date().toISOString();
		formValues.sendEndAt = new Date().toISOString();
		spreadOption = SPREAD_IMMEDIATE;
	};
	 */

	const onAddReceipientGroup = (group) => {
		const groupLabel = recipientGroupMap.byValue(group);
		const groupData = recipientGroupsByID[groupLabel];
		formValues.selectedCount += groupData.recipientCount;
		refreshEndTimeBySendSpread();
	};

	const onRemoveReceipientGroup = (group) => {
		const groupLabel = recipientGroupMap.byValue(group);
		const groupData = recipientGroupsByID[groupLabel];
		formValues.selectedCount -= groupData.recipientCount;
		refreshEndTimeBySendSpread();
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

<main class="">
	<div class="flex justify-between">
		<Headline>Campaigns</Headline>
		<AutoRefresh
			isLoading={false}
			onRefresh={() => {
				refreshCampaigns(false);
			}}
		/>
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
		pagination={tableURLParams}
		isGhost={isTableLoading}
	>
		{#each campaigns as campaign}
			<TableRow>
				<TableCell>
					{#if campaign.isTest}
						<TestLabel />
					{/if}
					<a href={`/campaign/${campaign.id}`}>
						{campaign.name}
					</a>
				</TableCell>
				<TableCell>
					{toEvent(campaign.notableEventName).name}
				</TableCell>
				<TableCell>
					<a href={`/campaign-template/?edit=${campaign.templateID}`}>
						{templateMap.byKey(campaign.templateID)}
					</a>
				</TableCell>
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

	<Modal headerText={modalText} visible={isModalVisible} onClose={closeModal} {isSubmitting}>
		<div class="relative flex justify-between items-center mb-8 w-full px-4">
			<!-- Connector Line -->
			<div class="absolute h-[2px] bg-gray-200 top-1/2 left-0 right-0 -translate-y-1/2 -z-10" />

			{#each campaignSteps as step, index}
				<div class="flex flex-col items-center w-20 sm:w-32">
					<!-- Step Circle -->
					<div
						class={`
          w-8 h-8 mt-8 rounded-full flex items-center justify-center text-sm font-medium
          transition-colors duration-200
          ${
						currentStep > index + 1
							? 'bg-blue-300 text-white'
							: currentStep === index + 1
								? 'bg-blue-600 text-white'
								: 'bg-white text-gray-500 border-2 border-gray-300'
					}
        `}
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
						currentStep > index + 1 || currentStep === index + 1 ? 'text-blue-600' : 'text-gray-500'
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
							onBlur={() => {
								formValues.name.length && campaignNameExits(formValues.name);
							}}>Name</TextField
						>
						<TextFieldSelect
							required
							id="template"
							bind:value={formValues.template}
							options={Array.from(templateMap.values())}>Template</TextFieldSelect
						>
						<div class="py-2">
							<SelectSquare
								label="Type"
								width="small"
								toolTipText={'Tests are not included in statistics'}
								options={testOptions}
								bind:value={formValues.isTest}
							/>
						</div>
					</FormColumn>
				</FormColumns>
			{:else if currentStep === 2}
				<!-- Recipients Step -->
				<FormColumns id={'step-2'}>
					<FormColumn>
						<TextFieldMultiSelect
							id="recipientGroupIDs"
							bind:value={formValues.recipientGroups}
							required
							onSelect={onAddReceipientGroup}
							onRemove={onRemoveReceipientGroup}
							options={recipientGroupMap.values()}>Recipient Groups</TextFieldMultiSelect
						>
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
										class="text-cta-blue hover:text-blue-700 text-sm"
										on:click|preventDefault={() =>
											(formValues.sendStartAt = new Date().toISOString())}
									>
										set to now
									</button>
								</div>

								{#if formValues.sendStartAt}
									<div class="pl-36 pt-4 pb-6">
										<div class="flex flex-col gap-2">
											<p class="text-sm font-semibold text-slate-600">
												Distribution Speed

												<span class="italic font-normal">
													(
													{#if spreadOption === SPREAD_MANUAL}
														Manual timing
													{:else if spreadOption === SPREAD_1MIN}
														1 minutes apart
													{:else if spreadOption === SPREAD_5MIN}
														5 minutes apart
													{:else if spreadOption === SPREAD_20MIN}
														20 minutes apart
													{:else if spreadOption === SPREAD_1HOUR}
														1 hour apart
													{/if}
													)
												</span>
											</p>
											<div class="flex items-center gap-4">
												<input
													type="range"
													min="0"
													max="4"
													bind:value={speedIndex}
													class="w-48 h-2 bg-gray-200 rounded-lg appearance-none cursor-pointer [&::-webkit-slider-thumb]:appearance-none [&::-webkit-slider-thumb]:w-4 [&::-webkit-slider-thumb]:h-4 [&::-webkit-slider-thumb]:rounded-full [&::-webkit-slider-thumb]:bg-blue-600 [&::-webkit-slider-thumb]:cursor-pointer hover:[&::-webkit-slider-thumb]:bg-blue-700"
													on:input={(event) => {
														const index = parseInt(event.currentTarget.value);
														const speeds = [
															SPREAD_MANUAL,
															SPREAD_1MIN,
															SPREAD_5MIN,
															SPREAD_20MIN,
															SPREAD_1HOUR
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
											class="text-cta-blue hover:text-blue-700 text-sm"
											on:click|preventDefault={() => {
												formValues.sendEndAt = new Date().toISOString();
											}}
										>
											set to now
										</button>
									{/if}
								</div>

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
									options={Array.from(sortOrder.keys())}>Delivery sort order</TextFieldSelect
								>
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

								<TextFieldSelect
									id="sortField"
									bind:value={formValues.sortField}
									required
									toolTipText="Choose which recipient field determines the delivery order"
									options={Array.from(sortField.keys())}>Delivery by</TextFieldSelect
								>

								<TextFieldSelect
									id="sortOrder"
									bind:value={formValues.sortOrder}
									toolTipText="Choose how recipients will be ordered for delivery"
									required
									options={Array.from(sortOrder.keys())}>Delivery order</TextFieldSelect
								>

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
							</div>
						</div>
					</FormColumn>
				</FormColumns>
			{:else if currentStep === 4}
				<FormColumns id={'step-4'}>
					<FormColumn>
						<div class="mb-6">
							<SelectSquare
								optional
								toolTipText="Consider privacy when saving data."
								label="Save submitted data?"
								options={saveSubbmitedDataOptions}
								bind:value={formValues.saveSubmittedData}
							/>
						</div>

						<div class="mb-6">
							<SelectSquare
								label="IP filtering"
								options={ipFilterOptions}
								bind:value={allowDenyType}
								onChange={() => {
									onChangeAllowDenyType();
								}}
							/>

							{#if allowDenyType !== 'none'}
								<TextFieldMultiSelect
									id="allowDenyIDs"
									toolTipText="Select the IP groups to allow or block"
									bind:value={formValues.allowDeny}
									options={Array.from(allowDenyMap.values())}>Lists</TextFieldMultiSelect
								>

								<TextFieldSelect
									id="deny-page"
									bind:value={formValues.denyPageValue}
									optional
									onSelect={(page) => {
										formValues.denyPageValue = page;
									}}
									options={Array.from(denyPageMap.values())}>Blocked Access Page</TextFieldSelect
								>
							{/if}
						</div>

						<div>
							<TextFieldSelect
								id="webhook"
								bind:value={formValues.webhookValue}
								optional
								options={Array.from(webhookMap.values())}>Webhook</TextFieldSelect
							>
						</div>
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
								<div class="bg-white p-6 rounded-lg shadow-sm">
									<h3 class="text-xl font-semibold text-pc-darkblue mb-4 border-b pb-2">
										Basic Information
									</h3>
									<div class="grid grid-cols-[120px_1fr] gap-y-3">
										<span class="text-grayblue-dark font-medium">Name:</span>
										<span class="text-pc-darkblue">{formValues.name || 'Not set'}</span>

										<span class="text-grayblue-dark font-medium">Template:</span>
										<span class="text-pc-darkblue">{formValues.template || 'Not set'}</span>

										<span class="text-grayblue-dark font-medium">Type:</span>
										<span class="text-pc-darkblue">{formValues.isTest ? 'Test' : 'Production'}</span
										>
									</div>
								</div>

								<!-- Recipients -->
								<div class="bg-white p-6 rounded-lg shadow-sm">
									<h3 class="text-xl font-semibold text-pc-darkblue mb-4 border-b pb-2">
										Recipients
									</h3>
									<div class="grid grid-cols-[120px_1fr] gap-y-3">
										<span class="text-grayblue-dark font-medium">Groups:</span>
										<span class="text-pc-darkblue">
											{formValues.recipientGroups.length
												? formValues.recipientGroups.join(', ')
												: 'None selected'}
										</span>

										<span class="text-grayblue-dark font-medium">Total:</span>
										<span class="text-pc-darkblue">{formValues.selectedCount} recipients</span>
									</div>
								</div>
							</div>

							<!-- Second Row: Schedule and Security -->
							<div class="grid grid-cols-2 gap-6">
								<!-- Schedule -->
								<div class="bg-white p-6 rounded-lg shadow-sm">
									<h3 class="text-xl font-semibold text-pc-darkblue mb-4 border-b pb-2">
										Schedule
									</h3>
									<div class="grid grid-cols-[120px_1fr] gap-y-3">
										<span class="text-grayblue-dark font-medium">Type:</span>
										<span class="text-pc-darkblue">{scheduleType}</span>

										{#if scheduleType === 'basic'}
											<span class="text-grayblue-dark font-medium">Start:</span>
											<span class="text-pc-darkblue">
												<Datetime value={formValues.sendStartAt} />
												<RelativeTime value={formValues.sendStartAt} />
											</span>

											<span class="text-grayblue-dark font-medium">End:</span>
											<span class="text-pc-darkblue">
												<Datetime value={formValues.sendEndAt} />
												<RelativeTime value={formValues.sendEndAt} />
											</span>

											{#if spreadOption && spreadOption !== SPREAD_MANUAL}
												<span class="text-grayblue-dark font-medium">Spread:</span>
												<span class="text-pc-darkblue">
													{spreadOptionMap.byValue(spreadOption)}
												</span>
											{/if}
										{:else if scheduleType === 'schedule'}
											<span class="text-grayblue-dark font-medium">Active days:</span>
											<span class="text-pc-darkblue">
												{formValues.constraintWeekDays.map((d) => dayMap[d]).join(', ') ||
													'None selected'}
											</span>

											{#if formValues.contraintStartTime && formValues.contraintEndTime}
												<span class="text-grayblue-dark font-medium">Hours:</span>
												<span class="text-pc-darkblue">
													{formValues.contraintStartTime} - {formValues.contraintEndTime}
												</span>
											{/if}
										{/if}

										{#if formValues.closeAt}
											<span class="text-grayblue-dark font-medium">Close at:</span>
											<span class="text-pc-darkblue">
												<Datetime value={formValues.closeAt} />
												<RelativeTime value={formValues.closeAt} />
											</span>
										{/if}

										{#if formValues.anonymizeAt}
											<span class="text-grayblue-dark font-medium">Anonymize at:</span>
											<span class="text-pc-darkblue">
												<Datetime value={formValues.anonymizeAt} />
												<RelativeTime value={formValues.anonymizeAt} />
											</span>
										{/if}
									</div>
								</div>

								<!-- Misc -->
								<div class="bg-white p-6 rounded-lg shadow-sm">
									<h3 class="text-xl font-semibold text-pc-darkblue mb-4 border-b pb-2">
										Security & Privacy
									</h3>
									<div class="grid grid-cols-[120px_1fr] gap-y-3">
										<span class="text-grayblue-dark font-medium">IP Filtering:</span>
										<span class="text-pc-darkblue">
											{#if allowDenyType === 'none'}
												None
											{:else}
												{allowDenyType === 'allow' ? 'Allow-list' : 'Deny-list'}:
												{formValues.allowDeny.length
													? formValues.allowDeny.join(', ')
													: 'No groups selected'}
											{/if}
										</span>

										<span class="text-grayblue-dark font-medium">Save data:</span>
										<span class="text-pc-darkblue"
											>{formValues.saveSubmittedData ? 'Enabled' : 'Disabled'}</span
										>

										<!--
										<span class="text-grayblue-dark font-medium">Anonymization:</span>
										<span class="text-pc-darkblue"
											>{formValues.isAnonymous ? 'Enabled' : 'Disabled'}</span
										>
										 -->

										{#if formValues.webhookValue}
											<span class="text-grayblue-dark font-medium">Webhook:</span>
											<span class="text-pc-darkblue">{formValues.webhookValue}</span>
										{/if}

										{#if formValues.denyPageValue}
											<span class="text-grayblue-dark font-medium">Deny Page:</span>
											<span class="text-pc-darkblue">{formValues.denyPageValue}</span>
										{/if}
									</div>
								</div>
							</div>
						</div>
					</FormColumn>
				</FormColumns>
			{/if}
			<FormError message={modalError} />
			<div class="col-span-3 flex justify-between items-center w-full mt-2 border-t py-6">
				{#if currentStep > 1}
					<button
						type="button"
						class="inline-flex items-center px-4 py-2 border border-gray-300 rounded-md shadow-sm text-sm font-medium text-gray-700 bg-white hover:bg-gray-50 focus:outline-none focus:ring-2 focus:ring-offset-2 focus:ring-blue-500"
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
						class="inline-flex items-center px-4 py-2 border border-transparent rounded-md shadow-sm text-sm font-medium text-white bg-blue-600 hover:bg-blue-700 focus:outline-none focus:ring-2 focus:ring-offset-2 focus:ring-blue-500"
						on:click={nextStep}
					>
						Next
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
						{modalMode === 'create' ? 'Create' : 'Update'}
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

	<DeleteAlert
		list={['This will remove statistics related to the campaign and recipients']}
		name={deleteValues.name}
		onClick={() => onClickDelete(deleteValues.id)}
		confirm
		bind:isVisible={isDeleteAlertVisible}
	/>
</main>
