<script>
	import { createEventDispatcher, onMount } from 'svelte';
	import TextField from '$lib/components/TextField.svelte';
	import TextFieldSelect from '$lib/components/TextFieldSelect.svelte';
	import TextareaField from '$lib/components/TextareaField.svelte';
	import jsyaml from '$lib/components/yaml/index.js';

	export let config = null;
	// basic info fields passed from parent
	export let name = '';
	export let description = '';
	export let startURL = '';

	const dispatch = createEventDispatcher();

	// track if initial parse has happened
	let initialized = false;
	// track if we should skip the next reactive dispatch (used during mount)
	let skipNextDispatch = false;

	// default empty config structure
	let configData = {
		version: '0.0',
		proxy: '',
		global: {
			tls: { mode: 'managed' },
			access: { mode: 'private', on_deny: '' },
			impersonate: { enabled: false, retain_ua: false },
			variables: { enabled: false, allowed: [] },
			capture: [],
			rewrite: [],
			response: [],
			rewrite_urls: []
		},
		hosts: []
	};

	// valid proxy template variables that can be used in rewrite rules
	const validProxyVariables = [
		// recipient fields
		'rID',
		'FirstName',
		'LastName',
		'Email',
		'To',
		'Phone',
		'ExtraIdentifier',
		'Position',
		'Department',
		'City',
		'Country',
		'Misc',
		// sender fields
		'From',
		'FromName',
		'FromEmail',
		'Subject',
		// general fields
		'BaseURL',
		'URL',
		// custom fields
		'CustomField1',
		'CustomField2',
		'CustomField3',
		'CustomField4'
	];

	// active tab for main sections
	let activeTab = 'basic';

	// expanded host index (-1 = none)
	let expandedHostIndex = -1;

	// active sub-tab per host - use reactive $: to track changes
	let hostActiveTabs = {};
	// reactive variable to force re-render when host tabs change
	$: currentHostTab = hostActiveTabs[expandedHostIndex] || 'settings';

	// unique id counter for form elements and rule tracking
	let idCounter = 0;
	const getUniqueId = (prefix) => `${prefix}-${idCounter++}`;
	const getRuleId = () => `rule-${idCounter++}`;

	// parse config only on initial mount
	onMount(() => {
		// skip the reactive dispatch that will fire when initialized becomes true
		skipNextDispatch = true;
		if (config) {
			parseYamlConfig(config);
		}
		initialized = true;
	});

	// reactively dispatch change event whenever configData changes
	$: if (initialized && configData) {
		if (skipNextDispatch) {
			skipNextDispatch = false;
		} else {
			const yaml = generateYaml();
			dispatch('change', yaml);
		}
	}

	// expose method to get current YAML (can be called by parent if needed)
	export function getYaml() {
		return generateYaml();
	}

	// expose import/export methods for parent to call
	export function triggerImport() {
		fileInput?.click();
	}

	export { exportConfig };

	// expose method to validate and navigate to first error
	// returns { valid: boolean, errors: object }
	export function validate() {
		validationErrors = validateConfig();
		const errorKeys = Object.keys(validationErrors);

		if (errorKeys.length === 0) {
			return { valid: true, errors: {} };
		}

		// navigate to first error
		const firstErrorKey = errorKeys[0];
		navigateToError(firstErrorKey);

		return { valid: false, errors: validationErrors };
	}

	// navigate to the tab/host/sub-tab containing the error
	function navigateToError(errorKey) {
		const parts = errorKey.split('.');

		if (parts[0] === 'global') {
			// global error - switch to global tab
			activeTab = 'global';

			// determine which sub-tab
			if (parts[1] === 'capture') {
				globalRulesTab = 'capture';
			} else if (parts[1] === 'rewrite') {
				globalRulesTab = 'rewrite';
			} else if (parts[1] === 'response') {
				globalRulesTab = 'response';
			} else if (parts[1] === 'rewrite_urls') {
				globalRulesTab = 'urlrewrite';
			}
		} else if (parts[0] === 'hosts') {
			// host error - switch to hosts tab and expand the host
			activeTab = 'hosts';
			const hostIndex = parseInt(parts[1], 10);
			expandedHostIndex = hostIndex;

			// determine which sub-tab
			if (parts[2] === 'to' || parts[2] === 'domain') {
				setHostActiveTab(hostIndex, 'settings');
			} else if (parts[2] === 'capture') {
				setHostActiveTab(hostIndex, 'capture');
			} else if (parts[2] === 'rewrite') {
				setHostActiveTab(hostIndex, 'rewrite');
			} else if (parts[2] === 'response') {
				setHostActiveTab(hostIndex, 'response');
			} else if (parts[2] === 'rewrite_urls') {
				setHostActiveTab(hostIndex, 'urlrewrite');
			}
		}
	}

	// parse YAML config using js-yaml library
	function parseYamlConfig(yamlStr) {
		if (!yamlStr || yamlStr.trim() === '') {
			resetConfig();
			return;
		}

		try {
			const parsed = jsyaml.load(yamlStr);
			if (parsed && typeof parsed === 'object') {
				configData.version = String(parsed.version || '0.0');
				configData.proxy = parsed.proxy || '';

				if (parsed.global) {
					configData.global.tls = parsed.global.tls || { mode: 'managed' };
					configData.global.access = parsed.global.access || { mode: 'private', on_deny: '' };
					configData.global.impersonate = parsed.global.impersonate || {
						enabled: false,
						retain_ua: false
					};
					configData.global.variables = parsed.global.variables || {
						enabled: false,
						allowed: []
					};
					configData.global.capture = (parsed.global.capture || []).map((r) => ({
						...r,
						_id: getRuleId()
					}));
					configData.global.rewrite = (parsed.global.rewrite || []).map((r) => ({
						...r,
						_id: getRuleId()
					}));
					configData.global.response = (parsed.global.response || []).map((r) => ({
						...r,
						_id: getRuleId()
					}));
					configData.global.rewrite_urls = (parsed.global.rewrite_urls || []).map((r) => ({
						...r,
						_id: getRuleId()
					}));
				}

				// extract hosts (keys that contain '.' and have a 'to' property)
				const hosts = [];
				for (const key of Object.keys(parsed)) {
					if (
						key !== 'version' &&
						key !== 'proxy' &&
						key !== 'global' &&
						parsed[key] &&
						typeof parsed[key] === 'object' &&
						parsed[key].to
					) {
						const hostData = parsed[key];
						hosts.push({
							domain: key,
							to: hostData.to || '',
							scheme: hostData.scheme || 'https',
							tls: { mode: hostData.tls?.mode || '' },
							access: {
								mode: hostData.access?.mode || '',
								on_deny: hostData.access?.on_deny || ''
							},
							capture: (hostData.capture || []).map((r) => ({ ...r, _id: getRuleId() })),
							rewrite: (hostData.rewrite || []).map((r) => ({ ...r, _id: getRuleId() })),
							response: (hostData.response || []).map((r) => ({ ...r, _id: getRuleId() })),
							rewrite_urls: (hostData.rewrite_urls || []).map((r) => ({ ...r, _id: getRuleId() }))
						});
					}
				}
				configData.hosts = hosts;

				// expand first host if exists
				if (hosts.length > 0 && expandedHostIndex === -1) {
					expandedHostIndex = 0;
				}
			}
		} catch (e) {
			console.warn('Failed to parse YAML config:', e);
		}
	}

	function resetConfig() {
		configData = {
			version: '0.0',
			proxy: '',
			global: {
				tls: { mode: 'managed' },
				access: { mode: 'private', on_deny: '' },
				impersonate: { enabled: false, retain_ua: false },
				variables: { enabled: false, allowed: [] },
				capture: [],
				rewrite: [],
				response: [],
				rewrite_urls: []
			},
			hosts: []
		};
		expandedHostIndex = -1;
	}

	// helper to remove internal _id fields before serialization
	function stripIds(obj) {
		if (Array.isArray(obj)) {
			return obj.map(stripIds);
		}
		if (obj && typeof obj === 'object') {
			const result = {};
			for (const [key, value] of Object.entries(obj)) {
				if (key !== '_id') {
					result[key] = stripIds(value);
				}
			}
			return result;
		}
		return obj;
	}

	// helper to remove empty values from objects for cleaner YAML output
	function cleanObject(obj) {
		if (Array.isArray(obj)) {
			return obj.map(cleanObject);
		}
		if (obj && typeof obj === 'object') {
			const result = {};
			for (const [key, value] of Object.entries(obj)) {
				const cleaned = cleanObject(value);
				// skip empty strings, empty arrays, empty objects, null, undefined
				if (
					cleaned !== '' &&
					cleaned !== null &&
					cleaned !== undefined &&
					!(Array.isArray(cleaned) && cleaned.length === 0) &&
					!(
						typeof cleaned === 'object' &&
						!Array.isArray(cleaned) &&
						Object.keys(cleaned).length === 0
					)
				) {
					result[key] = cleaned;
				}
			}
			return result;
		}
		return obj;
	}

	// generate YAML using js-yaml library
	function generateYaml() {
		// build the config object
		const output = {};

		output.version = configData.version || '0.0';

		if (configData.proxy) {
			output.proxy = configData.proxy;
		}

		// build global section
		const global = {};
		if (configData.global.tls?.mode) {
			global.tls = { mode: configData.global.tls.mode };
		}
		if (configData.global.access?.mode) {
			global.access = { mode: configData.global.access.mode };
			if (configData.global.access.on_deny) {
				global.access.on_deny = configData.global.access.on_deny;
			}
		}
		if (configData.global.impersonate?.enabled) {
			global.impersonate = {
				enabled: configData.global.impersonate.enabled
			};
			if (configData.global.impersonate.retain_ua) {
				global.impersonate.retain_ua = configData.global.impersonate.retain_ua;
			}
		}
		if (configData.global.variables?.enabled) {
			global.variables = {
				enabled: configData.global.variables.enabled
			};
			if (configData.global.variables.allowed?.length > 0) {
				global.variables.allowed = configData.global.variables.allowed;
			}
		}
		// filter and add global rules (only include touched/valid rules)
		const globalCapture = (configData.global.capture || []).filter(isCaptureRuleTouched);
		if (globalCapture.length > 0) {
			global.capture = stripIds(globalCapture);
		}
		const globalRewrite = (configData.global.rewrite || []).filter(isRewriteRuleTouched);
		if (globalRewrite.length > 0) {
			global.rewrite = stripIds(globalRewrite);
		}
		const globalResponse = (configData.global.response || []).filter(isResponseRuleTouched);
		if (globalResponse.length > 0) {
			global.response = stripIds(globalResponse);
		}
		const globalRewriteUrls = (configData.global.rewrite_urls || []).filter(
			isUrlRewriteRuleTouched
		);
		if (globalRewriteUrls.length > 0) {
			global.rewrite_urls = stripIds(globalRewriteUrls);
		}

		if (Object.keys(global).length > 0) {
			output.global = global;
		}

		// add hosts
		for (const host of configData.hosts) {
			if (!host.domain || !host.to) continue;

			const hostObj = { to: host.to };

			if (host.scheme && host.scheme !== 'https') {
				hostObj.scheme = host.scheme;
			}
			if (host.tls?.mode) {
				hostObj.tls = { mode: host.tls.mode };
			}
			if (host.access?.mode) {
				hostObj.access = { mode: host.access.mode };
				if (host.access.on_deny) {
					hostObj.access.on_deny = host.access.on_deny;
				}
			}
			// filter and add host rules (only include touched/valid rules)
			const hostCapture = (host.capture || []).filter(isCaptureRuleTouched);
			if (hostCapture.length > 0) {
				hostObj.capture = stripIds(hostCapture);
			}
			const hostRewrite = (host.rewrite || []).filter(isRewriteRuleTouched);
			if (hostRewrite.length > 0) {
				hostObj.rewrite = stripIds(hostRewrite);
			}
			const hostResponse = (host.response || []).filter(isResponseRuleTouched);
			if (hostResponse.length > 0) {
				hostObj.response = stripIds(hostResponse);
			}
			const hostRewriteUrls = (host.rewrite_urls || []).filter(isUrlRewriteRuleTouched);
			if (hostRewriteUrls.length > 0) {
				hostObj.rewrite_urls = stripIds(hostRewriteUrls);
			}

			output[host.domain] = cleanObject(hostObj);
		}

		// serialize to YAML with js-yaml
		return jsyaml.dump(output, {
			indent: 2,
			lineWidth: -1, // don't wrap long lines
			quotingType: "'", // prefer single quotes
			forceQuotes: false, // only quote when necessary
			noRefs: true // don't use YAML references
		});
	}

	// host management
	function addHost() {
		configData.hosts = [
			...configData.hosts,
			{
				domain: '',
				to: '',
				scheme: 'https',
				tls: { mode: '' },
				access: { mode: '', on_deny: '' },
				capture: [],
				rewrite: [],
				response: [],
				rewrite_urls: []
			}
		];
		expandedHostIndex = configData.hosts.length - 1;
		hostActiveTabs[expandedHostIndex] = 'settings';
	}

	function removeHost(index) {
		configData.hosts = configData.hosts.filter((_, i) => i !== index);
		if (expandedHostIndex >= configData.hosts.length) {
			expandedHostIndex = configData.hosts.length - 1;
		}
		if (expandedHostIndex < 0 && configData.hosts.length > 0) {
			expandedHostIndex = 0;
		}
	}

	function duplicateHost(index) {
		const host = configData.hosts[index];
		const newHost = JSON.parse(JSON.stringify(host));
		// assign new IDs to all rules in the duplicated host
		if (newHost.capture) newHost.capture = newHost.capture.map((r) => ({ ...r, _id: getRuleId() }));
		if (newHost.rewrite) newHost.rewrite = newHost.rewrite.map((r) => ({ ...r, _id: getRuleId() }));
		if (newHost.response)
			newHost.response = newHost.response.map((r) => ({ ...r, _id: getRuleId() }));
		if (newHost.rewrite_urls)
			newHost.rewrite_urls = newHost.rewrite_urls.map((r) => ({ ...r, _id: getRuleId() }));
		configData.hosts = [...configData.hosts, newHost];
		expandedHostIndex = configData.hosts.length - 1;
	}

	// global rule management
	function addGlobalCaptureRule() {
		configData.global.capture = [
			...configData.global.capture,
			{
				_id: getRuleId(),
				name: '',
				method: 'POST',
				path: '',
				find: '',
				engine: 'regex',
				from: 'request_body',
				required: false
			}
		];
	}

	function removeGlobalCaptureRule(index) {
		configData.global.capture = configData.global.capture.filter((_, i) => i !== index);
	}

	function addGlobalRewriteRule() {
		configData.global.rewrite = [
			...configData.global.rewrite,
			{ _id: getRuleId(), name: '', engine: 'regex', find: '', replace: '', from: 'response_body' }
		];
	}

	function removeGlobalRewriteRule(index) {
		configData.global.rewrite = configData.global.rewrite.filter((_, i) => i !== index);
	}

	function addGlobalResponseRule() {
		configData.global.response = [
			...configData.global.response,
			{ _id: getRuleId(), path: '', status: 200, headers: {}, body: '', forward: false }
		];
	}

	function removeGlobalResponseRule(index) {
		configData.global.response = configData.global.response.filter((_, i) => i !== index);
	}

	function addGlobalRewriteUrlRule() {
		configData.global.rewrite_urls = [
			...configData.global.rewrite_urls,
			{ _id: getRuleId(), find: '', replace: '', query: '', filter: '' }
		];
	}

	function removeGlobalRewriteUrlRule(index) {
		configData.global.rewrite_urls = configData.global.rewrite_urls.filter((_, i) => i !== index);
	}

	// host rule management
	function addHostCaptureRule(hostIndex) {
		configData.hosts[hostIndex].capture = [
			...(configData.hosts[hostIndex].capture || []),
			{
				_id: getRuleId(),
				name: '',
				method: 'POST',
				path: '',
				find: '',
				engine: 'regex',
				from: 'request_body',
				required: false
			}
		];
		configData.hosts = [...configData.hosts];
	}

	function removeHostCaptureRule(hostIndex, ruleIndex) {
		configData.hosts[hostIndex].capture = configData.hosts[hostIndex].capture.filter(
			(_, i) => i !== ruleIndex
		);
		configData.hosts = [...configData.hosts];
	}

	function addHostRewriteRule(hostIndex) {
		configData.hosts[hostIndex].rewrite = [
			...(configData.hosts[hostIndex].rewrite || []),
			{ _id: getRuleId(), name: '', engine: 'regex', find: '', replace: '', from: 'response_body' }
		];
		configData.hosts = [...configData.hosts];
	}

	function removeHostRewriteRule(hostIndex, ruleIndex) {
		configData.hosts[hostIndex].rewrite = configData.hosts[hostIndex].rewrite.filter(
			(_, i) => i !== ruleIndex
		);
		configData.hosts = [...configData.hosts];
	}

	function addHostResponseRule(hostIndex) {
		configData.hosts[hostIndex].response = [
			...(configData.hosts[hostIndex].response || []),
			{ _id: getRuleId(), path: '', status: 200, headers: {}, body: '', forward: false }
		];
		configData.hosts = [...configData.hosts];
	}

	function removeHostResponseRule(hostIndex, ruleIndex) {
		configData.hosts[hostIndex].response = configData.hosts[hostIndex].response.filter(
			(_, i) => i !== ruleIndex
		);
		configData.hosts = [...configData.hosts];
	}

	function addHostRewriteUrlRule(hostIndex) {
		configData.hosts[hostIndex].rewrite_urls = [
			...(configData.hosts[hostIndex].rewrite_urls || []),
			{ _id: getRuleId(), find: '', replace: '', query: '', filter: '' }
		];
		configData.hosts = [...configData.hosts];
	}

	function removeHostRewriteUrlRule(hostIndex, ruleIndex) {
		configData.hosts[hostIndex].rewrite_urls = configData.hosts[hostIndex].rewrite_urls.filter(
			(_, i) => i !== ruleIndex
		);
		configData.hosts = [...configData.hosts];
	}

	// response headers management
	function addResponseHeader(rule) {
		if (!rule.headers) rule.headers = {};
		const newKey = `Header-${Object.keys(rule.headers).length + 1}`;
		rule.headers[newKey] = '';
		configData = configData;
	}

	function removeResponseHeader(rule, key) {
		delete rule.headers[key];
		configData = configData;
	}

	function updateResponseHeaderKey(rule, oldKey, newKey) {
		if (oldKey !== newKey && newKey) {
			const value = rule.headers[oldKey];
			delete rule.headers[oldKey];
			rule.headers[newKey] = value;
			configData = configData;
		}
	}

	// options
	const tlsModes = [
		{ value: 'managed', label: 'Managed' },
		{ value: 'self-signed', label: 'Self-signed' }
	];
	const tlsModesWithEmpty = [
		{ value: '', label: '(Use global default)' },
		{ value: 'managed', label: 'Managed' },
		{ value: 'self-signed', label: 'Self-signed' }
	];
	const accessModes = [
		{ value: 'public', label: 'Public' },
		{ value: 'private', label: 'Private' }
	];
	const accessModesWithEmpty = [
		{ value: '', label: '(Use global default)' },
		{ value: 'public', label: 'Public' },
		{ value: 'private', label: 'Private' }
	];
	const schemes = [
		{ value: 'https', label: 'HTTPS' },
		{ value: 'http', label: 'HTTP' }
	];
	const methods = ['GET', 'POST', 'PUT', 'DELETE', 'PATCH'];
	const fromOptions = [
		{ value: 'request_body', label: 'Request Body' },
		{ value: 'request_header', label: 'Request Header' },
		{ value: 'response_body', label: 'Response Body' },
		{ value: 'response_header', label: 'Response Header' },
		{ value: 'any', label: 'Any' }
	];
	const headerFromOptions = [
		{ value: 'request_header', label: 'Request Header' },
		{ value: 'response_header', label: 'Response Header' }
	];

	// get from options based on engine - cookie ignores from, header only uses header options
	function getFromOptionsForEngine(engine) {
		if (engine === 'cookie') return [];
		if (engine === 'header') return headerFromOptions;
		return fromOptions;
	}

	// get default 'from' value for a given engine
	function getDefaultFromForEngine(engine) {
		if (engine === 'cookie') return '';
		if (engine === 'header') return 'request_header';
		return 'request_body';
	}

	// handle engine change - always reset 'from' to default for consistency
	function handleCaptureEngineChange(rule, newEngine) {
		rule.engine = newEngine;
		rule.from = getDefaultFromForEngine(newEngine);
		configData = configData;
	}

	const captureEngines = [
		{ value: 'regex', label: 'Regex' },
		{ value: 'header', label: 'Header' },
		{ value: 'cookie', label: 'Cookie' },
		{ value: 'json', label: 'JSON' },
		{ value: 'form', label: 'Form' },
		{ value: 'urlencoded', label: 'URL Encoded' },
		{ value: 'formdata', label: 'Form Data' },
		{ value: 'multipart', label: 'Multipart' }
	];
	const engines = [
		{ value: 'regex', label: 'Regex' },
		{ value: 'dom', label: 'DOM' }
	];
	const domActions = [
		{ value: 'setText', label: 'Set Text' },
		{ value: 'setHtml', label: 'Set HTML' },
		{ value: 'setAttr', label: 'Set Attribute' },
		{ value: 'removeAttr', label: 'Remove Attribute' },
		{ value: 'addClass', label: 'Add Class' },
		{ value: 'removeClass', label: 'Remove Class' },
		{ value: 'remove', label: 'Remove' }
	];
	const targets = [
		{ value: 'first', label: 'First' },
		{ value: 'last', label: 'Last' },
		{ value: 'all', label: 'All' }
	];

	// validation errors
	let validationErrors = {};

	// helper to get find value as string (find can be string or array)
	function getFindAsString(find) {
		if (!find) return '';
		if (typeof find === 'string') return find;
		if (Array.isArray(find) && find.length > 0) return String(find[0]);
		return '';
	}

	// helper to check if find has a value (find can be string or array)
	function hasFindValue(find) {
		if (!find) return false;
		if (typeof find === 'string') return !!find.trim();
		if (Array.isArray(find)) return find.length > 0 && find.some((f) => f && String(f).trim());
		return false;
	}

	// helper to check if a rule has been touched (user started filling it in)
	function isCaptureRuleTouched(rule) {
		return !!(rule.name?.trim() || rule.path?.trim() || hasFindValue(rule.find));
	}

	function isRewriteRuleTouched(rule) {
		return !!(rule.find?.trim() || rule.replace?.trim());
	}

	function isResponseRuleTouched(rule) {
		return !!(rule.path?.trim() || rule.body?.trim() || rule.status);
	}

	function isUrlRewriteRuleTouched(rule) {
		return !!(rule.find?.trim() || rule.replace?.trim());
	}

	function isHostTouched(host) {
		return !!(host.to?.trim() || host.domain?.trim());
	}

	// validate all rules and return errors object
	function validateConfig() {
		const errors = {};

		// validate capture rules (global) - only if touched
		configData.global.capture.forEach((rule, i) => {
			if (!isCaptureRuleTouched(rule)) return;
			const prefix = `global.capture.${i}`;
			if (!rule.name?.trim()) {
				errors[`${prefix}.name`] = 'Name is required';
			}
			if (!rule.path?.trim()) {
				errors[`${prefix}.path`] = 'Path is required';
			}
			// find is required except for path-based navigation tracking (path has value, find empty)
			if (!hasFindValue(rule.find) && !rule.path?.trim()) {
				errors[`${prefix}.find`] = 'Find pattern is required';
			}
		});

		// validate rewrite rules (global) - only if touched
		configData.global.rewrite.forEach((rule, i) => {
			if (!isRewriteRuleTouched(rule)) return;
			const prefix = `global.rewrite.${i}`;
			if (!rule.find?.trim()) {
				errors[`${prefix}.find`] = 'Find is required';
			}
			if (rule.engine === 'dom') {
				if (!rule.action) {
					errors[`${prefix}.action`] = 'Action is required for DOM engine';
				}
				// replace is required for most actions except 'remove'
				if (rule.action && rule.action !== 'remove' && !rule.replace?.trim()) {
					errors[`${prefix}.replace`] = `Replace is required for ${rule.action}`;
				}
			}
		});

		// validate response rules (global) - only if touched
		configData.global.response.forEach((rule, i) => {
			if (!isResponseRuleTouched(rule)) return;
			const prefix = `global.response.${i}`;
			if (!rule.path?.trim()) {
				errors[`${prefix}.path`] = 'Path is required';
			}
		});

		// validate URL rewrite rules (global) - only if touched
		configData.global.rewrite_urls.forEach((rule, i) => {
			if (!isUrlRewriteRuleTouched(rule)) return;
			const prefix = `global.rewrite_urls.${i}`;
			if (!rule.find?.trim()) {
				errors[`${prefix}.find`] = 'Find pattern is required';
			}
			if (!rule.replace?.trim()) {
				errors[`${prefix}.replace`] = 'Replace path is required';
			}
		});

		// validate hosts - only if touched
		configData.hosts.forEach((host, hostIndex) => {
			if (!isHostTouched(host)) return;
			const hostPrefix = `hosts.${hostIndex}`;
			if (!host.to?.trim()) {
				errors[`${hostPrefix}.to`] = 'Phishing domain is required';
			}
			if (!host.domain?.trim()) {
				errors[`${hostPrefix}.domain`] = 'Target domain is required';
			}

			// validate capture rules (host) - only if touched
			(host.capture || []).forEach((rule, i) => {
				if (!isCaptureRuleTouched(rule)) return;
				const prefix = `${hostPrefix}.capture.${i}`;
				if (!rule.name?.trim()) {
					errors[`${prefix}.name`] = 'Name is required';
				}
				if (!rule.path?.trim()) {
					errors[`${prefix}.path`] = 'Path is required';
				}
				// find is required except for path-based navigation tracking (path has value, find empty)
				if (!hasFindValue(rule.find) && !rule.path?.trim()) {
					errors[`${prefix}.find`] = 'Find pattern is required';
				}
			});

			// validate rewrite rules (host) - only if touched
			(host.rewrite || []).forEach((rule, i) => {
				if (!isRewriteRuleTouched(rule)) return;
				const prefix = `${hostPrefix}.rewrite.${i}`;
				if (!rule.find?.trim()) {
					errors[`${prefix}.find`] = 'Find is required';
				}
				if (rule.engine === 'dom') {
					if (!rule.action) {
						errors[`${prefix}.action`] = 'Action is required for DOM engine';
					}
					if (rule.action && rule.action !== 'remove' && !rule.replace?.trim()) {
						errors[`${prefix}.replace`] = `Replace is required for ${rule.action}`;
					}
				}
			});

			// validate response rules (host) - only if touched
			(host.response || []).forEach((rule, i) => {
				if (!isResponseRuleTouched(rule)) return;
				const prefix = `${hostPrefix}.response.${i}`;
				if (!rule.path?.trim()) {
					errors[`${prefix}.path`] = 'Path is required';
				}
			});

			// validate URL rewrite rules (host) - only if touched
			(host.rewrite_urls || []).forEach((rule, i) => {
				if (!isUrlRewriteRuleTouched(rule)) return;
				const prefix = `${hostPrefix}.rewrite_urls.${i}`;
				if (!rule.find?.trim()) {
					errors[`${prefix}.find`] = 'Find pattern is required';
				}
				if (!rule.replace?.trim()) {
					errors[`${prefix}.replace`] = 'Replace path is required';
				}
			});
		});

		return errors;
	}

	// reactively validate when configData changes
	$: if (initialized && configData) {
		validationErrors = validateConfig();
	}

	// helper to check if a field has an error
	function hasError(path) {
		return !!validationErrors[path];
	}

	// helper to get error message
	function getError(path) {
		return validationErrors[path] || '';
	}

	// global rules active tab
	let globalRulesTab = 'capture';

	// helper for host tab - now uses reactive currentHostTab
	function setHostActiveTab(hostIndex, tab) {
		hostActiveTabs = { ...hostActiveTabs, [hostIndex]: tab };
	}

	// handle input changes - dispatch to parent on every keystroke
	function handleNameInput(e) {
		const value = e.target.value;
		dispatch('nameChange', value);
	}

	function handleDescriptionInput(e) {
		const value = e.target.value;
		dispatch('descriptionChange', value);
	}

	function handleStartURLInput(e) {
		const value = e.target.value;
		dispatch('startURLChange', value);
	}

	// file input reference for import
	let fileInput = null;

	// export configuration to YAML file with metadata
	function exportConfig() {
		// build the config with _meta section
		const output = {};

		// add general section with proxy metadata
		output._general = {};
		if (name) {
			output._general.name = name;
		}
		if (description) {
			output._general.description = description;
		}
		if (startURL) {
			output._general.start_url = startURL;
		}

		// add version
		output.version = configData.version || '0.0';

		// add proxy if set
		if (configData.proxy) {
			output.proxy = configData.proxy;
		}

		// build global section
		const global = {};
		if (configData.global.tls?.mode) {
			global.tls = { mode: configData.global.tls.mode };
		}
		if (configData.global.access?.mode) {
			global.access = { mode: configData.global.access.mode };
			if (configData.global.access.on_deny) {
				global.access.on_deny = configData.global.access.on_deny;
			}
		}
		if (configData.global.impersonate?.enabled) {
			global.impersonate = {
				enabled: configData.global.impersonate.enabled
			};
			if (configData.global.impersonate.retain_ua) {
				global.impersonate.retain_ua = configData.global.impersonate.retain_ua;
			}
		}
		if (configData.global.variables?.enabled) {
			global.variables = {
				enabled: configData.global.variables.enabled
			};
			if (configData.global.variables.allowed?.length > 0) {
				global.variables.allowed = configData.global.variables.allowed;
			}
		}
		// filter and add global rules (only include touched/valid rules)
		const globalCapture = (configData.global.capture || []).filter(isCaptureRuleTouched);
		if (globalCapture.length > 0) {
			global.capture = stripIds(globalCapture);
		}
		const globalRewrite = (configData.global.rewrite || []).filter(isRewriteRuleTouched);
		if (globalRewrite.length > 0) {
			global.rewrite = stripIds(globalRewrite);
		}
		const globalResponse = (configData.global.response || []).filter(isResponseRuleTouched);
		if (globalResponse.length > 0) {
			global.response = stripIds(globalResponse);
		}
		const globalRewriteUrls = (configData.global.rewrite_urls || []).filter(
			isUrlRewriteRuleTouched
		);
		if (globalRewriteUrls.length > 0) {
			global.rewrite_urls = stripIds(globalRewriteUrls);
		}

		if (Object.keys(global).length > 0) {
			output.global = global;
		}

		// add hosts
		for (const host of configData.hosts) {
			if (!host.domain || !host.to) continue;

			const hostObj = { to: host.to };

			if (host.scheme && host.scheme !== 'https') {
				hostObj.scheme = host.scheme;
			}
			if (host.tls?.mode) {
				hostObj.tls = { mode: host.tls.mode };
			}
			if (host.access?.mode) {
				hostObj.access = { mode: host.access.mode };
				if (host.access.on_deny) {
					hostObj.access.on_deny = host.access.on_deny;
				}
			}
			// filter and add host rules (only include touched/valid rules)
			const hostCapture = (host.capture || []).filter(isCaptureRuleTouched);
			if (hostCapture.length > 0) {
				hostObj.capture = stripIds(hostCapture);
			}
			const hostRewrite = (host.rewrite || []).filter(isRewriteRuleTouched);
			if (hostRewrite.length > 0) {
				hostObj.rewrite = stripIds(hostRewrite);
			}
			const hostResponse = (host.response || []).filter(isResponseRuleTouched);
			if (hostResponse.length > 0) {
				hostObj.response = stripIds(hostResponse);
			}
			const hostRewriteUrls = (host.rewrite_urls || []).filter(isUrlRewriteRuleTouched);
			if (hostRewriteUrls.length > 0) {
				hostObj.rewrite_urls = stripIds(hostRewriteUrls);
			}

			output[host.domain] = cleanObject(hostObj);
		}

		// serialize to YAML
		const yamlContent = jsyaml.dump(output, {
			indent: 2,
			lineWidth: -1,
			quotingType: "'",
			forceQuotes: false,
			noRefs: true
		});

		// create blob and download
		const blob = new Blob([yamlContent], { type: 'application/x-yaml' });
		const url = URL.createObjectURL(blob);
		const a = document.createElement('a');
		a.href = url;
		// sanitize filename
		const safeName = (name || 'proxy-config').replace(/[^a-zA-Z0-9-_]/g, '_');
		a.download = `${safeName}.yaml`;
		document.body.appendChild(a);
		a.click();
		document.body.removeChild(a);
		URL.revokeObjectURL(url);
	}

	// trigger file input for import

	// handle file selection for import
	function handleImportFile(event) {
		const file = event.target.files?.[0];
		if (!file) return;

		const reader = new FileReader();
		reader.onload = (e) => {
			const content = e.target?.result;
			if (typeof content === 'string') {
				importConfig(content);
			}
		};
		reader.readAsText(file);

		// reset file input so same file can be imported again
		event.target.value = '';
	}

	// import configuration from YAML string with metadata
	function importConfig(yamlStr) {
		if (!yamlStr || yamlStr.trim() === '') {
			return;
		}

		try {
			const parsed = jsyaml.load(yamlStr);
			if (!parsed || typeof parsed !== 'object') {
				console.warn('Invalid YAML: not an object');
				return;
			}

			// extract and apply general section
			if (parsed._general) {
				if (parsed._general.name) {
					dispatch('nameChange', parsed._general.name);
				}
				if (parsed._general.description) {
					dispatch('descriptionChange', parsed._general.description);
				}
				if (parsed._general.start_url) {
					dispatch('startURLChange', parsed._general.start_url);
				}
			}

			// parse the rest as normal config
			configData.version = String(parsed.version || '0.0');
			configData.proxy = parsed.proxy || '';

			if (parsed.global) {
				configData.global.tls = parsed.global.tls || { mode: 'managed' };
				configData.global.access = parsed.global.access || { mode: 'private', on_deny: '' };
				configData.global.impersonate = parsed.global.impersonate || {
					enabled: false,
					retain_ua: false
				};
				configData.global.variables = parsed.global.variables || {
					enabled: false,
					allowed: []
				};
				configData.global.capture = (parsed.global.capture || []).map((r) => ({
					...r,
					_id: getRuleId()
				}));
				configData.global.rewrite = (parsed.global.rewrite || []).map((r) => ({
					...r,
					_id: getRuleId()
				}));
				configData.global.response = (parsed.global.response || []).map((r) => ({
					...r,
					_id: getRuleId()
				}));
				configData.global.rewrite_urls = (parsed.global.rewrite_urls || []).map((r) => ({
					...r,
					_id: getRuleId()
				}));
			} else {
				// reset global if not present
				configData.global = {
					tls: { mode: 'managed' },
					access: { mode: 'private', on_deny: '' },
					impersonate: { enabled: false, retain_ua: false },
					variables: { enabled: false, allowed: [] },
					capture: [],
					rewrite: [],
					response: [],
					rewrite_urls: []
				};
			}

			// extract hosts (keys that are not reserved and have a 'to' property)
			const hosts = [];
			for (const key of Object.keys(parsed)) {
				if (
					key !== '_general' &&
					key !== 'version' &&
					key !== 'proxy' &&
					key !== 'global' &&
					parsed[key] &&
					typeof parsed[key] === 'object' &&
					parsed[key].to
				) {
					const hostData = parsed[key];
					hosts.push({
						domain: key,
						to: hostData.to || '',
						scheme: hostData.scheme || 'https',
						tls: { mode: hostData.tls?.mode || '' },
						access: {
							mode: hostData.access?.mode || '',
							on_deny: hostData.access?.on_deny || ''
						},
						capture: (hostData.capture || []).map((r) => ({ ...r, _id: getRuleId() })),
						rewrite: (hostData.rewrite || []).map((r) => ({ ...r, _id: getRuleId() })),
						response: (hostData.response || []).map((r) => ({ ...r, _id: getRuleId() })),
						rewrite_urls: (hostData.rewrite_urls || []).map((r) => ({ ...r, _id: getRuleId() }))
					});
				}
			}
			configData.hosts = hosts;

			// expand first host if exists
			if (hosts.length > 0) {
				expandedHostIndex = 0;
			}

			// trigger reactive update
			configData = configData;
		} catch (e) {
			console.warn('Failed to parse imported YAML config:', e);
		}
	}
