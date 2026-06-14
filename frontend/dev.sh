#/bin/bash
# install dependencies when the vite binary is missing.
# checking for the binary instead of the node_modules directory means a partial
# install (for example one killed by the OOM killer) is retried instead of being
# skipped forever, which would leave the dev server crash looping on "vite: not found"
if [ ! -x "node_modules/.bin/vite" ]; then
	npm install
fi
while true; do \
	npm run dev; \
	echo "Process crashed! Restarting in 10 seconds..."; \
	sleep 10; \
done
