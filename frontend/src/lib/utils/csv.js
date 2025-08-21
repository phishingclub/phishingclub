import papaparse from 'papaparse';

/**
 * Parse CSV file to recipients
 * @param {File} file - CSV file
 * @returns {Promise<Array<*>>}
 **/
export const parseCSVToRecipients = async (file) => {
	const p = new Promise((resolve, reject) => {
		const recipients = {};
		papaparse.parse(file, {
			header: true,
			skipEmptyLines: true,
			complete: (results) => {
				if (results.errors) {
					console.info('CSV import errors', results.errors);
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

				results.data.forEach((row) => {
					const email = row[fieldsMap['email']];
					if (!email) {
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
						misc: row[fieldsMap['misc']] ?? null
					};
				});
				resolve(Object.values(recipients));
			}
		});
	});
	return await p;
};
