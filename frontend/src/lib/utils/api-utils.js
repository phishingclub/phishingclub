// max number of items to fetch per request
const global_pagination_max = 1000;

/**
 * @callback FetchFn
 * @param {TableURLParams} options
 * @returns {Promise<import("$lib/api/client").ApiResponse>}
 * @async
 */

/**
 * TableURLParams is a type that represents the query parameters for a table.
 *
 * @typedef {object} TableURLParams
 * @property {number} currentPage
 * @property {number} perPage
 * @property {string} sortBy
 * @property {string} sortOrder
 * @property {string} search
 */

/**
 * @param {FetchFn} fetchFn2
 * @param {TableURLParams} options
 * @returns {Promise<Object[]>}
 */

export const defaultOptions = {
	currentPage: 1,
	perPage: global_pagination_max,
	sortBy: 'name',
	sortOrder: 'asc',
	search: ''
};

export const fetchAllRows = async (fetchFn2, options = defaultOptions) => {
	let items = [];
	let res = await fetchFn2(options);

	// Add initial rows
	if (res.data?.rows) {
		items = items.concat(res.data.rows);
	}

	while (res.data?.hasNextPage) {
		// Update the page number
		options.currentPage += 1;
		// Fetch next page
		res = await fetchFn2({ ...options });
		// Add new rows
		if (res.data?.rows) {
			items = items.concat(res.data.rows);
		}
	}

	return items;
};

/**
 * @param {Date} date
 * @returns {string}
 */
export const utc_yyyy_mm_dd = (date) => {
	return date.toISOString().split('T')[0];
};

export const local_yyyy_mm_dd = (date) => {
	return (
		date.getFullYear() +
		'-' +
		String(date.getMonth() + 1).padStart(2, '0') +
		'-' +
		String(date.getDate()).padStart(2, '0')
	);
};

/**
 * Compares two times formatted as "HH:MM" to determine if the first is larger than the second.
 * @param {string} time1 - The first time to compare
 * @param {string} time2 - The second time to compare
 * @returns {boolean} - True if time1 is larger than time2, false otherwise
 */
export function isTimeLarger(time1, time2) {
	// Split the time strings into hours and minutes
	const [hours1, minutes1] = time1.split(':').map(Number);
	const [hours2, minutes2] = time2.split(':').map(Number);

	// Compare hours first
	if (hours1 > hours2) {
		return true;
	} else if (hours1 < hours2) {
		return false;
	}
	// If hours are equal, compare minutes
	return minutes1 >= minutes2;
}
