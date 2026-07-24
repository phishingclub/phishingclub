#!/bin/sh
# starts the virtual display, a window manager, the vnc viewer stack and chrome.
# every background process is killed when this script exits so the container
# stops cleanly instead of leaving a half dead display behind.
set -eu

DISPLAY_NUM="${DISPLAY_NUM:-99}"
SCREEN="${SCREEN:-1920x1080x24}"
DEVTOOLS_PORT="${DEVTOOLS_PORT:-9222}"
NOVNC_PORT="${NOVNC_PORT:-8109}"

# chrome binds devtools to loopback only, so it listens on a private port and
# socat republishes it on the container network below
CHROME_DEVTOOLS_PORT=9223
VNC_PORT=5900

WIDTH="${SCREEN%%x*}"
HEIGHT_DEPTH="${SCREEN#*x}"
HEIGHT="${HEIGHT_DEPTH%%x*}"

export DISPLAY=":${DISPLAY_NUM}"

cleanup() {
    trap - EXIT INT TERM
    kill 0 2>/dev/null || true
}
trap cleanup EXIT INT TERM

# the X socket is created inside a shared volume so other containers can render
# on this display too. access control is off because the socket is only reachable
# through that volume, never over the network
Xvfb "$DISPLAY" -screen 0 "$SCREEN" -ac -noreset \
    +extension RANDR +extension GLX +render &

for _ in $(seq 1 100); do
    [ -S "/tmp/.X11-unix/X${DISPLAY_NUM}" ] && break
    sleep 0.1
done
if [ ! -S "/tmp/.X11-unix/X${DISPLAY_NUM}" ]; then
    echo "xvfb failed to create a display socket" >&2
    exit 1
fi

# without a window manager the chrome window never takes X focus, which leaves
# document.hasFocus() false for every page and is a strong bot signal
openbox &

x11vnc -display "$DISPLAY" -rfbport "$VNC_PORT" -localhost -forever -shared -nopw -quiet &
websockify --web=/usr/share/novnc "$NOVNC_PORT" "127.0.0.1:${VNC_PORT}" &

# devtools refuses requests whose Host header is neither localhost nor an IP,
# so connect to this container by IP rather than by service name
socat "TCP-LISTEN:${DEVTOOLS_PORT},fork,reuseaddr" "TCP:127.0.0.1:${CHROME_DEVTOOLS_PORT}" &

# launching chrome by hand rather than through an automation library keeps the
# enable-automation switch off, so navigator.webdriver stays false
exec google-chrome-stable \
    --remote-debugging-address=127.0.0.1 \
    --remote-debugging-port="${CHROME_DEVTOOLS_PORT}" \
    --user-data-dir=/home/chrome/profile \
    --window-position=0,0 \
    --window-size="${WIDTH},${HEIGHT}" \
    --no-first-run \
    --no-default-browser-check \
    --disable-blink-features=AutomationControlled \
    --password-store=basic \
    --use-mock-keychain \
    ${CHROME_LANG:+--lang=${CHROME_LANG}} \
    ${CHROME_PROXY:+--proxy-server=${CHROME_PROXY}} \
    ${CHROME_EXTRA_FLAGS:-} \
    about:blank
