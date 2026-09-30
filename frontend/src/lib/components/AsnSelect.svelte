<script>
	import { onMount, onDestroy } from 'svelte';
	import { api } from '$lib/api/apiProxy.js';
	import ToolTip from './ToolTip.svelte';

	// value is an array of ASN number strings, for example ["15169", "13335"]
	export let value = [];
	export let toolTipText = '';
	export let installed = true;
	export let disabled = false;
	export let placeholder = 'Search by number or name...';

	let inputValue = '';
	let inputElement;
	let dropdownElement;
	let showDropdown = false;
	let results = [];
	let searchTimer = null;
	let dropdownPosition = { top: 0, left: 0, width: 0 };

	// details keyed by asn number string, filled from search and resolve
	let details = {};
	// asn numbers that are configured but absent from the installed data
	let orphans = new Set();

	const parseNum = (s) => {
		let v = String(s).toLowerCase().trim();
		v = v.replace(/^asn/, '').replace(/^as/, '').trim();
		if (v === '' || !/^\d+$/.test(v)) {
			return null;
		}
		return v;
	};

	const label = (raw) => {
		const n = parseNum(raw);
		if (n && details[n] && (details[n].name || details[n].handle)) {
			return `AS${n} ${details[n].name || details[n].handle}`;
		}
		return n ? `AS${n}` : String(raw);
	};

	const resolveSelected = async () => {
		if (!installed || !value || value.length === 0) {
			orphans = new Set();
			return;
		}
		try {
			const res = await api.ipdata.resolveASN(value);
			if (res.success && res.data) {
				const found = new Set();
				for (const r of res.data.results || []) {
					details[String(r.asn)] = r;
					found.add(String(r.asn));
				}
				details = details;
				const next = new Set();
				for (const raw of value) {
					const n = parseNum(raw);
					if (n && !found.has(n)) next.add(n);
				}
				orphans = next;
			}
		} catch (e) {
			console.error('failed to resolve ASNs', e);
		}
	};

	const updateDropdownPosition = () => {
		if (inputElement) {
			const rect = inputElement.getBoundingClientRect();
			const spaceBelow = window.innerHeight - rect.bottom;
			const spaceAbove = rect.top;
			// open upward when there is little room below and more room above, so
			// the list stays on screen when the field is near the bottom
			const openUp = spaceBelow < 260 && spaceAbove > spaceBelow;
			dropdownPosition = {
				left: rect.left,
				width: rect.width,
				openUp,
				top: rect.bottom + 4,
				bottom: window.innerHeight - rect.top + 4
			};
		}
	};

	const runSearch = () => {
		clearTimeout(searchTimer);
		const q = inputValue.trim();
		if (q === '') {
			results = [];
			showDropdown = false;
			return;
		}
		searchTimer = setTimeout(async () => {
			try {
				const res = await api.ipdata.searchASN(q, 50);
				if (res.success && res.data) {
					results = res.data.results || [];
				} else {
					results = [];
				}
				updateDropdownPosition();
				showDropdown = results.length > 0;
			} catch (e) {
				console.error('failed to search ASNs', e);
			}
		}, 200);
	};

	const selectResult = (r) => {
		const n = String(r.asn);
		details[n] = r;
		details = details;
		const current = (value || []).map(parseNum);
		if (!current.includes(n)) {
			value = [...(value || []), n];
		}
		inputValue = '';
		results = [];
		showDropdown = false;
		setTimeout(() => inputElement?.focus(), 0);
	};

	const addTyped = () => {
		const n = parseNum(inputValue);
		if (!n) return;
		const current = (value || []).map(parseNum);
		if (!current.includes(n)) {
			value = [...(value || []), n];
			resolveSelected();
		}
		inputValue = '';
		results = [];
		showDropdown = false;
	};

	// add every ASN currently shown in the results, for example all M247 systems
	const addAll = () => {
		const current = new Set((value || []).map(parseNum));
		const next = [...(value || [])];
		for (const r of results) {
			const n = String(r.asn);
			details[n] = r;
			if (!current.has(n)) {
				next.push(n);
				current.add(n);
			}
		}
		details = details;
		value = next;
		inputValue = '';
		results = [];
		showDropdown = false;
		setTimeout(() => inputElement?.focus(), 0);
	};

	const remove = (raw) => {
		const n = parseNum(raw);
		value = (value || []).filter((v) => parseNum(v) !== n);
		orphans.delete(n);
		orphans = orphans;
	};

	const clearAll = () => {
		value = [];
		orphans = new Set();
		inputValue = '';
		inputElement?.focus();
	};

	const closeDropdown = () => {
		showDropdown = false;
	};

	const handleBlur = () => {
		setTimeout(() => {
			const focused = document.activeElement;
			const container = inputElement?.closest('.asn-select-container');
			if (!container?.contains(focused)) {
				closeDropdown();
			}
		}, 100);
	};

	const handleKeyDown = (e) => {
		if (e.key === 'Enter') {
			e.preventDefault();
			if (parseNum(inputValue)) {
				addTyped();
			}
		} else if (e.key === 'Escape') {
			closeDropdown();
		}
	};

	const handleOutsideClick = (e) => {
		if (!showDropdown) return;
		const container = inputElement?.closest('.asn-select-container');
		if (container && !container.contains(e.target)) {
			closeDropdown();
		}
	};

	onMount(() => {
		resolveSelected();
		document.addEventListener('click', handleOutsideClick);
		window.addEventListener('scroll', updateDropdownPosition, true);
		window.addEventListener('resize', updateDropdownPosition);
		return () => {
			document.removeEventListener('click', handleOutsideClick);
			window.removeEventListener('scroll', updateDropdownPosition, true);
			window.removeEventListener('resize', updateDropdownPosition);
		};
	});

	onDestroy(() => {
		document.removeEventListener('click', handleOutsideClick);
		window.removeEventListener('scroll', updateDropdownPosition, true);
		window.removeEventListener('resize', updateDropdownPosition);
	});

	$: displayValue = value.length > 0 ? `${value.length} selected` : '';
