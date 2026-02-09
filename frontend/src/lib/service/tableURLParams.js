import { goto } from '$app/navigation';
import { getPerPagePreference } from '$lib/store/preferences';

const minPage = 1;
const minPerPage = 10;
const maxPerPage = 100;
export const defaultStartPage = 1;
export const defaultPerPage = getPerPagePreference();
const acceptedPerPageValues = [10, 25, 50];
const defaultSortBy = 'updated_at';
const defaultSortOrder = 'desc';

const defaultOptions = {
	page: 1,
	perPage: getPerPagePreference(),
	sortBy: 'name',
	sortOrder: 'asc',
	search: '',
	onPopState: () => {},
	prefix: '',
	noScroll: false
};

const param = (key, prefix) => {
	return prefix ? `${prefix}_${key}` : key;
};

class URLUpdateQueue {
	static instance;
	queue = [];
	isProcessing = false;

	static getInstance() {
		if (!URLUpdateQueue.instance) {
			URLUpdateQueue.instance = new URLUpdateQueue();
		}
		return URLUpdateQueue.instance;
	}

	async add(updateFn) {
		this.queue.push(updateFn);
		if (!this.isProcessing) {
			await this.process();
		}
	}

	async process() {
		this.isProcessing = true;
		while (this.queue.length > 0) {
			const update = this.queue.shift();
			await update();
		}
		this.isProcessing = false;
	}
}

/** @typedef {Object} TableURLParamsOptions
 * @property {number} [page]
 * @property {number} [perPage]
 * @property {string} [sortBy]
 * @property {string} [sortOrder]
 * @property {string} [search]
 * @property {() => void} [onPopState]
 * @property {string} [prefix]
 * @property {boolean} [noScroll]
 */

/**
 * @param {TableURLParamsOptions} [options]
 */