</script>

<div class="proxy-builder-wrapper">
	<div class="proxy-builder">
		<!-- main tabs -->
		<div class="main-tabs">
			<button
				type="button"
				class="main-tab"
				class:active={activeTab === 'basic'}
				on:click={() => (activeTab = 'basic')}
			>
				<svg
					class="tab-icon"
					viewBox="0 0 24 24"
					fill="none"
					stroke="currentColor"
					stroke-width="2"
				>
					<path
						d="M9 12h6m-6 4h6m2 5H7a2 2 0 01-2-2V5a2 2 0 012-2h5.586a1 1 0 01.707.293l5.414 5.414a1 1 0 01.293.707V19a2 2 0 01-2 2z"
					/>
				</svg>
				General
			</button>
			<button
				type="button"
				class="main-tab"
				class:active={activeTab === 'global'}
				on:click={() => (activeTab = 'global')}
			>
				<svg
					class="tab-icon"
					viewBox="0 0 24 24"
					fill="none"
					stroke="currentColor"
					stroke-width="2"
				>
					<path
						d="M10.325 4.317c.426-1.756 2.924-1.756 3.35 0a1.724 1.724 0 002.573 1.066c1.543-.94 3.31.826 2.37 2.37a1.724 1.724 0 001.065 2.572c1.756.426 1.756 2.924 0 3.35a1.724 1.724 0 00-1.066 2.573c.94 1.543-.826 3.31-2.37 2.37a1.724 1.724 0 00-2.572 1.065c-.426 1.756-2.924 1.756-3.35 0a1.724 1.724 0 00-2.573-1.066c-1.543.94-3.31-.826-2.37-2.37a1.724 1.724 0 00-1.065-2.572c-1.756-.426-1.756-2.924 0-3.35a1.724 1.724 0 001.066-2.573c-.94-1.543.826-3.31 2.37-2.37.996.608 2.296.07 2.572-1.065z"
					/>
					<path d="M15 12a3 3 0 11-6 0 3 3 0 016 0z" />
				</svg>
				Global Settings
			</button>
			<button
				type="button"
				class="main-tab"
				class:active={activeTab === 'hosts'}
				on:click={() => (activeTab = 'hosts')}
			>
				<svg
					class="tab-icon"
					viewBox="0 0 24 24"
					fill="none"
					stroke="currentColor"
					stroke-width="2"
				>
					<path
						d="M21 12a9 9 0 01-9 9m9-9a9 9 0 00-9-9m9 9H3m9 9a9 9 0 01-9-9m9 9c1.657 0 3-4.03 3-9s-1.343-9-3-9m0 18c-1.657 0-3-4.03-3-9s1.343-9 3-9m-9 9a9 9 0 019-9"
					/>
				</svg>
				Hosts
				{#if configData.hosts.length > 0}
					<span class="badge">{configData.hosts.length}</span>
				{/if}
			</button>
		</div>

		<!-- tab content -->
		<div class="tab-content">
			{#if activeTab === 'basic'}
				<!-- basic information tab content -->
				<div class="basic-panel">
					<!-- basic information section -->
					<!-- hidden file input for import -->
					<input
						type="file"
						accept=".yaml,.yml"
						bind:this={fileInput}
						on:change={handleImportFile}
						class="hidden"
					/>
					<div class="settings-section">
						<div class="settings-section-header">
							<h3 class="settings-section-title">General</h3>
						</div>
						<div class="settings-grid">
							<div class="field-wrapper">
								<label class="flex flex-col py-2">
									<div class="flex items-center">
										<p
											class="font-semibold text-slate-600 dark:text-gray-400 py-2 transition-colors duration-200"
										>
											Name
										</p>
									</div>
									<input
										type="text"
										value={name}
										on:input={handleNameInput}
										placeholder="Company Auth Proxy"
										required
										minlength="1"
										maxlength="64"
										class="w-full text-ellipsis rounded-md py-2 pl-2 text-gray-600 dark:text-gray-300 border border-transparent dark:border-gray-700/60 focus:outline-none focus:border-solid focus:border-slate-400 dark:focus:border-highlight-blue/80 focus:bg-gray-100 dark:focus:bg-gray-700/60 bg-grayblue-light dark:bg-gray-900/60 font-normal transition-colors duration-200"
									/>
								</label>
							</div>
							<div class="field-wrapper">
								<label class="flex flex-col py-2">
									<div class="flex items-center">
										<p
											class="font-semibold text-slate-600 dark:text-gray-400 py-2 transition-colors duration-200"
										>
											Description
										</p>
									</div>
									<input
										type="text"
										value={description}
										on:input={handleDescriptionInput}
										placeholder="Optional description"
										maxlength="255"
										class="w-full text-ellipsis rounded-md py-2 pl-2 text-gray-600 dark:text-gray-300 border border-transparent dark:border-gray-700/60 focus:outline-none focus:border-solid focus:border-slate-400 dark:focus:border-highlight-blue/80 focus:bg-gray-100 dark:focus:bg-gray-700/60 bg-grayblue-light dark:bg-gray-900/60 font-normal transition-colors duration-200"
									/>
								</label>
							</div>
							<div class="field-wrapper full">
								<label class="flex flex-col py-2">
									<div class="flex items-center">
										<p
											class="font-semibold text-slate-600 dark:text-gray-400 py-2 transition-colors duration-200"
										>
											Start URL
										</p>
									</div>
									<input
										type="text"
										value={startURL}
										on:input={handleStartURLInput}
										placeholder="https://login.example.com/auth"
										required
										minlength="3"
										class="w-full text-ellipsis rounded-md py-2 pl-2 text-gray-600 dark:text-gray-300 border border-transparent dark:border-gray-700/60 focus:outline-none focus:border-solid focus:border-slate-400 dark:focus:border-highlight-blue/80 focus:bg-gray-100 dark:focus:bg-gray-700/60 bg-grayblue-light dark:bg-gray-900/60 font-normal transition-colors duration-200"
									/>
								</label>
								<span class="settings-field-hint"
									>Domain must match a phishing domain in the Hosts tab</span
								>
							</div>
						</div>
					</div>
					<!-- proxy configuration section -->
					<div class="settings-section">
						<h3 class="settings-section-title">Proxy Settings</h3>
						<div class="settings-grid">
							<div class="field-wrapper">
								<TextField
									width="full"
									bind:value={configData.proxy}
									placeholder="socks5://proxy.example.com:1080 (optional)"
								>
									Forward Proxy
								</TextField>
								<span class="settings-field-hint">Route all traffic through this proxy</span>
							</div>
						</div>
					</div>
				</div>
			{:else if activeTab === 'hosts'}
				<div class="hosts-panel">
					<!-- hosts list sidebar -->
					<div class="hosts-sidebar">
						<div class="sidebar-header">
							<span class="sidebar-title">Domain Mappings</span>
							<button type="button" class="add-btn small" on:click={addHost} title="Add Host">
								<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
									<path d="M12 4v16m8-8H4" />
								</svg>
							</button>
						</div>
						<div class="flex-1 overflow-y-auto p-2">
							{#if configData.hosts.length > 0}
								<div class="flex flex-col gap-1.5">
									{#each configData.hosts as host, i}
										<button
											type="button"
											class="flex flex-col gap-2 w-full p-3 text-left bg-white dark:bg-slate-800/40 border rounded-lg cursor-pointer transition-all duration-150
												{expandedHostIndex === i
												? 'border-sky-600 dark:border-sky-400 bg-sky-50 dark:bg-sky-900/20 shadow-sm ring-1 ring-sky-600/20 dark:ring-sky-400/20'
												: 'border-gray-200 dark:border-gray-700/60 hover:border-gray-300 dark:hover:border-gray-600 hover:bg-gray-50 dark:hover:bg-gray-700/30'}"
											on:click={() => (expandedHostIndex = i)}
										>
											<div class="flex flex-col gap-0.5 min-w-0">
												<span
													class="text-[0.625rem] font-semibold text-gray-400 dark:text-gray-500 uppercase tracking-wider"
													>From</span
												>
												<span
													class="text-sm font-medium text-gray-800 dark:text-gray-100 truncate"
													title={host.to || 'New Host'}>{host.to || 'New Host'}</span
												>
											</div>
											<div class="flex flex-col gap-0.5 min-w-0">
												<span
													class="text-[0.625rem] font-semibold text-gray-400 dark:text-gray-500 uppercase tracking-wider"
													>To</span
												>
												<span
													class="text-sm text-gray-600 dark:text-gray-300 truncate"
													title={host.domain || '...'}>{host.domain || '...'}</span
												>
											</div>
											{#if host.capture?.length || host.rewrite?.length || host.response?.length}
												<div
													class="flex flex-wrap items-center gap-x-3 gap-y-1 pt-2 border-t border-gray-100 dark:border-gray-700/40 text-[0.6875rem]"
												>
													{#if host.capture?.length}
														<span
															class="flex items-center gap-1 text-green-600 dark:text-green-400"
														>
															<span class="w-1.5 h-1.5 rounded-full bg-green-500 dark:bg-green-400"
															></span>
															{host.capture.length} capture
														</span>
													{/if}
													{#if host.rewrite?.length}
														<span class="flex items-center gap-1 text-blue-600 dark:text-blue-400">
															<span class="w-1.5 h-1.5 rounded-full bg-blue-500 dark:bg-blue-400"
															></span>
															{host.rewrite.length} rewrite
														</span>
													{/if}
													{#if host.response?.length}
														<span
															class="flex items-center gap-1 text-amber-600 dark:text-amber-400"
														>
															<span class="w-1.5 h-1.5 rounded-full bg-amber-500 dark:bg-amber-400"
															></span>
															{host.response.length} response
														</span>
													{/if}
												</div>
											{/if}
										</button>
									{/each}
								</div>
							{:else}
								<div class="empty-state small">
									<p>No hosts configured</p>
									<button type="button" class="add-btn" on:click={addHost}>
										<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
											<path d="M12 4v16m8-8H4" />
										</svg>
										Add First Host
									</button>
								</div>
							{/if}
						</div>
					</div>

					<!-- host detail panel -->
					<div class="host-detail">
						{#if expandedHostIndex >= 0 && configData.hosts[expandedHostIndex]}
							<div class="detail-header">
								<div class="detail-title">
									<span class="domain-label"
										>{configData.hosts[expandedHostIndex].to || 'New Host'}</span
									>
									<span class="arrow">→</span>
									<span class="target-label"
										>{configData.hosts[expandedHostIndex].domain || 'target'}</span
									>
								</div>
								<div class="detail-actions">
									<button
										type="button"
										class="icon-btn"
										title="Duplicate"
										on:click={() => duplicateHost(expandedHostIndex)}
									>
										<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
											<rect x="9" y="9" width="13" height="13" rx="2" ry="2" />
											<path d="M5 15H4a2 2 0 01-2-2V4a2 2 0 012-2h9a2 2 0 012 2v1" />
										</svg>
									</button>
									<button
										type="button"
										class="icon-btn danger"
										title="Delete"
										on:click={() => removeHost(expandedHostIndex)}
									>
										<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
											<path
												d="M19 7l-.867 12.142A2 2 0 0116.138 21H7.862a2 2 0 01-1.995-1.858L5 7m5 4v6m4-6v6m1-10V4a1 1 0 00-1-1h-4a1 1 0 00-1 1v3M4 7h16"
											/>
										</svg>
									</button>
								</div>
							</div>

							<!-- sub tabs -->
							<div class="sub-tabs">
								<button
									type="button"
									class="sub-tab"
									class:active={currentHostTab === 'settings'}
									on:click={() => setHostActiveTab(expandedHostIndex, 'settings')}
								>
									Settings
								</button>
								<button
									type="button"
									class="sub-tab"
									class:active={currentHostTab === 'capture'}
									on:click={() => setHostActiveTab(expandedHostIndex, 'capture')}
								>
									Capture
									{#if configData.hosts[expandedHostIndex].capture?.length}
										<span class="sub-badge"
											>{configData.hosts[expandedHostIndex].capture.length}</span
										>
									{/if}
								</button>
								<button
									type="button"
									class="sub-tab"
									class:active={currentHostTab === 'rewrite'}
									on:click={() => setHostActiveTab(expandedHostIndex, 'rewrite')}
								>
									Rewrite
									{#if configData.hosts[expandedHostIndex].rewrite?.length}
										<span class="sub-badge"
											>{configData.hosts[expandedHostIndex].rewrite.length}</span
										>
									{/if}
								</button>
								<button
									type="button"
									class="sub-tab"
									class:active={currentHostTab === 'response'}
									on:click={() => setHostActiveTab(expandedHostIndex, 'response')}
								>
									Response
									{#if configData.hosts[expandedHostIndex].response?.length}
										<span class="sub-badge"
											>{configData.hosts[expandedHostIndex].response.length}</span
										>
									{/if}
								</button>
								<button
									type="button"
									class="sub-tab"
									class:active={currentHostTab === 'urlrewrite'}
									on:click={() => setHostActiveTab(expandedHostIndex, 'urlrewrite')}
								>
									URL Rewrite
									{#if configData.hosts[expandedHostIndex].rewrite_urls?.length}
										<span class="sub-badge"
											>{configData.hosts[expandedHostIndex].rewrite_urls.length}</span
										>
									{/if}
								</button>
							</div>

							<div class="sub-content">
								{#if currentHostTab === 'settings'}
									<div class="settings-grid host-settings">
										<div class="field-wrapper full">
											<TextField
												width="full"
												bind:value={configData.hosts[expandedHostIndex].to}
												placeholder="login.phish.test"
												required
												error={hasError(`hosts.${expandedHostIndex}.to`)}
											>
												Phishing Domain
											</TextField>
											{#if hasError(`hosts.${expandedHostIndex}.to`)}
												<span class="field-error">{getError(`hosts.${expandedHostIndex}.to`)}</span>
											{:else}
												<span class="form-hint"
													>Your phishing domain that will serve the content</span
												>
											{/if}
										</div>
										<div class="field-wrapper full">
											<TextField
												width="full"
												bind:value={configData.hosts[expandedHostIndex].domain}
												placeholder="login.target.com"
												required
												error={hasError(`hosts.${expandedHostIndex}.domain`)}
											>
												Target Domain
											</TextField>
											{#if hasError(`hosts.${expandedHostIndex}.domain`)}
												<span class="field-error"
													>{getError(`hosts.${expandedHostIndex}.domain`)}</span
												>
											{:else}
												<span class="form-hint">The legitimate domain being impersonated</span>
											{/if}
										</div>
										<div class="field-wrapper">
											<TextFieldSelect
												id={`host-${expandedHostIndex}-scheme`}
												bind:value={configData.hosts[expandedHostIndex].scheme}
												options={schemes}
												size="normal"
											>
												Scheme
											</TextFieldSelect>
										</div>
										{#if configData.hosts[expandedHostIndex].tls}
											<div class="field-wrapper">
												<TextFieldSelect
													id={`host-${expandedHostIndex}-tls-mode`}
													bind:value={configData.hosts[expandedHostIndex].tls.mode}
													options={tlsModesWithEmpty}
													size="normal"
													optional
												>
													TLS Mode
												</TextFieldSelect>
											</div>
										{/if}
										{#if configData.hosts[expandedHostIndex].access}
											<div class="field-wrapper">
												<TextFieldSelect
													id={`host-${expandedHostIndex}-access-mode`}
													bind:value={configData.hosts[expandedHostIndex].access.mode}
													options={accessModesWithEmpty}
													size="normal"
													optional
												>
													Access Mode
												</TextFieldSelect>
												<span class="form-hint"
													>Private requires visiting a lure URL first (recommended)</span
												>
											</div>
											{#if configData.hosts[expandedHostIndex].access?.mode === 'private'}
												<div class="field-wrapper">
													<TextField
														width="full"
														bind:value={configData.hosts[expandedHostIndex].access.on_deny}
														placeholder="404"
													>
														On Deny
													</TextField>
													<span class="form-hint"
														>Status code (e.g. 404, 503) or redirect URL (e.g. https://example.com)</span
													>
												</div>
											{/if}
										{/if}
									</div>
								{:else if currentHostTab === 'capture'}
									<div class="rules-description">
										<p>Extract credentials, tokens, and other data from requests and responses.</p>
									</div>
									<div class="rules-container">
										{#each configData.hosts[expandedHostIndex].capture || [] as rule, ruleIndex (rule._id)}
											<div class="rule-card">
												<div class="rule-header">
													<span class="rule-name">{rule.name || `Rule ${ruleIndex + 1}`}</span>
													<button
														type="button"
														class="icon-btn small danger"
														on:click={() => removeHostCaptureRule(expandedHostIndex, ruleIndex)}
													>
														<svg
															viewBox="0 0 24 24"
															fill="none"
															stroke="currentColor"
															stroke-width="2"
														>
															<path d="M6 18L18 6M6 6l12 12" />
														</svg>
													</button>
												</div>
												<div class="rule-grid">
													<div class="field-wrapper">
														<TextField
															width="full"
															bind:value={rule.name}
															placeholder="username"
															error={hasError(
																`hosts.${expandedHostIndex}.capture.${ruleIndex}.name`
															)}
														>
															Name
														</TextField>
														{#if hasError(`hosts.${expandedHostIndex}.capture.${ruleIndex}.name`)}
															<span class="field-error"
																>{getError(
																	`hosts.${expandedHostIndex}.capture.${ruleIndex}.name`
																)}</span
															>
														{/if}
													</div>
													<div class="field-wrapper">
														<TextFieldSelect
															id={`host-${expandedHostIndex}-capture-${ruleIndex}-method`}
															bind:value={rule.method}
															options={methods}
															size="normal"
														>
															Method
														</TextFieldSelect>
													</div>
													<div class="field-wrapper">
														<TextField
															width="full"
															bind:value={rule.path}
															placeholder="/login"
															error={hasError(
																`hosts.${expandedHostIndex}.capture.${ruleIndex}.path`
															)}
														>
															Path (regex)
														</TextField>
														{#if hasError(`hosts.${expandedHostIndex}.capture.${ruleIndex}.path`)}
															<span class="field-error"
																>{getError(
																	`hosts.${expandedHostIndex}.capture.${ruleIndex}.path`
																)}</span
															>
														{/if}
													</div>
													<div class="field-wrapper">
														<TextFieldSelect
															id={`host-${expandedHostIndex}-capture-${ruleIndex}-engine`}
															value={rule.engine}
															options={captureEngines}
															size="normal"
															onSelect={(val) => handleCaptureEngineChange(rule, val)}
														>
															Engine
														</TextFieldSelect>
													</div>
													{#if rule.engine !== 'cookie'}
														<div class="field-wrapper">
															<TextFieldSelect
																id={`host-${expandedHostIndex}-capture-${ruleIndex}-from`}
																bind:value={rule.from}
																options={getFromOptionsForEngine(rule.engine)}
																size="normal"
															>
																From
															</TextFieldSelect>
														</div>
													{/if}
													<div class="field-wrapper full">
														<TextField
															width="full"
															bind:value={rule.find}
															placeholder={rule.engine === 'regex'
																? 'username=([^&]+)'
																: rule.engine === 'header'
																	? 'Authorization'
																	: rule.engine === 'cookie'
																		? 'session_id'
																		: rule.engine === 'json'
																			? 'user.email'
																			: 'username'}
															error={hasError(
																`hosts.${expandedHostIndex}.capture.${ruleIndex}.find`
															)}
														>
															{#if rule.engine === 'regex'}
																Regex Pattern
															{:else if rule.engine === 'header'}
																Header Name
															{:else if rule.engine === 'cookie'}
																Cookie Name
															{:else if rule.engine === 'json'}
																JSON Path
															{:else}
																Field Name
															{/if}
														</TextField>
														{#if hasError(`hosts.${expandedHostIndex}.capture.${ruleIndex}.find`)}
															<span class="field-error"
																>{getError(
																	`hosts.${expandedHostIndex}.capture.${ruleIndex}.find`
																)}</span
															>
														{/if}
													</div>
													<div class="field-wrapper checkbox-wrapper">
														<label class="checkbox-label">
															<input
																type="checkbox"
																checked={rule.required}
																on:change={(e) => {
																	rule.required = e.currentTarget.checked;
																	configData = configData;
																}}
																class="checkbox-input"
															/>
															<span class="checkbox-text">Required</span>
														</label>
														<span class="form-hint"
															>Must be captured before session completes and campaign flow
															progresses</span
														>
													</div>
												</div>
											</div>
										{/each}
										<button
											type="button"
											class="add-rule-btn"
											on:click={() => addHostCaptureRule(expandedHostIndex)}
										>
											<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
												<path d="M12 4v16m8-8H4" />
											</svg>
											Add Capture Rule
										</button>
									</div>
								{:else if currentHostTab === 'rewrite'}
									<div class="rules-description">
										<p>
											Rewrite rules modify content passing through the proxy. Use <strong
												>Regex</strong
											>
											for text replacement or <strong>DOM</strong> for HTML element manipulation.
										</p>
									</div>
									<div class="rules-container">
										{#each configData.hosts[expandedHostIndex].rewrite || [] as rule, ruleIndex (rule._id)}
											<div class="rule-card">
												<div class="rule-header">
													<span class="rule-name">{rule.name || `Rule ${ruleIndex + 1}`}</span>
													<button
														type="button"
														class="icon-btn small danger"
														on:click={() => removeHostRewriteRule(expandedHostIndex, ruleIndex)}
													>
														<svg
															viewBox="0 0 24 24"
															fill="none"
															stroke="currentColor"
															stroke-width="2"
														>
															<path d="M6 18L18 6M6 6l12 12" />
														</svg>
													</button>
												</div>
												<div class="rule-grid">
													<div class="field-wrapper">
														<TextField
															width="full"
															bind:value={rule.name}
															placeholder="replace_logo"
														>
															Name
														</TextField>
													</div>
													<div class="field-wrapper">
														<TextFieldSelect
															id={`host-${expandedHostIndex}-rewrite-${ruleIndex}-engine`}
															bind:value={rule.engine}
															options={engines}
															size="normal"
														>
															Engine
														</TextFieldSelect>
													</div>
													{#if rule.engine === 'dom'}
														<div class="field-wrapper">
															<TextFieldSelect
																id={`host-${expandedHostIndex}-rewrite-${ruleIndex}-action`}
																bind:value={rule.action}
																options={domActions}
																size="normal"
															>
																Action
															</TextFieldSelect>
															{#if hasError(`hosts.${expandedHostIndex}.rewrite.${ruleIndex}.action`)}
																<span class="field-error"
																	>{getError(
																		`hosts.${expandedHostIndex}.rewrite.${ruleIndex}.action`
																	)}</span
																>
															{/if}
														</div>
														<div class="field-wrapper">
															<TextFieldSelect
																id={`host-${expandedHostIndex}-rewrite-${ruleIndex}-target`}
																bind:value={rule.target}
																options={targets}
																size="normal"
															>
																Target
															</TextFieldSelect>
															<span class="form-hint"
																>Also supports numeric list (1,3,5) or range (2-4)</span
															>
														</div>
													{:else}
														<div class="field-wrapper">
															<TextFieldSelect
																id={`host-${expandedHostIndex}-rewrite-${ruleIndex}-from`}
																bind:value={rule.from}
																options={fromOptions}
																size="normal"
															>
																From
															</TextFieldSelect>
														</div>
													{/if}
													<div class="field-wrapper full">
														<TextField
															width="full"
															bind:value={rule.find}
															placeholder={rule.engine === 'dom'
																? 'div.logo, #header img'
																: 'target\\.com'}
															error={hasError(
																`hosts.${expandedHostIndex}.rewrite.${ruleIndex}.find`
															)}
														>
															{#if rule.engine === 'dom'}
																Selector (CSS)
															{:else}
																Find (regex)
															{/if}
														</TextField>
														{#if hasError(`hosts.${expandedHostIndex}.rewrite.${ruleIndex}.find`)}
															<span class="field-error"
																>{getError(
																	`hosts.${expandedHostIndex}.rewrite.${ruleIndex}.find`
																)}</span
															>
														{:else}
															<span class="form-hint">
																{#if rule.engine === 'dom'}
																	CSS selector to find HTML elements
																{:else}
																	Regex pattern to search for in content
																{/if}
															</span>
														{/if}
													</div>
													<div class="field-wrapper full">
														<TextField
															width="full"
															bind:value={rule.replace}
															placeholder={rule.engine === 'dom'
																? rule.action === 'setAttr'
																	? 'href:https://example.com'
																	: rule.action === 'remove'
																		? ''
																		: 'New content'
																: 'phishing.com'}
															error={hasError(
																`hosts.${expandedHostIndex}.rewrite.${ruleIndex}.replace`
															)}
														>
															{#if rule.engine === 'dom' && rule.action === 'setAttr'}
																Value (attr:value)
															{:else if rule.engine === 'dom' && rule.action === 'remove'}
																Value (not required)
															{:else}
																Replace
															{/if}
														</TextField>
														{#if hasError(`hosts.${expandedHostIndex}.rewrite.${ruleIndex}.replace`)}
															<span class="field-error"
																>{getError(
																	`hosts.${expandedHostIndex}.rewrite.${ruleIndex}.replace`
																)}</span
															>
														{:else}
															<span class="form-hint">
																{#if rule.engine === 'dom'}
																	{#if rule.action === 'setAttr'}
																		Format: attribute:value (e.g. href:https://example.com)
																	{:else if rule.action === 'remove'}
																		Not required for remove action
																	{:else if rule.action === 'removeAttr'}
																		Attribute name to remove
																	{:else if rule.action === 'addClass' || rule.action === 'removeClass'}
																		CSS class name
																	{:else}
																		New content for matched elements
																	{/if}
																{:else}
																	Replacement text (use $1, $2 for capture groups)
																{/if}
															</span>
														{/if}
													</div>
												</div>
											</div>
										{/each}
										<button
											type="button"
											class="add-rule-btn"
											on:click={() => addHostRewriteRule(expandedHostIndex)}
										>
											<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
												<path d="M12 4v16m8-8H4" />
											</svg>
											Add Rewrite Rule
										</button>
									</div>
								{:else if currentHostTab === 'response'}
									<div class="rules-description">
										<p>
											Return custom responses for specific paths instead of proxying to the target.
										</p>
									</div>
									<div class="rules-container">
										{#each configData.hosts[expandedHostIndex].response || [] as rule, ruleIndex (rule._id)}
											<div class="rule-card">
												<div class="rule-header">
													<span class="rule-name">{rule.path || `Rule ${ruleIndex + 1}`}</span>
													<button
														type="button"
														class="icon-btn small danger"
														on:click={() => removeHostResponseRule(expandedHostIndex, ruleIndex)}
													>
														<svg
															viewBox="0 0 24 24"
															fill="none"
															stroke="currentColor"
															stroke-width="2"
														>
															<path d="M6 18L18 6M6 6l12 12" />
														</svg>
													</button>
												</div>
												<div class="rule-grid">
													<div class="field-wrapper">
														<TextField
															width="full"
															bind:value={rule.path}
															placeholder="/custom-page"
															error={hasError(
																`hosts.${expandedHostIndex}.response.${ruleIndex}.path`
															)}
														>
															Path
														</TextField>
														{#if hasError(`hosts.${expandedHostIndex}.response.${ruleIndex}.path`)}
															<span class="field-error"
																>{getError(
																	`hosts.${expandedHostIndex}.response.${ruleIndex}.path`
																)}</span
															>
														{/if}
													</div>
													<div class="field-wrapper">
														<TextField width="full" bind:value={rule.status} placeholder="200">
															Status
														</TextField>
													</div>
													<div class="field-wrapper full">
														<TextareaField
															fullWidth
															bind:value={rule.body}
															placeholder="<html>...</html>"
															height="medium"
														>
															Body
														</TextareaField>
													</div>
													<div class="field-wrapper full">
														<div class="headers-section">
															<div class="headers-label">
																<span>Headers</span>
																<button
																	type="button"
																	class="add-btn tiny"
																	on:click={() => addResponseHeader(rule)}
																	title="Add Header"
																>
																	<svg
																		viewBox="0 0 24 24"
																		fill="none"
																		stroke="currentColor"
																		stroke-width="2"
																	>
																		<path d="M12 4v16m8-8H4" />
																	</svg>
																</button>
															</div>
															{#if rule.headers && Object.keys(rule.headers).length > 0}
																<div class="headers-list">
																	{#each Object.entries(rule.headers) as [key, value]}
																		<div class="header-row">
																			<input
																				type="text"
																				value={key}
																				on:blur={(e) =>
																					updateResponseHeaderKey(rule, key, e.currentTarget.value)}
																				placeholder="Header-Name"
																				class="header-key-input"
																			/>
																			<input
																				type="text"
																				bind:value={rule.headers[key]}
																				placeholder="Header value"
																				class="header-value-input"
																			/>
																			<button
																				type="button"
																				class="icon-btn tiny danger"
																				on:click={() => removeResponseHeader(rule, key)}
																				title="Remove header"
																			>
																				<svg
																					viewBox="0 0 24 24"
																					fill="none"
																					stroke="currentColor"
																					stroke-width="2"
																				>
																					<path d="M6 18L18 6M6 6l12 12" />
																				</svg>
																			</button>
																		</div>
																	{/each}
																</div>
															{/if}
															<span class="form-hint"
																>Use <code>{'{{.Origin}}'}</code> to echo the request's Origin header</span
															>
														</div>
													</div>
													<div class="field-wrapper checkbox-wrapper">
														<label class="checkbox-label">
															<input
																type="checkbox"
																bind:checked={rule.forward}
																class="checkbox-input"
																on:change={(e) => {
																	rule.forward = e.currentTarget.checked;
																	configData = configData;
																}}
															/>
															<span class="checkbox-text">Forward to target</span>
														</label>
													</div>
												</div>
											</div>
										{/each}
										<button
											type="button"
											class="add-rule-btn"
											on:click={() => addHostResponseRule(expandedHostIndex)}
										>
											<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
												<path d="M12 4v16m8-8H4" />
											</svg>
											Add Response Rule
										</button>
									</div>
								{:else if currentHostTab === 'urlrewrite'}
									<div class="rules-description">
										<p>Transform URL paths to evade detection by masking original target URLs.</p>
									</div>
									<div class="rules-container">
										{#each configData.hosts[expandedHostIndex].rewrite_urls || [] as rule, ruleIndex (rule._id)}
											<div class="rule-card">
												<div class="rule-header">
													<span class="rule-name">{rule.find || `Rule ${ruleIndex + 1}`}</span>
													<button
														type="button"
														class="icon-btn small danger"
														on:click={() => removeHostRewriteUrlRule(expandedHostIndex, ruleIndex)}
													>
														<svg
															viewBox="0 0 24 24"
															fill="none"
															stroke="currentColor"
															stroke-width="2"
														>
															<path d="M6 18L18 6M6 6l12 12" />
														</svg>
													</button>
												</div>
												<div class="rule-grid">
													<div class="field-wrapper">
														<TextField width="full" bind:value={rule.find} placeholder="/old-path">
															Find
														</TextField>
													</div>
													<div class="field-wrapper">
														<TextField
															width="full"
															bind:value={rule.replace}
															placeholder="/new-path"
														>
															Replace
														</TextField>
													</div>
												</div>
											</div>
										{/each}
										<button
											type="button"
											class="add-rule-btn"
											on:click={() => addHostRewriteUrlRule(expandedHostIndex)}
										>
											<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
												<path d="M12 4v16m8-8H4" />
											</svg>
											Add URL Rewrite Rule
										</button>
									</div>
								{/if}
							</div>
						{:else}
							<div class="empty-state">
								<svg
									class="empty-icon"
									viewBox="0 0 24 24"
									fill="none"
									stroke="currentColor"
									stroke-width="1.5"
								>
									<path
										d="M21 12a9 9 0 01-9 9m9-9a9 9 0 00-9-9m9 9H3m9 9a9 9 0 01-9-9m9 9c1.657 0 3-4.03 3-9s-1.343-9-3-9m0 18c-1.657 0-3-4.03-3-9s1.343-9 3-9m-9 9a9 9 0 019-9"
									/>
								</svg>
								<p>Select a host or add a new one</p>
								<button type="button" class="add-btn" on:click={addHost}>
									<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
										<path d="M12 4v16m8-8H4" />
									</svg>
									Add Host
								</button>
							</div>
						{/if}
					</div>
				</div>
			{:else if activeTab === 'global'}
				<div class="global-panel">
					<div class="global-grid">
						<!-- TLS & Access -->
						<div class="global-section">
							<h3 class="section-title">
								<svg
									class="section-icon"
									viewBox="0 0 24 24"
									fill="none"
									stroke="currentColor"
									stroke-width="2"
								>
									<path
										d="M12 15v2m-6 4h12a2 2 0 002-2v-6a2 2 0 00-2-2H6a2 2 0 00-2 2v6a2 2 0 002 2zm10-10V7a4 4 0 00-8 0v4h8z"
									/>
								</svg>
								Security
							</h3>
							<div class="section-content">
								<div class="field-wrapper">
									<TextFieldSelect
										id="global-tls-mode"
										bind:value={configData.global.tls.mode}
										options={tlsModes}
										size="normal"
									>
										TLS Mode
									</TextFieldSelect>
									<span class="form-hint"
										>Controls certificate verification for upstream connections</span
									>
								</div>
								<div class="field-wrapper">
									<TextFieldSelect
										id="global-access-mode"
										bind:value={configData.global.access.mode}
										options={accessModes}
										size="normal"
									>
										Access Mode
									</TextFieldSelect>
									<span class="form-hint"
										>Private requires visiting a lure URL first (recommended)</span
									>
								</div>
								{#if configData.global.access?.mode === 'private'}
									<div class="field-wrapper">
										<TextField
											width="full"
											bind:value={configData.global.access.on_deny}
											placeholder="404"
										>
											On Deny
										</TextField>
										<span class="form-hint"
											>Status code (e.g. 404, 503) or redirect URL (e.g. https://example.com)</span
										>
									</div>
								{/if}
							</div>
						</div>

						<!-- Impersonation -->
						<div class="global-section">
							<h3 class="section-title">
								<svg
									class="section-icon"
									viewBox="0 0 24 24"
									fill="none"
									stroke="currentColor"
									stroke-width="2"
								>
									<path d="M16 7a4 4 0 11-8 0 4 4 0 018 0zM12 14a7 7 0 00-7 7h14a7 7 0 00-7-7z" />
								</svg>
								Client Browser Impersonation
							</h3>
							<div class="section-content">
								<label class="checkbox-label">
									<input
										type="checkbox"
										checked={configData.global.impersonate.enabled}
										on:change={(e) => {
											configData.global.impersonate.enabled = e.currentTarget.checked;
											configData = configData;
										}}
										class="checkbox-input"
									/>
									<span class="checkbox-text">Enable Impersonation</span>
								</label>
								<span class="form-hint"
									>Detects client browser and uses a matching fingerprint profile (Chrome or Firefox
									only, others default to Chrome)</span
								>
								{#if configData.global.impersonate.enabled}
									<label class="checkbox-label" style="margin-top: 0.5rem;">
										<input
											type="checkbox"
											checked={configData.global.impersonate.retain_ua}
											on:change={(e) => {
												configData.global.impersonate.retain_ua = e.currentTarget.checked;
												configData = configData;
											}}
											class="checkbox-input"
										/>
										<span class="checkbox-text">Retain User Agent</span>
									</label>
									<span class="form-hint"
										>Use the client's User-Agent header instead of the impersonated browser's
										default</span
									>
								{/if}
							</div>
						</div>

						<!-- Template Variables -->
						<div class="global-section">
							<h3 class="section-title">
								<svg
									class="section-icon"
									viewBox="0 0 24 24"
									fill="none"
									stroke="currentColor"
									stroke-width="2"
								>
									<path
										d="M7 8h10M7 12h4m1 8l-4-4H5a2 2 0 01-2-2V6a2 2 0 012-2h14a2 2 0 012 2v8a2 2 0 01-2 2h-3l-4 4z"
									/>
								</svg>
								Template Variables
							</h3>
							<div class="section-content">
								<label class="checkbox-label">
									<input
										type="checkbox"
										checked={configData.global.variables.enabled}
										on:change={(e) => {
											configData.global.variables.enabled = e.currentTarget.checked;
											if (!e.currentTarget.checked) {
												configData.global.variables.allowed = [];
											}
											configData = configData;
										}}
										class="checkbox-input"
									/>
									<span class="checkbox-text">Enable Variables</span>
								</label>
								<span class="form-hint"
									>Allow template variables like <code>{'{{.Email}}'}</code> in rewrite rules to be replaced
									with recipient data</span
								>
								{#if configData.global.variables.enabled}
									<div class="field-wrapper" style="margin-top: 0.75rem;">
										<label class="flex flex-col">
											<span class="text-sm font-medium text-pc-darkblue dark:text-white mb-1.5"
												>Allowed Variables (optional)</span
											>
											<div class="variables-selector">
												{#each validProxyVariables as varName}
													<label class="variable-chip">
														<input
															type="checkbox"
															checked={configData.global.variables.allowed?.includes(varName)}
															on:change={(e) => {
																if (e.currentTarget.checked) {
																	configData.global.variables.allowed = [
																		...(configData.global.variables.allowed || []),
																		varName
																	];
																} else {
																	configData.global.variables.allowed =
																		configData.global.variables.allowed?.filter(
																			(v) => v !== varName
																		) || [];
																}
																configData = configData;
															}}
															class="hidden"
														/>
														<span
															class="chip-text"
															class:selected={configData.global.variables.allowed?.includes(
																varName
															)}>{varName}</span
														>
													</label>
												{/each}
											</div>
										</label>
										<span class="form-hint"
											>Leave empty to allow all variables, or select specific ones to restrict which
											can be used</span
										>
									</div>
								{/if}
							</div>
						</div>
					</div>

					<!-- Global Rules Tabs -->
					<div class="global-rules">
						<div class="rules-tabs">
							<button
								type="button"
								class="rules-tab"
								class:active={(activeTab === 'global' && !globalRulesTab) ||
									globalRulesTab === 'capture'}
								on:click={() => (globalRulesTab = 'capture')}
							>
								Capture Rules
								{#if configData.global.capture?.length}
									<span class="sub-badge">{configData.global.capture.length}</span>
								{/if}
							</button>
							<button
								type="button"
								class="rules-tab"
								class:active={globalRulesTab === 'rewrite'}
								on:click={() => (globalRulesTab = 'rewrite')}
							>
								Rewrite Rules
								{#if configData.global.rewrite?.length}
									<span class="sub-badge">{configData.global.rewrite.length}</span>
								{/if}
							</button>
							<button
								type="button"
								class="rules-tab"
								class:active={globalRulesTab === 'response'}
								on:click={() => (globalRulesTab = 'response')}
							>
								Response Rules
								{#if configData.global.response?.length}
									<span class="sub-badge">{configData.global.response.length}</span>
								{/if}
							</button>
						</div>

						<div class="rules-content">
							{#if !globalRulesTab || globalRulesTab === 'capture'}
								<div class="rules-description">
									<p>Extract credentials, tokens, and other data from requests and responses.</p>
								</div>
								<div class="rules-container">
									{#each configData.global.capture || [] as rule, i (rule._id)}
										<div class="rule-card">
											<div class="rule-header">
												<span class="rule-name">{rule.name || `Rule ${i + 1}`}</span>
												<button
													type="button"
													class="icon-btn small danger"
													on:click={() => removeGlobalCaptureRule(i)}
												>
													<svg
														viewBox="0 0 24 24"
														fill="none"
														stroke="currentColor"
														stroke-width="2"
													>
														<path d="M6 18L18 6M6 6l12 12" />
													</svg>
												</button>
											</div>
											<div class="rule-grid">
												<div class="field-wrapper">
													<TextField
														width="full"
														bind:value={rule.name}
														placeholder="username"
														error={hasError(`global.capture.${i}.name`)}
													>
														Name
													</TextField>
													{#if hasError(`global.capture.${i}.name`)}
														<span class="field-error">{getError(`global.capture.${i}.name`)}</span>
													{/if}
												</div>
												<div class="field-wrapper">
													<TextFieldSelect
														id={`global-capture-${i}-method`}
														bind:value={rule.method}
														options={methods}
														size="normal"
													>
														Method
													</TextFieldSelect>
												</div>
												<div class="field-wrapper">
													<TextField
														width="full"
														bind:value={rule.path}
														placeholder="/login"
														error={hasError(`global.capture.${i}.path`)}
													>
														Path (regex)
													</TextField>
													{#if hasError(`global.capture.${i}.path`)}
														<span class="field-error">{getError(`global.capture.${i}.path`)}</span>
													{/if}
												</div>
												<div class="field-wrapper">
													<TextFieldSelect
														id={`global-capture-${i}-engine`}
														value={rule.engine}
														options={captureEngines}
														size="normal"
														onSelect={(val) => handleCaptureEngineChange(rule, val)}
													>
														Engine
													</TextFieldSelect>
												</div>
												{#if rule.engine !== 'cookie'}
													<div class="field-wrapper">
														<TextFieldSelect
															id={`global-capture-${i}-from`}
															bind:value={rule.from}
															options={getFromOptionsForEngine(rule.engine)}
															size="normal"
														>
															From
														</TextFieldSelect>
													</div>
												{/if}
												<div class="field-wrapper full">
													<TextField
														width="full"
														bind:value={rule.find}
														placeholder={rule.engine === 'regex'
															? 'username=([^&]+)'
															: rule.engine === 'header'
																? 'Authorization'
																: rule.engine === 'cookie'
																	? 'session_id'
																	: rule.engine === 'json'
																		? 'user.email'
																		: 'username'}
														error={hasError(`global.capture.${i}.find`)}
													>
														{#if rule.engine === 'regex'}
															Regex Pattern
														{:else if rule.engine === 'header'}
															Header Name
														{:else if rule.engine === 'cookie'}
															Cookie Name
														{:else if rule.engine === 'json'}
															JSON Path
														{:else}
															Field Name
														{/if}
													</TextField>
													{#if hasError(`global.capture.${i}.find`)}
														<span class="field-error">{getError(`global.capture.${i}.find`)}</span>
													{/if}
												</div>
												<div class="field-wrapper checkbox-wrapper">
													<label class="checkbox-label">
														<input
															type="checkbox"
															checked={rule.required}
															on:change={(e) => {
																rule.required = e.currentTarget.checked;
																configData = configData;
															}}
															class="checkbox-input"
														/>
														<span class="checkbox-text">Required</span>
													</label>
													<span class="form-hint"
														>Must be captured before session completes and campaign flow progresses</span
													>
												</div>
											</div>
										</div>
									{/each}
									<button type="button" class="add-rule-btn" on:click={addGlobalCaptureRule}>
										<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
											<path d="M12 4v16m8-8H4" />
										</svg>
										Add Capture Rule
									</button>
								</div>
							{:else if globalRulesTab === 'rewrite'}
								<div class="rules-description">
									<p>
										Rewrite rules modify content passing through the proxy. Use <strong
											>Regex</strong
										>
										for text replacement or <strong>DOM</strong> for HTML element manipulation.
									</p>
								</div>
								<div class="rules-container">
									{#each configData.global.rewrite || [] as rule, i (rule._id)}
										<div class="rule-card">
											<div class="rule-header">
												<span class="rule-name">{rule.name || `Rule ${i + 1}`}</span>
												<button
													type="button"
													class="icon-btn small danger"
													on:click={() => removeGlobalRewriteRule(i)}
												>
													<svg
														viewBox="0 0 24 24"
														fill="none"
														stroke="currentColor"
														stroke-width="2"
													>
														<path d="M6 18L18 6M6 6l12 12" />
													</svg>
												</button>
											</div>
											<div class="rule-grid">
												<div class="field-wrapper">
													<TextField width="full" bind:value={rule.name} placeholder="replace_logo">
														Name
													</TextField>
												</div>
												<div class="field-wrapper">
													<TextFieldSelect
														id={`global-rewrite-${i}-engine`}
														bind:value={rule.engine}
														options={engines}
														size="normal"
													>
														Engine
													</TextFieldSelect>
												</div>
												{#if rule.engine === 'dom'}
													<div class="field-wrapper">
														<TextFieldSelect
															id={`global-rewrite-${i}-action`}
															bind:value={rule.action}
															options={domActions}
															size="normal"
														>
															Action
														</TextFieldSelect>
														{#if hasError(`global.rewrite.${i}.action`)}
															<span class="field-error"
																>{getError(`global.rewrite.${i}.action`)}</span
															>
														{/if}
													</div>
													<div class="field-wrapper">
														<TextFieldSelect
															id={`global-rewrite-${i}-target`}
															bind:value={rule.target}
															options={targets}
															size="normal"
														>
															Target
														</TextFieldSelect>
														<span class="form-hint"
															>Also supports numeric list (1,3,5) or range (2-4)</span
														>
													</div>
												{:else}
													<div class="field-wrapper">
														<TextFieldSelect
															id={`global-rewrite-${i}-from`}
															bind:value={rule.from}
															options={fromOptions}
															size="normal"
														>
															From
														</TextFieldSelect>
													</div>
												{/if}
												<div class="field-wrapper full">
													<TextField
														width="full"
														bind:value={rule.find}
														placeholder={rule.engine === 'dom'
															? 'div.logo, #header img'
															: 'target\\.com'}
														error={hasError(`global.rewrite.${i}.find`)}
													>
														{#if rule.engine === 'dom'}
															Selector (CSS)
														{:else}
															Find (regex)
														{/if}
													</TextField>
													{#if hasError(`global.rewrite.${i}.find`)}
														<span class="field-error">{getError(`global.rewrite.${i}.find`)}</span>
													{:else}
														<span class="form-hint">
															{#if rule.engine === 'dom'}
																CSS selector to find HTML elements
															{:else}
																Regex pattern to search for in content
															{/if}
														</span>
													{/if}
												</div>
												<div class="field-wrapper full">
													<TextField
														width="full"
														bind:value={rule.replace}
														placeholder={rule.engine === 'dom'
															? rule.action === 'setAttr'
																? 'href:https://example.com'
																: rule.action === 'remove'
																	? ''
																	: 'New content'
															: 'phishing.com'}
														error={hasError(`global.rewrite.${i}.replace`)}
													>
														{#if rule.engine === 'dom' && rule.action === 'setAttr'}
															Value (attr:value)
														{:else if rule.engine === 'dom' && rule.action === 'remove'}
															Value (not required)
														{:else}
															Replace
														{/if}
													</TextField>
													{#if hasError(`global.rewrite.${i}.replace`)}
														<span class="field-error"
															>{getError(`global.rewrite.${i}.replace`)}</span
														>
													{:else}
														<span class="form-hint">
															{#if rule.engine === 'dom'}
																{#if rule.action === 'setAttr'}
																	Format: attribute:value (e.g. href:https://example.com)
																{:else if rule.action === 'remove'}
																	Not required for remove action
																{:else if rule.action === 'removeAttr'}
																	Attribute name to remove
																{:else if rule.action === 'addClass' || rule.action === 'removeClass'}
																	CSS class name
																{:else}
																	New content for matched elements
																{/if}
															{:else}
																Replacement text (use $1, $2 for capture groups)
															{/if}
														</span>
													{/if}
												</div>
											</div>
										</div>
									{/each}
									<button type="button" class="add-rule-btn" on:click={addGlobalRewriteRule}>
										<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
											<path d="M12 4v16m8-8H4" />
										</svg>
										Add Rewrite Rule
									</button>
								</div>
							{:else if globalRulesTab === 'response'}
								<div class="rules-description">
									<p>
										Return custom responses for specific paths instead of proxying to the target.
									</p>
								</div>
								<div class="rules-container">
									{#each configData.global.response || [] as rule, i (rule._id)}
										<div class="rule-card">
											<div class="rule-header">
												<span class="rule-name">{rule.path || `Rule ${i + 1}`}</span>
												<button
													type="button"
													class="icon-btn small danger"
													on:click={() => removeGlobalResponseRule(i)}
												>
													<svg
														viewBox="0 0 24 24"
														fill="none"
														stroke="currentColor"
														stroke-width="2"
													>
														<path d="M6 18L18 6M6 6l12 12" />
													</svg>
												</button>
											</div>
											<div class="rule-grid">
												<div class="field-wrapper">
													<TextField
														width="full"
														bind:value={rule.path}
														placeholder="/custom-page"
														error={hasError(`global.response.${i}.path`)}
													>
														Path
													</TextField>
													{#if hasError(`global.response.${i}.path`)}
														<span class="field-error">{getError(`global.response.${i}.path`)}</span>
													{/if}
												</div>
												<div class="field-wrapper">
													<TextField width="full" bind:value={rule.status} placeholder="200">
														Status
													</TextField>
												</div>
												<div class="field-wrapper full">
													<TextareaField
														fullWidth
														bind:value={rule.body}
														placeholder="<html>...</html>"
														height="medium"
													>
														Body
													</TextareaField>
												</div>
												<div class="field-wrapper full">
													<div class="headers-section">
														<div class="headers-label">
															<span>Headers</span>
															<button
																type="button"
																class="add-btn tiny"
																on:click={() => addResponseHeader(rule)}
																title="Add Header"
															>
																<svg
																	viewBox="0 0 24 24"
																	fill="none"
																	stroke="currentColor"
																	stroke-width="2"
																>
																	<path d="M12 4v16m8-8H4" />
																</svg>
															</button>
														</div>
														{#if rule.headers && Object.keys(rule.headers).length > 0}
															<div class="headers-list">
																{#each Object.entries(rule.headers) as [key, value]}
																	<div class="header-row">
																		<input
																			type="text"
																			value={key}
																			on:blur={(e) =>
																				updateResponseHeaderKey(rule, key, e.currentTarget.value)}
																			placeholder="Header-Name"
																			class="header-key-input"
																		/>
																		<input
																			type="text"
																			bind:value={rule.headers[key]}
																			placeholder="Header value"
																			class="header-value-input"
																		/>
																		<button
																			type="button"
																			class="icon-btn tiny danger"
																			on:click={() => removeResponseHeader(rule, key)}
																			title="Remove header"
																		>
																			<svg
																				viewBox="0 0 24 24"
																				fill="none"
																				stroke="currentColor"
																				stroke-width="2"
																			>
																				<path d="M6 18L18 6M6 6l12 12" />
																			</svg>
																		</button>
																	</div>
																{/each}
															</div>
														{/if}
														<span class="form-hint"
															>Use <code>{'{{.Origin}}'}</code> to echo the request's Origin header</span
														>
													</div>
												</div>
												<div class="field-wrapper checkbox-wrapper">
													<label class="checkbox-label">
														<input
															type="checkbox"
															bind:checked={rule.forward}
															on:change={(e) => {
																rule.forward = e.currentTarget.checked;
																configData = configData;
															}}
															class="checkbox-input"
														/>
														<span class="checkbox-text">Forward to target</span>
													</label>
												</div>
											</div>
										</div>
									{/each}
									<button type="button" class="add-rule-btn" on:click={addGlobalResponseRule}>
										<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
											<path d="M12 4v16m8-8H4" />
										</svg>
										Add Response Rule
									</button>
								</div>
							{/if}
						</div>
					</div>
				</div>
			{/if}
		</div>
	</div>
</div>

<style>
	/* wrapper */
	.proxy-builder-wrapper {
		width: 100%;
		height: 100%;
		min-height: 500px;
		overflow: auto;
	}

	.proxy-builder {
		display: flex;
		flex-direction: column;
		height: 100%;
		background: white;
		border-radius: 0.5rem;
	}

	:global(.dark) .proxy-builder {
		background: rgb(17 24 39 / 0.6);
	}

	/* settings section */
	.settings-section {
		padding: 1.5rem;
		border-bottom: 1px solid #e5e7eb;
	}

	.settings-section:last-child {
		border-bottom: none;
	}

	:global(.dark) .settings-section {
		border-bottom-color: rgb(55 65 81 / 0.6);
	}

	.settings-section-header {
		display: flex;
		justify-content: space-between;
		align-items: center;
		margin-bottom: 0.75rem;
	}

	.settings-section-title {
		font-size: 0.875rem;
		font-weight: 600;
		color: rgb(71, 85, 105);
		margin-bottom: 0;
	}

	.settings-section-header + .settings-grid {
		margin-top: 0;
	}

	.hidden {
		display: none;
	}

	:global(.dark) .settings-section-title {
		color: #9ca3af;
	}

	.settings-grid {
		display: grid;
		grid-template-columns: repeat(2, 1fr);
		gap: 0.5rem 1.5rem;
	}

	.field-wrapper {
		display: flex;
		flex-direction: column;
	}

	.field-wrapper.full {
		grid-column: span 2;
	}

	.field-wrapper.checkbox-wrapper {
		padding-top: 0.5rem;
	}

	.settings-field-hint,
	.form-hint {
		font-size: 0.75rem;
		color: #6b7280;
		margin-top: 0.25rem;
		padding-left: 0.125rem;
	}

	:global(.dark) .settings-field-hint,
	:global(.dark) .form-hint {
		color: #9ca3af;
	}

	.rules-description {
		padding: 0.75rem 1rem;
		margin-bottom: 1rem;
		background: #f8fafc;
		border-radius: 0.5rem;
		border: 1px solid #e2e8f0;
	}

	:global(.dark) .rules-description {
		background: rgba(30, 41, 59, 0.5);
		border-color: #334155;
	}

	.rules-description p {
		font-size: 0.875rem;
		color: #64748b;
		margin: 0;
		line-height: 1.5;
	}

	:global(.dark) .rules-description p {
		color: #94a3b8;
	}

	.rules-description strong {
		color: #475569;
		font-weight: 600;
	}

	:global(.dark) .rules-description strong {
		color: #cbd5e1;
	}

	/* main tabs */
	.main-tabs {
		display: flex;
		gap: 0.25rem;
		padding: 0.75rem;
		background: #f8fafc;
		border-bottom: 1px solid #e5e7eb;
		border-radius: 0.5rem 0.5rem 0 0;
	}

	:global(.dark) .main-tabs {
		background: rgb(31 41 55 / 0.6);
		border-bottom-color: rgb(55 65 81 / 0.6);
	}

	.main-tab {
		display: flex;
		align-items: center;
		gap: 0.5rem;
		padding: 0.625rem 1rem;
		font-size: 0.875rem;
		font-weight: 500;
		color: #64748b;
		background: transparent;
		border: none;
		border-radius: 0.375rem;
		cursor: pointer;
		transition: all 0.15s ease;
	}

	.main-tab:hover {
		background: #e2e8f0;
		color: #475569;
	}

	:global(.dark) .main-tab:hover {
		background: rgb(55 65 81 / 0.6);
		color: #d1d5db;
	}

	.main-tab.active {
		background: white;
		color: #0284c7;
		box-shadow: 0 1px 2px rgba(0, 0, 0, 0.05);
	}

	:global(.dark) .main-tab.active {
		background: rgb(17 24 39 / 0.8);
		color: #38bdf8;
	}

	.tab-icon {
		width: 1.125rem;
		height: 1.125rem;
	}

	.badge {
		display: inline-flex;
		align-items: center;
		justify-content: center;
		min-width: 1.25rem;
		height: 1.25rem;
		padding: 0 0.375rem;
		font-size: 0.75rem;
		font-weight: 600;
		background: #0284c7;
		color: white;
		border-radius: 9999px;
	}

	.main-tab:not(.active) .badge {
		background: #94a3b8;
	}

	:global(.dark) .main-tab:not(.active) .badge {
		background: rgb(75 85 99 / 0.8);
	}

	/* tab content */
	.tab-content {
		flex: 1;
		overflow: auto;
	}

	/* basic panel */
	.basic-panel {
		height: 100%;
		overflow-y: auto;
	}

	/* hosts panel */
	.hosts-panel {
		display: grid;
		grid-template-columns: 280px 1fr;
		height: 100%;
	}

	.hosts-sidebar {
		border-right: 1px solid #e5e7eb;
		background: #f8fafc;
		display: flex;
		flex-direction: column;
		overflow: hidden;
	}

	:global(.dark) .hosts-sidebar {
		background: rgb(31 41 55 / 0.4);
		border-right-color: rgb(55 65 81 / 0.6);
	}

	.sidebar-header {
		display: flex;
		align-items: center;
		justify-content: space-between;
		padding: 1rem;
		border-bottom: 1px solid #e5e7eb;
	}

	:global(.dark) .sidebar-header {
		border-bottom-color: rgb(55 65 81 / 0.6);
	}

	.sidebar-title {
		font-size: 0.75rem;
		font-weight: 600;
		text-transform: uppercase;
		letter-spacing: 0.05em;
		color: #64748b;
	}

	:global(.dark) .sidebar-title {
		color: #9ca3af;
	}

	/* host detail */
	.host-detail {
		display: flex;
		flex-direction: column;
		overflow: hidden;
	}

	.detail-header {
		display: flex;
		align-items: center;
		justify-content: space-between;
		padding: 1rem 1.5rem;
		background: #f8fafc;
		border-bottom: 1px solid #e5e7eb;
	}

	:global(.dark) .detail-header {
		background: rgb(31 41 55 / 0.4);
		border-bottom-color: rgb(55 65 81 / 0.6);
	}

	.detail-title {
		display: flex;
		align-items: center;
		gap: 0.5rem;
		font-size: 1rem;
		font-weight: 500;
	}

	.domain-label {
		color: #0284c7;
	}

	.arrow {
		color: #94a3b8;
	}

	.target-label {
		color: #64748b;
	}

	:global(.dark) .target-label {
		color: #9ca3af;
	}

	.detail-actions {
		display: flex;
		gap: 0.5rem;
	}

	/* sub tabs */
	.sub-tabs {
		display: flex;
		gap: 0.25rem;
		padding: 0.5rem 1rem;
		background: white;
		border-bottom: 1px solid #e5e7eb;
	}

	:global(.dark) .sub-tabs {
		background: rgb(17 24 39 / 0.6);
		border-bottom-color: rgb(55 65 81 / 0.6);
	}

	.sub-tab {
		display: flex;
		align-items: center;
		gap: 0.375rem;
		padding: 0.5rem 0.875rem;
		font-size: 0.8125rem;
		font-weight: 500;
		color: #64748b;
		background: transparent;
		border: none;
		border-radius: 0.375rem;
		cursor: pointer;
		transition: all 0.15s ease;
	}

	.sub-tab:hover {
		background: #f1f5f9;
		color: #475569;
	}

	:global(.dark) .sub-tab:hover {
		background: rgb(55 65 81 / 0.4);
		color: #d1d5db;
	}

	.sub-tab.active {
		background: #0284c7;
		color: white;
	}

	:global(.dark) .sub-tab.active {
		background: #0369a1;
	}

	.sub-badge {
		display: inline-flex;
		align-items: center;
		justify-content: center;
		min-width: 1.125rem;
		height: 1.125rem;
		padding: 0 0.25rem;
		font-size: 0.625rem;
		font-weight: 600;
		background: #e2e8f0;
		color: #475569;
		border-radius: 9999px;
	}

	:global(.dark) .sub-badge {
		background: rgb(55 65 81 / 0.6);
		color: #d1d5db;
	}

	.sub-tab.active .sub-badge {
		background: rgba(255, 255, 255, 0.2);
		color: white;
	}

	.sub-content {
		flex: 1;
		overflow-y: auto;
		padding: 1rem;
	}

	/* host settings grid */
	.host-settings {
		max-width: 600px;
	}

	/* rules container */
	.rules-container {
		display: flex;
		flex-direction: column;
		gap: 1rem;
	}

	.rule-card {
		background: #f8fafc;
		border: 1px solid #e5e7eb;
		border-radius: 0.5rem;
	}

	:global(.dark) .rule-card {
		background: rgb(31 41 55 / 0.4);
		border-color: rgb(55 65 81 / 0.6);
	}

	.rule-header {
		display: flex;
		align-items: center;
		justify-content: space-between;
		padding: 0.75rem 1rem;
		background: white;
		border-bottom: 1px solid #e5e7eb;
		border-radius: 0.5rem 0.5rem 0 0;
	}

	:global(.dark) .rule-header {
		background: rgb(17 24 39 / 0.4);
		border-bottom-color: rgb(55 65 81 / 0.6);
	}

	.rule-name {
		font-size: 0.875rem;
		font-weight: 500;
		color: #1e293b;
	}

	:global(.dark) .rule-name {
		color: #f1f5f9;
	}

	.rule-grid {
		display: grid;
		grid-template-columns: repeat(2, 1fr);
		gap: 0.25rem 1rem;
		padding: 1rem;
	}

	/* buttons */
	.add-btn {
		display: inline-flex;
		align-items: center;
		gap: 0.375rem;
		padding: 0.5rem 1rem;
		font-size: 0.875rem;
		font-weight: 500;
		color: white;
		background: #0284c7;
		border: none;
		border-radius: 0.375rem;
		cursor: pointer;
		transition: background 0.15s ease;
	}

	.add-btn:hover {
		background: #0369a1;
	}

	.add-btn.small {
		padding: 0.375rem 0.5rem;
	}

	.add-btn svg {
		width: 1rem;
		height: 1rem;
	}

	.add-rule-btn {
		display: flex;
		align-items: center;
		justify-content: center;
		gap: 0.5rem;
		width: 100%;
		padding: 0.75rem;
		font-size: 0.875rem;
		font-weight: 500;
		color: #64748b;
		background: transparent;
		border: 2px dashed #e2e8f0;
		border-radius: 0.5rem;
		cursor: pointer;
		transition: all 0.15s ease;
	}

	:global(.dark) .add-rule-btn {
		border-color: rgb(55 65 81 / 0.6);
		color: #9ca3af;
	}

	.add-rule-btn:hover {
		background: #f1f5f9;
		border-color: #94a3b8;
		color: #475569;
	}

	:global(.dark) .add-rule-btn:hover {
		background: rgb(55 65 81 / 0.4);
	}

	.add-rule-btn svg {
		width: 1rem;
		height: 1rem;
	}

	.icon-btn {
		display: flex;
		align-items: center;
		justify-content: center;
		width: 2rem;
		height: 2rem;
		padding: 0;
		background: transparent;
		border: 1px solid #e2e8f0;
		border-radius: 0.375rem;
		color: #64748b;
		cursor: pointer;
		transition: all 0.15s ease;
	}

	:global(.dark) .icon-btn {
		border-color: rgb(55 65 81 / 0.6);
		color: #9ca3af;
	}

	.icon-btn:hover {
		background: #f1f5f9;
		border-color: #94a3b8;
	}

	:global(.dark) .icon-btn:hover {
		background: rgb(55 65 81 / 0.4);
	}

	.icon-btn.danger:hover {
		background: #fef2f2;
		border-color: #fca5a5;
		color: #dc2626;
	}

	:global(.dark) .icon-btn.danger:hover {
		background: rgb(127 29 29 / 0.3);
		border-color: #f87171;
		color: #f87171;
	}

	.icon-btn.small {
		width: 1.5rem;
		height: 1.5rem;
	}

	.icon-btn svg {
		width: 1rem;
		height: 1rem;
	}

	/* empty state */
	.empty-state {
		display: flex;
		flex-direction: column;
		align-items: center;
		justify-content: center;
		padding: 3rem;
		text-align: center;
	}

	.empty-state.small {
		padding: 1.5rem;
	}

	.empty-state p {
		color: #64748b;
		margin-bottom: 1rem;
	}

	:global(.dark) .empty-state p {
		color: #9ca3af;
	}

	.empty-icon {
		width: 3rem;
		height: 3rem;
		color: #cbd5e1;
		margin-bottom: 1rem;
	}

	:global(.dark) .empty-icon {
		color: #4b5563;
	}

	/* global panel */
	.global-panel {
		display: flex;
		flex-direction: column;
		gap: 1.5rem;
		padding: 1.5rem;
	}

	.global-grid {
		display: grid;
		grid-template-columns: repeat(2, 1fr);
		gap: 1.5rem;
	}

	.global-section {
		background: #f8fafc;
		border: 1px solid #e5e7eb;
		border-radius: 0.5rem;
		padding: 1rem;
	}

	:global(.dark) .global-section {
		background: rgb(31 41 55 / 0.4);
		border-color: rgb(55 65 81 / 0.6);
	}

	.section-title {
		display: flex;
		align-items: center;
		gap: 0.5rem;
		font-size: 0.875rem;
		font-weight: 600;
		color: #475569;
		margin-bottom: 1rem;
		padding-bottom: 0.75rem;
		border-bottom: 1px solid #e5e7eb;
	}

	:global(.dark) .section-title {
		color: #d1d5db;
		border-bottom-color: rgb(55 65 81 / 0.6);
	}

	.section-icon {
		width: 1.125rem;
		height: 1.125rem;
		color: #0284c7;
	}

	:global(.dark) .section-icon {
		color: #38bdf8;
	}

	.section-content {
		display: flex;
		flex-direction: column;
		gap: 0.25rem;
	}

	/* global rules */
	.global-rules {
		background: #f8fafc;
		border: 1px solid #e5e7eb;
		border-radius: 0.5rem;
		overflow: hidden;
	}

	:global(.dark) .global-rules {
		background: rgb(31 41 55 / 0.4);
		border-color: rgb(55 65 81 / 0.6);
	}

	.rules-tabs {
		display: flex;
		gap: 0.25rem;
		padding: 0.75rem;
		background: white;
		border-bottom: 1px solid #e5e7eb;
	}

	:global(.dark) .rules-tabs {
		background: rgb(17 24 39 / 0.4);
		border-bottom-color: rgb(55 65 81 / 0.6);
	}

	.rules-tab {
		display: flex;
		align-items: center;
		gap: 0.375rem;
		padding: 0.5rem 0.875rem;
		font-size: 0.8125rem;
		font-weight: 500;
		color: #64748b;
		background: transparent;
		border: none;
		border-radius: 0.375rem;
		cursor: pointer;
		transition: all 0.15s ease;
	}

	.rules-tab:hover {
		background: #f1f5f9;
		color: #475569;
	}

	:global(.dark) .rules-tab:hover {
		background: rgb(55 65 81 / 0.4);
		color: #d1d5db;
	}

	.rules-tab.active {
		background: #0284c7;
		color: white;
	}

	:global(.dark) .rules-tab.active {
		background: #0369a1;
	}

	.rules-content {
		padding: 1rem;
	}

	/* checkbox styling */
	.checkbox-label {
		display: flex;
		align-items: center;
		gap: 0.5rem;
		cursor: pointer;
		padding: 0.5rem 0;
	}

	.checkbox-input {
		width: 1.25rem;
		height: 1.25rem;
		border: 2px solid #cbd5e1;
		border-radius: 0.25rem;
		background: #f8fafc;
		cursor: pointer;
		accent-color: #0284c7;
	}

	:global(.dark) .checkbox-input {
		border-color: rgb(55 65 81 / 0.6);
		background: rgb(17 24 39 / 0.6);
	}

	.checkbox-input:checked {
		background: #0284c7;
		border-color: #0284c7;
	}

	:global(.dark) .checkbox-input:checked {
		background: #0369a1;
		border-color: #0369a1;
	}

	.checkbox-input:focus {
		outline: none;
		border-color: #94a3b8;
	}

	:global(.dark) .checkbox-input:focus {
		border-color: #38bdf8;
	}

	.checkbox-text {
		font-size: 0.875rem;
		font-weight: 500;
		color: #475569;
	}

	:global(.dark) .checkbox-text {
		color: #9ca3af;
	}

	/* field error styles */
	.field-error {
		display: block;
		font-size: 0.75rem;
		color: #dc2626;
		margin-top: 0.25rem;
	}

	:global(.dark) .field-error {
		color: #f87171;
	}

	/* headers section styles */
	.headers-section {
		display: flex;
		flex-direction: column;
		gap: 0.5rem;
	}

	.headers-label {
		display: flex;
		align-items: center;
		gap: 0.5rem;
		font-size: 0.875rem;
		font-weight: 500;
		color: #475569;
	}

	:global(.dark) .headers-label {
		color: #9ca3af;
	}

	.headers-list {
		display: flex;
		flex-direction: column;
		gap: 0.5rem;
	}

	.header-row {
		display: flex;
		gap: 0.5rem;
		align-items: center;
	}

	.header-key-input,
	.header-value-input {
		flex: 1;
		padding: 0.375rem 0.5rem;
		font-size: 0.875rem;
		border-radius: 0.375rem;
		border: 1px solid transparent;
		background: #f1f5f9;
		color: #475569;
		transition: all 0.2s;
	}

	:global(.dark) .header-key-input,
	:global(.dark) .header-value-input {
		background: rgba(17, 24, 39, 0.6);
		border-color: rgba(55, 65, 81, 0.6);
		color: #d1d5db;
	}

	.header-key-input:focus,
	.header-value-input:focus {
		outline: none;
		border-color: #94a3b8;
		background: #f8fafc;
	}

	:global(.dark) .header-key-input:focus,
	:global(.dark) .header-value-input:focus {
		border-color: rgba(56, 189, 248, 0.5);
		background: rgba(55, 65, 81, 0.6);
	}

	.header-key-input {
		max-width: 180px;
	}

	.add-btn.tiny {
		width: 1.25rem;
		height: 1.25rem;
		padding: 0.125rem;
	}

	.icon-btn.tiny {
		width: 1.25rem;
		height: 1.25rem;
		padding: 0.125rem;
	}

	/* Variables selector styles */
	.variables-selector {
		display: flex;
		flex-wrap: wrap;
		gap: 0.375rem;
		padding: 0.5rem;
		background: #f8fafc;
		border-radius: 0.5rem;
		border: 1px solid #e2e8f0;
	}

	:global(.dark) .variables-selector {
		background: rgba(17, 24, 39, 0.4);
		border-color: rgba(55, 65, 81, 0.6);
	}

	.variable-chip {
		cursor: pointer;
	}

	.chip-text {
		display: inline-block;
		padding: 0.25rem 0.5rem;
		font-size: 0.75rem;
		font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace;
		border-radius: 0.25rem;
		background: #e2e8f0;
		color: #64748b;
		transition: all 0.15s;
		user-select: none;
	}

	:global(.dark) .chip-text {
		background: rgba(55, 65, 81, 0.6);
		color: #9ca3af;
	}

	.chip-text:hover {
		background: #cbd5e1;
		color: #475569;
	}

	:global(.dark) .chip-text:hover {
		background: rgba(75, 85, 99, 0.8);
		color: #d1d5db;
	}

	.chip-text.selected {
		background: #3b82f6;
		color: white;
	}

	:global(.dark) .chip-text.selected {
		background: #2563eb;
		color: white;
	}

	.chip-text.selected:hover {
		background: #2563eb;
	}

	:global(.dark) .chip-text.selected:hover {
		background: #1d4ed8;
	}

	.section-content code {
		font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace;
		font-size: 0.8125rem;
		padding: 0.125rem 0.375rem;
		background: #e2e8f0;
		border-radius: 0.25rem;
		color: #475569;
	}

	:global(.dark) .section-content code {
		background: rgba(55, 65, 81, 0.6);
		color: #d1d5db;
	}
</style>
