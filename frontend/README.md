# Description

Template for a static generated page

# Install

- Clone the repo
- Remove the git folder (`rm -rf ./.git`)
- Start a new git (`git init`)
- `npm install`
- `npm run dev`

# Development

- `npm run dev`

# Install / Update dependencies

- Attach to the frontend `make front-attach`
- Check for new updates, including breaking `npm run update-check`
- Update the dependencies `npm run update-break`
- Update the dependencies `npm run install`
- If it fails try deleting the `node_modules` folder and run `npm install` again
- If it works, delete the `node_modules` folder and exit the container.
- Rebuild the frontend `make frontend-build`
- The updated node_modules folder should be in the `./frontend` folder

## Bugs
There has been multiple bugs with watch rebuilding and hot reloading were files were not added to the build.
Multiple solutions have been tried. The current solution is to use vite dev and proxy requests to the backend.

This means that frontend should be reached via. HTTP on localhost:8003 and localhost:8002

A backup solution exists using nodemon via. `npm run dev-nodemon` and then using HTTPS on `localhost:8002`

There had been issues with vite watch not adding changed/new files to the build when via. containers.
To solve this, a switch to nodemon was made. This is not ideal, but it works.
We can test if the bug gets fixed by switching to `npm run build-dev` and add the `--watch` flag to it.

# Local build

- `npm run build` build is in `./build`
