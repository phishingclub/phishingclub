<script>
	import Headline from '$lib/components/Headline.svelte';
	import HeadTitle from '$lib/components/HeadTitle.svelte';
	import { AppStateService } from '$lib/service/appState';
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
	<div class="bg-white dark:bg-half-devil-gray p-6 rounded-lg shadow-sm">
		<div class="grid grid-cols-1 gap-6">
			<div class="border-t pt-6 border-grayblue-light dark:border-devil-gray">
				{#if isUpdateAvailable}
					<div class="bg-pleasant-gray dark:bg-devil-gray p-4 rounded-md mb-4">
						<div class="flex items-center justify-between mb-4">
							<div>
								<p class="text-grayblue-dark">Version {newVersion} is now available</p>
								<p class="mt-2">
									<a
										href="https://github.com/phishingclub/phishingclub/releases"
										target="_blank"
										class="text-cta-blue hover:text-active-blue underline"
									>
										View release notes →
									</a>
								</p>
								{#if !isUpdateLocal}
									<p class="mt-4">
										This instance is was not setup using the <code>systemd install</code> and must be
										updated manually.
									</p>
								{/if}
								<p class="mt-4 text-sm text-gray-600">
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
									Get update
								{:else}
									Update
								{/if}
							</button>
						</div>
					</div>
				{:else}
					<div class="bg-pleasant-gray dark:bg-devil-gray p-4 rounded-md text-center">
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
						<h3 class="mt-2 text-lg font-medium">System up to date!</h3>
						<p class="mt-1 text-grayblue-dark">
							An update notification is visible when a update is ready.
						</p>
						<p class="mt-3">
							<a
								href="https://github.com/phishingclub/phishingclub/releases"
								target="_blank"
								class="text-xs hover:text-gray-700"
							>
								View previous release information
							</a>
						</p>
					</div>
				{/if}
			</div>
		</div>
	</div>
	<Alert onConfirm={runUpdate} visible={isUpdateAlertVisible}>
		<p class="">Updating will make the application unavailable for a short time.</p>
		<p class="mt-4">Click <b>YES, PROCEEED</b> to continue</p>
	</Alert>
</main>
