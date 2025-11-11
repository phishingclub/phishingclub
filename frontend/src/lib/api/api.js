import {
	getJSON,
	postJSON,
	postMultipart,
	patchJSON,
	deleteJSON,
	deleteReq,
	newResponse,
	putJSON
} from './client.js';
/**
 * Represents the response object returned by the API functions.
 * @typedef {Object} ApiResponse
 * @property {boolean} success - Indicates whether the request was successful.
 * @property {number} statusCode - The status code of the response.
 * @property {string} error - The error message, if any.
 * @property {any} data - The data returned by the request.
 */

/**
 * @typedef {object} APISenderHeader
 * @property {string} [id]
 * @property {string} key
 * @property {string} value
 * @property {boolean} isRequestHeader
 * @property {string} [apiSenderID]
 */

/**
 * TableURLParams is a type that represents the query parameters for a table.
 *
 * @typedef {object} TableURLParams
 * @property {number} [currentPage]
 * @property {number} [perPage]
 * @property {string} [sortBy]
 * @property {string} [sortOrder]
 * @property {string} [search]
 */

/**
 * Calculates the offset for the specified page and per page count.
 *
 * @param {number} currentPage
 * @param {number} perPage
 * @returns {number}
 */
const getOffset = (currentPage, perPage) => {
	return (currentPage - 1) * perPage;
};

const appendQuery = (query) => {
	if (!query) {
		return '_'; // append after this method hack
	}

	// extract only the state properties we need, avoiding methods and internal properties
	// handle both tableURLParams objects and plain objects safely
	const currentPage = query.currentPage || query.page || 1;
	const perPage = query.perPage || 10;
	const sortBy = query.sortBy || '';
	const sortOrder = query.sortOrder || '';
	const search = query.search || '';

	const offset = getOffset(currentPage, perPage);

	let urlQuery = `offset=${offset}&limit=${perPage}`;
	if (sortBy) {
		//  normalize the sortby field by lowercasing and replacing spacing with underscores
		const normalizedSortBy = sortBy.toLowerCase().replace(/\s+/g, '_');
		urlQuery += `&sortBy=${normalizedSortBy}`;
	}
	if (sortOrder) {
		urlQuery += `&sortOrder=${sortOrder}`;
	} else {
		urlQuery += `&sortOrder=asc`;
	}
	if (search) {
		urlQuery += `&search=${search}`;
	}

	return urlQuery;
};
/**
 * API is for interacting with the backend API.
 * Use API.instance to get a singleton instance of the API class.
 */
export class API {
	/**
	 * @param {API|null} _instance
	 */
	static #_instance = null;

	/**
	 * instance is the singleton instance of the API class.
	 *
	 * @returns {API}
	 */
	static get instance() {
		if (!API.#_instance) {
			API.#_instance = new API();
		}
		return API.#_instance;
	}

	/**
	 * The base URL or PATH of the API.
	 *
	 * @type {string}
	 */
	#_url;

	/**
	 * The version of the API.
	 *
	 * @type {string}
	 */
	#_version;

	/**
	 * Creates a new API instance.
	 *
	 * @param {string} url
	 */
	constructor(url = '/api', version = '/v1') {
		this.#_version = version;
		this.#_url = url;
	}

	/**
	 * Constructs the full URL for the specified path.
	 *
	 * @param {string} path
	 * @returns {string}
	 */
	getPath(path) {
		return `${this.#_url}${this.#_version}${path}`;
	}

	/**
	 * builds a query arg for the company ID to
	 * be appended to the path or a empty string if the companyID is not provided.
	 */
	appendCompanyQuery(companyID) {
		if (!companyID) {
			return '';
		}
		return `&${this.companyQuery(companyID)}`;
	}

	companyQuery(companyID) {
		if (!companyID) {
			return '';
		}
		return `companyID=${companyID}`;
	}

	/**
	 * builds a query arg for the type ID to
	 * be appended to the path or a empty string if the typeID is not provided.
	 */
	appendTypeQuery(typeID) {
		if (!typeID) {
			return '';
		}
		return `&typeID=${typeID}`;
	}

	/**
	 * application is the API for application related operations.
	 */
	application = {
		/**
		 * Feature flags - get the features the application is build with
		 *
		 * @returns {Promise<ApiResponse>}
		 */
		features: async () => {
			return await getJSON(this.getPath('/features'));
		},

		/**
		 * Install the application.
		 *
		 * @param {string} username
		 * @param {string} userFullname
		 * @param {string} newPassword
		 * @returns {Promise<ApiResponse>}
		 */
		install: async (username, userFullname, newPassword) => {
			return await postJSON(this.getPath(`/install`), {
				username,
				userFullname,
				newPassword
			});
		},

		/**
		 * Health check endpoint
		 * @returns {Promise<boolean>}
		 */
		health: async () => {
			const res = await fetch(this.getPath('/healthz'), {
				method: 'GET'
			});
			return res.status === 200;
		},

		/**
		 * Check if update is available (cached)
		 * @returns {Promise<ApiResponse>}
		 */
		isUpdateAvailableCached: async () => {
			return await getJSON(this.getPath(`/update/available/cached`));
		},

		/**
		 * Check if update is available (manual check)
		 * @returns {Promise<ApiResponse>}
		 */
		isUpdateAvailable: async () => {
			return await getJSON(this.getPath(`/update/available`));
		},

		/**
		 * Get update details
		 * @returns {Promise<ApiResponse>}
		 */
		getUpdateDetails: async () => {
			return await getJSON(this.getPath(`/update`));
		},
		/**
		 * Performs an update
		 * @returns {Promise<ApiResponse>}
		 */
		runUpdate: async () => {
			return await postJSON(this.getPath(`/update`));
		},

		/**
		 * Create a backup
		 * @returns {Promise<ApiResponse>}
		 */
		createBackup: async () => {
			return await postJSON(this.getPath(`/backup/create`));
		},

		/**
		 * List available backups
		 * @returns {Promise<ApiResponse>}
		 */
		listBackups: async () => {
			return await getJSON(this.getPath(`/backup/list`));
		},

		/**
		 * Download a backup file
		 * @param {string} filename - name of the backup file
		 * @returns {Promise<Blob>}
		 */
		downloadBackup: async (filename) => {
			const response = await fetch(
				this.getPath(`/backup/download/${encodeURIComponent(filename)}`),
				{
					method: 'GET',
					credentials: 'same-origin'
				}
			);

			if (!response.ok) {
				throw new Error(`Failed to download backup: ${response.statusText}`);
			}

			return await response.blob();
		},

		/**
		 * Install example templates from GitHub during setup
		 * @returns {Promise<ApiResponse>}
		 */
		installTemplates: async () => {
			return await postJSON(this.getPath(`/install/templates`));
		}
	};

	/**
	 * asset is the API for asset (static files) related operations.
	 */
	asset = {
		/**
		 * Get assets for a domain.
		 *
		 * @param {string} domain
		 * @param {string|null} companyID
		 * @param {TableURLParams} options
		 * @returns {Promise<ApiResponse>}
		 */
		getByDomain: async (domain, companyID, options) => {
			return await getJSON(
				this.getPath(
					`/asset/domain/${domain}?${appendQuery(options)}${this.appendCompanyQuery(companyID)}`
				)
			);
		},

		/**
		 * Get asset by id
		 *
		 * @param {string}  id
		 * @returns {Promise<ApiResponse>}
		 */
		getByID: async (id) => {
			return await getJSON(this.getPath(`/asset/${id}`));
		},

		/**
		 * Get asset by id in base64 with mime type
		 *
		 * @param {string} domain
		 * @param {string} path
		 * @returns {Promise<ApiResponse>}
		 */
		getRaw: async (domain, path) => {
			return await getJSON(this.getPath(`/asset/view/domain/${domain}/${path}`));
		},

		/**
		 * Get all assets for a domain using pagination.
		 *
		 * @param {*} data  form data
		 * @returns {Promise<ApiResponse>}
		 */
		upload: async (data) => {
			const res = await fetch(this.getPath(`/asset`), {
				method: 'POST',
				// content-type is set automatically by the browser
				body: data
			});
			// TODO all of these to json things can fail we need to do something more
			const body = await res.json();

			return newResponse(body.success, res.status, body.error, body.data);
		},

		/**
		 * Update an asset
		 *
		 * @param {string} id
		 * @param {string} name
		 * @param {string} description
		 * @returns {Promise<ApiResponse>}
		 */
		update: async (id, name, description) => {
			return await patchJSON(this.getPath(`/asset/${id}`), {
				name: name,
				description: description
			});
		},

		/**
		 * Get all assets for a domain using pagination.
		 *
		 * @param {string} id
		 * @returns {Promise<ApiResponse>}
		 */
		delete: async (id) => {
			return await deleteJSON(this.getPath(`/asset/${id}`));
		}
	};

	/**
	 * atttachment is the API for attachment related operations.
	 * @type {Object}
	 */
	attachment = {
		/**
		 * Get attachments.
		 *
		 * @param {string|null} companyID   can be null for global context
		 * @param {TableURLParams} options
		 * @returns {Promise<ApiResponse>}
		 */
		getByContext: async (companyID, options) => {
			return await getJSON(
				this.getPath(`/attachment?${appendQuery(options)}${this.appendCompanyQuery(companyID)}`)
			);
		},

		/**
		 * Get an attachment by its ID.
		 *
		 * @param {string} id
		 * @returns {Promise<ApiResponse>}
		 */
		getByID: async (id) => {
			return await getJSON(this.getPath(`/attachment/${id}`));
		},

		/**
		 * Get the content and mime type of an attachment by its ID.
		 *
		 * @param {string} id
		 * @returns {Promise<ApiResponse>}
		 */
		getContentByID: async (id) => {
			return await getJSON(this.getPath(`/attachment/${id}/content`));
		},

		/**
		 *
		 * @param {object} attachment
		 * @param {string} attachment.id
		 * @param {string} attachment.name
		 * @param {string} attachment.description
		 * @param {Boolean} attachment.embeddedContent
		 * @returns {Promise<ApiResponse>}
		 */
		update: async ({ id, name, description, embeddedContent }) => {
			return await patchJSON(this.getPath(`/attachment/${id}`), {
				name: name,
				description: description,
				embeddedContent: embeddedContent
			});
		},

		/**
		 * Upload a new attachment.
		 *
		 * @param {FormData} data
		 * @returns {Promise<ApiResponse>}
		 */
		upload: async (data) => {
			const res = await fetch(this.getPath(`/attachment`), {
				method: 'POST',
				// content-type is set automatically by the browser
				body: data
			});
			// TODO all of these to json things can fail we need to do something more
			const body = await res.json();

			return newResponse(body.success, res.status, body.error, body.data);
		},

		/**
		 * Delete an attachment by its ID.
		 *
		 * @param {string} id
		 * @returns
		 */
		delete: async (id) => {
			return await deleteJSON(this.getPath(`/attachment/${id}`));
		}
	};

