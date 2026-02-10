<script>
	import { autoRefreshStore, getPageAutoRefresh, setPageAutoRefresh } from '$lib/store/autoRefresh';
	import { onDestroy, onMount } from 'svelte';
	import { page } from '$app/stores';
	import TextFieldSelect from './TextFieldSelect.svelte';
	import { BiMap } from '$lib/utils/maps';
	import { activeFormElement } from '$lib/store/activeFormElement';

	export let onRefresh;
	export let isLoading = false;
	export let pageId = JSON.stringify($page.route);

	const options = new BiMap({
		Disabled: '0',
		'5s': '5000',
		'30s': '30000',
		'1m': '60000',
		'5m': '300000'
	});

	let intervalId;
	let settings;

	$: {
		if (pageId) {
			settings = getPageAutoRefresh(pageId);
			autoRefreshStore.set(settings);
		}
	}

	function handleIntervalChange(optKey) {
		const value = Number(options.byKey(optKey));
		autoRefreshStore.setEnabled(value > 0);
		autoRefreshStore.setInterval(value);
	}

	const startAutoRefresh = () => {
		stopAutoRefresh();
		if ($autoRefreshStore.enabled && $autoRefreshStore.interval > 0) {
			intervalId = setInterval(async () => {
				// skip refresh if disabled, loading, or a dropdown is open
				if (!$autoRefreshStore.enabled || isLoading || $activeFormElement !== null) return;
				await onRefresh();
			}, $autoRefreshStore.interval);
		}
	};

	const stopAutoRefresh = () => {
		if (intervalId) {
			clearInterval(intervalId);
			intervalId = null;
		}
	};

	$: if ($autoRefreshStore) {
		startAutoRefresh();
		if (pageId) {
			setPageAutoRefresh(pageId, $autoRefreshStore);
		}
	}

	onMount(() => {
		startAutoRefresh();
	});

	onDestroy(() => {
		stopAutoRefresh();
	});
</script>

<div class="absolute top-0 right-0 min-w-[180px]">
	<div class="flex items-center gap-2">
		<span
			class="font-semibold text-slate-600 dark:text-gray-300 transition-colors duration-200 whitespace-nowrap"
			>Auto-Refresh</span
		>
		<div class="relative">
			<TextFieldSelect
				id="autoRefresh"
				value={$autoRefreshStore.enabled
					? options.byValue($autoRefreshStore.interval.toString())
					: 'Disabled'}
				onSelect={handleIntervalChange}
				options={options.keys()}
				inline={true}
				size={'small'}
			></TextFieldSelect>
		</div>
	</div>
</div>
