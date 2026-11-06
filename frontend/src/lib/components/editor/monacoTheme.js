// dark editor theme on the app gray-900 surface, so editors match dark modals
export const PC_DARK_THEME = 'pc-dark';

let defined = false;

/**
 * defines the theme once and returns its name
 * @param {*} monaco
 * @returns {string}
 */
export const pcDarkTheme = (monaco) => {
	if (!defined) {
		monaco.editor.defineTheme(PC_DARK_THEME, {
			base: 'vs-dark',
			inherit: true,
			rules: [],
			colors: {
				'editor.background': '#111827',
				'editorGutter.background': '#111827',
				'editorStickyScroll.background': '#111827'
			}
		});
		defined = true;
	}
	return PC_DARK_THEME;
};
