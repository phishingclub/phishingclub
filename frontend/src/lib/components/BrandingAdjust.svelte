<script>
	// Compact adjustment controls for a branding image: fit, size, background and
	// position. Uses the app's TextFieldSelect so the dropdowns match the rest of
	// the settings UI. Emits 'input' live while dragging the size slider so the
	// preview updates, and 'change' when a value is committed so it can be saved.
	import { createEventDispatcher } from 'svelte';
	import TextFieldSelect from '$lib/components/TextFieldSelect.svelte';

	export let display = {};
	export let disabled = false;

	const dispatch = createEventDispatcher();
	// unique id prefix so several instances on the page do not clash
	const uid = Math.random().toString(36).slice(2, 8);

	const fitOptions = [
		{ value: 'contain', label: 'Contain' },
		{ value: 'cover', label: 'Cover' },
		{ value: 'fill', label: 'Stretch' }
	];
	const backgroundOptions = [
		{ value: 'none', label: 'Transparent' },
		{ value: 'light', label: 'Light' },
		{ value: 'dark', label: 'Dark' }
	];
	const positionXOptions = [
		{ value: 'left', label: 'Left' },
		{ value: 'center', label: 'Center' },
		{ value: 'right', label: 'Right' }
	];
	const positionYOptions = [
		{ value: 'top', label: 'Top' },
		{ value: 'center', label: 'Center' },
		{ value: 'bottom', label: 'Bottom' }
	];

	let fit = 'contain';
	let scale = 100;
	let background = 'none';
	let positionX = 'center';
	let positionY = 'center';

	// keep the controls in sync with the incoming settings
	$: sync(display);
	function sync(d) {
		fit = d.fit || 'contain';
		scale = d.scale ?? 100;
		background = d.background || 'none';
		positionX = d.positionX || 'center';
		positionY = d.positionY || 'center';
	}

	const current = () => ({ fit, scale: Number(scale), background, positionX, positionY });
	const live = () => dispatch('input', current());
	const commit = () => dispatch('change', current());
	const selectChanged = () => {
		live();
		commit();
	};

	const labelClass =
		'text-xs font-semibold text-slate-500 dark:text-gray-400 mb-1 transition-colors duration-200';
</script>

<div class="mt-3 space-y-1">
	<div class="grid grid-cols-2 gap-3">
		<TextFieldSelect
			id="{uid}-fit"
			size="small"
			bind:value={fit}
			options={fitOptions}
			onSelect={selectChanged}>Fit</TextFieldSelect
		>
		<TextFieldSelect
			id="{uid}-bg"
			size="small"
			bind:value={background}
			options={backgroundOptions}
			onSelect={selectChanged}>Background</TextFieldSelect
		>
		<TextFieldSelect
			id="{uid}-px"
			size="small"
			bind:value={positionX}
			options={positionXOptions}
			onSelect={selectChanged}>Horizontal</TextFieldSelect
		>
		<TextFieldSelect
			id="{uid}-py"
			size="small"
			bind:value={positionY}
			options={positionYOptions}
			onSelect={selectChanged}>Vertical</TextFieldSelect
		>
	</div>

	<label class="flex flex-col pt-1">
		<span class={labelClass}>Size {scale}%</span>
		<input
			type="range"
			min="25"
			max="200"
			step="5"
			bind:value={scale}
			on:input={live}
			on:change={commit}
			{disabled}
			class="w-full accent-cta-blue dark:accent-highlight-blue"
		/>
	</label>

	<div class="flex justify-end pt-1">
		<button
			type="button"
			{disabled}
			on:click={() => dispatch('reset')}
			class="text-xs font-semibold text-cta-blue dark:text-highlight-blue hover:underline disabled:opacity-50 transition-colors duration-200"
		>
			Reset adjustments
		</button>
	</div>
</div>
