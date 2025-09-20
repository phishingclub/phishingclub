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

	// services
	const api = API.instance;
	const appStateService = AppStateService.instance;

	// installation steps - will be updated based on edition
	let steps = [{ name: 'Profile' }, { name: 'Complete' }];

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

	// Removed edition detection - single unified installation

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
		goto('/dashboard/');
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
				// Step 2 is always Complete now - no validation needed
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
			// Create user - no license setup during installation
			const userRes = await api.application.install(
				installForm.username,
				installForm.name,
				installForm.password
			);
			if (!userRes.success) {
				formError = userRes.error;
				return;
			}

			appStateService.setIsInstalled();
			// License configuration available in settings after installation
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
						<span
							class={`
								mt-2 text-sm font-medium text-center
								${currentStep > index + 1 || currentStep === index + 1 ? 'text-blue-600' : 'text-gray-500'}
							`}
						>
							{step.name}
						</span>
					</div>
				{/each}
			</div>
		</div>

		<div class="mt-8 sm:mx-auto sm:w-full sm:max-w-md">
			<div class="bg-white px-4 shadow sm:rounded-lg sm:px-10">
				<FormGrid bind:bindTo={form}>
					<FormColumns>
						<FormColumn>
							{#if currentStep === 1}
								<div class="space-y-6 w-full flex flex-col items-center" id="step-1">
									<TextField required bind:value={installForm.name}>Name</TextField>
									<TextField
										id="username"
										required
										bind:value={installForm.username}
										on:keyup={(e) => {
											const ele = /** @type {HTMLInputElement} */ (e.target);
											ele.setCustomValidity('');
										}}>Username</TextField
									>
									<PasswordField id="password" required bind:value={installForm.password}
										>Password</PasswordField
									>
									<PasswordField
										id="repeatPassword"
										required
										bind:value={installForm.repeatPassword}
										on:keyup={(e) => {
											const ele = /** @type {HTMLInputElement} */ (e.target);
											ele.setCustomValidity('');
										}}
									>
										Confirm Password
									</PasswordField>
								</div>
							{:else}
								<div class="text-center py-8" id="step-{currentStep}">
									<h3 class="text-lg font-medium text-gray-900 mb-4">Welcome to Phishing Club</h3>
									<div class="space-y-4 text-sm text-gray-600">
										<p>
											Get started by reading our
											<a
												href="https://phishing.club/guide/introduction/"
												target="_blank"
												rel="noopener noreferrer"
												class="text-blue-600 hover:text-blue-800 font-medium"
											>
												user guide
											</a>
										</p>
										<p>
											Have questions, bugs or suggestions? <br /> Contact us at
											<a
												href="mailto:support@phishing.club"
												class="text-blue-600 hover:text-blue-800 font-medium"
											>
												support@phishing.club
											</a>
										</p>
									</div>
								</div>
							{/if}

							<div class="w-full max-w-md mx-auto">
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
