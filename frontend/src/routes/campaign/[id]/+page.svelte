<script>
	import { page } from '$app/stores';
	import { onMount, tick } from 'svelte';
	import { api } from '$lib/api/apiProxy.js';
	import Headline from '$lib/components/Headline.svelte';
	import SubHeadline from '$lib/components/SubHeadline.svelte';
	import { addToast } from '$lib/store/toast';
	import { BiMap } from '$lib/utils/maps';
	import { fetchAllRows } from '$lib/utils/api-utils';
	import { AppStateService } from '$lib/service/appState';
	import ProxySvgIcon from '$lib/components/ProxySvgIcon.svelte';
	import TableRow from '$lib/components/table/TableRow.svelte';
	import TableCell from '$lib/components/table/TableCell.svelte';
	import { formatWeekDays, formatTimeConstraint, timeFormat } from '$lib/utils/date.js';
	import {
		defaultPerPage,
		defaultStartPage,
		newTableURLParams
	} from '$lib/service/tableURLParams.js';
	import Table from '$lib/components/table/Table.svelte';
	import TableCellEmpty from '$lib/components/table/TableCellEmpty.svelte';
	import TableCellAction from '$lib/components/table/TableCellAction.svelte';
	import TableUpdateButton from '$lib/components/table/TableUpdateButton.svelte';
	import TableViewButton from '$lib/components/table/TableViewButton.svelte';
	import Datetime from '$lib/components/Datetime.svelte';
	import HeadTitle from '$lib/components/HeadTitle.svelte';
	import { debounceTyping, onClickCopy } from '$lib/utils/common';
	import { showIsLoading, hideIsLoading } from '$lib/store/loading.js';
	import { toEvent } from '$lib/utils/events';
	import Modal from '$lib/components/Modal.svelte';
	import TableDropDownEllipsis from '$lib/components/table/TableDropDownEllipsis.svelte';
	import TableDropDownButton from '$lib/components/table/TableDropDownButton.svelte';
	import TestLabel from '$lib/components/TestLabel.svelte';
	import AutoRefresh from '$lib/components/AutoRefresh.svelte';
	import StatsCard from '$lib/components/StatsCard.svelte';
	import Alert from '$lib/components/Alert.svelte';
	import EventTimeline from '$lib/components/EventTimeline.svelte';
	import CellCopy from '$lib/components/table/CopyCell.svelte';
	import FormGrid from '$lib/components/FormGrid.svelte';
	import FormColumns from '$lib/components/FormColumns.svelte';
	import FormColumn from '$lib/components/FormColumn.svelte';
	import EventName from '$lib/components/table/EventName.svelte';
	import { goto } from '$app/navigation';
	import { globalButtonDisabledAttributes } from '$lib/utils/form';
	import FileField from '$lib/components/FileField.svelte';

	// services
	const appStateService = AppStateService.instance;

	// bindings
	let campaign = {
		name: null,
		created: null,
		sendStartAt: null,
		sendEndAt: null,
		anonymizedAt: null,
		closeAt: null,
		closedAt: null,
		template: null,
		isTest: false,
		constraintWeekDays: null,
		constraintStartTime: null,
		constraintEndTime: null,
		saveSubmittedData: false,
		isAnonymous: false,

		allowDenyIDs: [],
		webhookID: null,
		// groups by name, must be mapped to IDs before sending to the server
		recipientGroups: [],
		events: [],
		eventTypesIDToNameMap: {},
		notableEventName: ''
	};
	let campaignRecipients = [];
	let recipientEventsRecipient = {
		name: null,
		id: null
	};
	let timelineEvents = [];
	let isTimelineGhost = true;
	let recipientEvents = [];
	let template = null;
	// local state
	let result = {
		recipients: 0,
		emailsSent: 0,
		trackingPixelLoaded: 0,
		websiteLoaded: 0,
		submittedData: 0,
		reported: 0
	};
	// @ts-ignore
	const recipientTableUrlParams = newTableURLParams({
		prefix: 'recipient',
		sortBy: 'send_at',
		noScroll: true
	});
	// @ts-ignore
	const eventsTableURLParams = newTableURLParams({
		sortBy: 'created_at',
		sortOrder: 'desc',
		prefix: 'event',
		noScroll: true
	});
	// @ts-ignore
	const recipientEventsTableParams = newTableURLParams({
		sortBy: 'created_at',
		sortOrder: 'desc',
		prefix: 'event',
		noScroll: true
	});
	const debouncedRefreshRecipientEvents = debounceTyping(() => {
		return setRecipientEvents(recipientEventsRecipient.id);
	});
	let contextCompanyID = null;
	let templateMap = new BiMap({});
	let recipientGroupMap = new BiMap({});
	// self managed campaign are not scheduled for sending
	let isSelfManaged = false;
	let initialPageLoadComplete = false;
	let isEventsModalVisible = false;
	let isTemplateModalVisible = false;
	let isEventTableLoading = false;
	let isRecipientTableLoading = false;
	let isCloseModalVisible = false;
	let isAnonymizeModalVisible = false;
	let isSendMessageModalVisible = false;
	let isSetAsSentModalVisible = false;
	let isStorageAceModalVisible = false;
	let storedCookieData = '';
	let sendMessageRecipient = null;
	let setAsSentRecipient = null;
	let lastPoll3399Nano = '';

	// hooks
	onMount(() => {
		const context = appStateService.getContext();
		if (context) {
			contextCompanyID = context.companyID;
		}
		(async () => {
			await refresh();
			recipientTableUrlParams.onChange(refreshRecipients);
			eventsTableURLParams.onChange(refreshEvents);
			initialPageLoadComplete = true;
			// load graph data
			await refreshRecipientsTimes();
			await refreshCampaignEventsSince();
		})();
		return () => {
			recipientTableUrlParams.unsubscribe();
			eventsTableURLParams.unsubscribe();
		};
	});

	// lastPoll holds the last date we pulled from, so only pull the
	// latest events and add them to the graph
	const lastPoll = null;
	const refresh = async (showLoading = true) => {
		if (showLoading) {
			showIsLoading();
		}
		const templates = await fetchAllRows((options) => {
			return api.campaignTemplate.getAll(options, contextCompanyID);
		});
		templateMap = BiMap.FromArrayOfObjects(templates);
		const recipientGroups = await fetchAllRows((options) => {
			return api.recipient.getAllGroups(options, contextCompanyID);
		});
		recipientGroupMap = BiMap.FromArrayOfObjects(recipientGroups);

		await setResults();
		await setEventType();
		await setCampaign();
		await refreshCampaignRecipients();
		await getEvents();
		await refreshCampaignEventsSince();
		if (showLoading) {
			hideIsLoading();
		}
	};

	const setEventType = async () => {
		try {
			const res = await api.campaign.getAllEventTypes();
			if (!res.success) {
				addToast('Failed to load event types', 'Error');
				console.error('failed to load event types', res.error);
				return;
			}
			res.data.map((t) => (campaign.eventTypesIDToNameMap[t.id] = t.name));
		} catch (e) {
			addToast('Failed to load event types', 'Error');
			console.error('failed to load event types', e);
		}
	};

	// component logic
	const setCampaign = async () => {
		try {
			const t = await getCampaign();
			campaign.name = t.name;
			campaign.createdAt = t.createdAt;
			campaign.sendStartAt = t.sendStartAt;
			campaign.sendEndAt = t.sendEndAt;
			campaign.anonymizedAt = t.anonymizedAt;
			campaign.anonymizeAt = t.anonymizeAt;
			campaign.closeAt = t.closeAt;
			campaign.closedAt = t.closedAt;
			campaign.isTest = t.isTest;
			campaign.constraintWeekDays = t.constraintWeekDays;
			campaign.constraintStartTime = t.constraintStartTime;
			campaign.constraintEndTime = t.constraintEndTime;
			campaign.saveSubmittedData = t.saveSubmittedData;
			campaign.isAnonymous = t.isAnonymous;
			campaign.allowDeny = t.allowDeny;
			campaign.denyPage = t.denyPage;
			campaign.evasionPage = t.evasionPage;
			campaign.webhookID = t.webhookID;
			campaign.template = templateMap.byKey(t.templateID);
			campaign.recipientGroups = t.recipientGroupIDs.map((id) => recipientGroupMap.byKey(id));
			campaign.notableEventName = t.notableEventName;
			if (t.sendStartAt === null && t.sendEndAt === null) {
				isSelfManaged = true;
			}
		} catch (e) {
			addToast('Failed to load campaign', 'Error');
			console.error('failed to load campaign', e);
		}
	};

	const getCampaign = async () => {
		try {
			const res = await api.campaign.getByID($page.params.id);
			if (!res.success) {
				throw res.error;
			}
			return res.data;
		} catch (e) {
			addToast('Failed to load campaign', 'Error');
			console.error('failed to load campaign', e);
		}
	};

	/**
	 * @param {string} recipientID
	 */
	const setRecipientEvents = async (recipientID) => {
		try {
			const res = await api.recipient.getEvents(
				recipientID,
				recipientEventsTableParams,
				$page.params.id
			);
			if (res.success) {
				recipientEvents = res.data.rows;
				return;
			}
			throw res.error;
		} catch (e) {
			addToast('failed to load recipient events', 'Error');
			console.error('failed to load recipient events', e);
		}
	};

	const refreshRecipientsTimes = async () => {
		try {
			/* does not implement the Result<T>
			let rows = await fetchAllRows(
				(options) => api.campaign.getAllCampaignRecipients($page.params.id, options),
				{ ...defaultOptions, sortBy: 'created_at' }
			);
			*/
			const res = await api.campaign.getAllCampaignRecipients($page.params.id, null);
			if (!res.success) {
				throw res.error;
			}
			const events = res.data.map((v) => ({
				createdAt: v.sendAt,
				eventName: 'campaign_recipient_scheduled',
				recipient: v.recipient
			}));
			timelineEvents = [...timelineEvents, ...events.filter((v) => v.createdAt)];
		} catch (e) {
			addToast('failed to recipient schedule', 'Error');
			console.error('failed to load recipient schedule', e);
		}
	};

	const refreshCampaignEventsSince = async () => {
		try {
			if (!lastPoll3399Nano?.length) {
				lastPoll3399Nano = campaign.createdAt; // must be loaded before method is called
			}
			let rows = await fetchAllRows(
				(options) =>
					api.campaign.getAllEventsByCampaignID($page.params.id, options, lastPoll3399Nano),
				{
					currentPage: 1,
					perPage: 200,
					sortBy: 'created_at',
					sortOrder: 'asc',
					search: ''
				}
			);
			rows.forEach((v) => {
				if (v.createdAt > lastPoll3399Nano) {
					lastPoll3399Nano = v.createdAt;
				}
			});
			rows = rows.map((v) => ({
				...v,
				eventName: campaign.eventTypesIDToNameMap[v.eventID]
			}));

			// Only update timelineEvents, not the main events table
			timelineEvents = [...timelineEvents, ...rows];
			isTimelineGhost = false;

			// Don't update campaign.events here - that should only happen via getEvents()
		} catch (e) {
			addToast('failed to load events', 'Error');
			console.error('failed to load events since', e);
		}
	};

	/**
	 * @param {string} id
	 */
	const setTemplate = async (id) => {
		try {
			const res = await api.campaignTemplate.getByID(id, true);
			if (!res.success) {
				throw res.error;
			}
			template = res.data;
			return;
		} catch (e) {
			addToast('Failed to load recipient events', 'Error');
			console.error('failed to load recipient events', e);
		}
	};

	const setResults = async () => {
		try {
			const res = await api.campaign.getResultStats($page.params.id);
			if (!res.success) {
				throw res.error;
			}
			result.recipients = res.data.recipients;
			result.emailsSent = res.data.emailsSent;
			result.trackingPixelLoaded = res.data.trackingPixelLoaded;
			result.websiteLoaded = res.data.clickedLink;
			result.submittedData = res.data.submittedData;
			result.reported = res.data.reported;
		} catch (e) {
			addToast('Failed to load campaign result stats', 'Error');
			console.error('failed to load campaign result stats', e);
		}
	};

	/** @param {string} campaignRecipientID */
	const onClickCopyEmail = async (campaignRecipientID) => {
		try {
			const res = await api.campaign.getEmail(campaignRecipientID);
			if (!res.success) {
				throw res.error;
			}
			const blobText = new Blob([res.data], { type: 'text/plain' });
			const blobHtml = new Blob([res.data], { type: 'text/html' });
			const data = [
				new ClipboardItem({
					'text/plain': blobText,
					'text/html': blobHtml
				})
			];
			await navigator.clipboard.write(data);
			addToast('Email copied to clipboard', 'Success');
		} catch (e) {
			// handle missing template part
			if (e.includes('has no')) {
				addToast('Campaign template is incomplete', 'Error');
			} else {
				addToast('Failed to copy email', 'Error');
			}
			console.error('failed to copy email to clipboard', e);
		}
	};

	/** @param {string} campaignRecipientID */
	const onClickPreviewEmail = async (campaignRecipientID) => {
		try {
			const res = await api.campaign.getEmail(campaignRecipientID);
			if (!res.success) {
				throw res.error;
			}
			// open email in new tab as a blob
			const blob = new Blob([res.data], { type: 'text/html' });
			const url = URL.createObjectURL(blob);
			window.open(url, '_blank');
		} catch (e) {
			if (e.includes('has no')) {
				addToast('Campaign template is incomplete', 'Error');
			} else {
				addToast('Failed to preview email', 'Error');
			}
			console.error('failed to preview email', e);
		}
	};

	/** @param {string} recipientID */
	const openEventsModal = async (recipientID) => {
		recipientEventsTableParams.onChange(debouncedRefreshRecipientEvents);
		recipientEventsRecipient = {
			name: null,
			id: recipientID
		};
		try {
			showIsLoading();
			await setRecipientEvents(recipientID);
			isEventsModalVisible = true;
		} catch (e) {
			addToast('Failed to get recipient events', 'Error');
			console.error('failed to recipient events', e);
		} finally {
			hideIsLoading();
		}
	};

	const closeEventsModal = () => {
		recipientEventsTableParams.unsubscribe();
		recipientEventsTableParams.search = '';
		recipientEventsTableParams.sort('created_at', 'desc');
		recipientEventsTableParams.page = defaultStartPage;
		recipientEventsTableParams.perPage = defaultPerPage;

		recipientEvents = [];
		recipientEventsRecipient = {
			name: null,
			id: null
		};
		isEventsModalVisible = false;
	};

	const openTemplateModal = async (id) => {
		try {
			showIsLoading();
			await setTemplate(id);
			isTemplateModalVisible = true;
		} catch (e) {
			addToast('Failed to get template details', 'Error');
			console.error('failed to template details', e);
		} finally {
			hideIsLoading();
		}
		isTemplateModalVisible = true;
	};

	const closeTemplateModal = () => {
		isTemplateModalVisible = false;
		template = null;
	};

	/** @param {string} campaignRecipientID */
	const onClickCopyURL = async (campaignRecipientID) => {
		try {
			const res = await api.campaign.getURL(campaignRecipientID);
			if (!res.success) {
				throw res.error;
			}
			navigator.clipboard.writeText(res.data);
			addToast('Landing page URL copied to clipboard', 'Success');
		} catch (e) {
			if (e.includes('has no')) {
				addToast('Campaign template is incomplete', 'Error');
			} else {
				addToast('Failed to copy landing page URL to clipboard', 'Error');
			}
			console.error('failed to copy landing page URL to clipboard', e);
		}
	};

	/** @param {string} campaignRecipientID @param {Object} recipient */
	const onClickSetEmailSent = (campaignRecipientID, recipient) => {
		showSetAsSentModal(campaignRecipientID, recipient);
	};

	const onConfirmSetAsSent = async () => {
		try {
			showIsLoading();
			const res = await api.campaign.setEmailSent(setAsSentRecipient.id);
			if (!res.success) {
				throw res.error;
			}
			addToast('Email sent', 'Success');
			await setCampaign();
			await getEvents();
			await refreshCampaignRecipients();
			closeSetAsSentModal();
			return { success: true };
		} catch (e) {
			addToast('Failed to set email sent', 'Error');
			console.error('failed to set email sent', e);
			throw e;
		} finally {
			hideIsLoading();
		}
	};

	/** @param {string} campaignRecipientID @param {Object} recipient */
	const showSendMessageModal = (campaignRecipientID, recipient) => {
		sendMessageRecipient = {
			id: campaignRecipientID,
			name: `${recipient.firstName || ''} ${recipient.lastName || ''}`.trim(),
			email: recipient.email
		};
		isSendMessageModalVisible = true;
	};

	const closeSendMessageModal = () => {
		isSendMessageModalVisible = false;
		sendMessageRecipient = null;
	};

	/** @param {string} campaignRecipientID @param {Object} recipient */
	const showSetAsSentModal = (campaignRecipientID, recipient) => {
		setAsSentRecipient = {
			id: campaignRecipientID,
			name: `${recipient.firstName || ''} ${recipient.lastName || ''}`.trim(),
			email: recipient.email
		};
		isSetAsSentModalVisible = true;
	};

	const closeSetAsSentModal = () => {
		isSetAsSentModalVisible = false;
		setAsSentRecipient = null;
	};

	// helper function to get appropriate messaging based on sender type
	const getMessageType = () => {
		return campaign.template?.email ? 'email' : 'message';
	};

	const onConfirmSendMessage = async () => {
		try {
			showIsLoading();
			// Check if this is a resend before sending
			const isResend = campaignRecipients.find((r) => r.id === sendMessageRecipient.id)?.sentAt;
			const res = await api.campaign.sendMessage(sendMessageRecipient.id);
			if (!res.success) {
				throw res.error;
			}
			const messageType = getMessageType();
			const message = isResend
				? `${messageType.charAt(0).toUpperCase() + messageType.slice(1)} sent again successfully`
				: `${messageType.charAt(0).toUpperCase() + messageType.slice(1)} sent successfully`;
			addToast(message, 'Success');
			await setCampaign();
			await getEvents();
			await refreshCampaignRecipients();
			closeSendMessageModal();
			return { success: true };
		} catch (e) {
			addToast(`Failed to send ${getMessageType()}`, 'Error');
			console.error(`failed to send ${getMessageType()}`, e);
			throw e;
		} finally {
			hideIsLoading();
		}
	};

	// reactive statement to clean up send message modal state when it closes
	$: if (!isSendMessageModalVisible && sendMessageRecipient) {
		sendMessageRecipient = null;
	}

	// reactive statement to clean up set as sent modal state when it closes
	$: if (!isSetAsSentModalVisible && setAsSentRecipient) {
		setAsSentRecipient = null;
	}

	const showCloseCampaignModal = () => {
		isCloseModalVisible = true;
	};

	const closeCloseCampaignModal = () => {
		isCloseModalVisible = false;
	};

	const showAnonymizeModal = () => {
		isAnonymizeModalVisible = true;
	};

	const closeAnonymizeModal = () => {
		isAnonymizeModalVisible = false;
	};

	const closeStorageAceModal = () => {
		isStorageAceModalVisible = false;
		storedCookieData = '';
	};

	const onStorageAceModalOk = () => {
		closeStorageAceModal();
	};

	/** @param {string} eventData @param {string} eventName */
	const onClickCopyEventData = async (eventData, eventName) => {
		try {
			// remove the cookie emoji prefix before copying
			const dataWithoutEmoji = eventData.startsWith('🍪 ') ? eventData.substring(2) : eventData;
			await navigator.clipboard.writeText(dataWithoutEmoji);

			if (eventName === 'campaign_recipient_submitted_data' && eventData.startsWith('🍪')) {
				storedCookieData = eventData;
				isStorageAceModalVisible = true;
			}

			addToast('Copied to clipboard', 'Success');
		} catch (e) {
			addToast('Failed to copy data to clipboard', 'Error');
			console.error('failed to copy data to clipboard', e);
		}
	};

	const onClickCopyCookies = async () => {
		try {
			// remove the cookie emoji prefix before copying
			const dataWithoutEmoji = storedCookieData.startsWith('🍪 ')
				? storedCookieData.substring(2)
				: storedCookieData;
			await navigator.clipboard.writeText(dataWithoutEmoji);
			addToast('Copied to clipboard', 'Success');
		} catch (e) {
			addToast('Failed to copy cookie data', 'Error');
			console.error('failed to copy cookie data', e);
		}
	};

	const onConfirmCloseCampaign = async (a) => {
		let res;
		try {
			showIsLoading();
			res = await api.campaign.close($page.params.id);
			if (!res.success) {
				throw res.error;
			}
			await setCampaign();
			await getEvents();
			await refreshCampaignRecipients();
			addToast('Campaign closed', 'Success');
			closeCloseCampaignModal();
		} catch (e) {
			addToast('Failed to close campaign', 'Error');
			console.error('failed to close campaign', e);
		} finally {
			hideIsLoading();
		}
		return res;
	};

	const onClickCloseCampaign = async () => {
		try {
			showIsLoading();
			const res = await api.campaign.close($page.params.id);
			if (!res.success) {
				throw res.error;
			}
			addToast('Campaign closed', 'Success');
			await setCampaign();
			await getEvents();
			await refreshCampaignRecipients();
		} catch (e) {
			addToast('Failed to close campaign', 'Error');
			console.error('failed to close campaign', e);
		} finally {
			hideIsLoading();
		}
	};

	const onConfirmAnonymize = async () => {
		let res;
		try {
			showIsLoading();
			res = await api.campaign.anonymize($page.params.id);
			if (!res.success) {
				throw res.error;
			}
			await setCampaign();
			await getEvents();
			await refreshCampaignRecipients();
			// bug: have to clear ref and wait a tick or svelte does not re-render
			timelineEvents = [];
			await tick();
			await refreshRecipientsTimes();
			await refreshCampaignEventsSince();
			closeAnonymizeModal();
			addToast('Campaign anonymized', 'Success');
		} catch (e) {
			addToast('Failed to anonymize campaign', 'Error');
			console.error('failed to anonymize campaign', e);
		} finally {
			hideIsLoading();
		}
		return res;
	};

	const onClickExportEvents = async () => {
		try {
			showIsLoading();
			api.campaign.exportEvents($page.params.id);
		} catch (e) {
			addToast('Failed to export campagin events', 'Error');
			console.error('failed to export campaign events', e);
		} finally {
			hideIsLoading();
		}
	};

	const onClickExportSubmissions = async () => {
		try {
			showIsLoading();
			api.campaign.exportSubmissions($page.params.id);
		} catch (e) {
			addToast('Failed to export campaign submissions', 'Error');
			console.error('failed to export campaign submissions', e);
		} finally {
			hideIsLoading();
		}
	};

	const refreshRecipients = async (showIsLoading = true) => {
		try {
			if (showIsLoading) {
				isRecipientTableLoading = true;
			}
			await refreshCampaignRecipients();
		} catch (e) {
			addToast('Failed to load recipients', 'Error');
			console.error('failed to load recipients', e);
		} finally {
			if (showIsLoading) {
				isRecipientTableLoading = false;
			}
		}
	};

	const refreshCampaignRecipients = async () => {
		try {
			const res = await api.campaign.getAllCampaignRecipients(
				$page.params.id,
				recipientTableUrlParams
			);
			if (!res.success) {
				throw res.error;
			}
			campaignRecipients = res.data;
		} catch (e) {
			addToast('Failed to load recipients', 'Error');
			console.error('failed to load recipients', e);
		}
	};

	const refreshEvents = async (showIsLoading = true) => {
		try {
			if (showIsLoading) {
				isEventTableLoading = true;
			}
			await getEvents();
		} finally {
			if (showIsLoading) {
				isEventTableLoading = false;
			}
		}
	};

	const getEvents = async () => {
		try {
			const res = await api.campaign.getAllEventsByCampaignID(
				$page.params.id,
				eventsTableURLParams
			);
			if (res.success) {
				campaign = { ...campaign, events: res.data.rows };
			}
		} catch (e) {
			addToast('Failed to load events', 'Error');
			console.error('failed to load events', e);
		}
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

	const onClickUpdateCampaign = () => {
		goto(`/campaign?update=${$page.params.id}`);
	};

	const onUploadReportedCSV = async (event) => {
		const file = event.target.files?.[0];
		if (!file) return;

		// validate file type
		if (!file.name.toLowerCase().endsWith('.csv')) {
			addToast('Please select a CSV file', 'Error');
			event.target.value = '';
			return;
		}

		const formData = new FormData();
		formData.append('file', file);

		try {
			showIsLoading();
			const response = await fetch(`/api/v1/campaign/${$page.params.id}/upload/reported`, {
				method: 'POST',
				body: formData,
				credentials: 'include'
			});

			const result = await response.json();

			if (response.ok && result.success) {
				addToast(
					`Successfully processed ${result.data.processed} reported entries${result.data.skipped > 0 ? `, skipped ${result.data.skipped} invalid entries` : ''}`,
					'Success'
				);
				// refresh the stats, events, and recipients table
				await setResults();
				await refreshCampaignRecipients();
				await getEvents();
			} else {
				// handle validation errors
				const errorMessage = result.error || `HTTP ${response.status}`;
				addToast(`Upload failed: ${errorMessage}`, 'Error');
			}
		} catch (error) {
			console.error('Upload error:', error);
			addToast('Network error: Failed to upload CSV', 'Error');
		} finally {
			hideIsLoading();
			// clear the file input
			event.target.value = '';
		}
	};

	// helper function to format cookie capture data
	const formatEventData = (eventData, eventName) => {
		if (!eventData || eventName !== 'campaign_recipient_submitted_data') {
			return eventData;
		}

		try {
			// parse the event data as JSON
			const parsedData = JSON.parse(eventData);

			// check if it's the new cookie bundle format
			if (parsedData.capture_type === 'cookie' && parsedData.cookies) {
				const cookies = [];

				// iterate through each captured cookie
				for (const [captureName, cookieData] of Object.entries(parsedData.cookies)) {
					// convert SameSite attribute to browser extension format
					let sameSite = 'no_restriction';
					if (cookieData.sameSite) {
						switch (cookieData.sameSite.toLowerCase()) {
							case 'strict':
								sameSite = 'strict';
								break;
							case 'lax':
								sameSite = 'lax';
								break;
							case 'none':
								sameSite = 'no_restriction';
								break;
							default:
								sameSite = 'no_restriction';
						}
					}

					// determine if this is a host-only cookie
					const domain = cookieData.domain || '';
					const hostOnly = domain && !domain.startsWith('.');

					// convert to browser extension compatible format
					const browserCookie = {
						domain: domain,
						hostOnly: hostOnly,
						httpOnly: cookieData.httpOnly === 'true',
						name: cookieData.name || '',
						path: cookieData.path || '/',
						sameSite: sameSite,
						secure: cookieData.secure === 'true',
						session: !cookieData.expires && !cookieData.maxAge, // session cookie if no expiration
						storeId: '1',
						value: cookieData.value || ''
					};

					// handle expiration date
					if (cookieData.expires) {
						const expireDate = new Date(cookieData.expires);
						if (!isNaN(expireDate.getTime())) {
							browserCookie.expirationDate = expireDate.getTime() / 1000;
							browserCookie.session = false;
						}
					} else if (cookieData.maxAge) {
						// handle maxAge if present
						const maxAgeSeconds = parseInt(cookieData.maxAge);
						if (!isNaN(maxAgeSeconds)) {
							browserCookie.expirationDate = Date.now() / 1000 + maxAgeSeconds;
							browserCookie.session = false;
						}
					}

					cookies.push(browserCookie);
				}

				// return as array format for browser import with cookie emoji
				return '🍪 ' + JSON.stringify(cookies, null, 2);
			}

			// for other submitted data, return as is
			return eventData;
		} catch (e) {
			// if not valid JSON, return as is
			return eventData;
		}
	};
</script>

<HeadTitle title="Campaign {campaign.name ? ` - ${campaign.name}` : ''}" />

<main>
	{#if initialPageLoadComplete}
		<div class="relative">
			<div class="flex justify-between">
				<Headline
					>Campaign: {campaign.name ?? ''}
					{#if campaign.isTest}
						<TestLabel />
					{/if}
				</Headline>
			</div>
			<AutoRefresh
				isLoading={false}
				onRefresh={async () => {
					await setResults();
					await setCampaign();
					// await refreshCampaignRecipients();

					const res = await api.campaign.getAllCampaignRecipients(
						$page.params.id,
						recipientTableUrlParams
					);
					if (!res.success) {
						throw res.error;
					}
					// bug: svelte does not rerender the usage of campaignRecipients without
					// clearing the ref and a tick
					campaignRecipients = [];
					await tick();
					campaignRecipients = res.data;
					await getEvents();
					await refreshCampaignEventsSince();
				}}
			/>
		</div>

		<div
			class="grid grid-row-1 grid-cols-1 md:grid-cols-2 gap-6 mb-8 mt-4 lg:grid-cols-3 2xl:grid-cols-6"
		>
			<StatsCard
				title="Recipients"
				value={result.recipients}
				borderColor="border-recipients"
				iconColor="text-recipients"
			>
				<svg
					slot="icon"
					xmlns="http://www.w3.org/2000/svg"
					class="h-5 w-5 ml-2"
					fill="none"
					viewBox="0 0 24 24"
					stroke="currentColor"
				>
					<path
						stroke-linecap="round"
						stroke-linejoin="round"
						stroke-width="2"
						d="M12 4.354a4 4 0 110 5.292M15 21H3v-1a6 6 0 0112 0v1zm0 0h6v-1a6 6 0 00-9-5.197M13 7a4 4 0 11-8 0 4 4 0 018 0z"
					/>
				</svg>
			</StatsCard>

			<StatsCard
				title="Emails Sent"
				value={result.emailsSent}
				borderColor="border-message-sent"
				iconColor="text-message-sent"
				percentages={[
					{
						value: Math.round((result.emailsSent / result.recipients) * 100),
						relativeTo: 'of recipients',
						baseValue: result.recipients
					}
				]}
			>
				<svg
					slot="icon"
					xmlns="http://www.w3.org/2000/svg"
					class="h-5 w-5 ml-2"
					fill="none"
					viewBox="0 0 24 24"
					stroke="currentColor"
					stroke-width="1.5"
				>
					<path
						stroke-linecap="round"
						stroke-linejoin="round"
						d="M21.75 6.75v10.5a2.25 2.25 0 01-2.25 2.25h-15a2.25 2.25 0 01-2.25-2.25V6.75m19.5 0A2.25 2.25 0 0019.5 4.5h-15a2.25 2.25 0 00-2.25 2.25m19.5 0v.243a2.25 2.25 0 01-1.07 1.916l-7.5 4.615a2.25 2.25 0 01-2.36 0L3.32 8.91a2.25 2.25 0 01-1.07-1.916V6.75"
					/>
				</svg>
			</StatsCard>
			<StatsCard
				title="Emails Read"
				value={result.trackingPixelLoaded}
				borderColor="border-message-read"
				iconColor="text-message-read"
				percentages={[
					{
						value: Math.round((result.trackingPixelLoaded / result.recipients) * 100),
						relativeTo: 'of recipients',
						baseValue: result.recipients
					},
					{
						value: Math.round((result.trackingPixelLoaded / result.emailsSent) * 100),
						relativeTo: 'of sent',
						baseValue: result.emailsSent
					}
				]}
			>
				<svg
					slot="icon"
					xmlns="http://www.w3.org/2000/svg"
					class="h-5 w-5 ml-2"
					fill="none"
					viewBox="0 0 24 24"
					stroke="currentColor"
					stroke-width="1.5"
				>
					<path
						stroke-linecap="round"
						stroke-linejoin="round"
						d="M2.036 12.322a1.012 1.012 0 010-.639C3.423 7.51 7.36 4.5 12 4.5c4.638 0 8.573 3.007 9.963 7.178.07.207.07.431 0 .639C20.577 16.49 16.64 19.5 12 19.5c-4.638 0-8.573-3.007-9.963-7.178z"
					/>
					<path
						stroke-linecap="round"
						stroke-linejoin="round"
						d="M15 12a3 3 0 11-6 0 3 3 0 016 0z"
					/>
				</svg>
			</StatsCard>

			<StatsCard
				title="Website Visits"
				value={result.websiteLoaded}
				borderColor="border-page-visited"
				iconColor="text-page-visited"
				percentages={[
					{
						value: Math.round((result.websiteLoaded / result.recipients) * 100),
						relativeTo: 'of recipients',
						baseValue: result.recipients
					},
					{
						value: Math.round((result.websiteLoaded / result.emailsSent) * 100),
						relativeTo: 'of sent',
						baseValue: result.emailsSent
					},
					{
						value: Math.round((result.websiteLoaded / result.trackingPixelLoaded) * 100),
						relativeTo: 'of reads',
						baseValue: result.trackingPixelLoaded
					}
				]}
			>
				<svg
					slot="icon"
					xmlns="http://www.w3.org/2000/svg"
					class="h-5 w-5 ml-2"
					fill="none"
					viewBox="0 0 24 24"
					stroke="currentColor"
				>
					<path
						stroke-linecap="round"
						stroke-linejoin="round"
						stroke-width="2"
						d="M21 12a9 9 0 01-9 9m9-9a9 9 0 00-9-9m9 9H3m9 9a9 9 0 01-9-9m9 9c1.657 0 3-4.03 3-9s-1.343-9-3-9m0 18c-1.657 0-3-4.03-3-9s1.343-9 3-9m-9 9a9 9 0 019-9"
					/>
				</svg>
			</StatsCard>

			<StatsCard
				title="Data Submitted"
				value={result.submittedData}
				borderColor="border-submitted-data"
				iconColor="text-submitted-data"
				percentages={[
					{
						value: Math.round((result.submittedData / result.recipients) * 100),
						relativeTo: 'of recipients',
						baseValue: result.recipients
					},
					{
						value: Math.round((result.submittedData / result.emailsSent) * 100),
						relativeTo: 'of sent',
						baseValue: result.emailsSent
					},
					{
						value: Math.round((result.submittedData / result.trackingPixelLoaded) * 100),
						relativeTo: 'of reads',
						baseValue: result.trackingPixelLoaded
					},
					{
						value: Math.round((result.submittedData / result.websiteLoaded) * 100),
						relativeTo: 'of visits',
						baseValue: result.websiteLoaded
					}
				]}
			>
				<svg
					slot="icon"
					xmlns="http://www.w3.org/2000/svg"
					class="h-5 w-5 ml-2"
					fill="none"
					viewBox="0 0 24 24"
					stroke="currentColor"
					stroke-width="1.5"
				>
					<path
						stroke-linecap="round"
						stroke-linejoin="round"
						d="M9 12.75L11.25 15 15 9.75M21 12a9 9 0 11-18 0 9 9 0 0118 0z"
					/>
				</svg>
			</StatsCard>

			<StatsCard
				title="Reported"
				value={result.reported}
				borderColor="border-reported"
				iconColor="text-reported"
				percentages={[
					{
						value: Math.round((result.reported / result.recipients) * 100),
						relativeTo: 'of recipients',
						baseValue: result.recipients
					},
					{
						value: Math.round((result.reported / result.emailsSent) * 100),
						relativeTo: 'of sent',
						baseValue: result.emailsSent
					},
					{
						value: Math.round((result.reported / result.trackingPixelLoaded) * 100),
						relativeTo: 'of reads',
						baseValue: result.trackingPixelLoaded
					},
					{
						value: Math.round((result.reported / result.websiteLoaded) * 100),
						relativeTo: 'of visits',
						baseValue: result.websiteLoaded
					}
				]}
			>
				<svg
					slot="icon"
					xmlns="http://www.w3.org/2000/svg"
					class="h-5 w-5 ml-2"
					fill="none"
					viewBox="0 0 24 24"
					stroke="currentColor"
				>
					<path
						stroke-linecap="round"
						stroke-linejoin="round"
						stroke-width="2"
						d="M12 9v2m0 4h.01m-6.938 4h13.856c1.54 0 2.502-1.667 1.732-2.5L13.732 4c-.77-.833-1.964-.833-2.732 0L4.072 16.5c-.77.833.192 2.5 1.732 2.5z"
					/>
				</svg>
			</StatsCard>
		</div>
		<div class=" mb-6">
			<SubHeadline>Event Timeline</SubHeadline>
			<EventTimeline events={timelineEvents} isGhost={isTimelineGhost} />
		</div>

		<div class=" space-y-6">
			<!-- First Row: Details and Timeline -->
			<div class="grid grid-cols-1 lg:grid-cols-2">
				<!-- Primary Campaign Info -->
				<div class="py-6 rounded-lg">
					<h3 class="text-xl font-semibold text-pc-darkblue dark:text-white mb-4 border-b pb-2">
						Details
					</h3>
					<div class="grid grid-cols-[120px_1fr] gap-y-3">
						<span class="text-grayblue-dark font-medium">Status:</span>
						<span class="text-pc-darkblue dark:text-white font-semibold">
							{toEvent(campaign.notableEventName).name}
						</span>

						<span class="text-grayblue-dark font-medium">Template:</span>
						<span class="text-pc-darkblue dark:text-white">
							<button
								class="cursor-pointer text-cta-blue dark:text-white underline hover:opacity-75"
								on:click={() => {
									openTemplateModal(templateMap.byValue(campaign.template));
								}}
							>
								{campaign.template}
							</button>
						</span>

						<span class="text-grayblue-dark font-medium">Groups:</span>
						<span class="text-pc-darkblue dark:text-white">
							{#each campaign.recipientGroups as group, i}
								<a
									class="hover:underline text-cta-blue dark:text-white hover:opacity-75 underline"
									href="/recipient/group/{recipientGroupMap.byValue(group)}"
									target="_blank">{group}</a
								>{#if i !== (campaign.recipientGroups?.length ?? 0) - 1}
									,&nbsp;
								{/if}
							{/each}
						</span>

						<span class="text-grayblue-dark font-medium">Type:</span>
						<span class="text-pc-darkblue dark:text-white"
							>{isSelfManaged ? 'Self Managed' : 'Scheduled'}</span
						>

						<span class="text-grayblue-dark font-medium">
							{campaign?.allowDeny ? (campaign.allowDeny.allowed ? 'Allow' : 'Deny') : ''}
							IP Filters:
						</span>
						<span class="text-pc-darkblue dark:text-white">
							{#if campaign.allowDeny?.length}
								{#each campaign.allowDeny as allowDeny, i}
									<a
										href="/filter/?edit={allowDeny.id}"
										class="text-cta-blue dark:text-white hover:underline"
										target="_blank"
									>
										{allowDeny.name}
									</a>
									{#if i < campaign.allowDeny.length - 1},
									{/if}
								{/each}
							{:else}
								None
							{/if}
						</span>

						<span class="text-grayblue-dark font-medium">Deny Page:</span>
						<span class="text-pc-darkblue dark:text-white">
							{#if campaign.denyPage}
								{campaign.denyPage.name}
							{:else}
								None
							{/if}
						</span>

						<span class="text-grayblue-dark font-medium">Evasion Page:</span>
						<span class="text-pc-darkblue dark:text-white">
							{#if campaign.evasionPage}
								{campaign.evasionPage.name}
							{:else}
								None
							{/if}
						</span>

						<span class="text-grayblue-dark font-medium">Webhook:</span>
						<span class="text-pc-darkblue dark:text-white">
							{#if campaign.webhookID}
								Yes
							{:else}
								None
							{/if}
						</span>

						<span class="text-grayblue-dark font-medium">Data Saving:</span>
						<span class="text-pc-darkblue dark:text-white">
							{campaign.saveSubmittedData ? 'Enabled' : 'Disabled'}
						</span>

						<span class="text-grayblue-dark font-medium">Test:</span>
						<span class="text-pc-darkblue dark:text-white">{campaign.isTest ? 'Yes' : 'No'}</span>
					</div>
				</div>

				<div class="p-6 rounded-lg">
					<h3 class="text-xl font-semibold text-pc-darkblue dark:text-white mb-4 border-b pb-2">
						Timeline
					</h3>
					<div class="grid grid-cols-[120px_1fr] gap-y-3">
						<span class="text-grayblue-dark font-medium">Created:</span>
						<span class="text-pc-darkblue dark:text-white"
							><Datetime value={campaign.createdAt} /></span
						>

						{#if !isSelfManaged}
							<span class="text-grayblue-dark font-medium">Delivery start:</span>
							<span class="text-pc-darkblue dark:text-white"
								><Datetime value={campaign.sendStartAt} /></span
							>

							<span class="text-grayblue-dark font-medium">Delivery finish:</span>
							<span class="text-pc-darkblue dark:text-white"
								><Datetime value={campaign.sendEndAt} /></span
							>
						{/if}

						<span class="text-grayblue-dark font-medium">Close At:</span>
						<span class="text-pc-darkblue dark:text-white"
							><Datetime value={campaign.closeAt} /></span
						>

						<span class="text-grayblue-dark font-medium">Closed:</span>
						<span class="text-pc-darkblue dark:text-white"
							><Datetime value={campaign.closedAt} /></span
						>

						<span class="text-grayblue-dark font-medium">Anonymize At:</span>
						<span class="text-pc-darkblue dark:text-white"
							><Datetime value={campaign.anonymizeAt} /></span
						>

						<span class="text-grayblue-dark font-medium">Anonymized:</span>
						<span class="text-pc-darkblue dark:text-white"
							><Datetime value={campaign.anonymizedAt} /></span
						>
					</div>

					{#if campaign.constraintWeekDays}
						<div class="py-6 rounded-lg">
							<div class="flex gap-8">
								<!-- Days Schedule -->
								<div class="flex-1">
									<div class="flex items-center justify-between mb-3">
										<span class="text-grayblue-dark font-medium">Schedule:</span>
									</div>
									<div class="flex gap-1.5">
										{#each formatWeekDays(campaign.constraintWeekDays).days as day}
											<div class="relative flex flex-col items-center">
												<div
													class="w-10 h-10 flex items-center justify-center rounded-lg transition-all {day.isActive
														? 'bg-cta-blue text-white font-medium shadow-sm hover:bg-cta-blue'
														: 'bg-slate-50 text-slate-500 border border-slate-200 hover:bg-slate-100'}"
													title={day.full}
												>
													{day.short}
												</div>
											</div>
										{/each}
									</div>
									{#if formatWeekDays(campaign.constraintWeekDays).summary}
										<div class="text-sm text-slate-500 mt-2">
											{formatWeekDays(campaign.constraintWeekDays).summary}
										</div>
									{/if}
								</div>

								<!-- Time Schedule -->
								{#if campaign.constraintStartTime && campaign.constraintEndTime}
									<div class="flex-1">
										<div class="mb-3">
											<span class="text-grayblue-dark font-medium"></span>
											<button
												on:click={() => timeFormat.update((f) => !f)}
												class="text-xs px-2 py-1 rounded border justify-self-start border-slate-200 hover:bg-slate-50 text-slate-600"
											>
												{$timeFormat ? '12h' : '24h'}
											</button>
										</div>
										<div class="flex items-center gap-3">
											<div
												class="bg-cta-blue text-white w-16 text-center h-9 py-1 px-2 rounded-lg shadow-sm font-medium"
												class:w-24={!$timeFormat}
											>
												{formatTimeConstraint(campaign.constraintStartTime, $timeFormat)}
											</div>
											<svg
												xmlns="http://www.w3.org/2000/svg"
												class="h-5 w-5 text-slate-400"
												viewBox="0 0 20 20"
												fill="currentColor"
											>
												<path
													fill-rule="evenodd"
													d="M12.293 5.293a1 1 0 011.414 0l4 4a1 1 0 010 1.414l-4 4a1 1 0 01-1.414-1.414L14.586 11H3a1 1 0 110-2h11.586l-2.293-2.293a1 1 0 010-1.414z"
													clip-rule="evenodd"
												/>
											</svg>
											<div
												class="h-9 bg-cta-blue text-white text-center w-16 py-1 px-2 rounded-lg shadow-sm font-medium"
												class:w-24={!$timeFormat}
											>
												{formatTimeConstraint(campaign.constraintEndTime, $timeFormat)}
											</div>
										</div>
									</div>
								{/if}
							</div>
						</div>
						<div>&nbsp;</div>
					{/if}
				</div>
			</div>
			<!-- Second Row: Schedule Constraints and Actions -->
			<div class="grid grid-cols-1 lg:grid-cols-2 gap-6">
				<div></div>
				<div class="p-6 rounded-lg">
					<h3 class="text-xl font-semibold text-pc-darkblue dark:text-white mb-4 border-b pb-2">
						Actions
					</h3>
					<div class="space-y-4">
						<!-- Action Buttons -->
						<div class="flex flex-wrap gap-3">
							{#if !campaignUpdateDisabledAndTitle(campaign).disabled}
								<button
									on:click={onClickUpdateCampaign}
									class="px-4 py-2 bg-gradient-to-r from-blue-500 to-blue-600 hover:opacity-80 text-white rounded-md transition-colors"
								>
									Update
								</button>
							{/if}
							<button
								on:click={showCloseCampaignModal}
								disabled={!!campaign.closedAt}
								class="px-4 py-2 bg-gradient-to-r from-pc-red to-repeart-submissions hover:opacity-80 text-white rounded-md disabled:opacity-10 disabled:cursor-not-allowed transition-colors"
							>
								Close
							</button>
							<button
								on:click={showAnonymizeModal}
								disabled={!!campaign.anonymizedAt}
								class="px-4 py-2 bg-gradient-to-r from-pc-red to-repeart-submissions hover:opacity-80 text-white rounded-md disabled:opacity-10 disabled:cursor-not-allowed transition-colors"
							>
								Anonymize
							</button>
						</div>

						<!-- Export Buttons -->
						<div class="flex flex-wrap gap-3">
							<button
								on:click={onClickExportEvents}
								class="px-4 py-2 bg-gradient-to-r from-campaign-active to-campaign-scheduled hover:opacity-80 text-white rounded-md disabled:opacity-10 disabled:cursor-not-allowed transition-colors"
							>
								Export events
							</button>
							<button
								on:click={onClickExportSubmissions}
								class="px-4 py-2 bg-gradient-to-r from-campaign-active to-campaign-scheduled hover:opacity-80 text-white rounded-md disabled:opacity-10 disabled:cursor-not-allowed transition-colors"
							>
								Export submitters
							</button>
						</div>

						<!-- Upload Section -->
						<div class="border-t pt-4">
							<div>
								<FileField accept=".csv" on:change={onUploadReportedCSV}>
									Upload Reported CSV
								</FileField>
								<p class="mt-1 text-xs text-gray-500 dark:text-gray-400">
									CSV format: "Reported by" (email), "Date reported(UTC+02:00)"
								</p>
							</div>
						</div>
					</div>
				</div>
			</div>
		</div>

		<div class="mt-8">
			<SubHeadline>Events</SubHeadline>
			<Table
				columns={[
					{ column: 'Created at', size: 'small' },
					{ column: 'First name', size: 'medium' },
					{ column: 'Last name', size: 'medium' },
					{ column: 'Email', size: 'medium' },
					{ column: 'Event', size: 'small' },
					{ column: 'Details', size: 'small' },
					{ column: 'User-Agent', size: 'small' },
					{ column: 'Ip', size: 'small' }
				]}
				sortable={[
					'Created at',
					'First name',
					'Last name',
					'Email',
					'Event',
					'Details',
					'User-Agent',
					'Ip'
				]}
				pagination={eventsTableURLParams}
				plural="events"
				hasData={!!campaign.events.length}
				hasActions={false}
				isGhost={isEventTableLoading}
			>
				{#each campaign.events as event (event.id)}
					<TableRow>
						<TableCell isDate value={event.createdAt} />
						<TableCell>
							{#if event.recipient?.firstName}
								<a href={`/recipient/${event.recipient.id}`} class="block w-full py-1">
									{event.recipient.firstName}
								</a>
							{/if}
						</TableCell>
						<TableCell>
							{#if event.recipient?.lastName}
								<a href={`/recipient/${event.recipient.id}`} class="block w-full py-1">
									{event.recipient.lastName}
								</a>
							{/if}
						</TableCell>
						<TableCell>
							{#if event.recipient?.email}
								<a href={`/recipient/${event.recipient.id}`} class="block w-full py-1">
									{event.recipient.email}
								</a>
							{/if}
						</TableCell>
						<TableCell>
							<EventName eventName={campaign.eventTypesIDToNameMap[event.eventID]} />
						</TableCell>
						<TableCell>
							{#if campaign.eventTypesIDToNameMap[event.eventID] === 'campaign_recipient_submitted_data' && formatEventData(event.data, campaign.eventTypesIDToNameMap[event.eventID]).startsWith('🍪')}
								<button
									class="hover:bg-gray-100 dark:hover:bg-gray-700 px-2 py-1 rounded-md transition-colors w-full text-left text-ellipsis overflow-hidden text-gray-900 dark:text-gray-100"
									title={formatEventData(event.data, campaign.eventTypesIDToNameMap[event.eventID])}
									on:click={() =>
										onClickCopyEventData(
											formatEventData(event.data, campaign.eventTypesIDToNameMap[event.eventID]),
											campaign.eventTypesIDToNameMap[event.eventID]
										)}
								>
									{formatEventData(event.data, campaign.eventTypesIDToNameMap[event.eventID])}
								</button>
							{:else}
								<CellCopy
									text={formatEventData(event.data, campaign.eventTypesIDToNameMap[event.eventID])}
								/>
							{/if}
						</TableCell>
						<TableCell>
							<CellCopy text={event.userAgent} />
						</TableCell>
						<TableCell>
							<CellCopy text={event.ip} />
						</TableCell>
					</TableRow>
				{/each}
			</Table>
		</div>
		<SubHeadline>Recipients overview</SubHeadline>
		<Table
			columns={[
				{ column: 'First name', size: 'small' },
				{ column: 'Last name', size: 'small' },
				{ column: 'Email', size: 'large' },
				{ column: 'Status', size: 'small' },
				{ column: 'Send at', title: 'Scheduled', size: 'small' },
				{ column: 'Sent at', title: 'Delivered', size: 'small' },
				{ column: 'Cancelled at', size: 'small' }
			]}
			sortable={[
				'First name',
				'Last name',
				'Email',
				'Status',
				'Send at',
				'Sent at',
				'Cancelled at'
			]}
			pagination={recipientTableUrlParams}
			plural="recipients"
			hasData={!!campaignRecipients.length}
			isGhost={isRecipientTableLoading}
		>
			{#each campaignRecipients as recp (recp.id)}
				<TableRow>
					{#if recp?.anonymizedID}
						<TableCell value={'anonymized'} />
						<TableCell value={'anonymized'} />
						<TableCell value={'anonymized'} />
					{:else}
						<TableCell>
							<button
								on:click={() => openEventsModal(recp.recipientID)}
								class="block w-full py-1 text-left"
							>
								{recp.recipient.firstName}
							</button>
						</TableCell>
						<TableCell>
							<button
								on:click={() => openEventsModal(recp.recipientID)}
								class="block w-full py-1 text-left"
							>
								{recp.recipient.lastName}
							</button>
						</TableCell>
						<TableCell>
							{#if recp?.recipient?.email}
								<button
									on:click={() => openEventsModal(recp.recipientID)}
									class="block w-full py-1 text-left"
								>
									{recp.recipient.email}
								</button>
							{/if}
						</TableCell>
					{/if}
					<TableCell>
						<EventName eventName={recp?.notableEventName} />
					</TableCell>
					<TableCell value={recp?.sendAt} isDate />
					<TableCell value={recp?.sentAt} isDate />
					<TableCell value={recp?.cancelledAt} isDate />
					{#if !campaign.sentAt}
						<TableCellEmpty />
						<TableCellAction>
							<TableDropDownEllipsis>
								<TableUpdateButton
									name="Copy email"
									disabled={!!campaign.closedAt || !!campaign.anonymizedAt}
									on:click={() => onClickCopyEmail(recp.id)}
								/>
								<TableViewButton
									name="View email"
									disabled={!!campaign.closedAt || !!campaign.anonymizedAt || !recp.recipient}
									on:click={() => onClickPreviewEmail(recp.id)}
								/>
								<TableUpdateButton
									name="Copy lure URL"
									disabled={!!campaign.closedAt || !!campaign.anonymizedAt || !recp.recipient}
									on:click={() => onClickCopyURL(recp.id)}
								/>

								<TableViewButton
									name="Events"
									disabled={!recp.recipient}
									on:click={() => openEventsModal(recp.recipientID)}
								/>
								<TableDropDownButton
									name={recp.sentAt ? `Send ${getMessageType()} again` : `Send ${getMessageType()}`}
									title={recp.closedAt
										? 'Campaign is closed'
										: recp.cancelledAt
											? 'Recipient cancelled'
											: recp.sentAt
												? `Send ${getMessageType()} again (last sent: ${new Date(recp.sentAt).toLocaleDateString()})`
												: `Send ${getMessageType()} to recipient`}
									on:click={() => showSendMessageModal(recp.id, recp.recipient)}
									disabled={!!campaign.closedAt || recp.cancelledAt}
								/>
								{#if !campaign.sendStartAt}
									<!-- self managed campaign -->
									<TableDropDownButton
										name="Set as sent"
										title={recp.closedAt ? 'Campaign is closed' : ''}
										on:click={() => onClickSetEmailSent(recp.id, recp.recipient)}
										disabled={!!campaign.closedAt || recp.cancelledAt}
									/>
								{/if}
							</TableDropDownEllipsis>
						</TableCellAction>
					{/if}
				</TableRow>
			{/each}
		</Table>
	{/if}
	<Modal headerText={'Events'} visible={isEventsModalVisible} onClose={closeEventsModal}>
		<div class="mt-8"></div>
		<Table
			columns={[
				{ column: 'Created at', size: 'small' },
				{ column: 'Event', size: 'small' },
				{ column: 'Details', size: 'small' },
				{ column: 'User-Agent', size: 'small' },
				{ column: 'Ip', size: 'small' }
			]}
			sortable={['Created at', 'Event', 'Details', 'User-Agent', 'Ip']}
			pagination={recipientEventsTableParams}
			plural="events"
			hasData={!!recipientEvents.length}
			hasActions={false}
		>
			{#each recipientEvents as event}
				<TableRow>
					<TableCell isDate value={event.createdAt} />
					<TableCell>
						<EventName eventName={campaign.eventTypesIDToNameMap[event.eventID]} />
					</TableCell>
					<TableCell>
						{#if campaign.eventTypesIDToNameMap[event.eventID] === 'campaign_recipient_submitted_data' && formatEventData(event.data, campaign.eventTypesIDToNameMap[event.eventID]).startsWith('🍪')}
							<button
								class="hover:bg-gray-100 dark:hover:bg-gray-700 px-2 py-1 rounded-md transition-colors w-full text-left text-ellipsis overflow-hidden text-gray-900 dark:text-gray-100"
								title={formatEventData(event.data, campaign.eventTypesIDToNameMap[event.eventID])}
								on:click={() =>
									onClickCopyEventData(
										formatEventData(event.data, campaign.eventTypesIDToNameMap[event.eventID]),
										campaign.eventTypesIDToNameMap[event.eventID]
									)}
							>
								{formatEventData(event.data, campaign.eventTypesIDToNameMap[event.eventID])}
							</button>
						{:else}
							<CellCopy
								text={formatEventData(event.data, campaign.eventTypesIDToNameMap[event.eventID])}
							/>
						{/if}
					</TableCell>
					<TableCell>
						<CellCopy text={event.userAgent} />
					</TableCell>
					<TableCell>
						<CellCopy text={event.ip} />
					</TableCell>
				</TableRow>
			{/each}
		</Table>
	</Modal>

	<Modal
		headerText={'Template Details'}
		visible={isTemplateModalVisible}
		onClose={closeTemplateModal}
	>
		<div class="space-y-6">
			<!-- Full-width Landing Page Flow -->
			<div class="p-6 rounded-lg">
				<h3 class="text-xl font-semibold text-pc-darkblue dark:text-white mb-4 border-b pb-2">
					Phishing Flow
				</h3>

				<!-- Enhanced Flow Visualization -->
				<div class="flex items-center justify-center mb-6 text-sm">
					<div class="flex items-center flex-wrap justify-center gap-2">
						<!-- First block is always the delivery method -->
						<div class="text-center px-3 py-2 bg-pc-lightblue dark:bg-blue-600 rounded">
							<div class="font-medium text-gray-800 dark:text-white">
								{#if template.email}
									Email
								{:else}
									API
								{/if}
							</div>
						</div>

						<!-- Only show arrow if there's a destination -->
						{#if template.beforeLandingPage || template.beforeLandingProxy || template.landingPage || template.landingProxy}
							<div class="mx-2">→</div>
						{/if}

						<!-- Before Landing -->
						{#if template.beforeLandingPage}
							<div class="text-center px-3 py-2 bg-pc-lightblue dark:bg-blue-600 rounded">
								<div class="font-medium text-gray-800 dark:text-white">Before Landing</div>
							</div>
							<!-- Only show arrow if there's a next step -->
							{#if template.landingPage || template.landingProxy}
								<div class="mx-2">→</div>
							{/if}
						{:else if template.beforeLandingProxy}
							<div class="text-center px-3 py-2 bg-pc-lightblue dark:bg-blue-600 rounded">
								<div
									class="font-medium text-gray-800 dark:text-white flex items-center justify-center gap-1"
								>
									<ProxySvgIcon size="w-4 h-4" /> Before
								</div>
							</div>
							<!-- Only show arrow if there's a next step -->
							{#if template.landingPage || template.landingProxy}
								<div class="mx-2">→</div>
							{/if}
						{/if}

						<!-- Main Landing -->
						{#if template.landingPage}
							<div class="text-center px-3 py-2 bg-pc-lightblue dark:bg-blue-600 rounded">
								<div class="font-medium text-gray-800 dark:text-white">Main Landing</div>
							</div>
							<!-- Only show arrow if there's a next step -->
							{#if template.afterLandingPage || template.afterLandingProxy || template.afterLandingPageRedirectURL}
								<div class="mx-2">→</div>
							{/if}
						{:else if template.landingProxy}
							<div class="text-center px-3 py-2 bg-pc-lightblue dark:bg-blue-600 rounded">
								<div
									class="font-medium text-gray-800 dark:text-white flex items-center justify-center gap-1"
								>
									<ProxySvgIcon size="w-4 h-4" /> Main
								</div>
							</div>
							<!-- Only show arrow if there's a next step -->
							{#if template.afterLandingPage || template.afterLandingProxy || template.afterLandingPageRedirectURL}
								<div class="mx-2">→</div>
							{/if}
						{/if}

						<!-- After Landing or Redirect -->
						{#if template.afterLandingPage}
							<div class="text-center px-3 py-2 bg-pc-lightblue dark:bg-blue-600 rounded">
								<div class="font-medium text-gray-800 dark:text-white">After Landing</div>
							</div>
						{:else if template.afterLandingProxy}
							<div class="text-center px-3 py-2 bg-pc-lightblue dark:bg-blue-600 rounded">
								<div
									class="font-medium text-gray-800 dark:text-white flex items-center justify-center gap-1"
								>
									<ProxySvgIcon size="w-4 h-4" /> After
								</div>
							</div>
						{/if}
						{#if template.afterLandingPageRedirectURL}
							<div class="mx-2">→</div>
							<div class="text-center px-3 py-2 bg-pc-lightorange dark:bg-orange-600 rounded">
								<div class="font-medium text-gray-800 dark:text-white">Redirect</div>
							</div>
						{/if}
					</div>
				</div>
			</div>

			<!-- Basic Info and Email Config -->
			<div class="grid grid-cols-2 gap-6">
				<!-- Basic Info Section -->
				<div class="p-6 rounded-lg">
					<h3 class="text-xl font-semibold text-pc-darkblue dark:text-white mb-4 border-b pb-2">
						Basic Information
					</h3>
					<div class="grid grid-cols-[120px_1fr] gap-y-3">
						<span class="text-grayblue-dark font-medium">Name:</span>
						<span class="text-pc-darkblue dark:text-white">{template.name ?? ''}</span>

						<span class="text-grayblue-dark font-medium">Query Key:</span>
						<span class="text-pc-darkblue dark:text-white"
							>{template.urlIdentifier?.name ?? ''}</span
						>

						<span class="text-grayblue-dark font-medium">State Key:</span>
						<span class="text-pc-darkblue dark:text-white"
							>{template.stateIdentifier?.name ?? ''}</span
						>

						<span class="text-grayblue-dark font-medium">Delivery :</span>
						<span class="text-pc-darkblue dark:text-white">
							{#if template.email}
								Email ({template.email.name ?? ''})
							{:else}
								API Sender ({template.apiSender?.name ?? ''})
							{/if}
						</span>

						<span class="text-grayblue-dark font-medium">Before Page:</span>
						<span class="text-pc-darkblue dark:text-white">
							{#if template.beforeLandingPage}
								{template.beforeLandingPage.name}
							{:else if template.beforeLandingProxy}
								<span class="flex items-center gap-1">
									<ProxySvgIcon size="w-4 h-4" />
									{template.beforeLandingProxy.name}
								</span>
							{/if}
						</span>

						<span class="text-grayblue-dark font-medium">Main Page:</span>
						<span class="text-pc-darkblue dark:text-white">
							{#if template.landingPage}
								{template.landingPage.name}
							{:else if template.landingProxy}
								<span class="flex items-center gap-1">
									<ProxySvgIcon size="w-4 h-4" />
									{template.landingProxy.name}
								</span>
							{/if}
						</span>

						<span class="text-grayblue-dark font-medium">After Page:</span>
						<span class="text-pc-darkblue dark:text-white">
							{#if template.afterLandingPage}
								{template.afterLandingPage.name}
							{:else if template.afterLandingProxy}
								<span class="flex items-center gap-1">
									<ProxySvgIcon size="w-4 h-4" />
									{template.afterLandingProxy.name}
								</span>
							{/if}
						</span>

						<span class="text-grayblue-dark font-medium">Redirect URL:</span>
						<span class="text-pc-darkblue dark:text-white"
							>{template.afterLandingPageRedirectURL ?? ''}</span
						>
					</div>
				</div>

				<!-- Email Configuration -->
				{#if template.email}
					<div class="p-6 rounded-lg">
						<h3 class="text-xl font-semibold text-pc-darkblue dark:text-white mb-4 border-b pb-2">
							Email
						</h3>
						<div class="grid grid-cols-[120px_1fr] gap-y-3">
							<span class="text-grayblue-dark font-medium">Name:</span>
							<span class="text-pc-darkblue dark:text-white">{template.email.name}</span>

							<span class="text-grayblue-dark font-medium">Envelope:</span>
							<span class="text-pc-darkblue dark:text-white">{template.email.mailEnvelopeFrom}</span
							>

							<span class="text-grayblue-dark font-medium">From:</span>
							<span class="text-pc-darkblue dark:text-white">{template.email.mailHeaderFrom}</span>

							<span class="text-grayblue-dark font-medium">Subject:</span>
							<span class="text-pc-darkblue dark:text-white"
								>{template.email.mailHeaderSubject}</span
							>

							<span class="text-grayblue-dark font-medium">Tracking:</span>
							<span class="text-pc-darkblue dark:text-white"
								>{template.email.addTrackingPixel ? 'Enabled' : 'Disabled'}</span
							>
						</div>
					</div>
				{/if}
			</div>

			<!-- Domain and SMTP Config -->
			<div class="grid grid-cols-2 gap-6">
				<!-- Domain Configuration -->
				<div class="p-6 rounded-lg">
					<h3 class="text-xl font-semibold text-pc-darkblue dark:text-white mb-4 border-b pb-2">
						Domain
					</h3>
					<div class="grid grid-cols-[120px_1fr] gap-y-3">
						<span class="text-grayblue-dark font-medium">Host Site:</span>
						<span class="text-pc-darkblue dark:text-white"
							>{template.domain.hostWebsite ? 'Yes' : 'No'}</span
						>

						<span class="text-grayblue-dark font-medium">Domain:</span>
						<span class="text-pc-darkblue dark:text-white">
							<a
								href="https://{template.domain?.name}"
								target="_blank"
								class="text-cta-blue dark:text-white hover:underline"
							>
								{template.domain?.name}
							</a>
						</span>

						<span class="text-grayblue-dark font-medium">URL Path:</span>
						<span class="text-pc-darkblue dark:text-white">{template.urlPath}</span>

						<span class="text-grayblue-dark font-medium">TLS:</span>
						<span class="text-pc-darkblue dark:text-white">
							{template.domain.managedTLS ? 'Managed' : template.domain.ownManagedTLS ? 'Own' : ''}
						</span>
					</div>
				</div>

				<!-- SMTP/API Configuration -->
				{#if template.smtpConfiguration || template.apiSender}
					<div class="p-6 rounded-lg">
						<h3 class="text-xl font-semibold text-pc-darkblue dark:text-white mb-4 border-b pb-2">
							{template.smtpConfiguration ? 'Email SMTP' : 'API Sender'}
						</h3>
						<div class="grid grid-cols-[120px_1fr] gap-y-3">
							{#if template.smtpConfiguration}
								<span class="text-grayblue-dark font-medium">Name:</span>
								<span class="text-pc-darkblue dark:text-white"
									>{template.smtpConfiguration.name}</span
								>

								<span class="text-grayblue-dark font-medium">Host:</span>
								<span class="text-pc-darkblue dark:text-white"
									>{template.smtpConfiguration.host}</span
								>

								<span class="text-grayblue-dark font-medium">Port:</span>
								<span class="text-pc-darkblue dark:text-white"
									>{template.smtpConfiguration.port}</span
								>

								<span class="text-grayblue-dark font-medium">Username:</span>
								<span class="text-pc-darkblue dark:text-white">
									{template.smtpConfiguration.username || 'Not configured'}
								</span>

								<span class="text-grayblue-dark font-medium">Allow insecure: </span>
								<span class="text-pc-darkblue dark:text-white">
									{!template.smtpConfiguration.ignoreCertErrors ? 'Disabled' : 'Enabled'}
								</span>
							{:else}
								<span class="text-grayblue-dark font-medium">API Sender:</span>
								<span class="text-pc-darkblue dark:text-white">{template.apiSender?.name}</span>
							{/if}
						</div>
					</div>
				{/if}
			</div>
		</div>
	</Modal>
	<Alert headline="close" bind:visible={isCloseModalVisible} onConfirm={onConfirmCloseCampaign}>
		<div>
			<ul class="list-disc ml-8 mt-4 mb-4">
				<li class="list-tem">Stop any pending delivery</li>
				<li class="list-tem">Links in e-mails and landing pages will stop working</li>
				<li class="list-tem">Campaign will be set as completed</li>
			</ul>
		</div>
	</Alert>

	<Alert
		headline="anonymize"
		bind:visible={isAnonymizeModalVisible}
		onConfirm={onConfirmAnonymize}
		verification="confirm"
	>
		<div>
			<ul class="list-disc ml-8 mt-4 mb-4">
				<li class="list-tem">Stop any pending delivery</li>
				<li class="list-tem">Links in e-mails and landing pages will stop working</li>
				<li class="list-tem">Anonymization is permanent and not reversable</li>
				<li class="list-tem">Campaign will be set as completed</li>
			</ul>
		</div>
	</Alert>

	<Alert
		headline={`Send ${getMessageType().charAt(0).toUpperCase() + getMessageType().slice(1)}`}
		bind:visible={isSendMessageModalVisible}
		onConfirm={onConfirmSendMessage}
	>
		<div>
			{#if sendMessageRecipient}
				{@const recipient = campaignRecipients.find((r) => r.id === sendMessageRecipient.id)}
				{#if recipient}
					<p class="mb-4">
						{recipient.sentAt
							? `Are you sure you want to send the campaign ${getMessageType()} again to:`
							: `Are you sure you want to send the campaign ${getMessageType()} to:`}
					</p>
					<div class="bg-gray-50 dark:bg-gray-700 p-3 rounded mb-4">
						<p class="font-medium">{sendMessageRecipient.name}</p>
						<p class="text-gray-600">{sendMessageRecipient.email}</p>
						<p class="text-xs text-gray-600 dark:text-gray-400 mt-1">
							Sender type: {campaign.template?.email ? 'Email (SMTP)' : 'API Sender'}
						</p>
						{#if recipient.sentAt}
							<p class="text-sm text-amber-600 mt-1">
								⚠️ Previously sent on {new Date(recipient.sentAt).toLocaleString()}
							</p>
						{/if}
					</div>
					<p class="text-sm text-gray-500">This action will immediately send the message.</p>
				{/if}
			{/if}
		</div>
	</Alert>

	<Alert
		headline="Set as Sent"
		bind:visible={isSetAsSentModalVisible}
		onConfirm={onConfirmSetAsSent}
	>
		<div>
			{#if setAsSentRecipient}
				{@const recipient = campaignRecipients.find((r) => r.id === setAsSentRecipient.id)}
				{#if recipient}
					<p class="mb-4">Are you sure you want to mark the campaign as sent for:</p>
					<div class="bg-gray-50 dark:bg-gray-700 p-3 rounded mb-4">
						<p class="font-medium">{setAsSentRecipient.name}</p>
						<p class="text-gray-600">{setAsSentRecipient.email}</p>
						{#if recipient.sentAt}
							<p class="text-sm text-amber-600 mt-1">
								⚠️ Already marked as sent on {new Date(recipient.sentAt).toLocaleString()}
							</p>
						{/if}
					</div>
					<p class="text-sm text-gray-500">
						This action will mark the message as sent without actually sending it.
					</p>
				{/if}
			{/if}
		</div>
	</Alert>

	<Modal
		headerText={'Cookies captured'}
		visible={isStorageAceModalVisible}
		onClose={closeStorageAceModal}
	>
		<div class="mt-4">
			<!-- Introduction Section -->
			<div>
				<h3 class="text-xl font-semibold text-gray-700">Import cookie</h3>
				<p class="text-gray-600 mb-4">
					Cookies can be imported using the <a
						href="https://chromewebstore.google.com/detail/storageace/cpbgcbmddckpmhfbdckeolkkhkjjmplo"
						target="_blank"
						class="text-blue-600 dark:text-white hover:underline">StorageAce</a
					> extension.
				</p>
			</div>

			<!-- Copy Section -->
			<div class="bg-gray-50 dark:bg-gray-700 p-4 rounded-md">
				<button
					class="text-blue-600 dark:text-white hover:text-blue-800 dark:hover:text-gray-300 font-medium inline-flex items-center gap-2"
					on:click={onClickCopyCookies}
				>
					<svg class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
						<path
							stroke-linecap="round"
							stroke-linejoin="round"
							stroke-width="2"
							d="M8 16H6a2 2 0 01-2-2V6a2 2 0 012-2h8a2 2 0 012 2v2m-6 12h8a2 2 0 002-2v-8a2 2 0 00-2-2h-8a2 2 0 00-2 2v8a2 2 0 002 2z"
						></path>
					</svg>
					Copy cookies
				</button>
			</div>
		</div>
		<FormGrid on:submit={onStorageAceModalOk}>
			<FormColumns>
				<FormColumn>
					<!-- Empty form column for structure -->
				</FormColumn>
			</FormColumns>
			<div
				class="py-4 row-span-2 col-start-1 col-span-3 border-t-2 border-gray-200 dark:border-gray-700 w-full flex flex-row justify-center items-center sm:justify-center md:justify-center lg:justify-end xl:justify-end 2xl:justify-end bg-white dark:bg-gray-800 text-gray-900 dark:text-gray-100 transition-colors duration-200"
			>
				<button
					type="button"
					on:click={closeStorageAceModal}
					class="bg-blue-600 hover:bg-blue-500 dark:bg-blue-500 dark:hover:bg-blue-400 text-sm uppercase font-bold px-4 py-2 text-white rounded-md transition-colors duration-200"
				>
					Close
				</button>
			</div>
		</FormGrid>
	</Modal>
</main>