	/**
	 * campaign is the API for campaign related operations.
	 * @type {Object}
	 * @returns {Promise<ApiResponse>}
	 */
	campaign = {
		/**
		 * @param {string} campaignID
		 * @returns {Promise<ApiResponse>}
		 */
		anonymize: async (campaignID) => {
			return postJSON(this.getPath(`/campaign/${campaignID}/anonymize`));
		},

		/**
		 * @param {string} campaignID
		 * @returns {Promise<ApiResponse>}
		 */
		close: async (campaignID) => {
			return postJSON(this.getPath(`/campaign/${campaignID}/close`));
		},

		/**
		 * @param {string} campaignID
		 * @returns
		 */
		exportEvents: async (campaignID) => {
			window.open(this.getPath(`/campaign/${campaignID}/export/events`), '_blank');
		},

		/**
		 * Export campaign submissions as CSV
		 * @param {string} campaignID
		 * @returns
		 */
		exportSubmissions: async (campaignID) => {
			window.open(this.getPath(`/campaign/${campaignID}/export/submissions`), '_blank');
		},

		/**
		 *
		 * @param {object} campaign
		 * @param {string} [campaign.companyID]   uuid
		 * @param {string} campaign.templateID  uuid
		 * @param {string} campaign.name
		 * @param {boolean} [campaign.saveSubmittedData]
		 * @param {boolean} [campaign.isAnonymous]
		 * @param {boolean} [campaign.isTest]
		 * @param {boolean} [campaign.obfuscate]
		 * @param {string} campaign.sortField
		 * @param {string} campaign.sortOrder
		 * @param {string} campaign.sendStartAt
		 * @param {string} campaign.sendEndAt
		 * @param {string} [campaign.closeAt]
		 * @param {string} [campaign.anonymizeAt]
		 * @param {string[]} campaign.recipientGroupIDs []uuid
		 * @param {string[]} campaign.allowDenyIDs []uuid
		 * @param {string} campaign.denyPageID uuid
		 * @param {string} campaign.evasionPageID uuid
		 * @param {string} campaign.webhookID uuid
		 * @param {Array} [campaign.constraintWeekDays]
		 * @param {string} [campaign.constraintStartTime]
		 * @param {string} [campaign.constraintEndTime]
		 * @returns {Promise<ApiResponse>}
		 */
		create: async ({
			companyID,
			templateID,
			name,
			saveSubmittedData,
			saveBrowserMetadata,
			isAnonymous,
			isTest,
			obfuscate,
			sortField,
			sortOrder,
			sendStartAt,
			sendEndAt,
			closeAt,
			anonymizeAt,
			recipientGroupIDs,
			allowDenyIDs,
			denyPageID,
			evasionPageID,
			webhookID,
			constraintWeekDays,
			constraintStartTime,
			constraintEndTime
		}) => {
			return await postJSON(this.getPath('/campaign'), {
				companyID,
				templateID,
				name,
				isAnonymous,
				isTest,
				obfuscate,
				saveSubmittedData,
				saveBrowserMetadata,
				sortField,
				sortOrder,
				sendStartAt,
				sendEndAt,
				closeAt,
				anonymizeAt,
				recipientGroupIDs,
				allowDenyIDs,
				denyPageID,
				evasionPageID,
				webhookID,
				constraintWeekDays,
				constraintStartTime,
				constraintEndTime
			});
		},

		/**
		 *
		 * @param {object} campaign
		 * @param {string} campaign.id
		 * @param {string} campaign.name
		 * @param {boolean} [campaign.saveSubmittedData]
		 * @param {boolean} [campaign.isAnonymous]
		 * @param {boolean} [campaign.isTest]
		 * @param {boolean} [campaign.obfuscate]
		 * @param {string} campaign.sortField
		 * @param {string} campaign.sortOrder
		 * @param {string} campaign.sendStartAt
		 * @param {string} campaign.sendEndAt
		 * @param {string} [campaign.closeAt]
		 * @param {string} [campaign.anonymizeAt]
		 * @param {string} campaign.templateID  uuid
		 * @param {string[]} campaign.recipientGroupIDs []uuid
		 * @param {string[]} campaign.allowDenyIDs []uuid
		 * @param {string} campaign.denyPageID uuid
		 * @param {string} campaign.evasionPageID uuid
		 * @param {string} campaign.webhookID uuid
		 * @param {Array} [campaign.constraintWeekDays]
		 * @param {string} [campaign.constraintStartTime]
		 * @param {string} [campaign.constraintEndTime]
		 * @returns {Promise<ApiResponse>}
		 */
		update: async ({
			id,
			name,
			saveSubmittedData,
			saveBrowserMetadata,
			isAnonymous,
			isTest,
			obfuscate,
			sortField,
			sortOrder,
			sendStartAt,
			sendEndAt,
			closeAt,
			anonymizeAt,
			templateID,
			recipientGroupIDs,
			allowDenyIDs,
			denyPageID,
			evasionPageID,
			webhookID,
			constraintWeekDays,
			constraintStartTime,
			constraintEndTime
		}) => {
			return await postJSON(this.getPath(`/campaign/${id}`), {
				name,
				isAnonymous,
				isTest,
				obfuscate,
				saveSubmittedData,
				saveBrowserMetadata,
				sortField,
				sortOrder,
				sendStartAt,
				sendEndAt,
				closeAt,
				anonymizeAt,
				templateID,
				recipientGroupIDs,
				allowDenyIDs,
				denyPageID,
				evasionPageID,
				webhookID,
				constraintWeekDays,
				constraintStartTime,
				constraintEndTime
			});
		},

		/**
		 * Get a campaign by ID.
		 *
		 * @param {string} id
		 * @returns {Promise<ApiResponse>}
		 */
		getByID: async (id) => {
			return await getJSON(this.getPath(`/campaign/${id}`));
		},

		/**
		 * Get a campaign by name.
		 *
		 * @param {string} name
		 * @returns {Promise<ApiResponse>}
		 */
		getByName: async (name, companyID = null) => {
			return await getJSON(
				this.getPath(
					`/campaign/name/${name}?${appendQuery(null)}${this.appendCompanyQuery(companyID)}`
				)
			);
		},

		/**
		 * Get all campaigns using pagination.
		 *
		 * @param {TableURLParams} options
		 * @param {string|null} companyID
		 * @returns {Promise<ApiResponse>}
		 */
		getAll: async (options, companyID = null) => {
			return await getJSON(
				this.getPath(`/campaign?${appendQuery(options)}${this.appendCompanyQuery(companyID)}`)
			);
		},

		/**
		 * Get all campaigns using pagination.
		 *
		 * @param {TableURLParams} options
		 * @param {string} start RFC3339NANO
		 * @param {string} end RFC3339NANO
		 * @param {string|null} companyID
		 * @returns {Promise<ApiResponse>}
		 */
		getWithinDates: async (start, end, options, companyID = null) => {
			return await getJSON(
				this.getPath(
					`/campaign/calendar?${appendQuery(options)}${this.appendCompanyQuery(companyID)}&start=${start}&end=${end}`
				)
			);
		},

		/**
		 * Get all active campaigns using pagination.
		 *
		 * @param {TableURLParams} options
		 * @param {string|null} companyID
		 * @returns {Promise<ApiResponse>}
		 */
		getAllActive: async (options, companyID = null) => {
			return await getJSON(
				this.getPath(
					`/campaign/active?${appendQuery(options)}${this.appendCompanyQuery(companyID)}`
				)
			);
		},

		/**
		 * Get all upcoming campaigns using pagination.
		 *
		 * @param {TableURLParams} options
		 * @param {string|null} companyID
		 * @returns {Promise<ApiResponse>}
		 */
		getAllUpcoming: async (options, companyID = null) => {
			return await getJSON(
				this.getPath(
					`/campaign/upcoming?${appendQuery(options)}${this.appendCompanyQuery(companyID)}`
				)
			);
		},

		/**
		 * Get all active campaigns using pagination.
		 *
		 * @param {TableURLParams} options
		 * @param {string|null} companyID
		 * @returns {Promise<ApiResponse>}
		 */
		getAllFinished: async (options, companyID = null) => {
			return await getJSON(
				this.getPath(
					`/campaign/finished?${appendQuery(options)}${this.appendCompanyQuery(companyID)}`
				)
			);
		},

		/**
		 * Get all campaign recipients.
		 *
		 * @param {string} campaignID
		 * @param {TableURLParams} options
		 * @returns {Promise<ApiResponse>}
		 */
		getAllCampaignRecipients: async (campaignID, options) => {
			return await getJSON(
				this.getPath(`/campaign/${campaignID}/recipients?${appendQuery(options)}`)
			);
		},

		/**
		 * Get all event types.
		 *
		 * @returns {Promise<ApiResponse>}
		 */
		getAllEventTypes: async () => {
			return await getJSON(this.getPath(`/campaign/event-types`));
		},

		/**
		 * Get all events by campaign ID.
		 *
		 * @param {string} campaignID
		 * @param {TableURLParams} options
		 * @param {string} since RFC3339NANO
		 * @returns {Promise<ApiResponse>}
		 */
		getAllEventsByCampaignID: async (campaignID, options, since = '') => {
			return await getJSON(
				this.getPath(`/campaign/${campaignID}/events?${appendQuery(options)}&since=${since}`)
			);
		},

		/**
		 * Get campaigns stats
		 * if no company ID is provided it retrieves the global stats including all companies
		 */
		getStats: async (companyID = null, options = {}) => {
			return await getJSON(
				this.getPath(
					`/campaign/statistics?${appendQuery(options)}${this.appendCompanyQuery(companyID)}`
				)
			);
		},

		/**
		 * Get campaign result stats
		 * @param {string} campaignID
		 * @returns {Promise<ApiResponse>}
		 */
		getResultStats: async (campaignID) => {
			return await getJSON(this.getPath(`/campaign/${campaignID}/statistics`));
		},

		/**
		 * Get campaign recipient email.
		 *
		 * @param {string} campaignRecipientID
		 * @returns {Promise<ApiResponse>}
		 */
		getEmail: async (campaignRecipientID) => {
			return await getJSON(this.getPath(`/campaign/recipient/${campaignRecipientID}/email`));
		},

		/**
		 * Set email sent to now
		 *
		 * @param {string} campaignRecipient
		 * @returns {Promise<ApiResponse>}
		 */
		setEmailSent: async (campaignRecipient) => {
			return await postJSON(this.getPath(`/campaign/recipient/${campaignRecipient}/sent`));
		},

		/**
		 * Send message to campaign recipient (works for both email and API senders)
		 *
		 * @param {string} campaignRecipientID
		 * @returns {Promise<ApiResponse>}
		 */
		sendMessage: async (campaignRecipientID) => {
			return await postJSON(this.getPath(`/campaign/recipient/${campaignRecipientID}/send`));
		},

		/**
		 * Send email to campaign recipient (alias for sendMessage for backward compatibility)
		 *
		 * @param {string} campaignRecipientID
		 * @returns {Promise<ApiResponse>}
		 */
		sendEmail: async (campaignRecipientID) => {
			return await postJSON(this.getPath(`/campaign/recipient/${campaignRecipientID}/send`));
		},

		/**
		 * Get campaign recipient landingpage URL.
		 *
		 * @param {string} campaignRecipientID
		 * @return {Promise<ApiResponse>}
		 */
		getURL: async (campaignRecipientID) => {
			return await getJSON(this.getPath(`/campaign/recipient/${campaignRecipientID}/url`));
		},

		/**
		 * Delete a campaign.
		 *
		 * @param {string} id
		 * @returns {Promise<ApiResponse>}
		 */
		delete: async (id) => {
			return await deleteJSON(this.getPath(`/campaign/${id}`));
		},

		/**
		 * Get campaign statistics by campaign ID.
		 *
		 * @param {string} campaignID
		 * @returns {Promise<ApiResponse>}
		 */
		getCampaignStats: async (campaignID) => {
			return await getJSON(this.getPath(`/campaign/${campaignID}/stats`));
		},

		/**
		 * Get all campaign statistics.
		 *
		 * @param {string|null} companyID
		 * @returns {Promise<ApiResponse>}
		 */
		getAllCampaignStats: async (companyID = null) => {
			const companyQuery = companyID ? `?${this.companyQuery(companyID)}` : '';
			return await getJSON(this.getPath(`/campaign/stats/all${companyQuery}`));
		},

		/**
		 * Get all manual campaign statistics (those without campaignID).
		 *
		 * @param {string|null} companyID
		 * @returns {Promise<ApiResponse>}
		 */
		getManualCampaignStats: async (companyID = null) => {
			const companyQuery = companyID ? `?${this.companyQuery(companyID)}` : '';
			return await getJSON(this.getPath(`/campaign/stats/manual${companyQuery}`));
		},

		/**
		 * Create manual campaign statistics.
		 *
		 * @param {object} stats
		 * @param {string} stats.campaignName
		 * @param {string} [stats.companyId]
		 * @param {number} stats.totalRecipients
		 * @param {number} stats.emailsSent
		 * @param {number} stats.trackingPixelLoaded
		 * @param {number} stats.websiteVisits
		 * @param {number} stats.dataSubmissions
		 * @param {number} stats.reported
		 * @param {string} [stats.templateName]
		 * @param {string} [stats.campaignType]
		 * @param {string} [stats.campaignStartDate] - ISO date string
		 * @param {string} [stats.campaignEndDate] - ISO date string
		 * @param {string} [stats.campaignClosedAt] - ISO date string
		 * @returns {Promise<ApiResponse>}
		 */
		createStats: async (stats) => {
			return await postJSON(this.getPath('/campaign/stats'), stats);
		},

		/**
		 * Update manual campaign statistics by ID.
		 *
		 * @param {string} statsID
		 * @param {object} stats
		 * @param {string} stats.campaignName
		 * @param {string} [stats.companyId]
		 * @param {number} stats.totalRecipients
		 * @param {number} stats.emailsSent
		 * @param {number} stats.trackingPixelLoaded
		 * @param {number} stats.websiteVisits
		 * @param {number} stats.dataSubmissions
		 * @param {number} stats.reported
		 * @param {string} [stats.templateName]
		 * @param {string} [stats.campaignType]
		 * @param {string} [stats.campaignStartDate] - ISO date string
		 * @param {string} [stats.campaignEndDate] - ISO date string
		 * @param {string} [stats.campaignClosedAt] - ISO date string
		 * @returns {Promise<ApiResponse>}
		 */
		updateStats: async (statsID, stats) => {
			return await putJSON(this.getPath(`/campaign/stats/${statsID}`), stats);
		},

		/**
		 * Delete manual campaign statistics by ID.
		 *
		 * @param {string} statsID
		 * @returns {Promise<ApiResponse>}
		 */
		deleteStats: async (statsID) => {
			return await deleteJSON(this.getPath(`/campaign/stats/${statsID}`));
		}
	};

