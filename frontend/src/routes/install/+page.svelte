<script>
	import { onMount, tick } from 'svelte';
	import { goto } from '$app/navigation';
	import { API } from '$lib/api/api';
	import { AppStateService } from '$lib/service/appState';
	import HeadTitle from '$lib/components/HeadTitle.svelte';
	import { hideIsLoading, showIsLoading } from '$lib/store/loading';
	import FormError from '$lib/components/FormError.svelte';
	import TextField from '$lib/components/TextField.svelte';
	import PasswordField from '$lib/components/PasswordField.svelte';
	import FormGrid from '$lib/components/FormGrid.svelte';
	import FormSection from '$lib/components/form/FormSection.svelte';
	import FormButton from '$lib/components/FormButton.svelte';
	import RadioOption from '$lib/components/RadioOption.svelte';
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

	const secondaryButtonClass =
		'pc-focus min-w-[6rem] inline-flex items-center justify-center gap-1 px-4 py-2 rounded-md border border-gray-300 bg-transparent hover:bg-gray-100 text-slate-700 dark:bg-gray-700 dark:hover:bg-gray-600 dark:border-gray-500 dark:text-gray-100 text-sm font-bold uppercase transition-colors duration-200 disabled:opacity-50 disabled:cursor-not-allowed';
	const linkClass =
		'pc-focus rounded-sm font-medium text-cta-blue dark:text-highlight-blue underline hover:no-underline';

	// move focus to the new step so keyboard and screen reader users follow along
	let lastFocusedStep = currentStep;
	$: if (currentStep !== lastFocusedStep) {
		lastFocusedStep = currentStep;
		tick().then(() => {
			const el = document.getElementById(`step-${currentStep}`);
			if (!el) return;
			const first = el.querySelector('input:not([type="hidden"])');
			if (currentStep === 1 && first instanceof HTMLElement) {
				first.focus();
			} else {
				el.focus({ preventScroll: true });
			}
		});
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

<div class="fixed top-3 right-6 z-50">
	<ThemeToggle />
</div>

<main
	class="mx-auto w-full max-w-2xl py-6 sm:py-10 text-gray-900 dark:text-gray-100 transition-colors duration-200"
>
	<header class="text-center">
		<h1
			class="text-3xl sm:text-4xl font-titilium font-bold uppercase text-pc-darkblue dark:text-white transition-colors duration-200"
		>
			Setup
		</h1>
		<p class="mt-2 text-sm text-slate-600 dark:text-gray-300 transition-colors duration-200">
			Complete the setup to get started with Phishing Club
		</p>
	</header>

	<div
		class="mt-6 sm:mt-8 rounded-lg border border-gray-200 dark:border-gray-700 bg-white dark:bg-gray-800 shadow-sm transition-colors duration-200"
	>
		<nav
			aria-label="Setup steps"
			class="px-4 sm:px-8 pt-5 pb-4 border-b border-gray-200 dark:border-gray-700 transition-colors duration-200"
		>
			<ol class="flex items-start">
				{#each steps as step, index}
					{@const stepNumber = index + 1}
					{@const isDone = currentStep > stepNumber}
					{@const isCurrent = currentStep === stepNumber}
					<li
						class="relative flex-1 flex flex-col items-center min-w-0"
						aria-current={isCurrent ? 'step' : undefined}
					>
						{#if index < steps.length - 1}
							<span
								aria-hidden="true"
								class="absolute top-4 left-[calc(50%+1.25rem)] right-[calc(-50%+1.25rem)] h-0.5 -translate-y-1/2 rounded-full transition-colors duration-200 {isDone
									? 'bg-cta-blue dark:bg-highlight-blue'
									: 'bg-gray-200 dark:bg-gray-600'}"
							/>
						{/if}
						<span
							class="relative z-10 w-8 h-8 rounded-full flex items-center justify-center text-sm font-semibold transition-colors duration-200 {isDone
								? 'bg-blue-100 text-cta-blue dark:bg-highlight-blue/20 dark:text-blue-200'
								: isCurrent
									? 'bg-cta-blue text-white ring-4 ring-blue-100 dark:bg-highlight-blue dark:ring-highlight-blue/25'
									: 'bg-white text-gray-500 border-2 border-gray-300 dark:bg-gray-800 dark:text-gray-400 dark:border-gray-600'}"
						>
							{#if isDone}
								<svg
									xmlns="http://www.w3.org/2000/svg"
									class="h-4 w-4"
									viewBox="0 0 20 20"
									fill="currentColor"
									aria-hidden="true"
								>
									<path
										fill-rule="evenodd"
										d="M16.707 5.293a1 1 0 010 1.414l-8 8a1 1 0 01-1.414 0l-4-4a1 1 0 011.414-1.414L8 12.586l7.293-7.293a1 1 0 011.414 0z"
										clip-rule="evenodd"
									/>
								</svg>
								<span class="sr-only">Step {stepNumber}, {step.name}, done</span>
							{:else}
								<span aria-hidden="true">{stepNumber}</span>
								<span class="sr-only">Step {stepNumber}, {step.name}</span>
							{/if}
						</span>
						<span
							class="mt-1.5 text-xs sm:text-sm text-center whitespace-nowrap transition-colors duration-200 {isCurrent
								? 'font-semibold text-cta-blue dark:text-blue-300'
								: isDone
									? 'text-slate-600 dark:text-gray-300'
									: 'text-gray-500 dark:text-gray-400'}"
							aria-hidden="true"
						>
							{step.name}
						</span>
					</li>
				{/each}
			</ol>
		</nav>

		<div class="px-4 sm:px-8 pt-6">
			<FormGrid bind:bindTo={form}>
				<div class="mx-auto w-full max-w-md">
					{#if currentStep === 1}
						<div id="step-1" tabindex="-1" class="outline-none">
							<FormSection title="Profile">
								<p class="pb-1 text-sm text-slate-600 dark:text-gray-400">
									Create the administrator account you sign in with.
								</p>
								<TextField
									required
									minLength="1"
									maxLength="64"
									width="full"
									bind:value={installForm.name}>Name</TextField
								>
								<TextField
									id="username"
									required
									minLength="1"
									maxLength="64"
									width="full"
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
									width="full"
									bind:value={installForm.password}>Password</PasswordField
								>
								<PasswordField
									id="repeatPassword"
									required
									minLength="16"
									maxLength="64"
									width="full"
									bind:value={installForm.repeatPassword}
									on:keyup={(e) => {
										const ele = /** @type {HTMLInputElement} */ (e.target);
										ele.setCustomValidity('');
									}}
								>
									Confirm Password
								</PasswordField>
								<p class="text-xs text-gray-500 dark:text-gray-400">
									Password must be 16 to 64 characters.
								</p>
							</FormSection>
						</div>
					{:else if currentStep === 2}
						<div id="step-2" tabindex="-1" class="outline-none">
							<FormSection title="Display Mode">
								<p id="display-mode-help" class="pb-2 text-sm text-slate-600 dark:text-gray-400">
									Select which features are available
								</p>
								<div
									role="radiogroup"
									aria-labelledby="display-mode-help"
									class="space-y-3"
								>
									<RadioOption
										label="Phishing Simulation"
										checked={selectedDisplayMode === DISPLAY_MODE.WHITEBOX}
										on:change={() => (selectedDisplayMode = DISPLAY_MODE.WHITEBOX)}
									/>
									<RadioOption
										label="Red Team Phishing"
										checked={selectedDisplayMode === DISPLAY_MODE.BLACKBOX}
										on:change={() => (selectedDisplayMode = DISPLAY_MODE.BLACKBOX)}
									/>
								</div>
								<p class="pt-3 text-sm text-slate-600 dark:text-gray-400">
									Read about the difference between
									<a
										class={linkClass}
										href="https://phishing.club/blog/phishing-simulation-vs-red-team-phishing/"
										target="_blank"
										rel="noopener noreferrer">phishing simulation and red team phishing</a
									>
								</p>
							</FormSection>
						</div>
					{:else if currentStep === 3}
						<div id="step-3" tabindex="-1" class="outline-none">
							<FormSection title="Example Templates">
								<p class="text-sm text-slate-600 dark:text-gray-400">
									Install example templates? Includes phishing pages and emails from
									<a
										href="https://github.com/phishingclub/templates"
										target="_blank"
										rel="noopener noreferrer"
										class={linkClass}>template builder</a
									>
								</p>
								<label
									class="mt-3 flex items-center gap-3 p-3 border rounded-lg cursor-pointer transition-colors {installTemplates
										? 'bg-blue-50 dark:bg-highlight-blue/20 border-cta-blue dark:border-highlight-blue'
										: 'border-gray-300 dark:border-gray-600 hover:border-blue-300 dark:hover:border-highlight-blue/80'}"
								>
									<input type="checkbox" class="pc-checkbox" bind:checked={installTemplates} />
									<span class="text-sm font-medium text-gray-900 dark:text-gray-100">
										Yes, install example templates
									</span>
								</label>
								{#if templatesError}
									<div
										role="alert"
										class="mt-2 p-3 rounded-md border bg-yellow-50 border-yellow-200 dark:bg-yellow-900/20 dark:border-yellow-800 transition-colors duration-200"
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
							</FormSection>
						</div>
					{:else}
						<div id="step-{currentStep}" tabindex="-1" class="outline-none">
							<FormSection title="Welcome to Phishing Club">
								<div class="space-y-3 text-sm text-slate-600 dark:text-gray-400">
									<p>
										Get started by reading our
										<a
											href="https://phishing.club/guide/introduction/"
											target="_blank"
											rel="noopener noreferrer"
											class={linkClass}>user guide</a
										>
									</p>
									<p>
										Have questions, bugs or suggestions? Contact us at
										<a href="mailto:support@phishing.club" class="{linkClass} whitespace-nowrap"
											>support@phishing.club</a
										>
									</p>
									<p>Select Setup to create your account and finish.</p>
								</div>
							</FormSection>
						</div>
					{/if}
				</div>

				<FormError message={formError} />

				<div
					class="-mx-4 sm:-mx-8 px-4 sm:px-8 py-4 mt-6 flex flex-wrap items-center justify-between gap-2 border-t-2 border-gray-200 dark:border-gray-700 transition-colors duration-200"
				>
					{#if currentStep > 1}
						<button type="button" class={secondaryButtonClass} on:click={previousStep}>
							<svg
								class="h-4 w-4"
								xmlns="http://www.w3.org/2000/svg"
								fill="none"
								viewBox="0 0 24 24"
								stroke="currentColor"
								aria-hidden="true"
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
						<div></div>
					{/if}

					<FormButton type="button" size="large" {isSubmitting} on:click={nextStep}>
						{#if currentStep === steps.length}
							Setup
						{:else}
							Next
							<svg
								class="ml-1 h-4 w-4"
								xmlns="http://www.w3.org/2000/svg"
								fill="none"
								viewBox="0 0 24 24"
								stroke="currentColor"
								aria-hidden="true"
							>
								<path
									stroke-linecap="round"
									stroke-linejoin="round"
									stroke-width="2"
									d="M9 5l7 7-7 7"
								/>
							</svg>
						{/if}
					</FormButton>
				</div>
			</FormGrid>
		</div>
	</div>
</main>
