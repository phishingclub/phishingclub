<script>
	import { onMount } from 'svelte';
	import { api } from '$lib/api/apiProxy.js';
	import { immediateResponseHandler } from '$lib/api/middleware.js';
	import { addToast } from '$lib/store/toast';
	import { onClickCopy } from '$lib/utils/common';
	import SettingsCard from '$lib/components/SettingsCard.svelte';
	import StatusBadge from '$lib/components/StatusBadge.svelte';
	import { showIsLoading, hideIsLoading } from '$lib/store/loading';
	import Button from '$lib/components/Button.svelte';
	import Alert from '$lib/components/Alert.svelte';
	import Form from '$lib/components/Form.svelte';
	import FormButton from '$lib/components/FormButton.svelte';
	import FormError from '$lib/components/FormError.svelte';
	import TextFieldSelect from '$lib/components/TextFieldSelect.svelte';

	const logLevels = ['debug', 'info', 'warn', 'error'];
	const dbLogLevels = ['silent', 'info', 'warn', 'error'];

	let loaded = false;
	let logLevel = '';
	let dbLogLevel = '';
	let logError = '';
	let isSavingLog = false;

	let version = '';
	let updateAvailable = false;
	let isCheckingUpdate = false;

	let isWipingBrowserCache = false;
	let isWipeAlertVisible = false;

	onMount(async () => {
		showIsLoading();
		try {
			await refreshLogLevel();
			await refreshVersion();
			await refreshUpdateCached();
		} finally {
			loaded = true;
			hideIsLoading();
		}
	});

	async function refreshLogLevel() {
		try {
			const res = immediateResponseHandler(await api.log.getLevel());
			if (res.success) {
				logLevel = res.data.level;
				dbLogLevel = res.data.dbLevel;
			} else {
				console.error(res);
			}
		} catch (err) {
			console.error(err);
		}
	}

	// save both log levels together, only applied when the user clicks Save
	async function saveLogLevel() {
		logError = '';
		isSavingLog = true;
		try {
			const res = await api.log.setLevel(logLevel, dbLogLevel);
			if (!res.success) {
				logError = res.error || 'Failed to update logging';
				return;
			}
			addToast('Logging updated', 'Success');
		} catch (err) {
			logError = 'Failed to update logging';
			console.error(err);
		} finally {
			isSavingLog = false;
		}
	}

	async function refreshVersion() {
		try {
			const res = await api.version.get();
			if (!res.success) {
				throw res.error;
			}
			version = res.data;
		} catch (e) {
			console.error('failed to check version', e);
		}
	}

	async function refreshUpdateCached() {
		try {
			const res = await api.application.isUpdateAvailableCached();
			if (!res.success) {
				throw res.error;
			}
			updateAvailable = res.data.updateAvailable;
		} catch (e) {
			console.error('failed to check cached update status', e);
		}
	}

	async function checkForUpdate() {
		isCheckingUpdate = true;
		try {
			const res = await api.application.isUpdateAvailable();
			if (!res.success) {
				throw res.error;
			}
			updateAvailable = res.data.updateAvailable;
			if (updateAvailable) {
				addToast('Update available!', 'Success');
			} else {
				addToast('No updates available', 'Info');
			}
		} catch (e) {
			addToast('Failed to check for updates', 'Error');
			console.error('failed to check for updates', e);
		} finally {
			isCheckingUpdate = false;
		}
	}

	const onWipeBrowserCache = async () => {
		isWipingBrowserCache = true;
		try {
			const response = await api.reportTemplate.wipeBrowserCache();
			if (response.success) {
				addToast('Browser cache wiped', 'Success');
				return { success: true };
			}
			addToast(response.error || 'Failed to wipe browser cache', 'Error');
			return { success: false };
		} catch (e) {
			addToast('Failed to wipe browser cache', 'Error');
			return { success: false };
		} finally {
			isWipingBrowserCache = false;
		}
	};
</script>

