<script>
	import TextField from '$lib/components/TextField.svelte';
	import FormError from '$lib/components/FormError.svelte';
	import Button from '$lib/components/Button.svelte';
	import SettingsCard from '$lib/components/SettingsCard.svelte';

	let ipAddress = '';
	let isSubmitting = false;
	let lookupError = '';
	let lookupResult = null;

	async function handleGeoIPLookup() {
		if (!ipAddress.trim()) {
			lookupError = 'please enter an ip address';
			return;
		}
		isSubmitting = true;
		lookupError = '';
		lookupResult = null;
		try {
			const response = await fetch(`/api/v1/geoip/lookup?ip=${encodeURIComponent(ipAddress)}`, {
				method: 'GET',
				credentials: 'include'
			});
			if (!response.ok) {
				const errorData = await response.json();
				lookupError = errorData.message || 'failed to lookup ip address';
				return;
			}
			const data = await response.json();
			lookupResult = data.data;
		} catch (error) {
			lookupError = 'an error occurred while looking up the ip address';
			console.error('geoip lookup error:', error);
		} finally {
			isSubmitting = false;
		}
	}

	function handleKeyPress(event) {
		if (event.key === 'Enter') handleGeoIPLookup();
	}
</script>

<div class="flex flex-wrap gap-6">
	<SettingsCard title="GeoIP Lookup">
		<div class="space-y-4">
			<TextField
				type="text"
				bind:value={ipAddress}
				placeholder="e.g., 8.8.8.8"
				on:keypress={handleKeyPress}
				disabled={isSubmitting}
			>
				IP Address
			</TextField>

			<FormError message={lookupError} />

			<div class="text-xs text-gray-500 dark:text-gray-400 transition-colors duration-200">
				Data from
				<a
					href="https://github.com/ipverse/rir-ip"
					target="_blank"
					rel="noopener noreferrer"
					class="text-blue-600 dark:text-blue-400 hover:underline"
				>
					ipverse/rir-ip
				</a>
			</div>

			{#if lookupResult}
				<div
					class="p-3 rounded-md transition-colors duration-200 {lookupResult.found
						? 'bg-green-50 dark:bg-green-900/20'
						: 'bg-yellow-50 dark:bg-yellow-900/20'}"
				>
					{#if lookupResult.found}
						<p class="text-sm font-medium text-green-700 dark:text-green-300">
							<strong>Country:</strong> {lookupResult.country_code}
						</p>
					{:else}
						<p class="text-sm font-medium text-yellow-700 dark:text-yellow-300">No match</p>
					{/if}
				</div>
			{/if}
		</div>
		<svelte:fragment slot="footer">
			<Button size={'large'} on:click={handleGeoIPLookup} disabled={isSubmitting}>
				{#if isSubmitting}Looking up...{:else}Lookup{/if}
			</Button>
		</svelte:fragment>
	</SettingsCard>
</div>
