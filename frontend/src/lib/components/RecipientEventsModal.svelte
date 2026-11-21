<script>
	// The events of one recipient, used by the campaign Recipients table and the
	// program recipient tables. The rows and paging come from the page. A
	// program passes showTemplate and templates, the template name per campaign
	// id. Row actions, such as delete, go in the actions slot.
	import { createEventDispatcher } from 'svelte';
	import Modal from '$lib/components/Modal.svelte';
	import Table from '$lib/components/table/Table.svelte';
	import TableRow from '$lib/components/table/TableRow.svelte';
	import TableCell from '$lib/components/table/TableCell.svelte';
	import TableCellEmpty from '$lib/components/table/TableCellEmpty.svelte';
	import TableCellAction from '$lib/components/table/TableCellAction.svelte';
	import CellCopy from '$lib/components/table/CopyCell.svelte';
	import EventName from '$lib/components/table/EventName.svelte';
	import EventDataCell from '$lib/components/table/EventDataCell.svelte';

	export let visible = false;
	export let onClose = () => {};
	export let headerText = 'Events';
	/** @type {any[]} */
	export let events = [];
	/** event type id to event name */
	export let eventNames = {};
	export let pagination;
	export let hasNextPage = true;
	export let isGhost = false;
	export let showTemplate = false;
	/** campaign id to template name */
	export let templates = {};

	const dispatch = createEventDispatcher();

	// phones show the time and the event, the rest from md up
	$: columns = [
		{ column: 'Created at', size: 'medium', compact: true },
		...(showTemplate ? [{ column: 'Template', size: 'medium', hideBelow: 'md' }] : []),
		{ column: 'Event', size: 'large', compact: true },
		{ column: 'Details', hideBelow: 'md' },
		{ column: 'User-Agent', size: 'small', hideBelow: 'md' },
		{ title: 'IP', column: 'Ip', size: 'small', hideBelow: 'md' },
		{ column: 'Metadata', size: 'small', hideBelow: 'md' }
	];
	const sortable = ['Created at', 'Event', 'Details', 'User-Agent', 'Ip', 'Metadata'];
</script>

<Modal {headerText} {visible} {onClose}>
	<div class="mt-8" id="recipient-events-modal"></div>
	<Table
		{columns}
		{sortable}
		{pagination}
		plural="events"
		hasData={!!events.length}
		{hasNextPage}
		{isGhost}
		hasActions={!!$$slots.actions}
	>
		{#each events as event (event.id)}
			<TableRow>
				<TableCell isDate value={event.createdAt} />
				{#if showTemplate}
					<TableCell value={templates[event.campaignID] || ''} hideBelow="md" />
				{/if}
				<TableCell>
					<EventName eventName={eventNames[event.eventID]} />
				</TableCell>
				<TableCell hideBelow="md">
					<EventDataCell
						data={event.data}
						eventName={eventNames[event.eventID]}
						on:capture={(e) => dispatch('capture', e.detail)}
					/>
				</TableCell>
				<TableCell hideBelow="md">
					<CellCopy text={event.userAgent} />
				</TableCell>
				<TableCell hideBelow="md">
					<CellCopy text={event.ip} />
				</TableCell>
				<TableCell hideBelow="md">
					<CellCopy text={event.metadata || ''} />
				</TableCell>
				{#if $$slots.actions}
					<TableCellEmpty />
					<TableCellAction>
						<slot name="actions" {event} />
					</TableCellAction>
				{/if}
			</TableRow>
		{/each}
	</Table>
</Modal>
