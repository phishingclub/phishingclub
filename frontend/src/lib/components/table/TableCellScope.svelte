<!--
	TableCellScope component

	A reusable table cell component that displays the scope of an item based on its companyID.
	Used to distinguish between shared (global) items and company-specific items in tables.

	Usage:
	```svelte
	<TableCellScope companyID={item.companyID} />
	```

	When to use:
	- In company view mode to show which items are shared vs company-specific
	- Only display when contextCompanyID is set (use conditional rendering)
	- Place as the last data column before TableCellEmpty and TableCellAction

	Examples:
	- companyID = null → displays "Shared"
	- companyID = "123" → displays "Company"
-->
<script>
	import TableCell from './TableCell.svelte';

	/**
	 * The company ID of the item. If null/undefined, item is considered shared/global.
	 * @type {string | null | undefined}
	 */
	export let companyID;
</script>

<TableCell value={companyID ? 'Company' : 'Shared'} />
