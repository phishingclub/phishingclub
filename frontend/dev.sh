#/bin/bash
# if node_modules folder does not exist, install dependencies
if [ ! -d "node_modules" ]; then
	npm install
fi
while true; do \
	npm run dev; \
	echo "Process crashed! Restarting in 10 seconds..."; \
	sleep 10; \
done
