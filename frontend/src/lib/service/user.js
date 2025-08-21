//import { actions } from '$lib/state.js'
import { API } from '$lib/api/api.js';
import { AppStateService } from './appState';

/**
 * UserService is a singleton class that provides methods to interact with the user
 * Get a instance via. UserService.instance
 */
export class UserService {
	/**
	 * @returns {UserService|null}
	 */
	static #_instance = null;

	/**
	 * @returns {UserService}
	 */
	static get instance() {
		if (!UserService.#_instance) {
			UserService.#_instance = new UserService();
		}
		return UserService.#_instance;
	}

	/**
	 * @type {API}
	 */
	#apiClient = null;

	/**
	 * @type {AppStateService|null}
	 */
	#appStateService = null;

	/**
	 *
	 * @param {API} apiClient
	 */
	constructor(apiClient = API.instance, appStateService = AppStateService.instance) {
		this.#apiClient = apiClient;
		this.#appStateService = appStateService;
	}

	/**
	 * login
	 *
	 * @param {string} username
	 * @param {string} password
	 * @returns {Promise<import('$lib/api/api').ApiResponse>}
	 */
	async login(username, password, mfaTOTP = '', recoveryCode = '') {
		let res;
		const hasMFATOTP = mfaTOTP.length > 0;
		const hasRecoveryCode = recoveryCode.length > 0;
		const isPasswordLogin = !hasMFATOTP && !hasRecoveryCode;
		const isTOTPLogin = hasMFATOTP && !hasRecoveryCode;
		const isRecoveryCodeLogin = !hasMFATOTP && hasRecoveryCode;

		switch (true) {
			case isPasswordLogin: {
				res = await this.#apiClient.user.login(username, password);
				break;
			}
			case isTOTPLogin: {
				res = await this.#apiClient.user.loginTOTP(username, password, mfaTOTP);
				break;
			}
			case isRecoveryCodeLogin: {
				res = await this.#apiClient.user.loginMFARecoveryCode(username, password, recoveryCode);
				break;
			}
			default: {
				throw new Error('Invalid login method.');
			}
		}
		switch (res.statusCode) {
			case 200: {
				if (res.data.mfa) {
					return res;
				}
				// user is logged in - also check if app is installed
				const isInstalledRes = await this.#apiClient.option.get('is_installed');
				if (isInstalledRes.success && isInstalledRes.data.value === 'true') {
					this.#appStateService.setIsInstalled();
				} else {
					this.#appStateService.setIsNotInstalled();
				}
				this.#appStateService.setLoggedIn({
					name: res.data.user.name,
					username: res.data.user.username,
					company: res.data.user.company && res.data.user.company.name,
					role: res.data.user.role.name
				});
			}
		}
		return res;
	}

	/**
	 * logout
	 *
	 * @returns {Promise<import('$lib/api/api').ApiResponse>}
	 */
	async logout() {
		const res = await this.#apiClient.user.logout();
		if (res.success) {
			this.clear();
		}
		return res;
	}

	/**
	 * clear user state
	 */
	async clear() {
		this.#appStateService.setLoggedOut();
		location.reload();
	}

	/**
	 * change user name.
	 *
	 * @param {string} newUsername
	 * @returns {Promise<import('$lib/api/api').ApiResponse>}
	 */
	async changeUsername(newUsername) {
		const res = await this.#apiClient.user.changeUsername(newUsername);
		if (res.success) {
			this.#appStateService.setUsername(newUsername);
		}
		return res;
	}

	/**
	 * change user's full name.
	 *
	 * @param {string} newFullname
	 * @returns {Promise<import('$lib/api/api').ApiResponse>}
	 */
	async changeFullname(newFullname) {
		const res = await this.#apiClient.user.changeFullname(newFullname);
		if (res.success) {
			this.#appStateService.setUserFullName(newFullname);
		}
		return res;
	}

	/**
	 * change user's company name.
	 *
	 * @param {string} newCompany
	 * @returns {Promise<import('$lib/api/api').ApiResponse>}
	 */
	async changeCompany(newCompany) {
		const res = await this.#apiClient.user.changeCompany(newCompany);
		if (res.success) {
			this.#appStateService.setUserCompany(newCompany);
		}
		return res;
	}
}
