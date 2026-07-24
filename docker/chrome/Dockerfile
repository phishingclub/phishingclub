# development docker file
# real Google Chrome running headful on a virtual X display, used by the
# remote browser feature over the DevTools protocol
FROM debian:bookworm-slim@sha256:7b140f374b289a7c2befc338f42ebe6441b7ea838a042bbd5acbfca6ec875818

# chrome is pinned by version and checksum. to upgrade read the current values from
# https://dl.google.com/linux/chrome/deb/dists/stable/main/binary-amd64/Packages
ARG CHROME_VERSION=150.0.7871.186-1
ARG CHROME_SHA256=4193e00b6d5d5969ee63f7a69596868f546aa0e8cb077b3e0bf9cc1e2c719d00

# xvfb provides the display, openbox gives chrome a focused window, x11vnc plus
# novnc make the display watchable from a browser, socat republishes the loopback
# only devtools port on the container network
RUN apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates \
    curl \
    xvfb \
    openbox \
    x11vnc \
    novnc \
    websockify \
    socat \
    tini \
    fonts-liberation \
    fonts-noto-color-emoji \
    && rm -rf /var/lib/apt/lists/*

RUN curl -fsSL -o /tmp/chrome.deb \
      "https://dl.google.com/linux/chrome/deb/pool/main/g/google-chrome-stable/google-chrome-stable_${CHROME_VERSION}_amd64.deb" \
    && echo "${CHROME_SHA256}  /tmp/chrome.deb" | sha256sum -c - \
    && apt-get update \
    && apt-get install -y --no-install-recommends /tmp/chrome.deb \
    && rm -f /tmp/chrome.deb \
    && rm -rf /var/lib/apt/lists/*

# google ships a daily apt cron and a repo entry with the package. neither is
# wanted here because the version is pinned above
RUN rm -f /etc/cron.daily/google-chrome /etc/apt/sources.list.d/google-chrome.list

RUN groupadd -g 1000 chrome && \
    useradd -u 1000 -g chrome -m -d /home/chrome chrome

# an empty named volume inherits the permissions of the image directory it is
# mounted over, so the socket dir must be world writable before the volume exists
RUN mkdir -p /tmp/.X11-unix && chmod 1777 /tmp/.X11-unix

COPY start.sh /usr/local/bin/start.sh
RUN chmod +x /usr/local/bin/start.sh

USER chrome
WORKDIR /home/chrome

EXPOSE 8109 9222

# -g forwards signals to the whole process group so xvfb, openbox and the vnc
# stack stop with chrome instead of lingering
ENTRYPOINT ["/usr/bin/tini", "-g", "--", "/usr/local/bin/start.sh"]
