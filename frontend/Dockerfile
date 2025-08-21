FROM node:latest

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
