<script>
	import Headline from '$lib/components/Headline.svelte';
	import HeadTitle from '$lib/components/HeadTitle.svelte';
	import { hideIsLoading, showIsLoading } from '$lib/store/loading';
	import { onMount } from 'svelte';
	import { api } from '$lib/api/apiProxy.js';
	import Alert from '$lib/components/Alert.svelte';
	import { addToast } from '$lib/store/toast';

	let isUpdateAvailable = false;
	let isUpdateLocal = false;
	let downloadURL = false;
	let newVersion = null;
	let isUpdating = false;

	let isUpdateAlertVisible = false;

	// hooks
	onMount(() => {
		(async () => {
			showIsLoading();
			await getUpdateDetails();
			hideIsLoading();
		})();
	});

	const getUpdateDetails = async () => {
		try {
			const res = await api.application.getUpdateDetails();
			if (res.data) {
				newVersion = res.data.latestVersion;
				isUpdateAvailable = res.data.updateAvailable || false;
				isUpdateLocal = res.data.updateInApp || false;
			}
		} catch (e) {
			console.error('failed to get update details:', e);
			addToast('Failed to check for updates. Please try again later.', 'Error');
		}
	};

	const installUpdate = async () => {
		try {
			if (!isUpdateLocal) {
				window.open('https://user.phishing.club/downloads', '_blank');
			} else {
				isUpdateAlertVisible = true;
			}
		} catch (e) {
			console.error('failed to install handle update:', e);
		}
	};

	const runUpdate = async () => {
		showIsLoading();
		try {
			const res = await api.application.runUpdate();
			if (res.success) {
				await new Promise((resolve) => {
					setTimeout(() => {
						window.location.reload();
						resolve(); // Resolve the Promise after reloading
					}, 10000);
				});
			}
			return res;
		} catch (e) {
			console.error(e);
		} finally {
			hideIsLoading();
		}
	};
</script>

<HeadTitle title="Update - Settings" />
<main class="pb-8">
	<Headline>Update</Headline>
	<div class="pt-4">
		<div
			class="bg-white dark:bg-gray-800 p-6 rounded-lg shadow-sm dark:shadow-gray-900/50 border border-gray-100 dark:border-gray-700 transition-colors duration-200"
		>
			<div class="grid grid-cols-1 gap-6">
				<div>
					{#if isUpdateAvailable}
						<div class="mb-4">
							<div class="flex items-center justify-between mb-4">
								<div>
									<p class="text-gray-700 dark:text-gray-200 transition-colors duration-200">
										Version {newVersion} is now available
									</p>
									<p class="mt-2">
										<a
											href="https://github.com/phishingclub/phishingclub/releases"
											target="_blank"
											class="text-blue-600 dark:text-white hover:text-blue-700 dark:hover:text-gray-200 underline transition-colors duration-200"
										>
											View release notes
										</a>
									</p>
									{#if !isUpdateLocal}
										<p class="mt-4 text-gray-600 dark:text-gray-300 transition-colors duration-200">
											This instance is was not setup using the <code>systemd install</code> and must
											be updated manually.
										</p>
									{/if}
									<p
										class="mt-4 text-sm text-gray-600 dark:text-gray-300 transition-colors duration-200"
									>
										Consider creating a backup before updating to ensure you can restore if needed.
									</p>
								</div>
							</div>

							<div class="mt-6 flex">
								<button
									on:click={installUpdate}
									disabled={isUpdating}
									class="px-4 py-2 bg-cta-blue hover:bg-active-blue text-white rounded-md transition-colors disabled:opacity-50 disabled:cursor-not-allowed flex items-center"
								>
									{#if !isUpdateLocal}
										Download
									{:else}
										Update
									{/if}
								</button>
							</div>
						</div>
					{:else}
						<div class="text-center">
							<svg
								class="mx-auto h-12 w-12 text-cta-green"
								xmlns="http://www.w3.org/2000/svg"
								fill="none"
								viewBox="0 0 24 24"
								stroke="currentColor"
							>
								<path
									stroke-linecap="round"
									stroke-linejoin="round"
									stroke-width="2"
									d="M5 13l4 4L19 7"
								/>
							</svg>
							<h3
								class="mt-2 text-lg font-medium text-gray-900 dark:text-gray-100 transition-colors duration-200"
							>
								System up to date!
							</h3>
							<p class="mt-1 text-gray-600 dark:text-gray-300 transition-colors duration-200">
								An update notification is visible when a update is ready.
							</p>
							<p class="mt-3">
								<a
									href="https://github.com/phishingclub/phishingclub/releases"
									target="_blank"
									class="text-sm text-blue-600 dark:text-white hover:text-blue-700 dark:hover:text-gray-200 transition-colors duration-200"
								>
									View previous release information
								</a>
							</p>
						</div>
					{/if}
				</div>
			</div>
		</div>
	</div>
	<Alert onConfirm={runUpdate} visible={isUpdateAlertVisible}>
		<p class="">Updating will make the application unavailable for a short time.</p>
		<p class="mt-4">Click <b>YES, PROCEEED</b> to continue</p>
	</Alert>
</main>
