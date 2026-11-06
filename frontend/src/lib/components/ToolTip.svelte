<script context="module">
	let tooltipCounter = 0;
</script>

<script>
	import { tick } from 'svelte';

	const tooltipId = `tooltip-${++tooltipCounter}`;
	let showToolTip = false;
	let tooltipStyle = '';
	let trigger;
	let tip;

	const position = () => {
		if (!trigger || !tip) return;
		const r = trigger.getBoundingClientRect();
		const gap = 8;
		const vw = document.documentElement.clientWidth;
		const vh = window.innerHeight;
		const w = tip.offsetWidth;
		const h = tip.offsetHeight;
		const left = Math.max(gap, Math.min(r.left + r.width / 2 - w / 2, vw - w - gap));
		let top = r.bottom + gap;
		if (top + h > vh - gap) {
			top = Math.max(gap, r.top - h - gap);
		}
		tooltipStyle = `left: ${left}px; top: ${top}px;`;
	};

	const open = async () => {
		showToolTip = true;
		await tick();
		position();
	};

	const close = () => {
		showToolTip = false;
	};
</script>

<svelte:window on:scroll|capture={() => showToolTip && close()} on:resize={close} />

<span class="inline-flex items-center ml-1">
	<button
		bind:this={trigger}
		type="button"
		class="pc-focus inline-flex items-center justify-center p-1 rounded-full group"
		aria-label="More information"
		data-tooltip-trigger
		aria-describedby={tooltipId}
		aria-expanded={showToolTip}
		on:mouseenter={open}
		on:mouseleave={close}
		on:focus={open}
		on:blur={close}
		on:click|preventDefault|stopPropagation={open}
		on:keydown={(e) => {
			if (e.key === 'Escape' && showToolTip) {
				e.stopPropagation();
				close();
			}
		}}
	>
		<span
			aria-hidden="true"
			class="inline-flex items-center justify-center rounded-full border border-gray-400 text-gray-500 w-4 h-4 text-[10px] leading-none font-medium group-hover:border-gray-500 group-hover:text-gray-600 dark:border-gray-500 dark:text-gray-400 dark:group-hover:border-gray-300 dark:group-hover:text-gray-200 transition-colors duration-200"
			>?</span
		>
	</button>
	<span
		bind:this={tip}
		id={tooltipId}
		role="tooltip"
		hidden={!showToolTip}
		class="fixed z-[60] w-max max-w-[min(20rem,calc(100vw-1rem))] bg-gray-700 dark:bg-gray-900 text-white text-sm font-normal normal-case tracking-normal text-left leading-snug px-3 py-2 rounded-md shadow-xl ring-1 ring-black/10 dark:ring-gray-700 whitespace-normal break-words pointer-events-none"
		style={tooltipStyle}
	>
		<slot />
	</span>
</span>
