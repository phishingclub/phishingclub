// scrolls to the table a dashboard tile link points at. Tables settle their
// height a moment after loading, so the scroll is repeated a few times while
// the page has not been scrolled by hand.
export const scrollToTable = (id) => {
	if (!id) return;
	let last = null;
	const scroll = () => {
		if (last !== null && Math.abs(window.scrollY - last) > 2) return false;
		document.getElementById(id)?.scrollIntoView();
		last = window.scrollY;
		return true;
	};
	scroll();
	let runs = 0;
	const timer = setInterval(() => {
		if (++runs >= 6 || !scroll()) clearInterval(timer);
	}, 250);
};
