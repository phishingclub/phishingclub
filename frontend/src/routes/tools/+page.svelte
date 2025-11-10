<script>
	import { onMount, tick } from 'svelte';
	import HeadTitle from '$lib/components/HeadTitle.svelte';
	import Headline from '$lib/components/Headline.svelte';
	import Button from '$lib/components/Button.svelte';
	import TextField from '$lib/components/TextField.svelte';
	import FormError from '$lib/components/FormError.svelte';
	import TextFieldSelect from '$lib/components/TextFieldSelect.svelte';
	import TextFieldMultiSelect from '$lib/components/TextFieldMultiSelect.svelte';

	let ipAddress = '';
	let isSubmitting = false;
	let lookupError = '';
	let lookupResult = null;

	// ja4 builder
	let ja4Protocol = 't';
	let ja4Version = '13';
	let ja4Sni = 'i';
	let ja4Alpn = 'h2';
	let ja4Result = '';
	let customCipherInput = '';
	let customExtensionInput = '';
	let customSignatureAlgorithmInput = '';

	// common cipher suites - formatted for TextFieldMultiSelect
	const cipherSuites = [
		'* - Match Any',
		'custom - Custom (enter hex below)',
		'1301 - TLS_AES_128_GCM_SHA256',
		'1302 - TLS_AES_256_GCM_SHA384',
		'1303 - TLS_CHACHA20_POLY1305_SHA256',
		'c02b - TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256',
		'c02f - TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256',
		'c02c - TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384',
		'c030 - TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384',
		'cca9 - TLS_ECDHE_ECDSA_WITH_CHACHA20_POLY1305_SHA256',
		'cca8 - TLS_ECDHE_RSA_WITH_CHACHA20_POLY1305_SHA256',
		'c013 - TLS_ECDHE_RSA_WITH_AES_128_CBC_SHA',
		'c014 - TLS_ECDHE_RSA_WITH_AES_256_CBC_SHA',
		'009c - TLS_RSA_WITH_AES_128_GCM_SHA256',
		'009d - TLS_RSA_WITH_AES_256_GCM_SHA384',
		'002f - TLS_RSA_WITH_AES_128_CBC_SHA',
		'0035 - TLS_RSA_WITH_AES_256_CBC_SHA'
	];

	// common extensions - formatted for TextFieldMultiSelect
	const extensions = [
		'* - Match Any',
		'custom - Custom (enter hex below)',
		'0000 - server_name (SNI)',
		'0001 - max_fragment_length',
		'0005 - status_request',
		'000a - supported_groups',
		'000b - ec_point_formats',
		'000d - signature_algorithms',
		'0010 - application_layer_protocol_negotiation (ALPN)',
		'0012 - signed_certificate_timestamp',
		'0015 - padding',
		'0017 - extended_master_secret',
		'001b - compress_certificate',
		'0023 - session_ticket',
		'002b - supported_versions',
		'002d - psk_key_exchange_modes',
		'0033 - key_share',
		'4469 - encrypted_client_hello',
		'ff01 - renegotiation_info'
	];

	// common signature algorithms - formatted for TextFieldMultiSelect
	const signatureAlgorithms = [
		'* - Match Any',
		'custom - Custom (enter hex below)',
		'0403 - ECDSA-SHA256',
		'0401 - RSA-SHA256',
		'0501 - RSA-SHA384',
		'0601 - RSA-SHA512',
		'0804 - Ed25519',
		'0805 - Ed448',
		'0806 - RSA-PSS-RSAE-SHA256',
		'0503 - ECDSA-SHA384',
		'0603 - ECDSA-SHA512',
		'0807 - RSA-PSS-RSAE-SHA384',
		'0808 - RSA-PSS-RSAE-SHA512',
		'0809 - RSA-PSS-PSS-SHA256',
		'080a - RSA-PSS-PSS-SHA384',
		'080b - RSA-PSS-PSS-SHA512'
	];

	let selectedCiphers = ['* - Match Any'];
	let selectedExtensions = ['* - Match Any'];
	let selectedSignatureAlgorithms = ['* - Match Any'];

	// handle cipher selection with wildcard exclusivity
	async function handleCipherSelect(option) {
		await tick();
		if (option === '* - Match Any') {
			// wildcard was selected, clear everything else
			selectedCiphers = ['* - Match Any'];
			customCipherInput = '';
		} else if (selectedCiphers.includes('* - Match Any')) {
			// other item was selected while wildcard exists, remove wildcard
			selectedCiphers = selectedCiphers.filter((c) => c !== '* - Match Any');
		}
	}

	// handle extension selection with wildcard exclusivity
	async function handleExtensionSelect(option) {
		await tick();
		if (option === '* - Match Any') {
			// wildcard was selected, clear everything else
			selectedExtensions = ['* - Match Any'];
			customExtensionInput = '';
		} else if (selectedExtensions.includes('* - Match Any')) {
			// other item was selected while wildcard exists, remove wildcard
			selectedExtensions = selectedExtensions.filter((e) => e !== '* - Match Any');
		}
	}

	// handle signature algorithm selection with wildcard exclusivity
	async function handleSignatureAlgorithmSelect(option) {
		await tick();
		if (option === '* - Match Any') {
			// wildcard was selected, clear everything else
			selectedSignatureAlgorithms = ['* - Match Any'];
			customSignatureAlgorithmInput = '';
		} else if (selectedSignatureAlgorithms.includes('* - Match Any')) {
			// other item was selected while wildcard exists, remove wildcard
			selectedSignatureAlgorithms = selectedSignatureAlgorithms.filter(
				(s) => s !== '* - Match Any'
			);
		}
	}

	// sha256 hash function for ja4 b and c parts
	async function sha256(message) {
		const msgBuffer = new TextEncoder().encode(message);
		const hashBuffer = await crypto.subtle.digest('SHA-256', msgBuffer);
		const hashArray = Array.from(new Uint8Array(hashBuffer));
		const hashHex = hashArray.map((b) => b.toString(16).padStart(2, '0')).join('');
		return hashHex.substring(0, 12); // truncate to first 12 characters
	}

	async function buildJA4() {
		// check for wildcard in ciphers, extensions, or signature algorithms
		const hasWildcardCiphers = selectedCiphers.some((c) => c.startsWith('*'));
		const hasWildcardExtensions = selectedExtensions.some((e) => e.startsWith('*'));
		const hasWildcardSignatureAlgorithms = selectedSignatureAlgorithms.some((s) =>
			s.startsWith('*')
		);

		// check for custom in ciphers, extensions, and signature algorithms and add custom values
		let effectiveCiphers = [...selectedCiphers];
		let effectiveExtensions = [...selectedExtensions];
		let effectiveSignatureAlgorithms = [...selectedSignatureAlgorithms];

		if (selectedCiphers.some((c) => c.startsWith('custom')) && customCipherInput.trim()) {
			effectiveCiphers = effectiveCiphers.filter((c) => !c.startsWith('custom'));
			const customCiphers = customCipherInput
				.split(',')
				.map((c) => c.trim())
				.filter((c) => c);
			effectiveCiphers.push(...customCiphers.map((c) => `${c} - Custom`));
		} else {
			effectiveCiphers = effectiveCiphers.filter((c) => !c.startsWith('custom'));
		}

		if (selectedExtensions.some((e) => e.startsWith('custom')) && customExtensionInput.trim()) {
			effectiveExtensions = effectiveExtensions.filter((e) => !e.startsWith('custom'));
			const customExtensions = customExtensionInput
				.split(',')
				.map((e) => e.trim())
				.filter((e) => e);
			effectiveExtensions.push(...customExtensions.map((e) => `${e} - Custom`));
		} else {
			effectiveExtensions = effectiveExtensions.filter((e) => !e.startsWith('custom'));
		}

		if (
			selectedSignatureAlgorithms.some((s) => s.startsWith('custom')) &&
			customSignatureAlgorithmInput.trim()
		) {
			effectiveSignatureAlgorithms = effectiveSignatureAlgorithms.filter(
				(s) => !s.startsWith('custom')
			);
			const customSigAlgs = customSignatureAlgorithmInput
				.split(',')
				.map((s) => s.trim())
				.filter((s) => s);
			effectiveSignatureAlgorithms.push(...customSigAlgs.map((s) => `${s} - Custom`));
		} else {
			effectiveSignatureAlgorithms = effectiveSignatureAlgorithms.filter(
				(s) => !s.startsWith('custom')
			);
		}

		const cipherCount = hasWildcardCiphers
			? '*'
			: effectiveCiphers.length.toString().padStart(2, '0');

		// per ja4 spec: extension count includes sni and alpn
		let extensionCount;
		if (hasWildcardExtensions) {
			extensionCount = '*';
		} else {
			let count = effectiveExtensions.length;
			// add sni to count if present (d = domain)
			if (ja4Sni === 'd') {
				count++;
			}
			// add alpn to count if present (anything except '00')
			if (ja4Alpn !== '00' && ja4Alpn !== '*') {
				count++;
			}
			extensionCount = count.toString().padStart(2, '0');
		}

		// handle wildcard alpn - keep * as single character
		const alpnValue = ja4Alpn === '*' ? '*' : ja4Alpn;

		// part a: protocol_version_sni_ciphers_extensions_alpn
		const partA = `${ja4Protocol}${ja4Version}${ja4Sni}${cipherCount}${extensionCount}${alpnValue}`;

		// part b: hash of cipher suites (if wildcard, use *)
		let partB = '*';
		if (!hasWildcardCiphers && effectiveCiphers.length > 0) {
			// extract hex codes from cipher suites and sort in hex order (per ja4 spec)
			const cipherHexCodes = effectiveCiphers
				.map((c) => c.split(' - ')[0].toLowerCase())
				.sort() // sort alphabetically = hex order for 4-char hex strings
				.join(',');
			partB = await sha256(cipherHexCodes);
		} else if (effectiveCiphers.length === 0) {
			partB = '000000000000';
		}

		// part c: hash of extensions + signature algorithms (if wildcard, use *)
		// per ja4 spec: exclude sni (0000) and alpn (0010) from the hash
		// signature algorithms are appended after underscore (not sorted, kept in order)
		let partC = '*';
		if (!hasWildcardExtensions && !hasWildcardSignatureAlgorithms) {
			// extract hex codes from extensions, exclude sni (0000) and alpn (0010), and sort in hex order
			const extensionHexCodes = effectiveExtensions
				.map((e) => e.split(' - ')[0].toLowerCase())
				.filter((hex) => hex !== '0000' && hex !== '0010')
				.sort() // sort alphabetically = hex order for 4-char hex strings
				.join(',');

			// extract hex codes from signature algorithms (keep in original order, do NOT sort per spec)
			const sigAlgHexCodes = effectiveSignatureAlgorithms
				.map((s) => s.split(' - ')[0].toLowerCase())
				.join(',');

			// combine extensions and signature algorithms with underscore
			let combinedString = '';
			if (extensionHexCodes && sigAlgHexCodes) {
				combinedString = `${extensionHexCodes}_${sigAlgHexCodes}`;
			} else if (extensionHexCodes) {
				combinedString = extensionHexCodes;
			} else if (sigAlgHexCodes) {
				combinedString = `_${sigAlgHexCodes}`;
			}

			if (combinedString) {
				partC = await sha256(combinedString);
			} else {
				partC = '000000000000';
			}
		} else if (
			effectiveExtensions.length === 0 &&
			effectiveSignatureAlgorithms.length === 0 &&
			ja4Sni === 'i' &&
			(ja4Alpn === '00' || ja4Alpn === '*')
		) {
			// only set to zeros if truly no extensions (including sni/alpn) and no sig algs
			partC = '000000000000';
		}

		// format: a_b_c
		ja4Result = `${partA}_${partB}_${partC}`;
	}

	const protocolOptions = ['* - Match Any', 't - TCP/TLS', 'q - QUIC', 'd - DTLS'];
	const versionOptions = [
		'* - Match Any',
		'13 - TLS 1.3',
		'12 - TLS 1.2',
		'11 - TLS 1.1',
		'10 - TLS 1.0',
		's3 - SSL 3.0',
		's2 - SSL 2.0',
		'd3 - DTLS 1.3',
		'd2 - DTLS 1.2',
		'd1 - DTLS 1.0',
		'00 - Unknown'
	];
	const sniOptions = ['* - Match Any', 'i - Present', 'd - Not Present'];
	const alpnOptions = [
		'* - Match Any',
		'h9 - HTTP/0.9',
		'h0 - HTTP/1.0',
		'h1 - HTTP/1.1',
		'h2 - HTTP/2 (h2)',
		'hc - HTTP/2 over cleartext (h2c)',
		'h3 - HTTP/3 (h3)',
		'ht - HTTP (legacy/experimental)',
		's1 - SPDY/1 (spdy/1)',
		's2 - SPDY/2 (spdy/2)',
		's3 - SPDY/3 (spdy/3)',
		'sp - SPDY (legacy/experimental)',
		'wc - WebRTC (webrtc)',
		'cc - Confidential WebRTC (c-webrtc)',
		'00 - No ALPN'
	];

	// reactive statement to rebuild ja4 whenever any field changes
	$: ja4Protocol,
		ja4Version,
		ja4Sni,
		ja4Alpn,
		selectedCiphers,
		selectedExtensions,
		selectedSignatureAlgorithms,
		customCipherInput,
		customExtensionInput,
		customSignatureAlgorithmInput,
		buildJA4();

	// initialize ja4 on mount
	onMount(() => {
		buildJA4();
	});

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
		if (event.key === 'Enter') {
			handleGeoIPLookup();
		}
	}

	function handleProtocolSelect(value) {
		const selected = value.split(' - ')[0];
		ja4Protocol = selected;
	}

	function handleVersionSelect(value) {
		const selected = value.split(' - ')[0];
		ja4Version = selected;
	}

	function handleSniSelect(value) {
		const selected = value.split(' - ')[0];
		ja4Sni = selected;
	}

	function handleAlpnSelect(value) {
		const selected = value.split(' - ')[0];
		ja4Alpn = selected;
	}

	// helper functions to get display values from state
	function getProtocolDisplayValue(protocol) {
		const protocolMap = {
			t: 't - TCP/TLS',
			q: 'q - QUIC',
			d: 'd - DTLS',
			'*': '* - Match Any'
		};
		return protocolMap[protocol] || 't - TCP/TLS';
	}

	function getVersionDisplayValue(version) {
		const versionMap = {
			'13': '13 - TLS 1.3',
			'12': '12 - TLS 1.2',
			'11': '11 - TLS 1.1',
			'10': '10 - TLS 1.0',
			s3: 's3 - SSL 3.0',
			s2: 's2 - SSL 2.0',
			d3: 'd3 - DTLS 1.3',
			d2: 'd2 - DTLS 1.2',
			d1: 'd1 - DTLS 1.0',
			'00': '00 - Unknown',
			'*': '* - Match Any'
		};
		return versionMap[version] || '13 - TLS 1.3';
	}

	function getSniDisplayValue(sni) {
		const sniMap = {
			i: 'i - Present',
			d: 'd - Not Present',
			'*': '* - Match Any'
		};
		return sniMap[sni] || 'i - Present';
	}

	function getAlpnDisplayValue(alpn) {
		const alpnMap = {
			h9: 'h9 - HTTP/0.9',
			h0: 'h0 - HTTP/1.0',
			h1: 'h1 - HTTP/1.1',
			h2: 'h2 - HTTP/2 (h2)',
			hc: 'hc - HTTP/2 over cleartext (h2c)',
			h3: 'h3 - HTTP/3 (h3)',
			ht: 'ht - HTTP (legacy/experimental)',
			s1: 's1 - SPDY/1 (spdy/1)',
			s2: 's2 - SPDY/2 (spdy/2)',
			s3: 's3 - SPDY/3 (spdy/3)',
			sp: 'sp - SPDY (legacy/experimental)',
			wc: 'wc - WebRTC (webrtc)',
			cc: 'cc - Confidential WebRTC (c-webrtc)',
			'00': '00 - No ALPN',
			'*': '* - Match Any'
		};
		return alpnMap[alpn] || 'h2 - HTTP/2 (h2)';
	}