	/**
	 * campain templates is the API for campaign template related operations.
	 *
	 * @type {Object}
	 */
	campaignTemplate = {
		/**
		 * Get a campaign template by its ID.
		 *
		 * @param {string} id
		 * @param {boolean} full retrieve related data
		 * @returns {Promise<ApiResponse>}
		 */
		getByID: async (id, full = false) => {
			let p = this.getPath(`/campaign/template/${id}`);
			if (full) {
				p += '?full';
			}
			return await getJSON(p);
		},

		/**
		 * Get all campaign templates using pagination.
		 *
		 * @param {TableURLParams} options
		 * @param {string|null} companyID
		 * @returns {Promise<ApiResponse>}
		 */
		getAll: async (options, companyID = null, usableOnly = false) => {
			return await getJSON(
				this.getPath(
					`/campaign/template?${appendQuery(options)}${this.appendCompanyQuery(companyID)}&usableOnly=${usableOnly}`
				)
			);
		},

		/**
		 * Create a new campaign template.
		 *
		 * @param {object} template
		 * @param {string} template.name
		 * @param {string} template.companyID
		 * @param {string} template.domainID
		 * @param {string} template.beforeLandingPageID
		 * @param {string} template.beforeLandingProxyID
		 * @param {string} template.afterLandingPageID
		 * @param {string} template.afterLandingProxyID
		 * @param {string} template.landingPageID
		 * @param {string} template.landingProxyID
		 * @param {string} template.smtpConfigurationID
		 * @param {string} template.apiSenderID
		 * @param {string} template.afterLandingPageRedirectURL
		 * @param {string} template.urlIdentifierID
		 * @param {string} template.stateIdentifierID
		 * @param {string} template.urlPath
		 * @param {string} template.emailID
		 * @returns {Promise<ApiResponse>}
		 */
		create: async ({
			name,
			companyID,
			domainID,
			beforeLandingPageID,
			beforeLandingProxyID,
			afterLandingPageID,
			afterLandingProxyID,
			landingPageID,
			landingProxyID,
			smtpConfigurationID,
			apiSenderID,
			urlIdentifierID,
			stateIdentifierID,
			afterLandingPageRedirectURL,
			emailID: emailID
		}) => {
			return await postJSON(this.getPath('/campaign/template'), {
				name: name,
				companyID: companyID,
				domainID: domainID,
				beforeLandingPageID: beforeLandingPageID,
				beforeLandingProxyID: beforeLandingProxyID,
				afterLandingPageID: afterLandingPageID,
				afterLandingProxyID: afterLandingProxyID,
				landingPageID: landingPageID,
				landingProxyID: landingProxyID,
				smtpConfigurationID: smtpConfigurationID,
				apiSenderID: apiSenderID,
				afterLandingPageRedirectURL: afterLandingPageRedirectURL,
				urlIdentifierID: urlIdentifierID,
				stateIdentifierID: stateIdentifierID,
				emailID: emailID
			});
		},

		/**
		 * Update a campaign template.
		 *
		 * @param {Object} template
		 * @param {string} template.id
		 * @param {string} template.name
		 * @param {string} template.companyID
		 * @param {string} template.domainID
		 * @param {string} template.beforeLandingPageID
		 * @param {string} template.beforeLandingProxyID
		 * @param {string} template.afterLandingPageID
		 * @param {string} template.afterLandingProxyID
		 * @param {string} template.landingPageID
		 * @param {string} template.landingProxyID
		 * @param {string} template.smtpConfigurationID
		 * @param {string} template.apiSenderID
		 * @param {string} template.afterLandingPageRedirectURL
		 * @param {string} template.emailID
		 * @param {string} template.urlIdentifierID
		 * @param {string} template.stateIdentifierID
		 * @param {string} template.urlPath
		 * @returns {Promise<ApiResponse>}
		 */
		update: async ({
			id,
			name,
			companyID,
			domainID,
			beforeLandingPageID,
			beforeLandingProxyID,
			afterLandingPageID,
			afterLandingProxyID,
			landingPageID,
			landingProxyID,
			smtpConfigurationID,
			apiSenderID,
			afterLandingPageRedirectURL,
			emailID: emailID,
			urlIdentifierID: urlIdentifierID,
			stateIdentifierID: stateIdentifierID,
			urlPath: urlPath
		}) => {
			return await postJSON(this.getPath(`/campaign/template/${id}`), {
				name: name,
				companyID: companyID,
				domainID: domainID,
				beforeLandingPageID: beforeLandingPageID,
				beforeLandingProxyID: beforeLandingProxyID,
				afterLandingPageID: afterLandingPageID,
				afterLandingProxyID: afterLandingProxyID,
				landingPageID: landingPageID,
				landingProxyID: landingProxyID,
				smtpConfigurationID: smtpConfigurationID,
				apiSenderID: apiSenderID,
				afterLandingPageRedirectURL: afterLandingPageRedirectURL,
				emailID: emailID,
				urlIdentifierID: urlIdentifierID,
				stateIdentifierID: stateIdentifierID,
				urlPath: urlPath
			});
		},

		/**
		 * Delete a campaign template.
		 *
		 * @param {string} id
		 * @returns {Promise<ApiResponse>}
		 */
		delete: async (id) => {
			return await deleteJSON(this.getPath(`/campaign/template/${id}`));
		}
	};

