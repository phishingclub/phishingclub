// Where the browser lets the open list of a select be styled (appearance:
// base-select in app.css), the closed select is as wide as the option it shows,
// so it grows and shrinks when the value changes and moves what is next to it.
// A plain select is as wide as its widest option. This keeps that: each styled
// select gets the width of its widest option as its least width, measured when
// it shows up and again when its options change.

const SELECTOR = 'select.pc-select, select.pc-per-page, select.pc-table-filter';

// a select that is hidden when measured, for example one only shown at some
// screen sizes, is watched and measured once it gets a size
let hiddenWatch = null;

// measure sets the least width of one select to the width of its widest option
const measure = (el) => {
	if (!el.isConnected || !el.options.length) return;
	// changing the shown option of an open select closes its list, so an open
	// select is measured once its list closes
	let open = false;
	try {
		open = el.matches(':open');
	} catch {
		open = false;
	}
	if (open) {
		el.addEventListener('blur', () => measure(el), { once: true });
		el.addEventListener('change', () => measure(el), { once: true });
		return;
	}
	el.style.minWidth = '';
	const index = el.selectedIndex;
	let widest = 0;
	for (let i = 0; i < el.options.length; i++) {
		el.selectedIndex = i;
		widest = Math.max(widest, el.getBoundingClientRect().width);
	}
	el.selectedIndex = index;
	if (widest > 0) el.style.minWidth = `${Math.ceil(widest)}px`;
	else hiddenWatch?.observe(el);
};

// startStableSelectWidth measures every styled select now and later ones as
// they are added or their options change. It returns a stop function.
export const startStableSelectWidth = () => {
	if (typeof CSS === 'undefined' || !CSS.supports('appearance', 'base-select')) {
		return () => {};
	}
	const pending = new Set();
	let frame = 0;
	const queue = (el) => {
		pending.add(el);
		if (!frame) {
			frame = requestAnimationFrame(() => {
				frame = 0;
				const list = [...pending];
				pending.clear();
				list.forEach(measure);
			});
		}
	};
	const scan = (root) => {
		if (root.nodeType !== 1) return;
		if (root.matches(SELECTOR)) queue(root);
		else if (root.matches('option') && root.parentElement?.matches(SELECTOR)) {
			queue(root.parentElement);
		} else root.querySelectorAll(SELECTOR).forEach(queue);
	};
	hiddenWatch =
		typeof ResizeObserver === 'undefined'
			? null
			: new ResizeObserver((entries) => {
					for (const entry of entries) {
						if (entry.contentRect.width > 0) {
							hiddenWatch.unobserve(entry.target);
							queue(entry.target);
						}
					}
				});
	const observer = new MutationObserver((mutations) => {
		for (const m of mutations) {
			if (m.target.nodeType === 1 && m.target.matches(SELECTOR)) queue(m.target);
			m.addedNodes.forEach(scan);
		}
	});
	observer.observe(document.body, { childList: true, subtree: true });
	scan(document.body);
	// text sizes change once the web font has loaded
	document.fonts?.ready.then(() => document.querySelectorAll(SELECTOR).forEach(queue));
	return () => {
		observer.disconnect();
		hiddenWatch?.disconnect();
		hiddenWatch = null;
		if (frame) cancelAnimationFrame(frame);
	};
};
