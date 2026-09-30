<script>
	import { onMount } from 'svelte';
	import TextField from '$lib/components/TextField.svelte';
	import FormError from '$lib/components/FormError.svelte';
	import Button from '$lib/components/Button.svelte';
	import SettingsCard from '$lib/components/SettingsCard.svelte';

	let query = '';
	let isSubmitting = false;
	let lookupError = '';
	let results = null;
	let mode = 'search'; // 'ip' when the query was an IP address
	let asnAvailable = true;
	let searchTimer = null;

	onMount(async () => {
		try {
			const res = await fetch('/api/v1/geoip/metadata', { credentials: 'include' });
			if (res.ok) {
				const data = await res.json();
				asnAvailable = !!data.data?.asn_available;
			}
		} catch (_) {
			// leave asnAvailable true, a failed lookup will surface the reason
		}
	});

	const isIP = (s) => {
		const v = s.trim();
		return /^(\d{1,3}\.){3}\d{1,3}$/.test(v) || v.includes(':');
	};

	async function run(q) {
		isSubmitting = true;
		lookupError = '';
		try {
			let url;
			if (isIP(q)) {
				mode = 'ip';
				url = `/api/v1/ipdata/asn/lookup?ip=${encodeURIComponent(q)}`;
			} else {
				mode = 'search';
				url = `/api/v1/ipdata/asn/search?q=${encodeURIComponent(q)}&limit=50`;
			}
			const response = await fetch(url, { method: 'GET', credentials: 'include' });
			if (!response.ok) {
				const errorData = await response.json();
				lookupError = errorData.message || 'failed to look up ASN';
				return;
			}
			const data = await response.json();
			if (mode === 'ip' && data.data && data.data.available === false) {
				asnAvailable = false;
			}
			results = data.data?.results || [];
		} catch (error) {
			lookupError = 'an error occurred while looking up the ASN';
			console.error('asn lookup error:', error);
		} finally {
			isSubmitting = false;
		}
	}

	// search live as the user types a name or number, but leave IPs for Enter or
	// the button since a partial IP is not meaningful
	function handleInput() {
		clearTimeout(searchTimer);
		const q = query.trim();
		lookupError = '';
		if (q === '' || isIP(q)) {
			results = null;
			return;
		}
		searchTimer = setTimeout(() => run(q), 200);
	}

	function handleLookup() {
		clearTimeout(searchTimer);
		const q = query.trim();
		if (!q) {
			lookupError = 'please enter an IP, ASN number, or name';
			return;
		}
		run(q);
	}

	function handleKey(event) {
		if (event.key === 'Enter') {
			handleLookup();
			return;
		}
		handleInput();
	}
</script>

<div class="flex flex-wrap gap-6">
	<SettingsCard title="ASN Lookup">
		<div class="space-y-4">
			<TextField
				type="text"
				bind:value={query}
				placeholder="IP, ASN number, or name (e.g. 8.8.8.8, 3292, M247)"
				on:keyup={handleKey}
			>
				Search
			</TextField>

			<FormError message={lookupError} />

			<div class="text-xs text-gray-500 dark:text-gray-400 transition-colors duration-200">
				Data from
				<a
					href="https://github.com/ipverse/asn-ip"
					target="_blank"
					rel="noopener noreferrer"
					class="text-blue-600 dark:text-blue-400 hover:underline"
				>
					ipverse/asn-ip
				</a>
			</div>

			{#if !asnAvailable}
				<div
					class="p-3 rounded-md bg-yellow-50 dark:bg-yellow-900/20 transition-colors duration-200"
				>
					<p class="text-sm font-medium text-yellow-700 dark:text-yellow-300">
						No ASN data downloaded. Enable it in
						<a class="underline" href="/settings#ipdata">Settings, IP Data</a>.
					</p>
				</div>
			{:else if results !== null}
				{#if results.length > 0}
					<div
						class="p-3 rounded-md bg-green-50 dark:bg-green-900/20 transition-colors duration-200 space-y-1"
					>
						{#each results as r (r.asn)}
							<p class="text-sm font-medium text-green-700 dark:text-green-300">
								<strong>AS{r.asn}</strong>
								{r.name || r.handle}{#if r.country}
									· {r.country}{/if}{#if r.handle}
									<span class="opacity-70"> · {r.handle}</span>{/if}
							</p>
						{/each}
					</div>
				{:else}
					<div
						class="p-3 rounded-md bg-yellow-50 dark:bg-yellow-900/20 transition-colors duration-200"
					>
						<p class="text-sm font-medium text-yellow-700 dark:text-yellow-300">
							{mode === 'ip' ? 'No match' : 'No results'}
						</p>
					</div>
				{/if}
			{/if}
		</div>
		<svelte:fragment slot="footer">
			<Button size={'large'} on:click={handleLookup} disabled={isSubmitting}>
				{#if isSubmitting}Looking up...{:else}Lookup{/if}
			</Button>
		</svelte:fragment>
	</SettingsCard>
</div>
