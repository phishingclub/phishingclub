<script context="module">
	let instanceCount = 0;
</script>

<script>
	// one heading style for sections inside modal forms
	export let title = '';
	// extra classes for the section wrapper, such as a width cap
	export let css = '';

	const headingId = `form-section-${++instanceCount}`;
</script>

<section class="w-full mb-6 last:mb-0 {css}" aria-labelledby={headingId}>
	<h3
		id={headingId}
		class="text-base font-semibold text-pc-darkblue dark:text-white pb-2 mb-3 border-b border-gray-200 dark:border-gray-700 transition-colors duration-200"
	>
		{title}
	</h3>
	<div class="space-y-1">
		<slot />
	</div>
</section>
