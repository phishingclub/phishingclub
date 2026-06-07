FROM node:22-slim@sha256:32b9e321f262db540d55ac10dc529667cf4737546e097cdd36a843c62bcbf423

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
