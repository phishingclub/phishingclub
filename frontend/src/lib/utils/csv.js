import papaparse from 'papaparse';

/**
 * Parse CSV file to recipients
 * @param {File} file - CSV file
 * @returns {Promise<{recipients: Array<*>, skipped: Array<{line: number, reason: string, row: object}>}>}
 **/
export const parseCSVToRecipients = async (file) => {
	const p = new Promise((resolve, reject) => {
		const recipients = {};
		const skipped = [];
		papaparse.parse(file, {
			header: true,
			skipEmptyLines: true,
			complete: (results) => {
				if (results.errors && results.errors.length > 0) {
					console.info('CSV import errors', results.errors);
					// track parsing errors
					results.errors.forEach((error) => {
						skipped.push({
							line: error.row + 2, // +1 for header, +1 for 0-index
							reason: `parse error: ${error.message}`,
							row: error.row
						});
					});
				}
				if (!results.data) {
					reject('No data found in CSV file');
					return;
				}
				// lowercased map of headers
				const fieldsMap = {};
				for (let i = 0; i < results.meta.fields.length; i++) {
					const field = results.meta.fields[i];
					fieldsMap[field.toLowerCase()] = field;
				}

				results.data.forEach((row, index) => {
					const email = row[fieldsMap['email']];
					if (!email) {
						skipped.push({
							line: index + 2, // +1 for header, +1 for 0-index
							reason: 'missing email',
							row: row
						});
						return;
					}
					// check if email already exists in this import (duplicate within file)
					if (recipients[email]) {
						skipped.push({
							line: index + 2,
							reason: `duplicate email in file (first occurrence at line ${recipients[email]._line})`,
							row: row
						});
						return;
					}
					recipients[email] = {
						email: email,
						phone: row[fieldsMap['phone']] ?? null,
						extraIdentifier: row[fieldsMap['extraIdentifier'.toLocaleLowerCase()]] ?? null,
						firstName: row[fieldsMap['firstname']] ?? null,
						lastName: row[fieldsMap['lastname']] ?? null,
						position: row[fieldsMap['position']] ?? null,
						department: row[fieldsMap['department']] ?? null,
						city: row[fieldsMap['city']] ?? null,
						country: row[fieldsMap['country']] ?? null,
						misc: row[fieldsMap['misc']] ?? null,
						_line: index + 2 // track line number for duplicate detection
					};
				});

				// remove internal _line property before returning
				const recipientsList = Object.values(recipients).map((r) => {
					const { _line, ...recipient } = r;
					return recipient;
				});

				resolve({ recipients: recipientsList, skipped });
			}
		});
	});
	return await p;
};
