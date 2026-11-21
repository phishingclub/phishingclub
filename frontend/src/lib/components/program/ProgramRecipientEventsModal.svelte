<script>
	// The events of one recipient in a program, in the same modal as the events
	// of a campaign recipient, with the template of each send. Opened with
	// open(programID, recipientID, title).
	import { onMount } from 'svelte';
	import { api } from '$lib/api/apiProxy.js';
	import { newTableParams } from '$lib/service/tableParams.js';
	import { addToast } from '$lib/store/toast';
	import RecipientEventsModal from '$lib/components/RecipientEventsModal.svelte';
	import SessionCaptureModal from '$lib/components/SessionCaptureModal.svelte';

	/** @type {{ programID: string, recipientID: string, title: string } | null} */
	let selected = null;
	const params = newTableParams({ sortBy: 'created_at', sortOrder: 'desc' });
	let events = [];
	let templates = {};
	let hasNextPage = false;
	let isLoading = false;
	let eventNames = {};
	let isCaptureVisible = false;
	let captureData = '';
	// only the newest request may set the rows
	let seq = 0;

	const loadEventNames = async () => {
		if (Object.keys(eventNames).length) return;
		try {
			const res = await api.campaign.getAllEventTypes();
			if (res.success) {
				eventNames = Object.fromEntries((res.data ?? []).map((t) => [t.id, t.name]));
			}
		} catch (e) {
			console.error('failed to load event types', e);
		}
	};

	const load = async () => {
		if (!selected) return;
		const mine = ++seq;
		isLoading = true;
		try {
			const res = await api.program.recipientEvents(
				selected.programID,
				selected.recipientID,
				params
			);
			if (mine !== seq) return;
			if (!res.success) {
				throw res.error;
			}
			events = res.data?.rows ?? [];
			templates = res.data?.templates ?? {};
			hasNextPage = !!res.data?.hasNextPage;
		} catch (e) {
			if (mine === seq) {
				addToast('Failed to load the recipient events', 'Error');
			}
			console.error('failed to load program recipient events', e);
		} finally {
			if (mine === seq) {
				isLoading = false;
			}
		}
	};

	/**
	 * @param {string} programID
	 * @param {string} recipientID
	 * @param {string} title
	 */
	export const open = (programID, recipientID, title) => {
		selected = { programID, recipientID, title };
		events = [];
		templates = {};
		hasNextPage = false;
		params.reset();
		loadEventNames();
		load();
	};
	const close = () => {
		selected = null;
		seq++;
	};

	onMount(() => {
		params.onChange(load);
		return () => params.unsubscribe();
	});
</script>

<RecipientEventsModal
	headerText={selected ? `Events: ${selected.title}` : 'Events'}
	visible={!!selected}
	onClose={close}
	{events}
	{eventNames}
	pagination={params}
	{hasNextPage}
	isGhost={isLoading}
	showTemplate
	{templates}
	on:capture={(e) => {
		captureData = e.detail;
		isCaptureVisible = true;
	}}
/>
<SessionCaptureModal bind:visible={isCaptureVisible} bind:data={captureData} />
