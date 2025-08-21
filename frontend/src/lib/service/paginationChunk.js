export const getPaginatedChunk = (arr, page = 1, perPage = 2, search = '', sortByField = '') => {
	let filtered = [...arr];
	if (search !== '') {
		filtered = arr.filter((r) => r.fullName.toLowerCase().includes(search.toLowerCase()));
	}
	filtered = arr.filter((r) => r.fullName.toLowerCase().includes(search.toLowerCase()));
	const start = (page - 1) * perPage;
	const end = start + perPage;
	const sorted = filtered.sort((a, b) => {
		if (!a[sortByField] || !b[sortByField]) {
			return 0;
		}
		return a[sortByField].toLowerCase().localeCompare(b[sortByField].toLowerCase());
	});
	return sorted.slice(start, end);
};

export const getPaginatedChunkWithParams = (
	arr,
	{ page = 1, perPage = 2, search = '', sortBy = '', sortOrder = 'asc' } = {}
) => {
	let filtered = [...arr];
	if (search !== '') {
		filtered = arr.filter((r) =>
			Object.values(r).some((value) => {
				if (!value) {
					return false;
				}
				return value.toLowerCase().includes(search.toLowerCase());
			})
		);
	}
	const start = (page - 1) * perPage;
	const end = start + perPage;
	const sorted = filtered.sort((a, b) => {
		const aa = Object.fromEntries(
			Object.entries(a).map(([key, value]) => [key.toLowerCase().replace(/\s+/g, ''), value])
		);
		const bb = Object.fromEntries(
			Object.entries(b).map(([key, value]) => [key.toLowerCase().replace(/\s+/g, ''), value])
		);
		const sortByNormalized = sortBy.toLowerCase().replace(/\s+/g, '');
		if (!aa[sortByNormalized] || !bb[sortByNormalized]) {
			return 0;
		}
		const comparison = aa[sortByNormalized]
			.toLowerCase()
			.localeCompare(bb[sortByNormalized].toLowerCase());
		return sortOrder === 'asc' ? comparison : -comparison;
	});
	return sorted.slice(start, end);
};