</script>

<HeadTitle title="Tools" />

<main class="pb-8">
	<Headline>Tools</Headline>

	<div class="max-w-7xl pt-4 space-y-8">
		<div class="grid grid-cols-1 lg:grid-cols-2 xl:grid-cols-4 gap-8">
			<!-- JA4 Builder Card - Double Width and Double Height, First Position -->
			<div
				class="bg-white dark:bg-gray-800 p-6 rounded-lg shadow-sm dark:shadow-gray-900/50 border border-gray-100 dark:border-gray-700 h-[420px] lg:h-[858px] flex flex-col transition-colors duration-200 lg:col-span-2 lg:row-span-2"
			>
				<h2
					class="text-xl font-semibold text-gray-700 dark:text-gray-200 mb-4 transition-colors duration-200"
				>
					JA4 Fingerprint Builder
				</h2>

				{#if ja4Result}
					<div
						class="p-3 rounded-md bg-blue-50 dark:bg-blue-900/20 transition-colors duration-200 mb-4 cursor-pointer hover:bg-blue-100 dark:hover:bg-blue-900/30"
						on:click={() => {
							navigator.clipboard.writeText(ja4Result);
						}}
						role="button"
						tabindex="0"
						on:keypress={(e) => {
							if (e.key === 'Enter' || e.key === ' ') {
								navigator.clipboard.writeText(ja4Result);
							}
						}}
					>
						<p class="text-xs text-gray-600 dark:text-gray-400 mb-1">
							JA4 Fingerprint (click to copy):
						</p>
						<p
							class="text-sm font-mono font-medium text-blue-700 dark:text-blue-300 break-all select-all"
						>
							{ja4Result}
						</p>
						<p class="text-xs text-gray-500 dark:text-gray-500 mt-2 font-mono">
							[protocol][version][sni][cipher_count][ext_count][alpn]_[sorted_cipher_hash]_[sorted_ext+sigalg_hash]
						</p>
					</div>
				{/if}

				<div class="flex-1 overflow-y-auto pr-2 space-y-3">
					<div class="grid grid-cols-2 gap-3">
						<TextFieldSelect
							id="ja4-protocol"
							value={getProtocolDisplayValue(ja4Protocol)}
							options={protocolOptions}
							onSelect={handleProtocolSelect}
						>
							Protocol
						</TextFieldSelect>

						<TextFieldSelect
							id="ja4-version"
							value={getVersionDisplayValue(ja4Version)}
							options={versionOptions}
							onSelect={handleVersionSelect}
						>
							TLS Version
						</TextFieldSelect>
					</div>

					<div class="grid grid-cols-2 gap-3">
						<TextFieldSelect
							id="ja4-sni"
							value={getSniDisplayValue(ja4Sni)}
							options={sniOptions}
							onSelect={handleSniSelect}
						>
							SNI
						</TextFieldSelect>

						<TextFieldSelect
							id="ja4-alpn"
							value={getAlpnDisplayValue(ja4Alpn)}
							options={alpnOptions}
							onSelect={handleAlpnSelect}
						>
							ALPN
						</TextFieldSelect>
					</div>

					<TextFieldMultiSelect
						id="ja4-ciphers"
						bind:value={selectedCiphers}
						options={cipherSuites}
						onSelect={handleCipherSelect}
					>
						Cipher Suites ({selectedCiphers.length} selected)
					</TextFieldMultiSelect>

					{#if selectedCiphers.some((c) => c.startsWith('custom'))}
						<TextField
							type="text"
							bind:value={customCipherInput}
							placeholder="e.g., 1301,c02b,009c"
						>
							Custom Cipher Hex Codes (comma-separated)
						</TextField>
					{/if}

					<TextFieldMultiSelect
						id="ja4-extensions"
						bind:value={selectedExtensions}
						options={extensions}
						onSelect={handleExtensionSelect}
					>
						Extensions ({selectedExtensions.length} selected)
					</TextFieldMultiSelect>

					{#if selectedExtensions.some((e) => e.startsWith('custom'))}
						<TextField
							type="text"
							bind:value={customExtensionInput}
							placeholder="e.g., 0000,000a,000d"
						>
							Custom Extension Hex Codes (comma-separated)
						</TextField>
					{/if}

					<TextFieldMultiSelect
						id="ja4-signature-algorithms"
						bind:value={selectedSignatureAlgorithms}
						options={signatureAlgorithms}
						onSelect={handleSignatureAlgorithmSelect}
					>
						Signature Algorithms ({selectedSignatureAlgorithms.length} selected)
					</TextFieldMultiSelect>

					{#if selectedSignatureAlgorithms.some((s) => s.startsWith('custom'))}
						<TextField
							type="text"
							bind:value={customSignatureAlgorithmInput}
							placeholder="e.g., 0403,0804,0401"
						>
							Custom Signature Algorithm Hex Codes (comma-separated)
						</TextField>
					{/if}
				</div>

				<div class="mt-4 pt-4 border-t border-gray-200 dark:border-gray-700">
					<a
						href="https://github.com/FoxIO-LLC/ja4"
						target="_blank"
						rel="noopener noreferrer"
						class="text-xs text-blue-600 dark:text-blue-400 hover:underline transition-colors duration-200"
					>
						Learn more about JA4
					</a>
				</div>
			</div>

			<!-- GeoIP Lookup Card -->
			<div
				class="bg-white dark:bg-gray-800 p-6 rounded-lg shadow-sm dark:shadow-gray-900/50 border border-gray-100 dark:border-gray-700 h-[420px] flex flex-col transition-colors duration-200"
			>
				<h2
					class="text-xl font-semibold text-gray-700 dark:text-gray-200 mb-6 transition-colors duration-200"
				>
					GeoIP Lookup
				</h2>
				<div class="flex flex-col h-full">
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
							Data from{' '}
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
									<p class="text-sm font-medium text-green-700 dark:text-green-300 mb-1">
										<strong>Country:</strong>
										{lookupResult.country_code}
									</p>
									<p class="text-xs text-green-600 dark:text-green-400"></p>
								{:else}
									<p class="text-sm font-medium text-yellow-700 dark:text-yellow-300 mb-1">
										No match
									</p>
								{/if}
							</div>
						{/if}
					</div>

					<div class="mt-auto pt-4">
						<Button size={'large'} on:click={handleGeoIPLookup} disabled={isSubmitting}>
							{#if isSubmitting}
								Looking up...
							{:else}
								Lookup
							{/if}
						</Button>
					</div>
				</div>
			</div>
		</div>
	</div>
</main>