</script>

<div class="flex justify-start flex-col asn-select-container">
	<div class="flex flex-col py-2 relative">
		<div class="flex items-center">
			<p
				class="font-semibold text-slate-600 dark:text-gray-400 py-2 transition-colors duration-200"
			>
				ASNs
			</p>
			{#if toolTipText.length > 0}
				<ToolTip>
					{toolTipText}
				</ToolTip>
			{/if}
			<div
				class="bg-gray-100 dark:bg-gray-800/60 ml-2 px-2 rounded-md transition-colors duration-200 h-6 flex items-center"
			>
				<p class="text-slate-600 dark:text-gray-400 text-xs">optional</p>
			</div>
		</div>
	</div>

	{#if !installed}
		<p class="text-sm text-slate-500 dark:text-gray-400">
			No ASN data downloaded. Enable it in
			<a class="text-cta-blue dark:text-highlight-blue hover:opacity-80" href="/settings#ipdata"
				>Settings, IP Data</a
			>.
		</p>
	{:else}
		<div class="relative">
			<div class="flex items-center relative w-60">
				<input
					bind:this={inputElement}
					type="text"
					autocomplete="off"
					bind:value={inputValue}
					on:input={runSearch}
					on:keydown={handleKeyDown}
					on:blur={handleBlur}
					{disabled}
					class="w-full relative rounded-md py-2 pr-10 text-gray-600 dark:text-gray-300 border border-transparent focus:outline-none focus:border-solid focus:border focus:border-slate-400 dark:focus:border-highlight-blue/80 focus:bg-gray-100 dark:focus:bg-gray-700/60 bg-grayblue-light dark:bg-gray-900/60 font-normal transition-colors duration-200"
					class:pl-10={showDropdown}
					class:pl-4={!showDropdown}
					placeholder={showDropdown ? '' : value.length > 0 ? displayValue : placeholder}
				/>
				{#if showDropdown}
					<img
						class="absolute w-4 left-3 select-none pointer-events-none z-10"
						src="/search-icon.svg"
						alt=""
						aria-hidden="true"
					/>
				{/if}
				{#if value.length > 0}
					<button
						class="absolute right-3 z-10"
						type="button"
						aria-label="Clear selection"
						on:click={(e) => {
							e.stopPropagation();
							clearAll();
						}}
					>
						<img class="w-4" src="/remove-value.svg" alt="" />
					</button>
				{/if}
			</div>

			{#if showDropdown}
				<div
					bind:this={dropdownElement}
					class="fixed z-[9999]"
					style="{dropdownPosition.openUp
						? `bottom: ${dropdownPosition.bottom}px`
						: `top: ${dropdownPosition.top}px`}; left: {dropdownPosition.left}px; width: {dropdownPosition.width}px;"
				>
					<ul
						class="bg-gray-100 dark:bg-gray-900 list-none z-[9999] rounded-md min-w-fit shadow-lg border border-gray-200 dark:border-gray-700/60 max-h-56 overflow-y-auto transition-colors duration-200"
					>
						{#if results.length > 1}
							<li role="none">
								<button
									type="button"
									class="w-full text-left rounded-md text-cta-blue dark:text-highlight-blue hover:bg-grayblue-dark dark:hover:bg-highlight-blue/40 hover:text-white py-2 px-2 cursor-pointer focus:bg-grayblue-dark dark:focus:bg-highlight-blue/40 focus:text-white focus:outline-none font-medium transition-colors duration-200"
									on:click|preventDefault|stopPropagation={addAll}
									on:blur={handleBlur}
								>
									Add all {results.length} results
								</button>
							</li>
						{/if}
						{#each results as r (r.asn)}
							<li role="none">
								<button
									type="button"
									class="w-full text-left bg-slate-100 dark:bg-gray-900 rounded-md text-gray-600 dark:text-gray-300 hover:bg-grayblue-dark dark:hover:bg-highlight-blue/40 hover:text-white py-2 px-2 cursor-pointer focus:bg-grayblue-dark dark:focus:bg-highlight-blue/40 focus:text-white focus:outline-none transition-colors duration-200"
									on:click|preventDefault|stopPropagation={() => selectResult(r)}
									on:blur={handleBlur}
								>
									AS{r.asn}
									{r.name || r.handle}{#if r.country}
										<span class="opacity-70"> · {r.country}</span>{/if}
								</button>
							</li>
						{/each}
					</ul>
				</div>
			{/if}

			{#if value.length > 0}
				<div class="flex flex-row flex-wrap mt-4 gap-2 max-h-48 overflow-y-auto">
					{#each value as raw (raw)}
						<button
							type="button"
							on:click|preventDefault={() => remove(raw)}
							class="flex flex-row items-center px-2 py-1 rounded-md text-xs transition-colors duration-200 flex-shrink-0 {orphans.has(
								parseNum(raw)
							)
								? 'bg-amber-100 dark:bg-amber-900/40 text-amber-800 dark:text-amber-200 hover:bg-amber-200 dark:hover:bg-amber-900/60'
								: 'bg-gray-100 dark:bg-gray-800/60 hover:bg-gray-200 dark:hover:bg-gray-700/80 text-gray-900 dark:text-gray-300'}"
							title={orphans.has(parseNum(raw))
								? 'This ASN is not in the installed data. The filter keeps it, but it will not match until the data is updated.'
								: `Remove ${label(raw)}`}
							style="max-width: 280px;"
						>
							<span class="truncate block" style="max-width: 250px;">{label(raw)}</span>
							{#if orphans.has(parseNum(raw))}
								<span class="ml-1" aria-hidden="true">⚠</span>
							{/if}
							<img class="w-3 ml-1 pointer-events-none flex-shrink-0" src="/delete2.svg" alt="" />
						</button>
					{/each}
				</div>
			{/if}
		</div>
	{/if}
</div>
