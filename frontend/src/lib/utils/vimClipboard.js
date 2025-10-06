/**
 * hacky vim copy to clipboard on visual mode d or y key down
 * supports yy and dd and thats it.
 */

/**
 * sets up clipboard integration for vim mode in monaco editor
 * @param {Object} editor - monaco editor instance
 * @param {Object} vimModeInstance - vim mode instance from monaco-vim
 * @param {boolean} localVimMode - current vim mode state
 * @param {Object} monaco - monaco editor module
 * @returns {Function} cleanup function to remove event listeners
 */
export function setupVimClipboardIntegration(editor, vimModeInstance, localVimMode, monaco) {
	if (!editor || !vimModeInstance || !localVimMode) {
		return () => {};
	}

	// check if clipboard api is available
	if (typeof navigator === 'undefined' || !navigator.clipboard) {
		console.warn('clipboard api not available');
		return () => {}; // noop cleanup
	}

	// listen for y and d key presses to copy to system clipboard
	const keyDownDisposable = editor.onKeyDown(async (e) => {
		if (!localVimMode) return;

		// detect y or d key presses
		if (e.keyCode === monaco.KeyCode.KeyY || e.keyCode === monaco.KeyCode.KeyD) {
			// get current selection immediately
			const selection = editor.getSelection();

			// only copy to clipboard if we have selected text (visual mode)
			if (selection && !selection.isEmpty()) {
				// copy selected text to clipboard
				try {
					const selectedText = editor.getModel().getValueInRange(selection);
					if (selectedText && selectedText.trim()) {
						await navigator.clipboard.writeText(selectedText);
					}
				} catch (e) {
					console.error(e);
				}
			}
		}
	});

	// cleanup function to remove event listener
	const cleanup = () => {
		if (keyDownDisposable) {
			keyDownDisposable.dispose();
		}
	};

	// store cleanup function on vim instance for later use
	if (vimModeInstance) {
		vimModeInstance._clipboardCleanup = cleanup;
	}

	return cleanup;
}

/**
 * destroys vim clipboard integration
 * @param {Object} vimModeInstance - vim mode instance from monaco-vim
 */
export function destroyVimClipboardIntegration(vimModeInstance) {
	if (vimModeInstance && vimModeInstance._clipboardCleanup) {
		vimModeInstance._clipboardCleanup();
		delete vimModeInstance._clipboardCleanup;
	}
}
