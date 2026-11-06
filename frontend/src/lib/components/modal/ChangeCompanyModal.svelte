<script>
	import { addToast } from '$lib/store/toast';
	import Modal from '../Modal.svelte';
	import { fetchAllRows } from '$lib/utils/api-utils';
	import { AppStateService } from '$lib/service/appState';
	import { API } from '$lib/api/api';
	import { onMount } from 'svelte';
	import TextFieldSelect from '../TextFieldSelect.svelte';
	import { showIsLoading } from '$lib/store/loading';

	// services
	const appState = AppStateService.instance;
	const api = API.instance;

	// external
	export let visible = false;

	// local
	let context = null;
	let selectedCompany = null;
	let inContext = false;
	let companies = [];
	let companyNameList = [];
	let isLoadingCompanies = false;

	onMount(() => {
		const appStateUnsubscribe = appState.subscribe((s) => {
			// sync any changes to user and scope changes
			inContext = appState.isCompanyContext();
			context = {
				current: s.context.current,
				companyName: s.context.companyName
			};

			if (!context.companyName) {
				try {
					const ctxString = localStorage.getItem('context');
					const ctx = JSON.parse(ctxString);
					appState.setCompanyContext(ctx.id, ctx.name);
				} catch (e) {
					// do nothing failure to parse is expected if there is nothing
				}
			}
		});
		// refreshContexts();
		window.addEventListener('storage', handleStorageChanges);

		return () => {
			appStateUnsubscribe();
			window.removeEventListener('storage', handleStorageChanges);
		};
	});

	// force refresh in tabs/windows not in focus when changing context
	const handleStorageChanges = (e) => {
		if (e.key === 'context' && document.hidden) {
			location.reload();
		}
	};

	const refreshContexts = async () => {
		isLoadingCompanies = true;
		try {
			companies = await getContexts();
			companyNameList = companies.map((c) => c.name);
		} catch (e) {
			addToast('Failed to load contexts', 'Error');
			console.error('Failed to load contexts', e);
		} finally {
			isLoadingCompanies = false;
		}
	};

	const getContexts = async () => {
		if (!appState.isLoggedIn()) {
			return [];
		}
		let data = [];
		try {
			return await fetchAllRows((options) => {
				return api.company.getAll(options);
			});
		} catch (e) {
			addToast('Failed to load contexts', 'Error');
			console.error('failed to get companies for context', e);
		}
		return [];
	};

	const onClickSwitch = async () => {
		// selectedCompany is free text from the combobox, so it may not match a
		// company when the user types without picking from the list
		const c = companies.find((c) => c.name === selectedCompany);
		if (!c) {
			addToast('Select a company from the list', 'Error');
			return;
		}
		showIsLoading();
		visible = false;
		appState.setCompanyContext(c.id, c.name);
		localStorage.setItem('context', JSON.stringify({ id: c.id, name: c.name }));
		location.reload();
	};

	const onClickSwitchToAdministratorContext = async () => {
		showIsLoading();
		visible = false;
		appState.clearContext();
		localStorage.setItem('context', '');
		location.reload();
	};

	$: {
		if (visible) {
			refreshContexts();
		}
	}
</script>

<Modal headerText={'Change company'} bind:visible>
	<div class="flex flex-col h-full w-full sm:w-[28rem] max-w-full">
		<div class="flex-grow py-6">
			{#if isLoadingCompanies}
				<div class="flex items-center justify-center py-8">
					<div class="text-gray-500 dark:text-gray-500 transition-colors duration-200">
						Loading companies...
					</div>
				</div>
			{:else if companies.length === 0}
				<div class="flex flex-col items-center justify-center py-8 text-center">
					<div class="text-gray-600 dark:text-gray-300 mb-2 font-semibold transition-colors duration-200">
						No companies found.
					</div>
					<div class="text-sm text-gray-500 dark:text-gray-400 mb-4 transition-colors duration-200">
						You need to create a company first before you can switch to it.
					</div>
					<a
						href="/company/"
						class="pc-focus bg-cta-blue hover:bg-active-blue text-sm uppercase font-bold px-4 py-2 text-white rounded-md transition-colors duration-200"
						on:click={() => {
							visible = false;
						}}
					>
						Create Company
					</a>
				</div>
			{:else}
				<TextFieldSelect id={'context'} bind:value={selectedCompany} options={companyNameList}>
					Company
				</TextFieldSelect>
			{/if}
		</div>

		<!-- Button Section -->
		<div
			class="border-t-2 border-gray-200 dark:border-gray-700 py-4 mt-36 flex flex-wrap gap-2 justify-end transition-colors duration-200"
		>
			{#if inContext}
				<button
					type="button"
					class="pc-focus bg-transparent hover:bg-gray-100 border border-gray-300 text-slate-700 dark:bg-gray-700 dark:hover:bg-gray-600 dark:border-gray-500 dark:text-gray-100 text-sm uppercase font-bold px-4 py-2 rounded-md disabled:opacity-50 disabled:cursor-not-allowed transition-colors duration-200"
					disabled={isLoadingCompanies}
					on:click={onClickSwitchToAdministratorContext}
				>
					Shared view
				</button>
			{/if}

			<button
				type="submit"
				class="pc-focus min-w-[8rem] bg-cta-blue hover:bg-active-blue dark:hover:bg-highlight-blue text-sm uppercase font-bold px-6 py-2 text-white rounded-md disabled:opacity-50 disabled:cursor-not-allowed transition-colors duration-200"
				disabled={isLoadingCompanies || !selectedCompany}
				on:click={onClickSwitch}
			>
				Switch
			</button>
		</div>
	</div>
</Modal>
