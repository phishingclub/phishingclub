### Platform backend
Install AIR for auto-reloading `go install github.com/cosmtrek/air@latest`

To start the project locally run: `make backend-dev`

Check the terminal output to see the address, username and password

### Production / Deployment
The program must be executable
`chmod +x ./path/to/binary`
The program must have rights to serve on privliged ports
`sudo setcap CAP_NET_BIND_SERVICE=+eip /path/to/binary`

### Known Issues

#### Hot reloading not working / New files now working
If a file accessible in the frontend after adding it, save an existing file to trigger a rebuild. This should now include the new file. If this does not work, try to run `make sorry` which will restart all services.

 ### Debugging with AIR via. docker and delve
 To debug the backend you must uncomment the full bin line in the docker air toml file.
 Attach to the debugger to trigger starting the backend.
 Do not edit files while in debug mode, instead stop the debugger, edit the file and start the debugger again.

### docker-compose
docker-compose is a plugin for docker that allows you to define multiple services in a single file.
Before this was a stand alone python script run with `docker-compose` but as a plugin this is `docker compose`.

In the makefile, you can edit the top line to change if docker compose is called with or without the dash (-) in the middle.

# Notes about allow listing

{
  admin_allowed
  trusted_proxies
  trusted_ip_header
}

if no admin_allowed is set, all IPs are welcome.

If no trusted proxies are set, headers such as X-Forwarded-By will not be used.

If TrustedIPHeader is set, then this header is used for finding the real IP.
For example cloudflare uses cf-connecting-ip.

If TrustedIPHeader is not set and trusted_proxies is set, then it trusts the IP
from X-Forwarded

# SSO Setup
## Microsoft Entra-ID

### Ensure only specific tenant user's can log in.
In 'properties' set 'Assignment required' to 'Yes'.
In 'Users and groups' add the users or groups that should be able to log into the application.