	/**
	 * company is the API for company related operations.
	 *
	 * @type {Object}
	 */
	company = {
		/**
		 * Get a company by its ID.
		 *
		 * @param {string} id
		 * @returns {Promise<ApiResponse>}
		 */
		getByID: async (id) => {
			return await getJSON(this.getPath(`/company/${id}`));
		},

		/**
		 * Exports a companies data
		 *
		 * @param {string} id
		 * @returns
		 */
		export: async (id) => {
			window.open(this.getPath(`/company/${id ? id : 'shared'}/export`), '_blank');
		},

		/**
		 * Get all companies using pagination
		 *
		 * @param {TableURLParams} options
		 * @returns {Promise<ApiResponse>}
		 */
		getAll: async (options) => {
			return await getJSON(this.getPath(`/company?${appendQuery(options)}`));
		},

		/**
		 * Create a new company.
		 *
		 * @param {string} name
		 * @param {string} comment
		 * @returns {Promise<ApiResponse>}
		 */
		create: async (name, comment) => {
			return await postJSON(this.getPath(`/company`), {
				name: name,
				comment: comment
			});
		},

		/**
		 * Update a company.
		 *
		 * @param {string} id
		 * @param {string} name
		 * @param {string} comment
		 * @returns {Promise<ApiResponse>}
		 */
		update: async (id, name, comment) => {
			return await postJSON(this.getPath(`/company/${id}`), {
				name: name,
				comment: comment
			});
		},

		/**
		 * Delete a company.
		 *
		 * @param {string} id
		 */
		delete: async (id) => {
			return await deleteJSON(this.getPath(`/company/${id}`));
		}
	};

	/**
	 * domain is the API for domain related operations.
	 */
	domain = {
		/**
		 * creates a new domain.
		 *
		 * @param {object} domain
		 * @param {string} domain.name
		 * @param {string} domain.type
		 * @param {string} domain.proxyTargetDomain
		 * @param {boolean} domain.managedTLS
		 * @param {boolean} domain.ownManagedTLS
		 * @param {boolean} domain.selfSignedTLS
		 * @param {string} domain.ownManagedTLSKey
		 * @param {string} domain.ownManagedTLSPem
		 * @param {boolean} domain.hostWebsite
		 * @param {string} domain.pageContent
		 * @param {string} domain.pageNotFoundContent
		 * @param {string} domain.redirectURL
		 * @param {string} domain.companyID
		 * @returns {Promise<ApiResponse>}
		 */
		create: async ({
			name,
			type,
			proxyTargetDomain,
			managedTLS,
			ownManagedTLS,
			selfSignedTLS,
			ownManagedTLSKey,
			ownManagedTLSPem,
			hostWebsite,
			pageContent,
			pageNotFoundContent,
			redirectURL,
			companyID
		}) => {
			return await postJSON(this.getPath('/domain/'), {
				name: name,
				type: type,
				proxyTargetDomain: proxyTargetDomain,
				managedTLS: managedTLS,
				ownManagedTLS: ownManagedTLS,
				selfSignedTLS: selfSignedTLS,
				ownManagedTLSKey: ownManagedTLSKey,
				ownManagedTLSPem: ownManagedTLSPem,
				hostWebsite: hostWebsite,
				pageContent: pageContent,
				pageNotFoundContent: pageNotFoundContent,
				redirectURL: redirectURL,
				companyID: companyID
			});
		},

		/**
		 * updates a domain.
		 *
		 * @param {object} domain
		 * @param {string} domain.id
		 * @param {string} [domain.type]
		 * @param {string} [domain.proxyTargetDomain]
		 * @param {boolean} domain.managedTLS
		 * @param {boolean} domain.ownManagedTLS
		 * @param {boolean} domain.selfSignedTLS
		 * @param {string} domain.ownManagedTLSKey
		 * @param {string} domain.ownManagedTLSPem
		 * @param {boolean} [domain.hostWebsite]
		 * @param {string} [domain.pageContent]
		 * @param {string} [domain.pageNotFoundContent]
		 * @param {string} [domain.redirectURL]
		 * @param {string} domain.companyID
		 * @returns {Promise<ApiResponse>}
		 */
		update: async ({
			id,
			type,
			proxyTargetDomain,
			managedTLS,
			ownManagedTLS,
			selfSignedTLS,
			ownManagedTLSKey,
			ownManagedTLSPem,
			hostWebsite,
			pageContent,
			pageNotFoundContent,
			redirectURL,
			companyID
		}) => {
			const payload = {
				managedTLS: managedTLS,
				ownManagedTLS: ownManagedTLS,
				selfSignedTLS: selfSignedTLS,
				ownManagedTLSKey: ownManagedTLSKey,
				ownManagedTLSPem: ownManagedTLSPem,
				companyID: companyID
			};

			// conditionally add fields if they are provided
			if (type !== undefined) payload.type = type;
			if (proxyTargetDomain !== undefined) payload.proxyTargetDomain = proxyTargetDomain;
			if (hostWebsite !== undefined) payload.hostWebsite = hostWebsite;
			if (pageContent !== undefined) payload.pageContent = pageContent;
			if (pageNotFoundContent !== undefined) payload.pageNotFoundContent = pageNotFoundContent;
			if (redirectURL !== undefined) payload.redirectURL = redirectURL;

			return await postJSON(this.getPath(`/domain/${id}`), payload);
		},

		/**
		 * deletes a domain.
		 *
		 * @param {string} id
		 * @returns {Promise<ApiResponse>}
		 */
		delete: async (id) => {
			return await deleteReq(this.getPath(`/domain/${id}`));
		},

		/**
		 * gets a domain by its ID.
		 *
		 * @param {string} id
		 * @returns {Promise<ApiResponse>}
		 */
		getByID: async (id) => {
			return await getJSON(this.getPath(`/domain/${id}`));
		},

		/**
		 * get a domain by its name.
		 *
		 * @param {string} name
		 * @returns {Promise<ApiResponse>}
		 */
		getByName: async (name) => {
			return await getJSON(this.getPath(`/domain/name/${name}`));
		},

		/**
		 * get domains
		 *
		 * @param {TableURLParams} options
		 * @param {string|null} companyID
		 * @returns  {Promise<ApiResponse>}
		 */
		getAll: async (options, companyID = null) => {
			return await getJSON(
				this.getPath(`/domain?${appendQuery(options)}${this.appendCompanyQuery(companyID)}`)
			);
		},

		/**
		 * get domains subsets
		 *
		 * @param {TableURLParams} options
		 * @param {string|null} companyID
		 * @returns  {Promise<ApiResponse>}
		 */
		getAllSubset: async (options, companyID = null) => {
			return await getJSON(
				this.getPath(`/domain/subset?${appendQuery(options)}${this.appendCompanyQuery(companyID)}`)
			);
		},

		/**
		 * get domains subsets excluding proxy domains
		 *
		 * @param {TableURLParams} options
		 * @param {string|null} companyID
		 * @returns  {Promise<ApiResponse>}
		 */
		getAllSubsetWithoutProxies: async (options, companyID = null) => {
			return await getJSON(
				this.getPath(
					`/domain/subset/noproxies?${appendQuery(options)}${this.appendCompanyQuery(companyID)}`
				)
			);
		}
	};

	/**
	 * page s the API for page related operations.
	 */
	page = {
		/**
		 * Get a page by its ID.
		 *
		 * @param {string} id
		 * @returns {Promise<ApiResponse>}
		 */
		getByID: async (id) => {
			return await getJSON(this.getPath(`/page/${id}`));
		},

		/**
		 * Get all pages using pagination.
		 *
		 * @param {TableURLParams} options
		 * @param {string|null} companyID
		 * @returns {Promise<ApiResponse>}
		 */
		getAll: async (options, companyID = null) => {
			return await getJSON(
				this.getPath(`/page?${appendQuery(options)}${this.appendCompanyQuery(companyID)}`)
			);
		},

		/**
		 * Get page content by id
		 */
		getContentByID: async (id) => {
			return await getJSON(this.getPath(`/page/${id}/content`));
		},

		/**
		 * Get all pages using pagination.
		 *
		 * @param {TableURLParams} options
		 * @param {string|null} companyID
		 * @returns {Promise<ApiResponse>}
		 */
		getOverviews: async (options, companyID = null) => {
			return await getJSON(
				this.getPath(`/page/overview?${appendQuery(options)}${this.appendCompanyQuery(companyID)}`)
			);
		},

		/**
		 * Create a new page.
		 *
		 * @param {string} name
		 * @param {string} content
		 * @param {string} companyID
		 * @returns {Promise<ApiResponse>}
		 */
		create: async (name, content, companyID) => {
			const payload = {
				name: name,
				content: content,
				companyID: companyID
			};
			return await postJSON(this.getPath('/page'), payload);
		},

		/**
		 * Update a page.
		 *
		 * @param {string} id
		 * @param {object} page
		 * @param {string} page.name
		 * @param {string} page.content
		 * @returns {Promise<ApiResponse>}
		 */
		update: async (id, page) => {
			return await patchJSON(this.getPath(`/page/${id}`), page);
		},

		/**
		 * Delete a page.
		 *
		 * @param {string} id
		 * @returns {Promise<ApiResponse>}
		 */
		delete: async (id) => {
			return await deleteJSON(this.getPath(`/page/${id}`));
		}
	};

	/**
	 * log is the API for log related operations.
	 */
	log = {
		/**
		 * Get the log level.
		 *
		 * @returns {Promise<ApiResponse>}
		 */
		getLevel: async () => {
			return await getJSON(this.getPath('/log'));
		},

		/**
		 * Set the log level.
		 *
		 * @param {string} level
		 * @param {string} dbLevel
		 * @returns {Promise<ApiResponse>}
		 */
		setLevel: async (level, dbLevel) => {
			return await postJSON(this.getPath('/log'), {
				level: level,
				dbLevel: dbLevel
			});
		},

		/**
		 * Test the log levels. Can only be observed in the backend logs.
		 *
		 * @returns {Promise<ApiResponse>}
		 */
		testLevels: async () => {
			return await getJSON(this.getPath('/log/test'));
		}
	};

