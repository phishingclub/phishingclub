const defaultOptions = {
	page: 1,
	perPage: 10,
	sortBy: 'name',
	sortOrder: 'asc',
	search: ''
};

/**
 * @param {Object} settings
 * @param {number} [settings.page]
 * @param {number} [settings.perPage]
 * @param {string} [settings.sortBy]
 * @param {string} [settings.sortOrder]
 * @param {string} [settings.search]
 * @returns {Object} { currentPage: number, perPage: number, next: function, previous: function
 */

export const newTableParams = (options = {}) => {
	options = { ...defaultOptions, ...options };
	let state = { ...defaultOptions, ...options };
	let firstState = { ...defaultOptions, ...options };
	return {
		_listeners: [],
		onChange: function (callback) {
			this._listeners.push(callback);
		},
		unsubscribe: function () {
			this._listeners = [];
		},
		_notifyListeners: function (property, value) {
			this._listeners.forEach((callback) => callback(property, value));
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
			this._notifyListeners('search', search);
			this._notifyListeners('page', 1);
		},
		set perPage(perPage) {
			state.perPage = perPage;
			state.page = 1;
			this._notifyListeners('perPage', perPage);
			this._notifyListeners('page', 1);
		},
		reset: function () {
			state = { ...firstState };
		},
		sort: function (sortBy, sortOrder) {
			// handle sorting order and logic
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
						sortOrder = options.sortOrder;
						sortBy = options.sortBy;
						break;
				}
			}
			state.sortBy = sortBy;
			state.sortOrder = sortOrder;
			this._notifyListeners('sortBy', sortBy);
			this._notifyListeners('sortOrder', sortOrder);
		},
		next: function () {
			state.page = state.page + 1;
			this._notifyListeners('page', state.page);
			return state.page;
		},
		previous: function () {
			if (state.page !== 1) {
				state.page = state.page - 1;
			}
			this._notifyListeners('page', state.page);
			return state.page;
		}
	};
};
