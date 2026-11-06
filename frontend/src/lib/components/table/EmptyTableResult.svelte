<script>
	/** @type {*} */
	export let page = null;
	export let plural;
	export let colspan = 100;
</script>

<tr data-empty-result class="bg-pleasant-gray dark:bg-gray-800/40 transition-colors duration-200">
	<td class="px-4 py-12 sm:py-16" {colspan}>
		<!-- sticky and the measured scroll box width keep the message centered in the visible part -->
		<div
			class="sticky left-4 flex flex-col items-center gap-2 text-center"
			style="width: calc(var(--pc-scroll-w, 100%) - 2rem)"
		>
			<svg
				class="w-8 h-8 text-slate-400 dark:text-gray-500"
				fill="none"
				viewBox="0 0 24 24"
				stroke="currentColor"
				stroke-width="1.5"
				aria-hidden="true"
			>
				<path
					stroke-linecap="round"
					stroke-linejoin="round"
					d="M3.75 9.776c.112-.017.227-.026.344-.026h15.812c.117 0 .232.009.344.026m-16.5 0a2.25 2.25 0 0 0-1.883 2.542l.857 6a2.25 2.25 0 0 0 2.227 1.932H19.05a2.25 2.25 0 0 0 2.227-1.932l.857-6a2.25 2.25 0 0 0-1.883-2.542m-16.5 0V6A2.25 2.25 0 0 1 6 3.75h3.879a1.5 1.5 0 0 1 1.06.44l2.122 2.12a1.5 1.5 0 0 0 1.06.44H18A2.25 2.25 0 0 1 20.25 9v.776"
				/>
			</svg>
			<p class="text-base sm:text-lg text-slate-600 dark:text-gray-300 transition-colors duration-200">
				{#if page === 1 || page == null}
					No {plural} found
				{:else}
					No more results found
				{/if}
			</p>
		</div>
	</td>
</tr>