	/**
	 * email is the API for email related operations.
	 */
	email = {
		/**
		 * Add attachments to a email.
		 *
		 * @param {string} emailID
		 * @param {string[]} attachmentIDs
		 * @returns {Promise<ApiResponse>}
		 */
		addAttachments: async (emailID, attachmentIDs) => {
			return await postJSON(this.getPath(`/email/${emailID}/attachment`), {
				ids: attachmentIDs
			});
		},

		/**
		 * Remove an attachment from a email.
		 *
		 * @param {string} emailID
		 * @param {string} attachmentID
		 * @returns
		 */
		removeAttachment: async (emailID, attachmentID) => {
			return await deleteJSON(this.getPath(`/email/${emailID}/attachment`), {
				attachmentID: attachmentID
			});
		},

		/**
		 * Get a email by ID.
		 *
		 * @param {string} id
		 * @returns {Promise<ApiResponse>}
		 */
		getByID: async (id) => {
			return await getJSON(this.getPath(`/email/${id}`));
		},

		/**
		 * Get the content of a email by ID.
		 *
		 * @param {string} id
		 * @returns {Promise<ApiResponse>}
		 */
		getContentByID: async (id) => {
			return await getJSON(this.getPath(`/email/${id}/content`));
		},

		/**
		 * Get all emails using pagination.
		 *
		 * @param {TableURLParams} options
		 * @param {string|null} companyID
		 * @returns {Promise<ApiResponse>}
		 */
		getAll: async (options, companyID = null) => {
			return await getJSON(
				this.getPath(`/email?${appendQuery(options)}${this.appendCompanyQuery(companyID)}`)
			);
		},

		/**
		 * Get all email overviews
		 *
		 * @param {TableURLParams} options
		 * @param {string|null} companyID
		 * @returns {Promise<ApiResponse>}
		 */
		getOverviews: async (options, companyID = null) => {
			return await getJSON(
				this.getPath(`/email/overview?${appendQuery(options)}${this.appendCompanyQuery(companyID)}`)
			);
		},

		/**
		 * Send a test email
		 *
		 *
		 * @param {object} params
		 * @param {string} params.id
		 * @param {string} params.smtpID
		 * @param {string} params.domainID
		 * @param {string} params.recipientID
		 * @returns {Promise<ApiResponse>}
		 */
		sendTest: async ({ id: emailID, smtpID, domainID, recipientID }) => {
			return await postJSON(this.getPath(`/email/${emailID}/send-test`), {
				smtpID: smtpID,
				recipientID: recipientID,
				domainID: domainID
			});
		},

		/**
		 * Create a new email.
		 *
		 * @param {Object} email
		 * @param {string} email.name
		 * @param {string} email.content
		 * @param {string} email.mailEnvelopeFrom
		 * @param {string} email.mailHeaderFrom
		 * @param {string} email.mailHeaderSubject
		 * @param {bool} email.addTrackingPixel
		 * @param {string} [email.companyID]
		 * @returns {Promise<ApiResponse>}
		 */
		create: async (email) => {
			return await postJSON(this.getPath('/email'), email);
		},

		/**
		 * Update a email.
		 *
		 * @param {Object} email
		 * @param {string} email.id
		 * @param {string} [email.name]
		 * @param {string} [email.content]
		 * @param {string} [email.mailEnvelopeFrom]
		 * @param {string} [email.mailHeaderFrom]
		 * @param {string} [email.mailHeaderSubject]
		 * @param {boolean} [email.addTrackingPixel]
		 * @param {string} [email.companyID]
		 * @returns {Promise<ApiResponse>}
		 */
		update: async ({
			id,
			name,
			content,
			mailEnvelopeFrom,
			mailHeaderFrom,
			mailHeaderSubject,
			addTrackingPixel,
			companyID
		}) => {
			return await postJSON(this.getPath(`/email/${id}`), {
				name: name,
				content: content,
				mailEnvelopeFrom: mailEnvelopeFrom,
				mailHeaderSubject: mailHeaderSubject,
				mailHeaderFrom: mailHeaderFrom,
				addTrackingPixel: addTrackingPixel,
				companyID: companyID
			});
		},

		/**
		 * Delete a email.
		 *
		 * @param {string} id
		 * @returns {Promise<ApiResponse>}
		 */
		delete: async (id) => {
			return await deleteJSON(this.getPath(`/email/${id}`));
		}
	};

	/**
	 * session is the API for session related operations.
	 */
	session = {
		/**
		 * Ping session.
		 *
		 * @returns {Promise<ApiResponse>}
		 */
		ping: async () => {
			return await getJSON(this.getPath(`/session/ping`));
		},

		/**
		 * Revoke a session.
		 *
		 * @param {string} sessionID
		 * @returns {Promise<ApiResponse>}
		 */
		revoke: async (sessionID) => {
			return await deleteReq(this.getPath(`/session/${sessionID}`));
		}
	};

	/**
	 * smtpConfiguration is the API for SMTPConfiguration related operations.
	 */
	smtpConfiguration = {
		/**
		 * Get all SMTPConfigurations using pagination.
		 *
		 * @param {TableURLParams} options
		 * @param {string|null} companyID
		 * @returns {Promise<ApiResponse>}
		 */
		getAll: async (options, companyID) => {
			return await getJSON(
				this.getPath(
					`/smtp-configuration?${appendQuery(options)}${this.appendCompanyQuery(companyID)}`
				)
			);
		},

		/**
		 * Get a SMTPConfiguration by its ID.
		 *
		 * @param {string} id
		 * @returns {Promise<ApiResponse>}
		 */
		getByID: async (id) => {
			return await getJSON(this.getPath(`/smtp-configuration/${id}`));
		},

		/**
		 * Send a test email using the configuration.
		 *
		 * @param {string} id
		 * @param {Object} data
		 * @param {string} data.email
		 * @param {string} data.mailFrom
		 * @returns {Promise<ApiResponse>}
		 */
		sendTestEmail: async (id, data) => {
			return await postJSON(this.getPath(`/smtp-configuration/${id}/test-email`), data);
		},

		/**
		 * Create a new SMTPConfiguration.
		 *
		 * @param {Object} configuration
		 * @param {string} configuration.name
		 * @param {string} configuration.host
		 * @param {number} configuration.port
		 * @param {string} configuration.username
		 * @param {string} configuration.password
		 * @param {boolean} configuration.ignoreCertErrors
		 * @param {string} configuration.companyID
		 * @returns
		 */
		create: async (configuration) => {
			return await postJSON(this.getPath('/smtp-configuration'), configuration);
		},

		/**
		 * Update a SMTPConfiguration.
		 *
		 * @param {Object} configuration
		 * @param {string} configuration.id
		 * @param {string} configuration.name
		 * @param {string} configuration.host
		 * @param {number} configuration.port
		 * @param {string} configuration.username
		 * @param {string} configuration.password
		 * @param {boolean} configuration.ignoreCertErrors
		 * @param {string} configuration.companyID
		 * @returns {Promise<ApiResponse>}
		 */
		update: async (configuration) => {
			return await patchJSON(
				this.getPath(`/smtp-configuration/${configuration.id}`),
				configuration
			);
		},

		/**
		 * Delete a SMTPConfiguration.
		 *
		 * @param {string} id
		 * @returns {Promise<ApiResponse>}
		 */
		delete: async (id) => {
			return await deleteJSON(this.getPath(`/smtp-configuration/${id}`));
		},

		/**
		 * Add a new SMTP header to a SMTPConfiguration.
		 *
		 * @param {string} smtpConfigurationID
		 * @param {string} key
		 * @param {string} value
		 * @returns {Promise<ApiResponse>}
		 */
		addHeader: async (smtpConfigurationID, key, value) => {
			return await patchJSON(this.getPath(`/smtp-configuration/${smtpConfigurationID}/header`), {
				key: key,
				value: value
			});
		},

		/**
		 * Delete a SMTP header from a SMTPConfiguration.
		 *
		 * @param {string} smtpConfigurationID
		 * @param {string} headerID
		 * @returns {Promise<ApiResponse>}
		 */
		deleteHeader: async (smtpConfigurationID, headerID) => {
			return await deleteJSON(
				this.getPath(`/smtp-configuration/${smtpConfigurationID}/header/${headerID}`)
			);
		}
	};

