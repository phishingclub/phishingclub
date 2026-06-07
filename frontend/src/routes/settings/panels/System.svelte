<script>
	import { onMount } from 'svelte';
	import { api } from '$lib/api/apiProxy.js';
	import { immediateResponseHandler } from '$lib/api/middleware.js';
	import { addToast } from '$lib/store/toast';
	import { onClickCopy } from '$lib/utils/common';
	import SettingsCard from '$lib/components/SettingsCard.svelte';
	import SettingsLoading from '$lib/components/SettingsLoading.svelte';
	import Button from '$lib/components/Button.svelte';
	import Form from '$lib/components/Form.svelte';
	import TextFieldSelect from '$lib/components/TextFieldSelect.svelte';

	const logLevels = ['debug', 'info', 'warn', 'error'];
	const dbLogLevels = ['silent', 'info', 'warn', 'error'];

	let loaded = false;
	let logLevel = '';
	let dbLogLevel = '';

	let version = '';
	let updateAvailable = false;
	let isCheckingUpdate = false;

	let isWipingBrowserCache = false;

	onMount(async () => {
		try {
			await refreshLogLevel();
			await refreshVersion();
			await refreshUpdateCached();
		} finally {
			loaded = true;
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

	async function setLogLevel() {
		try {
			const res = await api.log.setLevel(logLevel, dbLogLevel);
			if (!res.success) {
				console.error(res);
			}
		} catch (err) {
			console.error(err);
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
			} else {
				addToast(response.error || 'Failed to wipe browser cache', 'Error');
			}
		} catch (e) {
			addToast('Failed to wipe browser cache', 'Error');
		} finally {
			isWipingBrowserCache = false;
		}
	};
</script>

{#if !loaded}
	<SettingsLoading />
{:else}
<div class="flex flex-wrap gap-6">
	<SettingsCard title="Logging">
		<Form>
			<TextFieldSelect
				id="appLogLevel"
				required
				bind:value={logLevel}
				onSelect={setLogLevel}
				options={logLevels}>Application log level</TextFieldSelect
			>

			<TextFieldSelect
				id="dbLogLevel"
				required
				bind:value={dbLogLevel}
				onSelect={setLogLevel}
				options={dbLogLevels}>Database log level</TextFieldSelect
			>
		</Form>
	</SettingsCard>

	<SettingsCard title="Browser Cache">
		<p class="text-gray-600 dark:text-gray-300 text-sm transition-colors duration-200">
			Chromium is downloaded and cached for PDF reports and remote browser sessions. Wipe to force a
			fresh download.
		</p>
		<svelte:fragment slot="footer">
			<Button
				size={'large'}
				backgroundColor="bg-red-600"
				disabled={isWipingBrowserCache}
				on:click={onWipeBrowserCache}
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
					on:click|preventDefault={() => onClickCopy(version)}
					class="flex items-center gap-2 hover:bg-gray-100 dark:hover:bg-gray-700 py-1 px-2 rounded-md text-gray-700 dark:text-gray-200 transition-colors duration-200"
				>
					<span class="font-mono">{version}</span>
					<img class="w-4 h-4" src="/icon-copy.svg" alt="copy version" />
				</button>
			</div>
			<div class="flex items-center justify-between gap-2">
				<span class="text-gray-500 dark:text-gray-400 transition-colors duration-200">Status</span>
				{#if updateAvailable}
					<a
						href="/settings/update/"
						class="text-blue-600 dark:text-white hover:underline transition-colors duration-200"
						>Update available</a
					>
				{:else}
					<span class="text-gray-700 dark:text-gray-200 transition-colors duration-200">Up to date</span
					>
				{/if}
			</div>
			<div class="flex items-center justify-between gap-2">
				<span class="text-gray-500 dark:text-gray-400 transition-colors duration-200">Licenses</span>
				<a
					href="/licenses.txt"
					class="text-blue-600 dark:text-white hover:underline transition-colors duration-200"
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
