<script>
	// asks before a paused program is resumed
	import Alert from '$lib/components/Alert.svelte';

	export let visible = false;
	export let name = '';
	/** @type {() => Promise<any>} */
	export let onConfirm;
</script>

<Alert headline="resume program" tone="affirmative" ok="Resume" bind:visible {onConfirm}>
	<div>
		<p class="mb-2 text-gray-700 dark:text-gray-200 transition-colors duration-200">
			Resume <strong>{name}</strong>? Resuming continues the program from where it stopped.
		</p>
		<ul
			class="list-disc ml-8 mt-4 mb-4 text-gray-700 dark:text-gray-200 transition-colors duration-200"
		>
			<li>Held sends are spread over the sending days and hours again</li>
			<li>Recipients who came due while paused get a new time</li>
			<li>New group members are enrolled, recipients who left are removed</li>
		</ul>
	</div>
</Alert>