	/**
	 * user is the API for user related operations - these actions also affect the user's sessions
	 */
	user = {
		/**
		 * Create a new user
		 *
		 * @param {Object} user
		 * @param {string} user.username
		 * @param {string} user.password
		 * @param {string} user.email
		 * @param {string} user.fullname
		 * @returns {Promise<ApiResponse>}
		 */
		create: async ({ username, password, email, fullname }) => {
			return await postJSON(this.getPath(`/user`), {
				username,
				password,
				email,
				fullname
			});
		},

		/**
		 * Delete a user by ID.
		 *
		 * @param {string} id
		 * @returns {Promise<ApiResponse>}
		 */
		delete: async (id) => {
			return await deleteJSON(this.getPath(`/user/${id}`));
		},

		/**
		 * Get all users using pagination.
		 *
		 * @param {TableURLParams} options
		 * @returns {Promise<ApiResponse>}
		 */
		getAll: async (options) => {
			return await getJSON(this.getPath(`/user?${appendQuery(options)}`));
		},

		/**
		 * Get a user by ID.
		 *
		 * @param {string} id
		 * @returns {Promise<ApiResponse>}
		 * */
		getByID: async (id) => {
			return await getJSON(this.getPath(`/user/${id}`));
		},

		/**
		 * Get user's sessions.
		 *
		 * @param {TableURLParams} options
		 * @returns {Promise<ApiResponse>}
		 */
		getAllSessions: async (options) => {
			return await getJSON(this.getPath(`/user/sessions?${appendQuery(options)}`));
		},

		/**
		 * Update a user.
		 *
		 * @param {Object} user
		 * @param {string} user.id
		 * @param {string} user.username
		 * @param {string} user.email
		 * @param {string} user.fullname
		 * @returns {Promise<ApiResponse>}
		 */
		updateByID: async ({ id, username, email, fullname }) => {
			return await postJSON(this.getPath(`/user/${id}`), {
				username: username,
				email: email,
				name: fullname
			});
		},

		/**
		 * Get user's api key in a masked format.
		 *
		 * @returns {Promise<ApiResponse>}
		 */
		getAPIKeyMasked: async () => {
			return await getJSON(this.getPath(`/user/api`));
		},

		/**
		 * Upsert the logged-in users api key
		 *
		 * @returns {Promise<ApiResponse>}
		 */
		upsertAPIKey: async () => {
			return await postJSON(this.getPath(`/user/api`), {});
		},

		/**
		 * Removes the logged-in users api key
		 *
		 * @returns {Promise<ApiResponse>}
		 */
		removeAPIKey: async () => {
			return await deleteJSON(this.getPath(`/user/api`), {});
		},

		/**
		 * Login.
		 *
		 * @param {string} username
		 * @param {string} password
		 * @returns {Promise<ApiResponse>}
		 */
		login: async (username, password) => {
			return await postJSON(this.getPath(`/user/login`), {
				username,
				password
			});
		},

		/**
		 * Login with TOTP MFA.
		 *
		 * @param {string} username
		 * @param {string} password
		 * @param {string} token
		 * @returns {Promise<ApiResponse>}
		 * */
		loginTOTP: async (username, password, token) => {
			return await postJSON(this.getPath(`/user/login`), {
				username,
				password,
				totp: token
			});
		},

		/**
		 * Login with MFA recovery code.
		 *
		 * @param {string} username
		 * @param {string} password
		 * @param {string} recoveryCode
		 * @returns {Promise<ApiResponse>}
		 * */
		loginMFARecoveryCode: async (username, password, recoveryCode) => {
			return await postJSON(this.getPath(`/user/login`), {
				username,
				password,
				totp: '',
				recoveryCode: recoveryCode
			});
		},

		/**
		 * Log out.
		 *
		 * @returns {Promise<ApiResponse>}
		 */
		logout: async () => {
			return await postJSON(this.getPath(`/user/logout`));
		},

		/**
		 * Change the user's username
		 *
		 * @param {string} newUsername
		 * @returns {Promise<ApiResponse>}
		 */
		changeUsername: async (newUsername) => {
			return await postJSON(this.getPath(`/user/username`), {
				username: newUsername
			});
		},

		/**
		 * Change the user's full name
		 *
		 * @param {string} newFullname
		 * @returns {Promise<ApiResponse>}
		 */
		changeFullname: async (newFullname) => {
			return await postJSON(this.getPath(`/user/fullname`), {
				fullname: newFullname
			});
		},

		/**
		 * Change the user's password
		 *
		 * @param {string} currentPassword
		 * @param {string} newPassword
		 * @returns {Promise<ApiResponse>}
		 */
		changePassword: async (currentPassword, newPassword) => {
			return await postJSON(this.getPath(`/user/password`), {
				currentPassword,
				newPassword
			});
		},

		/**
		 *
		 * @param {string} password
		 * @returns {Promise<ApiResponse>}
		 */
		setupTOTPMFA: async (password) => {
			return await postJSON(this.getPath(`/user/mfa/totp/setup`), {
				password: password
			});
		},

		/**
		 * Verify TOTP MFA setup
		 *
		 * @param {string} token
		 * @returns {Promise<ApiResponse>}
		 */
		setupVerifyTOTPMFA: async (token) => {
			return await postJSON(this.getPath(`/user/mfa/totp/setup/verify`), {
				token: token
			});
		},

		/**
		 * Check if TOTP MFA is enabled
		 *
		 * @returns {Promise<ApiResponse>}
		 */
		isTOTPMFAEnabled: async () => {
			return await getJSON(this.getPath(`/user/mfa/totp`));
		},

		/**
		 * Verify TOTP MFA
		 *
		 * @param {string} token
		 * @returns
		 */
		verifyTOTPMFA: async (token) => {
			return await postJSON(this.getPath(`/user/mfa/totp/verify`), {
				token: token
			});
		},

		/**
		 * Disable TOTP MFA
		 * Requires the TOTP token to complete
		 *
		 * @param {string} totpToken
		 * @returns
		 */
		disableTOTPMFA: async (totpToken = '') => {
			return await postJSON(this.getPath(`/user/mfa/totp`), {
				token: totpToken
			});
		},

		/**
		 * Disable TOTP MFA
		 * Requires the TOTP token to complete
		 *
		 * @param {string} userID
		 * @returns
		 */
		invalidateSessions: async (userID = '') => {
			if (userID) {
				return await postJSON(this.getPath(`/user/sessions/invalidate`), { userID });
			}
			const res = await fetch(this.getPath(`/user/sessions/invalidate`), {
				method: 'POST',
				headers: {
					'Content-Type': 'application/json'
				}
			});
			let body = {};
			try {
				body = await res.json();
			} catch (e) {
				body = {
					success: false,
					error: 'invalid JSON in response'
				};
			}
			return newResponse(body.success, res.status, body.error, body.data);
		}
	};

	/**
	 * option is the API settings.
	 */
	option = {
		/**
		 * Get setting by key.
		 *
		 * @param {'is_installed'|'max_file_upload_size_mb'|'repeat_offender_months'|'sso_login'} key
		 * @returns {Promise<ApiResponse>}
		 */
		get: async (key) => {
			return await getJSON(this.getPath(`/option/${key}`));
		},

		/**
		 * Set setting by key and value.
		 *
		 * @param {'max_file_upload_size_mb'|'repeat_offender_months'|'sso_login'} key
		 * @param {string} value
		 * @returns {Promise<ApiResponse>}
		 */
		set: async (key, value) => {
			return await postJSON(this.getPath(`/option`), {
				key: key,
				value: value
			});
		}
	};

	/**
	 * log is the API for log related operations.
	 */
	recipient = {
		/**
		 * Get all recipients using pagination.
		 *
		 * @param {string|null} companyID
		 * @returns {Promise<ApiResponse>}
		 */
		countRepeatOffenders: async (companyID = null) => {
			return await getJSON(
				this.getPath(
					`/recipient/repeat-offenders?${appendQuery(null)}${this.appendCompanyQuery(companyID)}`
				)
			);
		},

		/**
		 * Get all recipients using pagination.
		 *
		 * @param {TableURLParams} options
		 * @param {string|null} companyID
		 * @returns {Promise<ApiResponse>}
		 */
		getAll: async (options, companyID = null) => {
			return await getJSON(
				this.getPath(`/recipient?${appendQuery(options)}${this.appendCompanyQuery(companyID)}`)
			);
		},

		/**
		 * Get all orphaned recipients (recipients not in any group) using pagination.
		 *
		 * @param {TableURLParams} options
		 * @param {string|null} companyID
		 * @returns {Promise<ApiResponse>}
		 */
		getOrphaned: async (options, companyID = null) => {
			return await getJSON(
				this.getPath(
					`/recipient/orphaned?${appendQuery(options)}${this.appendCompanyQuery(companyID)}`
				)
			);
		},

		/**
		 * Delete all orphaned recipients (recipients not in any group).
		 *
		 * @param {string|null} companyID
		 * @returns {Promise<ApiResponse>}
		 */
		deleteAllOrphaned: async (companyID = null) => {
			return await deleteReq(
				this.getPath(`/recipient/orphaned/delete?${this.appendCompanyQuery(companyID)}`)
			);
		},

		/**
		 * Get campaign events related by recipient id and optional campaign id
		 *
		 * @param {string} recipientID
		 * @param {TableURLParams} options
		 * @param {string} [campaignID]
		 * @returns {Promise<ApiResponse>}
		 */
		getEvents: async (recipientID, options, campaignID) => {
			let path = `/recipient/${recipientID}/events?${appendQuery(options)}`;
			if (campaignID) {
				path += `&campaignID=${campaignID}`;
			}
			return await getJSON(this.getPath(path));
		},

		/**
		 * Get a recipient stats by ID.
		 *
		 * @param {string} id
		 * @returns {Promise<ApiResponse>}
		 */
		getStatsByID: async (id) => {
			return await getJSON(this.getPath(`/recipient/${id}/stats`));
		},

		/**
		 * Get a recipient by its ID.
		 *
		 * @param {string} id
		 * @returns {Promise<ApiResponse>}
		 */
		getByID: async (id) => {
			return await getJSON(this.getPath(`/recipient/${id}`));
		},

		/**
		 * Update a recipient.
		 *
		 * @param {object} recipient
		 * @param {string} recipient.id
		 * @param {string} recipient.email
		 * @param {string} recipient.phone
		 * @param {string} recipient.extraIdentifier
		 * @param {string} recipient.firstName
		 * @param {string} recipient.lastName
		 * @param {string} recipient.position
		 * @param {string} recipient.department
		 * @param {string} recipient.city
		 * @param {string} recipient.country
		 * @param {string} recipient.misc
		 * @param {string} recipient.companyID
		 * @returns {Promise<ApiResponse>}
		 */
		update: async ({
			id,
			email,
			phone,
			extraIdentifier,
			firstName,
			lastName,
			position,
			department,
			city,
			country,
			misc,
			companyID
		}) => {
			return await patchJSON(this.getPath(`/recipient/${id}`), {
				email: email,
				phone: phone,
				extraIdentifier: extraIdentifier,
				firstName: firstName,
				lastName: lastName,
				position: position,
				department: department,
				city: city,
				country: country,
				misc: misc,
				companyID: companyID
			});
		},

		/**
		 * Create a new recipient.
		 *
		 * @param {object} recipient
		 * @param {string} recipient.email
		 * @param {string} recipient.phone
		 * @param {string} recipient.extraIdentifier
		 * @param {string} recipient.firstName
		 * @param {string} recipient.lastName
		 * @param {string} recipient.position
		 * @param {string} recipient.department
		 * @param {string} recipient.city
		 * @param {string} recipient.country
		 * @param {string} recipient.misc
		 * @param {string} recipient.companyID
		 * @returns {Promise<ApiResponse>}
		 */
		create: async ({
			email,
			phone,
			extraIdentifier,
			firstName,
			lastName,
			position,
			department,
			city,
			country,
			misc,
			companyID
		}) => {
			return await postJSON(this.getPath('/recipient/'), {
				email: email,
				phone: phone,
				extraIdentifier: extraIdentifier,
				firstName: firstName,
				lastName: lastName,
				position: position,
				department: department,
				city: city,
				country: country,
				misc: misc,
				companyID: companyID
			});
		},

		/**
		 * Delete a recipient.
		 *
		 * @param {string} id
		 * @returns {Promise<ApiResponse>}
		 */
		delete: async (id) => {
			return await deleteJSON(this.getPath(`/recipient/${id}`));
		},

		/**
		 * Create a new recipient group.
		 *
		 * @param {string} name
		 * @param {string} companyID
		 * @param {Object[]} recipients // TODO define type for recipient
		 * @returns {Promise<ApiResponse>}
		 */
		createGroup: async (name, companyID, recipients) => {
			return await postJSON(this.getPath('/recipient/group'), {
				name: name,
				companyID: companyID,
				recipients: recipients ?? []
			});
		},

		/**
		 * Update a recipient group - not the recipients in the group.
		 *
		 * @param {object} group
		 * @param {string} group.id
		 * @param {string} [group.name ]
		 * @param {string} [group.companyID]
		 * @returns {Promise<ApiResponse>}
		 */
		updateGroup: async ({ id, name, companyID }) => {
			return await patchJSON(this.getPath(`/recipient/group/${id}`), {
				name: name,
				companyID: companyID
			});
		},

		/**
		 * Get all recipient groups using pagination.
		 *
		 * @param {TableURLParams} options
		 * @param {string|null} companyID
		 * @returns {Promise<ApiResponse>}
		 */
		getAllGroups: async (options, companyID) => {
			return await getJSON(
				this.getPath(
					`/recipient/group?${appendQuery(options)}${this.appendCompanyQuery(companyID)}`
				)
			);
		},

		/**
		 * Get all recipients in a group using pagination.
		 *
		 * @param {string} id
		 * @param {TableURLParams} options
		 * @returns {Promise<ApiResponse>}
		 */
		getAllByGroupID: async (id, options) => {
			return await getJSON(
				this.getPath(`/recipient/group/${id}/recipients?${appendQuery(options)}`)
			);
		},

		/**
		 * Get a recipient group by its ID.
		 *
		 * @param {string} id
		 * @returns {Promise<ApiResponse>}
		 */
		getGroupByID: async (id) => {
			return await getJSON(this.getPath(`/recipient/group/${id}`));
		},

		/**
		 * Import recipients.
		 *
		 * @param {Object} import
		 * @param {Object[]} import.recipients // TODO define type for recipients
		 * @param {string} import.companyID
		 * @param {boolean} import.ignoreOverwriteEmptyFields existing recipient data for empty fields
		 * @returns {Promise<ApiResponse>}
		 */
		import: async ({ recipients, companyID, ignoreOverwriteEmptyFields = false }) => {
			return await postJSON(this.getPath(`/recipient/import`), {
				recipients: recipients,
				ignoreOverwriteEmptyFields: ignoreOverwriteEmptyFields,
				companyID: companyID
			});
		},

		/**
		 * Import recipients to a group.
		 *
		 * @param {Object} import
		 * @param {Object[]} import.recipients // TODO define type for recipients
		 * @param {string} import.groupID
		 * @param {string} import.companyID
		 * @param {boolean} import.ignoreOverwriteEmptyFields existing recipient data for empty fields
		 * @returns {Promise<ApiResponse>}
		 */
		importToGroup: async ({
			recipients,
			groupID,
			companyID,
			ignoreOverwriteEmptyFields = false
		}) => {
			return await putJSON(this.getPath(`/recipient/group/${groupID}/import`), {
				recipients: recipients,
				ignoreOverwriteEmptyFields: ignoreOverwriteEmptyFields,
				companyID: companyID
			});
		},

		/**
		 * @param {string} recipientID
		 * @returns
		 */
		export: async (recipientID) => {
			window.open(this.getPath(`/recipient/${recipientID}/export`), '_blank');
		},

		/**
		 * Add recipients to a group.
		 *
		 * @param {string} groupID
		 * @param {Object[]} recipients // TODO define type for recipients
		 * @returns {Promise<ApiResponse>}
		 */
		addToGroup: async (groupID, recipients) => {
			return await postJSON(this.getPath(`/recipient/group/${groupID}/recipients`), {
				recipientIDs: recipients
			});
		},

		/**
		 * Remove recipients from a group.
		 *
		 * @param {string} groupID
		 * @param {*[]} recipients // TODO define type for recipients
		 * @returns {Promise<ApiResponse>}
		 */
		removeFromGroup: async (groupID, recipients) => {
			return await deleteJSON(this.getPath(`/recipient/group/${groupID}/recipients`), {
				recipientIDs: recipients
			});
		},

		/**
		 * Delete a recipient group.
		 *
		 * @param {string} id
		 * @returns {Promise<ApiResponse>}
		 */
		deleteGroup: async (id) => {
			return await deleteJSON(this.getPath(`/recipient/group/${id}`));
		}
	};

