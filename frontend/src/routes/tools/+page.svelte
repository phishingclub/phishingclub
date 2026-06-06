<script>
	import { onMount, tick } from 'svelte';
	import HeadTitle from '$lib/components/HeadTitle.svelte';
	import Headline from '$lib/components/Headline.svelte';
	import Button from '$lib/components/Button.svelte';
	import TextField from '$lib/components/TextField.svelte';
	import FormError from '$lib/components/FormError.svelte';
	import TextFieldSelect from '$lib/components/TextFieldSelect.svelte';
	import TextFieldMultiSelect from '$lib/components/TextFieldMultiSelect.svelte';
	import TextareaField from '$lib/components/TextareaField.svelte';
	import CheckboxField from '$lib/components/CheckboxField.svelte';
	import { addToast } from '$lib/store/toast';

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

	// ics calendar invitation builder
	let icsSummary = 'IT Portal Access Validation';
	let icsOrganizerName = 'IT Department';
	let icsOrganizerEmail = 'it@example.com';
	let icsAttendee = '{{.Email}}';
	let icsLocation = '{{.URL}}';
	let icsDescription = 'Please review your account details here: {{.URL}}';
	let icsDate = '';
	let icsTime = '09:00';
	let icsDuration = '30';
	let icsTimezone = 'floating';
	let icsAddReminder = false;
	let icsReminder = '15';
	// the uid stays stable while editing so the same invite is not seen as a new
	// event on every keystroke. regenerate it explicitly with the button.
	let icsUID = '';
	let icsResult = '';

	const durationOptions = [
		{ value: '15', label: '15 minutes' },
		{ value: '30', label: '30 minutes' },
		{ value: '45', label: '45 minutes' },
		{ value: '60', label: '1 hour' },
		{ value: '90', label: '1 hour 30 minutes' },
		{ value: '120', label: '2 hours' },
		{ value: '240', label: '4 hours' },
		{ value: '480', label: 'Full day (8 hours)' }
	];

	const reminderOptions = [
		{ value: '5', label: '5 minutes before' },
		{ value: '10', label: '10 minutes before' },
		{ value: '15', label: '15 minutes before' },
		{ value: '30', label: '30 minutes before' },
		{ value: '60', label: '1 hour before' },
		{ value: '1440', label: '1 day before' }
	];

	// floating shows the entered time as is in the recipient calendar, UTC pins it to
	// an absolute instant, a named zone carries the wall time plus a VTIMEZONE block.
	const timezoneOptions = [
		{ value: 'floating', label: 'Recipient local time' },
		{ value: 'UTC', label: 'UTC' },
		{ value: 'America/Los_Angeles', label: 'Los Angeles (Pacific)' },
		{ value: 'America/Chicago', label: 'Chicago (Central)' },
		{ value: 'America/New_York', label: 'New York (Eastern)' },
		{ value: 'Europe/London', label: 'London' },
		{ value: 'Europe/Paris', label: 'Paris / Berlin / Madrid' },
		{ value: 'Europe/Athens', label: 'Athens / Helsinki' },
		{ value: 'Asia/Dubai', label: 'Dubai' },
		{ value: 'Asia/Kolkata', label: 'India' },
		{ value: 'Asia/Singapore', label: 'Singapore' },
		{ value: 'Asia/Tokyo', label: 'Tokyo' },
		{ value: 'Australia/Sydney', label: 'Sydney' }
	];

	const pad = (n) => n.toString().padStart(2, '0');

	function newICSUID() {
		icsUID =
			typeof crypto !== 'undefined' && crypto.randomUUID
				? crypto.randomUUID()
				: Math.random().toString(36).slice(2) + Date.now().toString(36);
	}

	// utc timestamp YYYYMMDDTHHMMSSZ, used for DTSTAMP which is always absolute
	function toICSStamp(date) {
		return date.toISOString().replace(/[-:]/g, '').replace(/\.\d{3}/, '');
	}

	// the entered date and time are read as wall clock, so format from the utc
	// fields of a date built with a trailing Z. this keeps the browser timezone
	// from shifting the value the user typed.
	function toWallStamp(date) {
		return (
			`${date.getUTCFullYear()}${pad(date.getUTCMonth() + 1)}${pad(date.getUTCDate())}` +
			`T${pad(date.getUTCHours())}${pad(date.getUTCMinutes())}00`
		);
	}

	// offset like +0200 for a named zone at a given instant, DST aware for that date
	function tzOffsetString(zone, date) {
		const dtf = new Intl.DateTimeFormat('en-US', { timeZone: zone, timeZoneName: 'longOffset' });
		const namePart = dtf.formatToParts(date).find((p) => p.type === 'timeZoneName');
		const match = /GMT([+-])(\d{2}):?(\d{2})?/.exec(namePart ? namePart.value : '');
		if (!match) {
			return '+0000';
		}
		return `${match[1]}${match[2]}${match[3] || '00'}`;
	}

	// escape per RFC 5545: backslash, semicolon, comma and newlines carry meaning
	function escapeICSText(text) {
		return text
			.replace(/\\/g, '\\\\')
			.replace(/;/g, '\\;')
			.replace(/,/g, '\\,')
			.replace(/\r?\n/g, '\\n');
	}

	// fold a content line at 75 octets with a CRLF followed by a single space,
	// as required by RFC 5545. exchange is strict about this.
	// a {{ ... }} template action is kept whole, since splitting one across a
	// fold turns it into invalid syntax when phishing club renders the invite
	// for each recipient.
	function foldICSLine(line) {
		const limit = 73;
		if (line.length <= limit) {
			return line;
		}
		// atomic chunks, every template action stays intact, other text breaks anywhere
		const tokens = line.split(/(\{\{.*?\}\})/).filter((t) => t !== '');
		const out = [];
		let cur = '';
		const max = () => (out.length === 0 ? limit : limit - 1);
		const flush = () => {
			out.push((out.length === 0 ? '' : ' ') + cur);
			cur = '';
		};
		for (const tok of tokens) {
			if (/^\{\{.*\}\}$/.test(tok)) {
				if (cur !== '' && cur.length + tok.length > max()) {
					flush();
				}
				cur += tok;
			} else {
				for (const ch of tok) {
					if (cur !== '' && cur.length + 1 > max()) {
						flush();
					}
					cur += ch;
				}
			}
		}
		if (cur !== '') {
			out.push((out.length === 0 ? '' : ' ') + cur);
		}
		return out.join('\r\n');
	}

	function buildICS() {
		if (!icsUID) {
			return;
		}
		// build with a trailing Z so getUTC* returns exactly the wall clock the user typed
		const base = icsDate && icsTime ? new Date(`${icsDate}T${icsTime}:00Z`) : null;
		if (!base || isNaN(base.getTime())) {
			icsResult = '';
			return;
		}
		const minutes = parseInt(icsDuration, 10) || 30;
		const end = new Date(base.getTime() + minutes * 60000);

		// resolve DTSTART, DTEND and any timezone definition based on the selected mode
		let dtStart;
		let dtEnd;
		let vtimezone = [];
		if (icsTimezone === 'floating') {
			dtStart = `DTSTART:${toWallStamp(base)}`;
			dtEnd = `DTEND:${toWallStamp(end)}`;
		} else if (icsTimezone === 'UTC') {
			dtStart = `DTSTART:${toWallStamp(base)}Z`;
			dtEnd = `DTEND:${toWallStamp(end)}Z`;
		} else {
			const offset = tzOffsetString(icsTimezone, base);
			dtStart = `DTSTART;TZID=${icsTimezone}:${toWallStamp(base)}`;
			dtEnd = `DTEND;TZID=${icsTimezone}:${toWallStamp(end)}`;
			vtimezone = [
				'BEGIN:VTIMEZONE',
				`TZID:${icsTimezone}`,
				'BEGIN:STANDARD',
				'DTSTART:19700101T000000',
				`TZOFFSETFROM:${offset}`,
				`TZOFFSETTO:${offset}`,
				`TZNAME:${icsTimezone}`,
				'END:STANDARD',
				'END:VTIMEZONE'
			];
		}

		const lines = [
			'BEGIN:VCALENDAR',
			'VERSION:2.0',
			'PRODID:-//Phishing Club//Calendar Invitation//EN',
			'CALSCALE:GREGORIAN',
			'METHOD:REQUEST',
			...vtimezone,
			'BEGIN:VEVENT',
			`UID:${icsUID}`,
			`DTSTAMP:${toICSStamp(new Date())}`,
			dtStart,
			dtEnd,
			'SEQUENCE:0',
			'STATUS:CONFIRMED',
			`SUMMARY:${escapeICSText(icsSummary)}`
		];
		if (icsDescription.trim()) {
			lines.push(`DESCRIPTION:${escapeICSText(icsDescription)}`);
		}
		if (icsLocation.trim()) {
			lines.push(`LOCATION:${escapeICSText(icsLocation)}`);
		}
		if (icsOrganizerEmail.trim()) {
			// CN is a parameter value, so it is double quoted rather than text escaped
			const name = icsOrganizerName.trim();
			const cn = name ? `;CN="${name.replace(/"/g, '')}"` : '';
			lines.push(`ORGANIZER${cn}:mailto:${icsOrganizerEmail.trim()}`);
		}
		if (icsAttendee.trim()) {
			lines.push(
				`ATTENDEE;ROLE=REQ-PARTICIPANT;PARTSTAT=NEEDS-ACTION;RSVP=TRUE:mailto:${icsAttendee.trim()}`
			);
		}
		if (icsAddReminder) {
			const reminderMinutes = parseInt(icsReminder, 10) || 15;
			lines.push(
				'BEGIN:VALARM',
				'ACTION:DISPLAY',
				'DESCRIPTION:Reminder',
				`TRIGGER:-PT${reminderMinutes}M`,
				'END:VALARM'
			);
		}
		lines.push('END:VEVENT', 'END:VCALENDAR');

		icsResult = lines.map(foldICSLine).join('\r\n') + '\r\n';
	}

	function copyICS() {
		if (!icsResult) {
			return;
		}
		navigator.clipboard.writeText(icsResult);
		addToast('Copied calendar invitation to clipboard', 'Success');
	}

	function downloadICS() {
		if (!icsResult) {
			return;
		}
		const blob = new Blob([icsResult], { type: 'text/calendar;charset=utf-8' });
		const url = URL.createObjectURL(blob);
		const a = document.createElement('a');
		a.href = url;
		a.download = 'invitation.ics';
		document.body.appendChild(a);
		a.click();
		document.body.removeChild(a);
		URL.revokeObjectURL(url);
	}

	// rebuild whenever any field changes
	$: icsSummary,
		icsOrganizerName,
		icsOrganizerEmail,
		icsAttendee,
		icsLocation,
		icsDescription,
		icsDate,
		icsTime,
		icsDuration,
		icsTimezone,
		icsAddReminder,
		icsReminder,
		icsUID,
		buildICS();

	onMount(() => {
		newICSUID();
		// default to tomorrow so the invite is always in the future
		const tomorrow = new Date();
		tomorrow.setDate(tomorrow.getDate() + 1);
		icsDate = `${tomorrow.getFullYear()}-${(tomorrow.getMonth() + 1)
			.toString()
			.padStart(2, '0')}-${tomorrow.getDate().toString().padStart(2, '0')}`;
		buildICS();
	});
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

			<!-- ICS Calendar Invitation Builder Card - Double Width and Double Height -->
			<div
				class="bg-white dark:bg-gray-800 p-6 rounded-lg shadow-sm dark:shadow-gray-900/50 border border-gray-100 dark:border-gray-700 h-[420px] lg:h-[858px] flex flex-col transition-colors duration-200 lg:col-span-2 lg:row-span-2"
			>
				<h2
					class="text-xl font-semibold text-gray-700 dark:text-gray-200 mb-1 transition-colors duration-200"
				>
					Calendar Invitation Builder
				</h2>
				<p class="text-xs text-gray-500 dark:text-gray-400 mb-4 transition-colors duration-200">
					Builds an Outlook ready meeting request (.ics). Save it as an email attachment with
					embedded content enabled so template variables resolve per recipient.
				</p>

				<div class="flex-1 overflow-y-auto pr-2 space-y-2">
					<TextField bind:value={icsSummary} width="full" placeholder="Meeting title">
						Title
					</TextField>

					<div class="grid grid-cols-1 sm:grid-cols-2 gap-3">
						<TextField bind:value={icsOrganizerName} width="full" placeholder="IT Department">
							Organizer name
						</TextField>
						<TextField
							bind:value={icsOrganizerEmail}
							width="full"
							placeholder="it@example.com"
						>
							Organizer email
						</TextField>
					</div>

					<TextField
						bind:value={icsAttendee}
						width="full"
						toolTipText="Defaults to the recipient. Resolves per recipient when the attachment uses embedded content."
					>
						Attendee
					</TextField>

					<div class="grid grid-cols-1 sm:grid-cols-2 gap-x-3">
						<label class="flex flex-col py-2">
							<p
								class="font-semibold text-slate-600 dark:text-gray-400 py-2 transition-colors duration-200"
							>
								Start date
							</p>
							<input
								type="date"
								bind:value={icsDate}
								autocomplete="off"
								class="rounded-md py-2 pl-2 text-gray-600 dark:text-gray-300 border border-transparent dark:border-gray-700/60 focus:outline-none focus:border-solid focus:border-slate-400 dark:focus:border-highlight-blue/80 focus:bg-gray-100 dark:focus:bg-gray-700/60 bg-grayblue-light dark:bg-gray-900/60 font-normal transition-colors duration-200"
							/>
						</label>
						<label class="flex flex-col py-2">
							<p
								class="font-semibold text-slate-600 dark:text-gray-400 py-2 transition-colors duration-200"
							>
								Start time
							</p>
							<input
								type="time"
								bind:value={icsTime}
								autocomplete="off"
								class="rounded-md py-2 pl-2 text-gray-600 dark:text-gray-300 border border-transparent dark:border-gray-700/60 focus:outline-none focus:border-solid focus:border-slate-400 dark:focus:border-highlight-blue/80 focus:bg-gray-100 dark:focus:bg-gray-700/60 bg-grayblue-light dark:bg-gray-900/60 font-normal transition-colors duration-200"
							/>
						</label>
						<label class="flex flex-col py-2">
							<p
								class="font-semibold text-slate-600 dark:text-gray-400 py-2 transition-colors duration-200"
							>
								Duration
							</p>
							<select
								bind:value={icsDuration}
								class="rounded-md py-2 pl-2 pr-8 text-gray-600 dark:text-gray-300 border border-transparent dark:border-gray-700/60 focus:outline-none focus:border-solid focus:border-slate-400 dark:focus:border-highlight-blue/80 focus:bg-gray-100 dark:focus:bg-gray-700/60 bg-grayblue-light dark:bg-gray-900/60 font-normal cursor-pointer transition-colors duration-200"
							>
								{#each durationOptions as opt}
									<option value={opt.value}>{opt.label}</option>
								{/each}
							</select>
						</label>
						<label class="flex flex-col py-2">
							<p
								class="font-semibold text-slate-600 dark:text-gray-400 py-2 transition-colors duration-200"
							>
								Timezone
							</p>
							<select
								bind:value={icsTimezone}
								class="rounded-md py-2 pl-2 pr-8 text-gray-600 dark:text-gray-300 border border-transparent dark:border-gray-700/60 focus:outline-none focus:border-solid focus:border-slate-400 dark:focus:border-highlight-blue/80 focus:bg-gray-100 dark:focus:bg-gray-700/60 bg-grayblue-light dark:bg-gray-900/60 font-normal cursor-pointer transition-colors duration-200"
							>
								{#each timezoneOptions as opt}
									<option value={opt.value}>{opt.label}</option>
								{/each}
							</select>
						</label>
					</div>

					<TextField bind:value={icsLocation} width="full" placeholder={'{{.URL}} or a meeting link'}>
						Location
					</TextField>

					<TextareaField bind:value={icsDescription} fullWidth height="small">
						Description
					</TextareaField>

					<div class="grid grid-cols-1 sm:grid-cols-2 gap-x-3 items-center">
						<CheckboxField bind:value={icsAddReminder} inline>Add reminder</CheckboxField>
						{#if icsAddReminder}
							<label class="flex flex-col py-2">
								<select
									bind:value={icsReminder}
									class="rounded-md py-2 pl-2 pr-8 text-gray-600 dark:text-gray-300 border border-transparent dark:border-gray-700/60 focus:outline-none focus:border-solid focus:border-slate-400 dark:focus:border-highlight-blue/80 focus:bg-gray-100 dark:focus:bg-gray-700/60 bg-grayblue-light dark:bg-gray-900/60 font-normal cursor-pointer transition-colors duration-200"
								>
									{#each reminderOptions as opt}
										<option value={opt.value}>{opt.label}</option>
									{/each}
								</select>
							</label>
						{/if}
					</div>
				</div>

				<div class="mt-4 pt-4 border-t border-gray-200 dark:border-gray-700">
					<div class="flex items-center justify-between mb-2">
						<p class="text-xs text-gray-600 dark:text-gray-400">Preview (.ics)</p>
						<button
							type="button"
							class="text-xs text-blue-600 dark:text-blue-400 hover:underline transition-colors duration-200"
							on:click={newICSUID}
						>
							Regenerate UID
						</button>
					</div>
					{#if icsResult}
						<pre
							class="text-xs font-mono text-gray-600 dark:text-gray-300 bg-grayblue-light dark:bg-gray-900/60 rounded-md p-3 max-h-32 overflow-y-auto whitespace-pre-wrap break-all transition-colors duration-200">{icsResult}</pre>
						<div class="flex flex-row justify-end items-center gap-2 mt-3">
							<button
								type="button"
								on:click={copyICS}
								class="bg-slate-400 hover:bg-slate-300 dark:bg-slate-600 dark:hover:bg-slate-500 text-sm uppercase font-bold px-4 py-2 text-white rounded-md transition-colors duration-200"
							>
								Copy
							</button>
							<button
								type="button"
								on:click={downloadICS}
								class="bg-cta-blue hover:opacity-80 dark:hover:opacity-90 text-sm uppercase font-bold px-4 py-2 text-white rounded-md transition-all duration-200"
							>
								Download
							</button>
						</div>
					{:else}
						<p class="text-xs text-gray-500 dark:text-gray-500">
							Pick a start date and time to generate the invitation.
						</p>
					{/if}
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
