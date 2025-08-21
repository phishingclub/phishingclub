import { API } from '$lib/api/api.js';
import { AppStateService } from './appState';

/**
 * Session class
 *
 * Use Session.instance to get the default global singleton instance
 * The first time you call Session.instance or the constructor, it will be initialized with the global api client
 */
export class Session {
	/**
	 * Global singleton session instance
	 * @type {Session|null}
	 */
	static #_instance = null;

	static get instance() {
		if (!Session.#_instance) {
			Session.#_instance = new Session();
		}
		return Session.#_instance;
	}

	/**
	 * The interval in milliseconds between each session ping
	 *
	 * @type {number}
	 */
	#intervalMS = 1000 * 60;

	/**
	 * @type {API|null}
	 */
	#apiClient = null;

	/**
	 * @type {AppStateService|null}
	 */
	#appStateService = null;

	/**
	 * @type {number|null}
	 */
	#intervalID = null;

	/**
	 * @type {boolean}
	 */
	#isRunning = false;

	get isRunning() {
		return this.#isRunning;
	}

	/**
	 * @type {boolean}
	 */
	#debug = false;

	/**
	 * If no client is provided, use it automatically uses the global api client
	 * @param {API} apiClient
	 * @param {AppStateService} appStateService
	 */
	constructor(apiClient = API.instance, appStateService = AppStateService.instance) {
		this.#apiClient = apiClient;
		this.#appStateService = appStateService;
	}

	/**
	 * log to console if debug is enabled
	 * @param {...*} x
	 */
	#log(...x) {
		if (this.#debug) {
			console.log('session:', ...x);
		}
	}

	/**
	 * ping session
	 *
	 * @throws {Error} if session ping fails
	 */
	async ping() {
		this.#log('pinging...');
		const sessionPingResult = await this.#apiClient.session.ping();
		// user is not logged in
		if (!sessionPingResult.success) {
			this.#appStateService.setLogin(AppStateService.LOGIN.LOGGED_OUT);
			return;
		}
		// user is logged in
		this.#appStateService.setLogin(AppStateService.LOGIN.LOGGED_IN, {
			name: sessionPingResult.data.name,
			username: sessionPingResult.data.username,
			company: sessionPingResult.data.company,
			role: sessionPingResult.data.role
		});
		// check if app is installed
		this.#log('user is logged in - retrieving install status');
		const res = await this.#apiClient.option.get('is_installed');
		if (res.data.value === 'true') {
			this.#appStateService.setIsInstalled();
		} else {
			this.#appStateService.setIsNotInstalled();
		}
		this.#log('ping success');
	}

	debugOn() {
		this.#debug = true;
	}

	debugOff() {
		this.#debug = false;
	}

	/**
	 * start session ping
	 *
	 * @throws {Error} if session is already started
	 * @throws {Error} if session initialization failed
	 */
	async start() {
		if (this.#isRunning) {
			this.#log('already started');
			throw new Error('session is already started');
		}
		this.#isRunning = true;
		this.#log('initial ping');
		try {
			// initial ping
			// setup continous ping
			this.#intervalID = window.setInterval(async () => {
				try {
					await this.ping();
				} catch (e) {
					this.#log('ping failed', e);
				}
			}, this.#intervalMS);
			await this.ping();
			this.#log('ping success');
			this.#log('continous ping is running');
		} catch (e) {
			this.#isRunning = false;
			this.#log('initial ping failed', e);
			throw e;
		}
	}

	/**
	 * stop session ping
	 *
	 * @throws {Error} if session is not started
	 */
	stop = () => {
		if (!this.#isRunning) {
			this.#log('not started');
			throw new Error('session is not started');
		}
		clearInterval(this.#intervalID);
		this.#isRunning = false;
		this.#log('stopped');
	};
}