	/**
	 * api sender is the API for the API sender related operations.
	 *
	 * @type {Object}
	 **/
	apiSender = {
		/**
		 * Create a new API sender.
		 *
		 * @param {Object} sender
		 * @param {string} sender.name
		 * @param {string} sender.apiKey
		 * @param {string} sender.companyID
		 * @param {string} sender.customField1
		 * @param {string} sender.customField2
		 * @param {string} sender.customField3
		 * @param {string} sender.customField4
		 * @param {string} sender.requestMethod
		 * @param {string} sender.requestURL
		 * @param {APISenderHeader[]} sender.requestHeaders
		 * @param {string} sender.requestBody
		 * @param {string|number} sender.expectedResponseStatusCode
		 * @param {APISenderHeader[]} sender.expectedResponseHeaders
		 * @param {string} sender.expectedResponseBody
		 * @returns {Promise<ApiResponse>}
		 */
		create: async ({
			name,
			apiKey,
			companyID,
			customField1,
			customField2,
			customField3,
			customField4,
			requestMethod,
			requestURL,
			requestHeaders,
			requestBody,
			expectedResponseStatusCode,
			expectedResponseHeaders,
			expectedResponseBody
		}) => {
			if (typeof expectedResponseStatusCode === 'string' && expectedResponseStatusCode.length > 0) {
				expectedResponseStatusCode = parseInt(expectedResponseStatusCode);
			} else {
				expectedResponseStatusCode = null;
			}
			return await postJSON(this.getPath('/api-sender'), {
				name: name,
				apiKey: apiKey,
				companyID: companyID,
				customField1: customField1,
				customField2: customField2,
				customField3: customField3,
				customField4: customField4,
				requestMethod: requestMethod,
				requestURL: requestURL,
				requestHeaders: requestHeaders,
				requestBody: requestBody,
				expectedResponseStatusCode: expectedResponseStatusCode,
				expectedResponseHeaders: expectedResponseHeaders,
				expectedResponseBody: expectedResponseBody
			});
		},

		/**
		 * Update an API sender.
		 *
		 * @param {Object} sender
		 * @param {string} sender.id
		 * @param {string} sender.name
		 * @param {string} sender.apiKey
		 * @param {string} sender.companyID
		 * @param {string} sender.customField1
		 * @param {string} sender.customField2
		 * @param {string} sender.customField3
		 * @param {string} sender.customField4
		 * @param {string} sender.requestMethod
		 * @param {string} sender.requestURL
		 * @param {APISenderHeader[]} sender.requestHeaders
		 * @param {string} sender.requestBody
		 * @param {string|number} sender.expectedResponseStatusCode
		 * @param {APISenderHeader[]} sender.expectedResponseHeaders
		 * @param {string} sender.expectedResponseBody
		 */
		update: async ({
			id,
			name,
			apiKey,
			companyID,
			customField1,
			customField2,
			customField3,
			customField4,
			requestMethod,
			requestURL,
			requestHeaders,
			requestBody,
			expectedResponseStatusCode,
			expectedResponseHeaders,
			expectedResponseBody
		}) => {
			if (typeof expectedResponseStatusCode === 'string' && expectedResponseStatusCode.length > 0) {
				expectedResponseStatusCode = parseInt(expectedResponseStatusCode);
			} else if (typeof expectedResponseStatusCode === 'number') {
				// noop
			} else {
				expectedResponseStatusCode = null;
			}

			return await patchJSON(this.getPath(`/api-sender/${id}`), {
				name: name,
				apiKey: apiKey,
				companyID: companyID,
				customField1: customField1,
				customField2: customField2,
				customField3: customField3,
				customField4: customField4,
				requestMethod: requestMethod,
				requestURL: requestURL,
				requestHeaders: requestHeaders,
				requestBody: requestBody,
				expectedResponseStatusCode: expectedResponseStatusCode,
				expectedResponseHeaders: expectedResponseHeaders,
				expectedResponseBody: expectedResponseBody
			});
		},

		/**
		 * Get all API senders using pagination.
		 *
		 * @param {TableURLParams} options
		 * @param {string|null} companyID
		 * @returns {Promise<ApiResponse>}
		 */
		getAll: async (options, companyID = null) => {
			return await getJSON(
				this.getPath(`/api-sender?${appendQuery(options)}${this.appendCompanyQuery(companyID)}`)
			);
		},

		/**
		 * Get all overview API senders using pagination.
		 *
		 * @param {TableURLParams} options
		 * @param {string|null} companyID
		 * @returns {Promise<ApiResponse>}
		 */
		getAllOverview: async (options, companyID = null) => {
			return await getJSON(
				this.getPath(
					`/api-sender/overview?${appendQuery(options)}${this.appendCompanyQuery(companyID)}`
				)
			);
		},

		/**
		 * Get an API sender by its ID.
		 *
		 * @param {string} id
		 * @returns {Promise<ApiResponse>}
		 */
		getByID: async (id) => {
			return await getJSON(this.getPath(`/api-sender/${id}`));
		},

		/**
		 * Delete an API sender by its ID.
		 *
		 * @param {string} id
		 * @returns {Promise<ApiResponse>}
		 */
		delete: async (id) => {
			return await deleteJSON(this.getPath(`/api-sender/${id}`));
		},

		/**
		 * Send a test request to an API sender.
		 * @param {string} id
		 * @returns {Promise<ApiResponse>}
		 */
		test: async (id) => {
			return await postJSON(this.getPath(`/api-sender/${id}/test`));
		}
	};