{#if loaded}
<div class="flex flex-wrap gap-6">
	<SettingsCard title="Logging" description="How much the server writes to its logs.">
		<Form on:submit={saveLogLevel} fullWidth>
			<TextFieldSelect
				id="appLogLevel"
				size="full"
				required
				bind:value={logLevel}
				options={logLevels}>Application log level</TextFieldSelect
			>

			<TextFieldSelect
				id="dbLogLevel"
				size="full"
				required
				bind:value={dbLogLevel}
				options={dbLogLevels}>Database log level</TextFieldSelect
			>
			<FormError message={logError} />
			<div class="mt-6 flex justify-end">
				<FormButton size={'large'} isSubmitting={isSavingLog}>Save Changes</FormButton>
			</div>
		</Form>
	</SettingsCard>

	<SettingsCard title="Browser Cache" description="Chromium is downloaded and cached for PDF reports and remote browser sessions. Wipe to force a fresh download.">
		<svelte:fragment slot="footer">
			<Button
				size={'large'}
				variant="danger-outline"
				disabled={isWipingBrowserCache}
				on:click={() => (isWipeAlertVisible = true)}
			>
				{isWipingBrowserCache ? 'Wiping...' : 'Wipe Browser Cache'}
			</Button>
		</svelte:fragment>
	</SettingsCard>

	<SettingsCard title="About">
		<div class="space-y-3 text-sm">
			<div class="flex items-center justify-between gap-2">
				<span class="text-gray-500 dark:text-gray-400 transition-colors duration-200">Version</span>
				<button
					type="button"
					on:click|preventDefault={() => onClickCopy(version)}
					aria-label="Copy version"
					title="Copy version"
					class="pc-focus flex min-w-0 items-center gap-2 hover:bg-gray-100 dark:hover:bg-gray-700 py-1 px-2 rounded-md text-gray-700 dark:text-gray-200 transition-colors duration-200"
				>
					<span class="font-mono truncate">{version}</span>
					<svg
						class="h-4 w-4 shrink-0 text-gray-500 dark:text-gray-400"
						viewBox="0 0 24 24"
						fill="none"
						stroke="currentColor"
						stroke-width="2"
						stroke-linecap="round"
						stroke-linejoin="round"
						aria-hidden="true"
						><rect x="9" y="9" width="11" height="11" rx="1.5" /><path
							d="M5 15H4.5A1.5 1.5 0 0 1 3 13.5v-9A1.5 1.5 0 0 1 4.5 3h9A1.5 1.5 0 0 1 15 4.5V5"
						/></svg
					>
				</button>
			</div>
			<div class="flex items-center justify-between gap-2">
				<span class="text-gray-500 dark:text-gray-400 transition-colors duration-200">Status</span>
				{#if updateAvailable}
					<a href="/settings/update/" class="pc-focus rounded-full hover:underline">
						<StatusBadge tone="info">Update available</StatusBadge>
					</a>
				{:else}
					<StatusBadge tone="success">Up to date</StatusBadge>
				{/if}
			</div>
			<div class="flex items-center justify-between gap-2">
				<span class="text-gray-500 dark:text-gray-400 transition-colors duration-200">Licenses</span>
				<a
					href="/licenses.txt"
					class="pc-focus rounded-sm text-cta-blue dark:text-highlight-blue hover:underline"
					>View licenses</a
				>
			</div>
		</div>
		<svelte:fragment slot="footer">
			<Button size={'large'} disabled={isCheckingUpdate} on:click={checkForUpdate}>
				{isCheckingUpdate ? 'Checking...' : 'Check for Updates'}
			</Button>
		</svelte:fragment>
	</SettingsCard>
</div>
{/if}

<Alert
	headline="Wipe browser cache"
	bind:visible={isWipeAlertVisible}
	onConfirm={onWipeBrowserCache}
	ok="Wipe cache"
>
	<p>
		Chromium will be deleted and downloaded again on the next PDF report or remote browser session,
		which can take a few minutes.
	</p>
</Alert>

