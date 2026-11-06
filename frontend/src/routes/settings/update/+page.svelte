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
				window.open('https://github.com/phishingclub/phishingclub/releases', '_blank', 'noopener,noreferrer');
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
	<Headline docSlug="management">Update</Headline>
	<div class="pt-4">
		<div
			class="max-w-xl bg-white dark:bg-gray-800 p-6 rounded-lg shadow-sm dark:shadow-gray-900/50 border border-gray-100 dark:border-gray-700 transition-colors duration-200"
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
											rel="noopener noreferrer"
											class="pc-focus rounded-sm text-cta-blue dark:text-highlight-blue underline hover:no-underline"
										>
											View release notes
										</a>
									</p>
									{#if !isUpdateLocal}
										<p class="mt-4 text-gray-600 dark:text-gray-300 transition-colors duration-200">
											This instance was not set up with the <code>systemd install</code> and must be
											updated manually.
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
									type="button"
									class="pc-focus px-6 py-2 bg-cta-blue hover:bg-active-blue text-white text-sm font-semibold uppercase rounded-md transition-colors disabled:opacity-50 disabled:cursor-not-allowed flex items-center"
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
								System is up to date
							</h3>
							<p class="mt-1 text-gray-600 dark:text-gray-300 transition-colors duration-200">
								You will see a notification here when an update is ready.
							</p>
							<p class="mt-3">
								<a
									href="https://github.com/phishingclub/phishingclub/releases"
									target="_blank"
									rel="noopener noreferrer"
									class="pc-focus rounded-sm text-sm text-cta-blue dark:text-highlight-blue hover:underline"
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
		<p class="mt-4">Select <b>Yes, proceed</b> to continue.</p>
	</Alert>
</main>
