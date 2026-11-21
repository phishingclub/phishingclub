<script>
	// asks before a program is closed for good
	import Alert from '$lib/components/Alert.svelte';

	export let visible = false;
	export let name = '';
	/** @type {() => Promise<any>} */
	export let onConfirm;
</script>

<Alert headline="close program" ok="Close program" bind:visible {onConfirm}>
	<div>
		<p class="mt-4 text-gray-700 dark:text-gray-200 transition-colors duration-200">
			Close <strong>{name}</strong>?
		</p>
		<ul
			class="list-disc ml-8 mt-4 mb-4 text-gray-700 dark:text-gray-200 transition-colors duration-200"
		>
			<li>Stop enrolling and sending</li>
			<li>Links in sent templates and landing pages will stop working</li>
			<li>Results are kept</li>
			<li>A closed program cannot be resumed or edited</li>
		</ul>
	</div>
</Alert>
