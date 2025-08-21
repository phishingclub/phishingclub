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
		showIsLoading();
		visible = false;
		const c = companies.find((c) => c.name === selectedCompany);
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
	<main class="flex flex-col h-full">
		<!-- Company Selection Section -->
		<div class="flex-grow p-6">
			<!--
			<h2 class="text-lg mb-4 text-pc-darkblue">
				Viewing as: {context?.companyName ?? 'Shared'}
			</h2>
			-->
			{#if isLoadingCompanies}
				<div class="flex items-center justify-center py-8">
					<div class="text-gray-500">Loading companies...</div>
				</div>
			{:else}
				<TextFieldSelect id={'context'} bind:value={selectedCompany} options={companyNameList}>
					Company
				</TextFieldSelect>
			{/if}
		</div>

		<!-- Button Section -->
		<div class="border-t p-6 mt-36 flex flex-wrap gap-4 justify-end">
			{#if inContext}
				<button
					type="button"
					class="bg-slate-400 hover:bg-slate-300 text-sm mr-2 uppercase font-bold px-4 py-2 text-white rounded-md disabled:opacity-50 disabled:cursor-not-allowed"
					disabled={isLoadingCompanies}
					on:click={onClickSwitchToAdministratorContext}
				>
					Shared view
				</button>
			{/if}

			<button
				type="submit"
				class="bg-cta-blue hover:bg-blue-700 text-sm uppercase font-bold px-4 py-2 text-white rounded-md disabled:opacity-50 disabled:cursor-not-allowed"
				disabled={isLoadingCompanies || !selectedCompany}
				on:click={onClickSwitch}
			>
				Switch
			</button>
		</div>
	</main>
</Modal>
