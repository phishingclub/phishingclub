<script>
	// asks before a program is paused, as on the campaign page
	import Alert from '$lib/components/Alert.svelte';

	export let visible = false;
	export let name = '';
	// the program has an end date it still closes at
	export let hasEndDate = false;
	/** @type {() => Promise<any>} */
	export let onConfirm;
</script>

<Alert headline="pause program" tone="affirmative" ok="Pause" bind:visible {onConfirm}>
	<div>
		<p class="mb-2 text-gray-700 dark:text-gray-200 transition-colors duration-200">
			Pause <strong>{name}</strong>? Pausing stops the program from sending.
		</p>
		<ul
			class="list-disc ml-8 mt-4 mb-4 text-gray-700 dark:text-gray-200 transition-colors duration-200"
		>
			<li>Templates already sent are not affected</li>
			<li>Landing pages and tracking stay live</li>
			<li>Sends already planned are held until you resume</li>
			<li>Group changes are picked up when you resume</li>
			<li>Automated reports keep their schedule</li>
			{#if hasEndDate}
				<li>The program still closes at its end date</li>
			{/if}
		</ul>
	</div>
</Alert>
