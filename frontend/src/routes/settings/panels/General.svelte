<script>
	import { onMount } from 'svelte';
	import { api } from '$lib/api/apiProxy.js';
	import { immediateResponseHandler } from '$lib/api/middleware.js';
	import { addToast } from '$lib/store/toast';
	import { hideIsLoading, showIsLoading } from '$lib/store/loading';
	import { displayMode, DISPLAY_MODE } from '$lib/store/displayMode';
	import SettingsCard from '$lib/components/SettingsCard.svelte';
	import SettingsLoading from '$lib/components/SettingsLoading.svelte';
	import RadioOption from '$lib/components/RadioOption.svelte';
	import Form from '$lib/components/Form.svelte';
	import FormButton from '$lib/components/FormButton.svelte';
	import FormError from '$lib/components/FormError.svelte';
	import TextField from '$lib/components/TextField.svelte';

	let loaded = false;
	let currentDisplayMode = DISPLAY_MODE.WHITEBOX;
	let displayModeError = '';

	let formValues = {
		maxFileSize: null,
		repeatOffenderMonths: null
	};
	let updateSettingsError = '';

	onMount(async () => {
		try {
			await refreshDisplayMode();
			await refreshSettings();
		} finally {
			loaded = true;
		}
	});

	async function refreshDisplayMode() {
		try {
			const res = immediateResponseHandler(await api.option.get('display_mode'));
			if (res.success && res.data.value) {
				currentDisplayMode = res.data.value;
				displayMode.setMode(res.data.value);
			}
		} catch (e) {
			console.error('failed to refresh display mode', e);
		}
	}

	async function setDisplayMode(mode) {
		try {
			showIsLoading();
			const res = await api.option.set('display_mode', mode);
			if (res.success) {
				currentDisplayMode = mode;
				displayMode.setMode(mode);
				addToast('Display mode updated', 'Success');
				displayModeError = '';
			} else {
				displayModeError = res.error || 'Failed to update display mode';
			}
		} catch (e) {
			console.error('failed to set display mode', e);
			displayModeError = 'Failed to update display mode';
		} finally {
			hideIsLoading();
		}
	}

	async function refreshSettings() {
		try {
			const res = immediateResponseHandler(await api.option.get('max_file_upload_size_mb'));
			if (res.success) {
				formValues.maxFileSize = res.data.value;
			} else {
				throw res.error;
			}
			const resRepeat = immediateResponseHandler(await api.option.get('repeat_offender_months'));
			if (resRepeat.success) {
				formValues.repeatOffenderMonths = resRepeat.data.value;
			} else {
				throw resRepeat.error;
			}
		} catch (err) {
			console.error(err);
		}
	}

	const onClickUpdateSettings = async () => {
		updateSettingsError = '';
		try {
			const res = await api.option.set('max_file_upload_size_mb', formValues.maxFileSize);
			if (!res.success) {
				updateSettingsError = res.error;
				return;
			}
			const resRepeat = await api.option.set(
				'repeat_offender_months',
				formValues.repeatOffenderMonths
			);
			if (!resRepeat.success) {
				updateSettingsError = resRepeat.error;
				return;
			}
			addToast('Settings updated', 'Success');
		} catch (e) {
			addToast('Failed to update settings', 'Error');
			console.error('failed to update settings', e);
		}
	};
</script>

{#if !loaded}
	<SettingsLoading />
{:else}
<div class="flex flex-wrap gap-6">
	<SettingsCard title="Display Mode">
		<div class="space-y-4">
			<p class="text-gray-600 dark:text-gray-300 text-sm transition-colors duration-200">
				Select which features are available
			</p>
			<div class="space-y-3">
				<RadioOption
					checked={currentDisplayMode === DISPLAY_MODE.WHITEBOX}
					label="Phishing Simulation"
					on:change={() => setDisplayMode(DISPLAY_MODE.WHITEBOX)}
				/>
				<RadioOption
					checked={currentDisplayMode === DISPLAY_MODE.BLACKBOX}
					label="Red Team Phishing"
					on:change={() => setDisplayMode(DISPLAY_MODE.BLACKBOX)}
				/>
				<p class="text-gray-600 dark:text-gray-300 text-sm transition-colors duration-200">
					Read about the difference between <a
						class="white underline"
						href="https://phishing.club/blog/phishing-simulation-vs-red-team-phishing/"
						target="_blank">phishing simulation and red team phishing</a
					>
				</p>
			</div>
			<FormError message={displayModeError} />
		</div>
	</SettingsCard>

	<SettingsCard title="General Settings">
		<Form on:submit={onClickUpdateSettings} fullWidth>
			<TextField required width="full" type="number" min="1" bind:value={formValues.maxFileSize}
				>Upload max file size (MB)</TextField
			>
			<TextField
				required
				width="full"
				type="number"
				min={1}
				max={1000}
				bind:value={formValues.repeatOffenderMonths}>Repeat Offender Memory (Months)</TextField
			>
			<FormError message={updateSettingsError} />
			<div class="mt-6 flex justify-end">
				<FormButton size={'medium'}>Save Changes</FormButton>
			</div>
		</Form>
	</SettingsCard>
</div>
{/if}
