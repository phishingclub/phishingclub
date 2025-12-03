<script>
	import { onMount } from 'svelte';
	import { goto } from '$app/navigation';
	import { API } from '$lib/api/api';
	import { AppStateService } from '$lib/service/appState';
	import HeadTitle from '$lib/components/HeadTitle.svelte';
	import { hideIsLoading, showIsLoading } from '$lib/store/loading';
	import FormError from '$lib/components/FormError.svelte';
	import TextField from '$lib/components/TextField.svelte';
	import PasswordField from '$lib/components/PasswordField.svelte';
	import FormGrid from '$lib/components/FormGrid.svelte';
	import FormColumns from '$lib/components/FormColumns.svelte';
	import FormColumn from '$lib/components/FormColumn.svelte';
	import ThemeToggle from '$lib/components/ThemeToggle.svelte';
	import { setupTheme, setupOSThemeListener } from '$lib/theme.js';
	import { displayMode, DISPLAY_MODE } from '$lib/store/displayMode';

	// services
	const api = API.instance;
	const appStateService = AppStateService.instance;

	// installation steps - will be updated based on edition
	let steps = [
		{ name: 'Profile' },
		{ name: 'Display Mode' },
		{ name: 'Templates' },
		{ name: 'Complete' }
	];

	let currentStep = 1;
	let formError = '';
	let isSubmitting = false;
	let form = null;

	// form values
	let installForm = {
		name: '',
		username: '',
		password: '',
		repeatPassword: ''
	};

	// display mode step
	let selectedDisplayMode = DISPLAY_MODE.WHITEBOX;

	// templates step
	let installTemplates = false;
	let templatesError = '';

	// dev
	const isDevelopement = false;

	// initialize theme system
	onMount(() => {
		setupTheme();
		setupOSThemeListener();
	});

	// if already installed or not a superadministrator redirect to the dashboard
	const user = appStateService.getUser();
	const isInstalled = appStateService.isInstalled();
	if (isInstalled || user.role !== 'superadministrator') {
		console.info('install - navigating to dashboard');
		if (isDevelopement) {
			console.info('Skipping navigation due to development mode');
		} else {
			goto('/dashboard/');
		}
	}

	const nextStep = () => {
		if (validateCurrentStep()) {
			if (currentStep === steps.length) {
				onInstall();
			} else {
				currentStep = Math.min(currentStep + 1, steps.length);
				formError = '';
			}
		}
	};

	const previousStep = () => {
		currentStep = Math.max(currentStep - 1, 1);
		formError = '';
	};

	const checkCurrentStepValidity = () => {
		/** @type {NodeListOf<HTMLInputElement>} */
		const currentStepElements = document.querySelectorAll(
			`[id="step-${currentStep}"] input:not([type="hidden"]), [id="step-${currentStep}"] select, [id="step-${currentStep}"] textarea`
		);
		for (let i = 0; i < currentStepElements.length; i++) {
			const element = currentStepElements[i];
			if (element.hasAttribute('required') && !element.checkValidity()) {
				element.reportValidity();
				return false;
			}
		}
		return true;
	};

	const validateCurrentStep = () => {
		switch (currentStep) {
			case 1:
				return validateProfile();
			case 2:
				// step 2 is display mode - no validation needed
				return true;
			case 3:
				// step 3 is templates - no validation needed
				return true;
			case 4:
				// step 4 is always complete now - no validation needed
				return true;
			default:
				return true;
		}
	};

	const validateProfile = () => {
		if (!checkCurrentStepValidity()) return false;

		/** @type {HTMLInputElement} */
		const usernameInput = document.querySelector('#username');
		/** @type {HTMLInputElement} */
		const passwordInput = document.querySelector('#password');
		/** @type {HTMLInputElement} */
		const repeatPasswordInput = document.querySelector('#repeatPassword');

		if (installForm.username.trim().toLowerCase() == 'admin') {
			usernameInput.setCustomValidity("'admin' is not allowed as username");
			usernameInput.reportValidity();
			return false;
		}
		if (installForm.password !== installForm.repeatPassword) {
			repeatPasswordInput.setCustomValidity('Passwords do not match');
			repeatPasswordInput.reportValidity();
			return false;
		}
		repeatPasswordInput.setCustomValidity('');
		return true;
	};

	const onInstall = async () => {
		showIsLoading();
		isSubmitting = true;
		try {
			// create user - no license setup during installation
			const userRes = await api.application.install(
				installForm.username,
				installForm.name,
				installForm.password
			);
			if (!userRes.success) {
				formError = userRes.error;
				return;
			}

			// set display mode
			const displayModeRes = await api.option.set('display_mode', selectedDisplayMode);
			if (!displayModeRes.success) {
				console.warn('failed to set display mode', displayModeRes.error);
				// continue with installation even if display mode setting fails
			} else {
				displayMode.setMode(selectedDisplayMode);
			}

			// install templates if requested
			if (installTemplates) {
				const templatesRes = await api.application.installTemplates();
				if (!templatesRes.success) {
					templatesError = templatesRes.error || 'Failed to install templates';
					console.warn('failed to install templates', templatesRes.error);
					// continue with installation even if templates fail
				} else {
					console.info('templates installed successfully');
				}
			}

			appStateService.setIsInstalled();
			// license configuration available in settings after installation
			console.info('install: setup completed - refreshing');
			location.reload();
		} catch (e) {
			console.error('failed to setup', e);
			formError = 'Installation failed. Please try again.';
		} finally {
			hideIsLoading();
			isSubmitting = false;
		}
	};
