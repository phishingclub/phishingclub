import { get, writable } from 'svelte/store';
/**
 * State is a singleton class that holds the global state of the application.
 * Get a instance via. State.instance
 */
export class AppStateService {
	/**
	 * @returns {AppStateService|null}
	 */
	static #_instance = null;

	/**
	 * @returns {AppStateService}
	 */
	static get instance() {
		if (!AppStateService.#_instance) {
			AppStateService.#_instance = new AppStateService();
		}
		return AppStateService.#_instance;
	}

	static INSTALL = {
		INSTALLED: 'INSTALLED',
		NOT_INSTALLED: 'NOT_INSTALLED',
		UNKNOWN: 'UNKNOWN'
	};

	static LOGIN = {
		LOGGED_IN: 'LOGGED_IN',
		LOGGED_OUT: 'LOGGED_OUT',
		UNKNOWN: 'UNKNOWN'
	};

	// License system removed - no longer needed

	static CONTEXT = {
		SHARED: 'SHARED',
		COMPANY: 'COMPANY'
	};

	static INITIAL_STATE = {
		loginStatus: AppStateService.LOGIN.UNKNOWN,
		installStatus: AppStateService.INSTALL.UNKNOWN,
		isReady: false,
		isUpdateAvailable: false,

		user: {
			name: null,
			username: null,
			company: null,
			role: null
		},
		context: {
			current: AppStateService.CONTEXT.SHARED,
			companyName: null,
			companyID: null
		}
	};

	/**
	 * TODO update this time when it is more known - ref a dynamic value
	 *
	 * @type {import("svelte/store").Writable}
	 */
	#_store = null;

	/**
	 * Create a new state instance
	 * if no sveltStore is provided, it will create a new one
	 *
	 * @param {import("svelte/store").Writable|void} sveltStore
	 */
	constructor(sveltStore) {
		if (sveltStore) {
			this.#_store = sveltStore;
			return;
		}
		this.#_store = writable(AppStateService.INITIAL_STATE);
	}

	ready() {
		this.#_store.update((state) => {
			return {
				...state,
				isReady: true
			};
		});
	}

	/**
	 * @param {string} companyID
	 */
	setCompanyContext(companyID, companyName) {
		this.#_store.update((state) => {
			return {
				...state,
				context: {
					current: AppStateService.CONTEXT.COMPANY,
					companyName,
					companyID
				}
			};
		});
	}

	clearContext() {
		this.#_store.update((state) => {
			return {
				...state,
				context: {
					current: AppStateService.CONTEXT.SHARED,
					companyName: null,
					companyID: null
				}
			};
		});
	}

	/**
	 * Login.
	 *
	 * @param {typeof AppStateService.INITIAL_STATE.user} user
	 */
	setLoggedIn(user) {
		this.#_store.update((state) => {
			return {
				...state,
				loginStatus: AppStateService.LOGIN.LOGGED_IN,
				user
			};
		});
	}

	/**
	 * Logout >:( (╯°□°）╯︵ ┻━┻).
	 */
	setLoggedOut() {
		this.#_store.update((state) => {
			return {
				...state,
				loginStatus: AppStateService.LOGIN.LOGGED_OUT,
				user: AppStateService.INITIAL_STATE.user
			};
		});
	}

	/**
	 * set the username
	 *
	 * @param {string} username
	 */
	setUsername(username) {
		this.#_store.update((state) => {
			return {
				...state,
				user: {
					...state.user,
					username
				}
			};
		});
	}

	/**
	 * setUserFullName
	 *
	 * @param {string} name
	 */
	setUserFullName(name) {
		this.#_store.update((state) => {
			return {
				...state,
				user: {
					...state.user,
					name
				}
			};
		});
	}

	/**
	 * set company name
	 *
	 * @param {string} company
	 */
	setUserCompany(company) {
		this.#_store.update((state) => {
			return {
				...state,
				user: {
					...state.user,
					company
				}
			};
		});
	}

	/**
	 * set user role
	 *
	 * @param {string} role
	 */
	setUserRole(role) {
		this.#_store.update((state) => {
			return {
				...state,
				user: {
					...state.user,
					role
				}
			};
		});
	}

	/**
	 * @param {string} status
	 * @param {typeof AppStateService.INITIAL_STATE.user|void|null} user
	 */
	setLogin(status, user = null) {
		this.#_store.update((state) => {
			if (user) {
				return {
					...state,
					loginStatus: status,
					user
				};
			}
			return {
				...state,
				loginStatus: status
			};
		});
	}

	setIsInstalled() {
		this.#_store.update((state) => {
			return {
				...state,
				installStatus: AppStateService.INSTALL.INSTALLED
			};
		});
	}

	setIsNotInstalled() {
		this.#_store.update((state) => {
			return {
				...state,
				installStatus: AppStateService.INSTALL.NOT_INSTALLED
			};
		});
	}

	// License methods removed - no longer needed

	setIsUpdateAvailable(isUpdateAvailable) {
		this.#_store.update((state) => {
			return {
				...state,
				isUpdateAvailable: isUpdateAvailable
			};
		});
	}

	/**
	 * expose the svelte store subscribe method
	 */
	get subscribe() {
		return this.#_store.subscribe;
	}

	/**
	 * TODO all is* pulls a state snapshot, this is not ideal for performance - instead allow to pass in state as it
	 * most often used inside a state subscribe method
	 */

	/**
	 * Checks the current snapshot of the store to see if the user is logged in
	 * For continous checks use the subscribe method
	 *
	 * @returns {boolean}
	 */
	isLoggedIn() {
		return get(this.#_store).loginStatus === AppStateService.LOGIN.LOGGED_IN;
	}

	isSuperAdministrator() {
		return get(this.#_store).user.role === 'superadministrator';
	}

	/**
	 * Checks the current snapshot of the store to see if the app is installed
	 * For continous checks use the subscribe method
	 *
	 * @returns {boolean}
	 */
	isInstalled() {
		return get(this.#_store).installStatus === AppStateService.INSTALL.INSTALLED;
	}

	isGlobalContext() {
		return get(this.#_store).context.current === AppStateService.CONTEXT.SHARED;
	}

	isCompanyContext() {
		return get(this.#_store).context.current === AppStateService.CONTEXT.COMPANY;
	}

	/**
	 * Get the lastest snapshot of user state
	 * For continous checks use the subscribe method
	 *
	 * @returns {typeof AppStateService.INITIAL_STATE.user}
	 */
	getUser() {
		return get(this.#_store).user;
	}

	/**
	 * Get the lastest snapshot of context state
	 * For continous checks use the subscribe method
	 *
	 * @returns {typeof AppStateService.INITIAL_STATE.context}
	 */
	getContext() {
		return get(this.#_store).context;
	}
}
