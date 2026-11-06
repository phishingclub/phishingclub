<script>
	import { onMount, onDestroy } from 'svelte';
	import { displayMode, DISPLAY_MODE } from '$lib/store/displayMode';
	import { AppStateService } from '$lib/service/appState';
	import HeadTitle from '$lib/components/HeadTitle.svelte';
	import Headline from '$lib/components/Headline.svelte';
	import TabBar from '$lib/components/TabBar.svelte';
	import General from './panels/General.svelte';
	import Access from './panels/Access.svelte';
	import Scim from './panels/Scim.svelte';
	import Data from './panels/Data.svelte';
	import Reports from './panels/Reports.svelte';
	import RedTeam from './panels/RedTeam.svelte';
	import System from './panels/System.svelte';
	import Branding from './panels/Branding.svelte';
	import IPData from './panels/IPData.svelte';
	import Reporting from './panels/Reporting.svelte';

	// Red Team panel is only relevant in red team phishing (blackbox) mode.
	// Reporting holds the global reporting inbox, so it is only shown in the
	// global context. A company's reporting inbox is in its company settings.
	$: tabs = [
		{ id: 'general', label: 'General', component: General },
		{ id: 'access', label: 'Access', component: Access },
		{ id: 'scim', label: 'SCIM', component: Scim },
		{ id: 'data', label: 'Data', component: Data },
		{ id: 'ipdata', label: 'IP Data', component: IPData },
		{ id: 'reports', label: 'PDF Reports', component: Reports },
		...(!companyContext.isCompany
			? [{ id: 'reporting', label: 'Reporting Inbox', component: Reporting }]
			: []),
		...($displayMode === DISPLAY_MODE.BLACKBOX
			? [{ id: 'redteam', label: 'Red Team', component: RedTeam }]
			: []),
		{ id: 'branding', label: 'Branding', component: Branding },
		{ id: 'system', label: 'System', component: System }
	];

	let active = 'general';
	// a tab that is not shown in the current context, or not yet, is displayed
	// as General without forgetting the requested tab
	$: shown = tabs.some((t) => t.id === active) ? active : 'general';

	$: ActiveComponent = (tabs.find((t) => t.id === shown) || tabs[0]).component;

	// company context: when viewing as a company, link to that company's settings
	const appState = AppStateService.instance;
	let companyContext = { isCompany: false, companyID: null };
	let unsubAppState;

	onMount(() => {
		// a tab that only appears once the mode or context is known is kept and
		// shown as soon as it is there
		const hash = window.location.hash.replace('#', '');
		if (hash) {
			active = hash;
		}
		unsubAppState = appState.subscribe((s) => {
			companyContext = {
				isCompany: s.context.current === AppStateService.CONTEXT.COMPANY && !!s.context.companyID,
				companyID: s.context.companyID
			};
		});
	});

	onDestroy(() => {
		if (unsubAppState) unsubAppState();
	});

	const selectTab = (id) => {
		active = id;
		// replace the current history entry instead of pushing a new one, so
		// switching tabs does not stack up history and the back button leaves
		// the page rather than walking back through each visited tab
		history.replaceState(history.state, '', `#${id}`);
	};
</script>

<HeadTitle title="Settings" />
<main class="pb-8">
	<Headline docSlug="settings">Settings</Headline>

	<TabBar items={tabs} active={shown} label="Settings sections" on:select={(e) => selectTab(e.detail)}>
		<svelte:fragment slot="end">
			{#if companyContext.isCompany}
				<a
					href="/company/{companyContext.companyID}"
					class="pc-focus rounded-md flex items-center gap-1.5 px-2 sm:px-4 py-3 text-sm font-medium text-cta-blue dark:text-highlight-blue hover:underline whitespace-nowrap"
				>
					Company settings
					<svg
						class="w-4 h-4"
						fill="none"
						stroke="currentColor"
						viewBox="0 0 24 24"
						aria-hidden="true"
					>
						<path
							stroke-linecap="round"
							stroke-linejoin="round"
							stroke-width="2"
							d="M10 6H6a2 2 0 00-2 2v10a2 2 0 002 2h10a2 2 0 002-2v-4M14 4h6m0 0v6m0-6L10 14"
						/>
					</svg>
				</a>
			{/if}
		</svelte:fragment>
	</TabBar>

	<div class="max-w-7xl">
		<svelte:component this={ActiveComponent} />
	</div>
</main>
