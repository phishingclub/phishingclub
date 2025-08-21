import { api } from '$lib/api/apiProxy.js';

export let previewQR = async (url = 'https://empty.test', dotSize = 4) => {
	const res = await api.utils.qr({
		url,
		dotSize
	});
	if (!res.success) {
		throw res.error;
	}
	return res.data;
};
