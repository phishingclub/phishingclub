<script>
	import ProxySvgIcon from '$lib/components/ProxySvgIcon.svelte';

	// what the recipient goes through, in order
	export let deliveryType = 'email'; // 'email' or 'api'
	export let deliveryName = '';
	export let beforeName = '';
	export let beforeIsProxy = false;
	export let landingName = '';
	export let landingIsProxy = false;
	export let afterName = '';
	export let afterIsProxy = false;
	export let redirectURL = '';
	// 'auto' is vertical on phones and horizontal from md up
	export let direction = 'auto';

	$: steps = [
		{
			label: deliveryType === 'api' ? 'API Sender' : 'Email',
			name: deliveryName,
			icon: deliveryType === 'api' ? 'api' : 'email'
		},
		{
			label: 'Before Landing',
			name: beforeName,
			icon: beforeIsProxy ? 'proxy' : 'page',
			optional: true
		},
		{
			label: landingIsProxy ? 'Landing Proxy' : 'Landing Page',
			name: landingName,
			icon: landingIsProxy ? 'proxy' : 'page',
			main: true
		},
		{
			label: 'After Landing',
			name: afterName,
			icon: afterIsProxy ? 'proxy' : 'page',
			optional: true
		},
		{ label: 'Redirect', name: redirectURL, icon: 'redirect', optional: true }
	];

	$: horizontal = direction === 'auto';

	const nodeClass = (step) => {
		if (!step.name) {
			return 'border-dashed border-gray-300 bg-white text-gray-400 dark:border-gray-600 dark:bg-gray-800 dark:text-gray-500';
		}
		if (step.main) {
			return 'border-cta-blue bg-cta-blue text-white shadow-sm dark:border-highlight-blue dark:bg-highlight-blue';
		}
		return 'border-cta-blue/40 bg-blue-50 text-cta-blue dark:border-highlight-blue/50 dark:bg-highlight-blue/15 dark:text-highlight-blue';
	};

	// the line to the next step is solid only when both ends are part of the flow
	const lineClass = (step, next) =>
		step.name && next?.name
			? 'border-gray-300 dark:border-gray-500'
			: 'border-dashed border-gray-300 dark:border-gray-600';
</script>

<ol
	aria-label="Recipient flow"
	class="flex flex-col {horizontal ? 'md:flex-row md:items-start' : ''} w-full"
>
	{#each steps as step, i}
		{@const next = steps[i + 1]}
		<li
			class="relative flex items-start gap-3 min-w-0 {i < steps.length - 1 ? 'pb-4' : ''}
				{horizontal ? 'md:flex-1 md:flex-col md:items-center md:gap-2 md:pb-0 md:text-center' : ''}"
		>
			{#if next}
				<span
					aria-hidden="true"
					class="absolute left-5 top-11 bottom-1 border-l {lineClass(step, next)}
						{horizontal
						? 'md:left-[calc(50%+1.5rem)] md:right-auto md:top-5 md:bottom-auto md:w-[calc(100%-3rem)] md:border-l-0 md:border-t'
						: ''}"
				></span>
			{/if}
			<span
				class="relative flex-shrink-0 w-10 h-10 rounded-full border-2 flex items-center justify-center transition-colors duration-200 {nodeClass(
					step
				)}"
				aria-hidden="true"
			>
				{#if step.icon === 'proxy'}
					<ProxySvgIcon size="w-5 h-5" title="" />
				{:else}
					<svg class="w-5 h-5" fill="none" viewBox="0 0 24 24" stroke-width="1.5" stroke="currentColor">
						{#if step.icon === 'email'}
							<path
								stroke-linecap="round"
								stroke-linejoin="round"
								d="M21.75 6.75v10.5a2.25 2.25 0 0 1-2.25 2.25h-15a2.25 2.25 0 0 1-2.25-2.25V6.75m19.5 0A2.25 2.25 0 0 0 19.5 4.5h-15a2.25 2.25 0 0 0-2.25 2.25m19.5 0v.243a2.25 2.25 0 0 1-1.07 1.916l-7.5 4.615a2.25 2.25 0 0 1-2.36 0L3.32 8.91a2.25 2.25 0 0 1-1.07-1.916V6.75"
							/>
						{:else if step.icon === 'api'}
							<path
								stroke-linecap="round"
								stroke-linejoin="round"
								d="M17.25 6.75 22.5 12l-5.25 5.25m-10.5 0L1.5 12l5.25-5.25m7.5-3-4.5 16.5"
							/>
						{:else if step.icon === 'redirect'}
							<path
								stroke-linecap="round"
								stroke-linejoin="round"
								d="M13.5 6H5.25A2.25 2.25 0 0 0 3 8.25v10.5A2.25 2.25 0 0 0 5.25 21h10.5A2.25 2.25 0 0 0 18 18.75V10.5m-10.5 6L21 3m0 0h-5.25M21 3v5.25"
							/>
						{:else}
							<path
								stroke-linecap="round"
								stroke-linejoin="round"
								d="M19.5 14.25v-2.625a3.375 3.375 0 0 0-3.375-3.375h-1.5A1.125 1.125 0 0 1 13.5 7.125v-1.5a3.375 3.375 0 0 0-3.375-3.375H8.25m2.25 0H5.625c-.621 0-1.125.504-1.125 1.125v17.25c0 .621.504 1.125 1.125 1.125h12.75c.621 0 1.125-.504 1.125-1.125V11.25a9 9 0 0 0-9-9Z"
							/>
						{/if}
					</svg>
				{/if}
			</span>
			<span class="min-w-0 flex-1 pt-0.5 {horizontal ? 'md:w-full md:px-2 md:pt-0' : ''}">
				<span
					class="block text-xs font-medium uppercase tracking-wide {step.name
						? 'text-gray-600 dark:text-gray-300'
						: 'text-gray-400 dark:text-gray-500'}"
				>
					{step.label}
				</span>
				{#if step.name}
					<span
						class="block truncate text-sm font-semibold text-pc-darkblue dark:text-white"
						title={step.name}
					>
						{step.name}
					</span>
				{:else}
					<span class="block text-sm italic text-gray-400 dark:text-gray-500">
						{step.optional ? 'Skipped' : 'Not selected'}
					</span>
				{/if}
			</span>
		</li>
	{/each}
</ol>