</script>

<HeadTitle title="Setup" />

<div
	class="inset-0 z-50 min-h-screen bg-white dark:bg-gray-900 text-gray-900 dark:text-gray-100 transition-colors duration-200"
>
	<!-- theme toggle -->
	<div class="fixed top-3 right-6 z-50">
		<ThemeToggle />
	</div>
	<div class="flex flex-col justify-center py-12 sm:px-6 lg:px-8">
		<p class="text-center text-sm text-gray-600 dark:text-gray-300 transition-colors duration-200">
			Complete the setup to get started with Phishing Club
		</p>

		<div class="sm:mx-auto sm:max-w-2xl mt-8">
			<div class="flex justify-between items-center mb-8 w-full px-4">
				{#each steps as step, index}
					<div class="flex flex-col items-center w-32">
						<div
							class={`
								w-8 h-8 rounded-full flex items-center justify-center text-sm font-medium
								transition-colors duration-200
								${
									currentStep > index + 1
										? 'bg-blue-300 text-white'
										: currentStep === index + 1
											? 'bg-blue-600 text-white'
											: 'bg-white dark:bg-gray-700 text-gray-500 dark:text-gray-300 border-2 border-gray-300 dark:border-gray-600'
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
						<span
							class={`
								mt-2 text-sm font-medium text-center
								${currentStep > index + 1 || currentStep === index + 1 ? 'text-blue-600 dark:text-blue-400' : 'text-gray-500 dark:text-gray-400'}
							`}
						>
							{step.name}
						</span>
					</div>
				{/each}
			</div>
		</div>

		<div class="mt-8 sm:mx-auto sm:w-full sm:max-w-2xl">
			<div
				class="bg-white dark:bg-gray-800 px-4 shadow sm:rounded-lg sm:px-10 transition-colors duration-200"
			>
				<FormGrid bind:bindTo={form}>
					<FormColumns>
						<FormColumn>
							<div class="w-full sm:min-w-[500px]">
								{#if currentStep === 1}
									<div class="space-y-6 w-full flex flex-col items-center" id="step-1">
										<TextField required minLength="1" maxLength="64" bind:value={installForm.name}
											>Name</TextField
										>
										<TextField
											id="username"
											required
											minLength="1"
											maxLength="64"
											bind:value={installForm.username}
											on:keyup={(e) => {
												const ele = /** @type {HTMLInputElement} */ (e.target);
												ele.setCustomValidity('');
											}}>Username</TextField
										>
										<PasswordField
											id="password"
											required
											minLength="16"
											maxLength="64"
											bind:value={installForm.password}>Password</PasswordField
										>
										<PasswordField
											id="repeatPassword"
											required
											minLength="16"
											maxLength="64"
											bind:value={installForm.repeatPassword}
											on:keyup={(e) => {
												const ele = /** @type {HTMLInputElement} */ (e.target);
												ele.setCustomValidity('');
											}}
										>
											Confirm Password
										</PasswordField>
									</div>
								{:else if currentStep === 2}
									<div class="text-center py-8" id="step-2">
										<h3 class="text-lg font-medium text-gray-900 dark:text-gray-100 mb-4">
											Display Mode
										</h3>
										<div class="space-y-4 text-sm text-gray-600 dark:text-gray-300">
											<p>Select which features are available</p>
										</div>
										<div class="mt-6 space-y-4 max-w-xs mx-auto">
											<label
												class="flex items-start justify-start gap-3 p-4 border rounded-lg cursor-pointer transition-colors {selectedDisplayMode ===
												DISPLAY_MODE.WHITEBOX
													? 'bg-blue-50 dark:bg-blue-900/20 border-blue-500 dark:border-blue-600'
													: 'border-gray-300 dark:border-gray-600'}"
											>
												<input
													type="radio"
													bind:group={selectedDisplayMode}
													value={DISPLAY_MODE.WHITEBOX}
													class="mt-1 w-4 h-4 text-blue-600 bg-gray-100 dark:bg-gray-700 border-gray-300 dark:border-gray-600 focus:ring-blue-500 focus:ring-2"
												/>
												<div class="text-left">
													<span class="text-sm font-medium text-gray-900 dark:text-gray-100 block">
														Whitebox
													</span>
													<span class="text-xs text-gray-600 dark:text-gray-400 block mt-1">
														Phishing Simulation
													</span>
												</div>
											</label>
											<label
												class="flex items-start justify-start gap-3 p-4 border rounded-lg cursor-pointer transition-colors {selectedDisplayMode ===
												DISPLAY_MODE.BLACKBOX
													? 'bg-blue-50 dark:bg-blue-900/20 border-blue-500 dark:border-blue-600'
													: 'border-gray-300 dark:border-gray-600'}"
											>
												<input
													type="radio"
													bind:group={selectedDisplayMode}
													value={DISPLAY_MODE.BLACKBOX}
													class="mt-1 w-4 h-4 text-blue-600 bg-gray-100 dark:bg-gray-700 border-gray-300 dark:border-gray-600 focus:ring-blue-500 focus:ring-2"
												/>
												<div class="text-left">
													<span class="text-sm font-medium text-gray-900 dark:text-gray-100 block">
														Blackbox
													</span>
													<span class="text-xs text-gray-600 dark:text-gray-400 block mt-1">
														Red Team Phishing.
													</span>
												</div>
											</label>
											<p
												class="text-gray-600 dark:text-gray-300 text-sm transition-colors duration-200"
											>
												Read about the difference between <br />
												<a
													class="white underline"
													href="https://phishing.club/blog/white-box-vs-black-box-phishing/"
													target="_blank">whitebox and blackbox phishing</a
												>
											</p>
										</div>
									</div>
								{:else if currentStep === 3}
									<div class="text-center py-8" id="step-3">
										<h3 class="text-lg font-medium text-gray-900 dark:text-gray-100 mb-4">
											Example Templates
										</h3>
										<div class="space-y-4 text-sm text-gray-600 dark:text-gray-300">
											<p>Install example templates?</p>
											<p class="text-xs text-gray-500 dark:text-gray-400">
												Includes phishing pages and emails from
												<a
													href="https://github.com/phishingclub/templates"
													target="_blank"
													rel="noopener noreferrer"
													class="text-blue-600 hover:text-blue-800 dark:text-blue-400 dark:hover:text-blue-300 font-medium"
												>
													template builder
												</a>
											</p>
										</div>
										<div class="mt-6">
											<label class="flex items-center justify-center gap-3">
												<input
													type="checkbox"
													bind:checked={installTemplates}
													class="w-4 h-4 text-blue-600 bg-gray-100 dark:bg-gray-700 border-gray-300 dark:border-gray-600 rounded focus:ring-blue-500 focus:ring-2"
												/>
												<span class="text-sm font-medium text-gray-900 dark:text-gray-100">
													Yes, install example templates
												</span>
											</label>
										</div>
										{#if templatesError}
											<div
												class="mt-4 p-3 bg-yellow-50 dark:bg-yellow-900/20 border border-yellow-200 dark:border-yellow-800 rounded-md"
											>
												<p class="text-sm text-yellow-800 dark:text-yellow-200">
													<strong>Note:</strong>
													{templatesError}
												</p>
												<p class="text-xs text-yellow-700 dark:text-yellow-300 mt-1">
													You can manually import templates later from Settings.
												</p>
											</div>
										{/if}
									</div>
								{:else}
									<div class="text-center py-8 mx-auto max-w-md" id="step-{currentStep}">
										<h3 class="text-lg font-medium text-gray-900 dark:text-gray-100 mb-4">
											Welcome to Phishing Club
										</h3>
										<div class="space-y-4 text-sm text-gray-600 dark:text-gray-300">
											<p>
												Get started by reading our
												<a
													href="https://phishing.club/guide/introduction/"
													target="_blank"
													rel="noopener noreferrer"
													class="text-blue-600 hover:text-blue-800 dark:text-blue-400 dark:hover:text-blue-300 font-medium"
												>
													user guide
												</a>
											</p>
											<p>
												Have questions, bugs or suggestions? <br /> Contact us at
												<a
													href="mailto:support@phishing.club"
													class="text-blue-600 hover:text-blue-800 dark:text-blue-400 dark:hover:text-blue-300 font-medium"
												>
													support@phishing.club
												</a>
											</p>
										</div>
									</div>
								{/if}

								<div class="w-full">
									<FormError message={formError} />
								</div>

								<div class="flex justify-between items-center w-full mt-8">
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
										<div />
									{/if}

									<button
										type="button"
										class="inline-flex items-center px-4 py-2 border border-transparent rounded-md shadow-sm text-sm font-medium text-white bg-blue-600 hover:bg-blue-700 focus:outline-none focus:ring-2 focus:ring-offset-2 focus:ring-blue-500"
										on:click={nextStep}
										disabled={isSubmitting}
									>
										{#if currentStep === steps.length}
											Setup
										{:else}
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
										{/if}
									</button>
								</div>
							</div>
						</FormColumn>
					</FormColumns>
				</FormGrid>
			</div>
		</div>

		<div class="mt-8 text-center text-sm text-gray-500">
			<!--
			<p>>-></p>
			-->
		</div>
	</div>
</div>