export const newTableURLParams = (
	options = {
		page: defaultStartPage,
		sortBy: defaultSortBy,
		sortOrder: defaultSortOrder,
		search: '',
		onPopState: () => {},
		prefix: '',
		noScroll: false
	}
) => {
	// get stored perPage preference at runtime (fresh value)
	const storedPerPage = getPerPagePreference();

	let urlParams = new URLSearchParams(window.location.search);
	const initialPath = window.location.pathname;

	// first add the default values
	const state = {
		...defaultOptions,
		perPage: storedPerPage // use fresh value from localStorage instead of stale defaultOptions
	};
	if (options.page) {
		state.page = options.page;
	}
	if (options.perPage) {
		state.perPage = options.perPage;
	}
	if (options.sortBy) {
		state.sortBy = options.sortBy;
	}
	if (options.sortOrder) {
		state.sortOrder = options.sortOrder;
	}
	if (options.search) {
		state.search = options.search;
	}
	if (options.onPopState) {
		state.onPopState = options.onPopState;
	}
	if (options.prefix) {
		state.prefix = options.prefix;
	}
	if (options.noScroll) {
		state.noScroll = options.noScroll;
	}

	// if there is a state set in the url params, use it
	const urlParamsPage = parseInt(urlParams.get(param('page', state.prefix)));
	if (urlParamsPage) {
		state.page = urlParamsPage;
	}
	// don't override perPage from URL - localStorage preference takes priority
	// const urlParamsperPage = parseInt(urlParams.get(param('perPage', state.prefix)));
	// if (urlParamsperPage) {
	// 	state.perPage = urlParamsperPage;
	// }
	const urlParamsSortBy = urlParams.get(param('sortBy', state.prefix));
	if (urlParamsSortBy) {
		state.sortBy = urlParamsSortBy;
	}
	const urlParamsSortOrder = urlParams.get(param('sortOrder', state.prefix));
	if (urlParamsSortOrder) {
		state.sortOrder = urlParamsSortOrder;
	}
	const urlParamsSearch = urlParams.get(param('search', state.prefix));
	if (urlParamsSearch) {
		state.search = urlParamsSearch;
	}

	// validate values or set back to defaults
	if (state.page < minPage) {
		console.warn('adjusting pagination: currentPage < minPage');
		state.page = defaultOptions.page;
	}
	if (state.perPage < minPerPage || state.perPage > maxPerPage) {
		console.warn('adjusting pagination: perPage < minPerPage || perPage > maxPerPage');
		state.perPage = storedPerPage;
	}
	if (acceptedPerPageValues.includes(state.perPage) === false) {
		console.warn('adjusting pagination: acceptedPerPageValues.includes(perPage) === false');
		const closestPerPage = acceptedPerPageValues.reduce((prev, curr) => {
			if (Math.abs(curr - state.perPage) < Math.abs(prev - state.perPage)) {
				return curr;
			} else {
				return prev;
			}
		}, acceptedPerPageValues[0]);
		state.perPage = closestPerPage;
	}
	if (['asc', 'desc'].includes(state.sortOrder) === false) {
		state.sortOrder = defaultOptions.sortOrder;
	}

	const popstateHandler = () => {
		// Don't process if we're navigating to a different route
		if (window.location.pathname !== initialPath) {
			return;
		}

		const url = new URL(window.location.toString());
		const newPage = parseInt(url.searchParams.get(param('page', state.prefix)));
		const newPerPage = parseInt(url.searchParams.get(param('perPage', state.prefix)));
		const newSortBy = url.searchParams.get(param('sortBy', state.prefix));
		const newSortOrder = url.searchParams.get(param('sortOrder', state.prefix));
		const newSearch = url.searchParams.get(param('search', state.prefix));

		if (newPage) state.page = newPage;
		if (newPerPage) state.perPage = newPerPage;
		if (newSortBy) state.sortBy = newSortBy;
		if (newSortOrder) state.sortOrder = newSortOrder;
		if (newSearch) state.search = newSearch;

		state.onPopState();
		pagination._notifyListeners('page', state.page);
		pagination._notifyListeners('perPage', state.perPage);
		pagination._notifyListeners('sortBy', state.sortBy);
		pagination._notifyListeners('sortOrder', state.sortOrder);
		pagination._notifyListeners('search', state.search);
	};

	window.addEventListener('popstate', popstateHandler);

	const updateURL = async () => {
		const url = new URL(window.location.toString());
		url.searchParams.set(param('page', state.prefix), state.page.toString());
		url.searchParams.set(param('perPage', state.prefix), state.perPage.toString());
		url.searchParams.set(param('sortBy', state.prefix), state.sortBy);
		url.searchParams.set(param('sortOrder', state.prefix), state.sortOrder);
		url.searchParams.set(param('search', state.prefix), state.search);
		await goto(`?${url.searchParams.toString()}`, { replaceState: true, invalidateAll: false });
	};

	URLUpdateQueue.getInstance().add(updateURL);

	const pagination = {
		_listeners: [],
		onChange: function (callback) {
			this._listeners.push(callback);
		},
		unsubscribe: function () {
			this._listeners = [];
			window.removeEventListener('popstate', popstateHandler);
		},
		_notifyListeners: function (property, value) {
			this._listeners.forEach((callback) => {
				callback(property, value);
			});
		},
		_goto(url, opts) {
			const o = { ...opts };
			if (options.noScroll) {
				o.noScroll = true;
			}
			const updateURL = async () => {
				await goto(`?${url.searchParams.toString()}`, o);
			};
			URLUpdateQueue.getInstance().add(updateURL);
		},
		get currentPage() {
			return state.page;
		},
		get perPage() {
			return state.perPage;
		},
		get sortBy() {
			return state.sortBy;
		},
		get sortOrder() {
			return state.sortOrder;
		},
		get search() {
			return state.search;
		},
		set search(search) {
			state.search = search;
			state.page = 1;
			const url = new URL(window.location.toString());
			url.searchParams.set(param('search', state.prefix), search);
			url.searchParams.set(param('page', state.prefix), '1');
			this._goto(url, {
				keepFocus: true,
				replaceState: true
			});
			this._notifyListeners('search', search);
			this._notifyListeners('page', 1);
		},
		set perPage(perPage) {
			state.perPage = perPage;
			state.page = 1;
			const url = new URL(window.location.toString());
			url.searchParams.set(param('perPage', state.prefix), perPage.toString());
			url.searchParams.set(param('page', state.prefix), '1');
			this._goto(url);
			this._notifyListeners('perPage', perPage);
			this._notifyListeners('page', 1);
		},
		sort: function (sortBy, sortOrder) {
			if (sortBy !== state.sortBy) {
				sortOrder = 'asc';
			} else {
				switch (sortOrder) {
					case '':
						sortOrder = 'asc';
						break;
					case 'asc':
						sortOrder = 'desc';
						break;
					case 'desc':
						sortOrder = defaultSortOrder;
						sortBy = defaultSortBy;
						break;
				}
			}
			state.sortBy = sortBy;
			state.sortOrder = sortOrder;
			const url = new URL(window.location.toString());
			url.searchParams.set(param('sortBy', state.prefix), sortBy);
			url.searchParams.set(param('sortOrder', state.prefix), sortOrder);
			this._goto(url);
			this._notifyListeners('sortBy', sortBy);
			this._notifyListeners('sortOrder', sortOrder);
		},
		next: function () {
			state.page = state.page + 1;
			const url = new URL(window.location.toString());
			url.searchParams.set(param('page', state.prefix), state.page.toString());
			this._goto(url);
			this._notifyListeners('page', state.page);
			return state.page;
		},
		previous: function () {
			if (state.page !== 1) {
				state.page = state.page - 1;
				const url = new URL(window.location.toString());
				url.searchParams.set(param('page', state.prefix), state.page.toString());
				this._goto(url);
				this._notifyListeners('page', state.page);
			}
			return state.page;
		}
	};

	return pagination;
};
