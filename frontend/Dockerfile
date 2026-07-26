FROM node:22-slim@sha256:6c74791e557ce11fc957704f6d4fe134a7bc8d6f5ca4403205b2966bd488f6b3

WORKDIR /app

#COPY package.json package.json
#COPY package-lock.json package-lock.json
RUN chown -R node:node /app
USER node
#RUN npm install # handled in the dev.sh

## Add user
# Add group with ID 1000 and user with ID 1000
#COPY ./ /app/
CMD [ "bash", "/app/dev.sh" ]