	/**
	 * log is the API for log related operations.
	 */
	allowDeny = {
		/**
		 * Create a new allowdeny list.
		 *
		 * @param {Object} allowdeny
		 * @param {string} allowdeny.name
		 * @param {string} allowdeny.cidrs
		 * @param {string} allowdeny.ja4Fingerprints
		 * @param {string} allowdeny.countryCodes
		 * @param {boolean} allowdeny.allowed
		 * @param {string} allowdeny.companyID
		 * @returns {Promise<ApiResponse>}
		 */
		create: async ({ name, cidrs, ja4Fingerprints, countryCodes, allowed, companyID }) => {
			return await postJSON(this.getPath('/allow-deny'), {
				name: name,
				cidrs: cidrs,
				ja4Fingerprints: ja4Fingerprints,
				countryCodes: countryCodes,
				allowed: allowed,
				companyID: companyID
			});
		},

		/**
		 * GetAll allowdeny list.
		 *
		 * @param {TableURLParams} options
		 * @param {string|null} companyID
		 * @returns {Promise<ApiResponse>}
		 */
		getAll: async (options, companyID = null) => {
			return await getJSON(
				this.getPath(`/allow-deny?${appendQuery(options)}${this.appendCompanyQuery(companyID)}`)
			);
		},

		/**
		 * GetAllOverview gets allowdeny list without cidrs.
		 *
		 * @param {TableURLParams} options
		 * @param {string|null} companyID
		 * @returns {Promise<ApiResponse>}
		 */
		getAllOverview: async (options, companyID = null) => {
			return await getJSON(
				this.getPath(
					`/allow-deny/overview?${appendQuery(options)}${this.appendCompanyQuery(companyID)}`
				)
			);
		},

		/**
		 * Get an allowdeny list by its ID.
		 *
		 * @param {string} id
		 * @returns {Promise<ApiResponse>}
		 */
		getByID: async (id) => {
			return await getJSON(this.getPath(`/allow-deny/${id}`));
		},

		/**
		 * Update an allowdeny list.
		 *
		 * @param {Object} allowdeny
		 * @param {string} allowdeny.id
		 * @param {string} allowdeny.name
		 * @param {string} allowdeny.cidrs
		 * @param {string} allowdeny.ja4Fingerprints
		 * @param {string} allowdeny.countryCodes
		 * @param {string} allowdeny.companyID
		 * @returns {Promise<ApiResponse>}
		 */
		update: async ({ id, name, cidrs, ja4Fingerprints, countryCodes, companyID }) => {
			return await patchJSON(this.getPath(`/allow-deny/${id}`), {
				name: name,
				cidrs: cidrs,
				ja4Fingerprints: ja4Fingerprints,
				countryCodes: countryCodes,
				companyID: companyID
			});
		},

		/**
		 * Delete an allowdeny list by its ID.
		 *
		 * @param {string} id
		 * @returns {Promise<ApiResponse>}
		 */
		delete: async (id) => {
			return await deleteJSON(this.getPath(`/allow-deny/${id}`));
		}
	};

	/**
	 * geoip is the API for GeoIP related operations.
	 */
	geoip = {
		/**
		 * Get GeoIP metadata including available country codes.
		 *
		 * @returns {Promise<ApiResponse>}
		 */
		getMetadata: async () => {
			return await getJSON(this.getPath('/geoip/metadata'));
		}
	};

	/**
	 * webhook is the API for web hook related operations.
	 */
	webhook = {
		/**
		 * Create a new webhook.
		 *
		 * @param {Object} webhook
		 * @param {string} webhook.name
		 * @param {string} webhook.url
		 * @param {string} [webhook.secret]
		 * @param {string} [webhook.companyID]
		 * @returns {Promise<ApiResponse>}
		 */
		create: async ({ name, url, secret, companyID }) => {
			return await postJSON(this.getPath('/webhook'), {
				name: name,
				url: url,
				secret: secret,
				companyID: companyID
			});
		},

		/**
		 * GetAll webhooks.
		 *
		 * @param {TableURLParams} options
		 * @param {string|null} companyID
		 * @returns {Promise<ApiResponse>}
		 */
		getAll: async (options, companyID = null) => {
			return await getJSON(
				this.getPath(`/webhook?${appendQuery(options)}${this.appendCompanyQuery(companyID)}`)
			);
		},

		/**
		 * Get a webhook by its ID.
		 *
		 * @param {string} id
		 * @returns {Promise<ApiResponse>}
		 */
		getByID: async (id) => {
			return await getJSON(this.getPath(`/webhook/${id}`));
		},

		/**
		 * Update a webhook.
		 *
		 * @param {Object} webhook
		 * @param {string} webhook.id
		 * @param {string} webhook.name
		 * @param {string} webhook.url
		 * @param {string} webhook.secret
		 * @param {string} webhook.companyID
		 * @returns {Promise<ApiResponse>}
		 */
		update: async ({ id, name, url, secret, companyID }) => {
			return await patchJSON(this.getPath(`/webhook/${id}`), {
				name: name,
				url: url,
				secret: secret,
				companyID: companyID
			});
		},

		/**
		 * Delete a webhook by its ID.
		 *
		 * @param {string} id
		 * @returns {Promise<ApiResponse>}
		 */
		delete: async (id) => {
			return await deleteJSON(this.getPath(`/webhook/${id}`));
		},

		/**
		 * Test a webhook.
		 *
		 * @param {string} id
		 * @returns {Promise<ApiResponse>}
		 */
		test: async (id) => {
			return await postJSON(this.getPath(`/webhook/${id}/test`));
		}
	};

	/**
	 * identifier is for campaign identifiers, ala. 'rid' in gophish
	 */
	identifier = {
		/**
		 * @param {TableURLParams} options
		 * @returns {Promise<ApiResponse>}
		 */
		getAll: async (options) => {
			return await getJSON(this.getPath(`/identifier?${appendQuery(options)}`));
		}
	};

	/**
	 * sso is for handling sso configuration
	 */
	sso = {
		/**
		 * @param {object} sso
		 * @param {string} sso.clientID
		 * @param {string} sso.tenantID
		 * @param {string} sso.clientSecret
		 * @param {string} sso.redirectURL
		 * @returns {Promise<ApiResponse>}
		 */
		upsert: async (sso) => {
			return await postJSON(this.getPath(`/sso/entra-id`), sso);
		},

		isEnabled: async () => {
			return await getJSON(this.getPath(`/sso/entra-id/enabled`));
		}
	};

	/**
	 * utils is for useful utils
	 */
	utils = {
		/**
		 * @param {object} qr
		 * @param {string} qr.url
		 * @param {number} qr.dotSize
		 * @returns {Promise<ApiResponse>}
		 */
		qr: async (qr) => {
			return await postJSON(this.getPath(`/qr/html`), qr);
		}
	};

	version = {
		/**
		 * @returns {Promise<ApiResponse>}
		 */
		get: async () => {
			return await getJSON(this.getPath(`/version`));
		}
	};

	/**
	 * proxy is the API for Proxy related operations.
	 */
	proxy = {
		/**
		 * Get a Proxy by its ID.
		 *
		 * @param {string} id
		 * @returns {Promise<ApiResponse>}
		 */
		getByID: async (id) => {
			return await getJSON(this.getPath(`/proxy/${id}`));
		},

		/**
		 * Get all Proxies using pagination.
		 *
		 * @param {TableURLParams} options
		 * @param {string|null} companyID
		 * @returns {Promise<ApiResponse>}
		 */
		getAll: async (options, companyID = null) => {
			return await getJSON(
				this.getPath(`/proxy?${appendQuery(options)}${this.appendCompanyQuery(companyID)}`)
			);
		},

		/**
		 * Get all Proxies overview using pagination.
		 *
		 * @param {TableURLParams} options
		 * @param {string|null} companyID
		 * @returns {Promise<ApiResponse>}
		 */
		getAllSubset: async (options, companyID = null) => {
			return await getJSON(
				this.getPath(`/proxy/overview?${appendQuery(options)}${this.appendCompanyQuery(companyID)}`)
			);
		},

		/**
		 * Create a new Proxy.
		 *
		 * @param {object} proxy
		 * @param {string} proxy.name
		 * @param {string} proxy.description
		 * @param {string} proxy.startURL
		 * @param {string} proxy.proxyConfig
		 * @param {string} proxy.companyID
		 * @returns {Promise<ApiResponse>}
		 */
		create: async ({ name, description, startURL, proxyConfig, companyID }) => {
			return await postJSON(this.getPath('/proxy'), {
				name: name,
				description: description,
				startURL: startURL,
				proxyConfig: proxyConfig,
				companyID: companyID
			});
		},

		/**
		 * Update a Proxy.
		 *
		 * @param {string} id
		 * @param {object} proxy
		 * @param {string} proxy.name
		 * @param {string} proxy.description
		 * @param {string} proxy.startURL
		 * @param {string} proxy.proxyConfig
		 * @returns {Promise<ApiResponse>}
		 */
		update: async (id, proxy) => {
			return await patchJSON(this.getPath(`/proxy/${id}`), proxy);
		},

		/**
		 * Delete a Proxy.
		 *
		 * @param {string} id
		 * @returns {Promise<ApiResponse>}
		 */
		delete: async (id) => {
			return await deleteJSON(this.getPath(`/proxy/${id}`));
		}
	};

	/**
	 * ipAllowList is the API for IP Allow List related operations.
	 */
	ipAllowList = {
		/**
		 * Get IP allow list entries for a specific proxy configuration.
		 *
		 * @param {string} proxyConfigID
		 * @returns {Promise<ApiResponse>}
		 */
		getForProxyConfig: async (proxyConfigID) => {
			return await getJSON(this.getPath(`/ip-allow-list/proxy-config/${proxyConfigID}`));
		},

		/**
		 * Clear all entries for a specific proxy configuration.
		 *
		 * @param {string} proxyConfigID
		 * @returns {Promise<ApiResponse>}
		 */
		clearForProxyConfig: async (proxyConfigID) => {
			return await deleteJSON(this.getPath(`/ip-allow-list/clear-proxy-config/${proxyConfigID}`));
		}
	};

	/**
	 * import is for importing assets, landing pages and etc
	 */
	import = {
		import: async (fileOrFormData) => {
			if (fileOrFormData instanceof FormData) {
				return await postMultipart(this.getPath('/import'), fileOrFormData);
			} else if (fileOrFormData) {
				const formData = new FormData();
				formData.append('file', fileOrFormData);
				return await postMultipart(this.getPath('/import'), formData);
			}
		}
	};
}
