/**
 * Resets a collection of form elements to their default values
 * or empty values.
 * @param {{[key: string]: FormElement}} formElementsMap
 */
export const resetForm = (formElementsMap) => {
	const elements = Object.values(formElementsMap);
	for (let i = 0; i < elements.length; i++) {
		const formElement = elements[i];
		if (formElement.element.type === 'checkbox') {
			if (formElement.default) {
				formElement.element.checked = !!formElement.default;
				continue;
			}
			formElement.element.checked = false;
			continue;
		}
		if (
			formElement.element.type === 'text' ||
			formElement.element.type === 'textarea' ||
			formElement.element.type === 'password'
		) {
			if (formElement.default) {
				formElement.element.value = formElement.default.toString();
				continue;
			}
			formElement.element.value = '';
			continue;
		}
	}
};

/**
 * @typedef FormElement
 * @type {{element: HTMLInputElement, default: string|boolean|null, value: string, checked: boolean, _element: HTMLInputElement }}}
 */

/**
 * @typedef FormElementMap
 * @type {{[key: string]: FormElement}}
 */

/**
 * A wrapper for a form elements that includes the default value
 *
 * @param {string|boolean|null} defaultValue
 * @returns {FormElement}
 */
export const newFormElement = (defaultValue = null) => {
	return {
		default: defaultValue,

		// dont use this directly
		_element: null,
		// use directly instead
		get element() {
			return this._element;
		},

		set element(element) {
			this._element = element;
			if (this._element.type === 'checkbox') {
				this.default = !!this._element.checked;
			}
			if (
				this._element.type === 'text' ||
				this._element.type === 'textarea' ||
				this._element.type === 'password'
			) {
				this.default = this._element.value;
			}
		},

		/**
		 * shortcut for element.value
		 *
		 * @returns {string}
		 */
		get value() {
			return this.element.value ?? '';
		},
		/**
		 * shortcut for element.checked
		 * @returns {boolean}
		 */
		get checked() {
			return !!this.element.checked;
		}
	};
};

export const buttonDisabledAttributes = (element, attribute, reason) => {
	if (!element[attribute]) {
		return { disabled: true, title: reason };
	}
	return { disabled: false, title: '' };
};

export const globalButtonDisabledAttributes = (element, context) => {
	if (context) {
		return buttonDisabledAttributes(element, 'companyID', 'Only available in shared view.');
	}
	return { disabled: false, title: '' };
};
