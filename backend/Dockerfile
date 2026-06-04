# development docker file
FROM golang:1.25.10@sha256:c138bff780910acf4254ab3a6f7ff0f64bbd841f27bd82bfa986fe122c109538

EXPOSE 8000 8001

WORKDIR /app

# Add user
# Add group with ID 1000 and user with ID 1000
RUN groupadd -g 1000 appuser && \
    useradd -r -u 1000 -g appuser appuser -d /home/appuser -m

# chromium runtime dependencies (required when remote_browser.enabled is true)
RUN apt-get update && apt-get install -y --no-install-recommends \
    libglib2.0-0 \
    libnss3 \
    libatk1.0-0 \
    libatk-bridge2.0-0 \
    libcups2 \
    libdrm2 \
    libxkbcommon0 \
    libxcomposite1 \
    libxdamage1 \
    libxfixes3 \
    libxrandr2 \
    libgbm1 \
    libasound2 \
    libpango-1.0-0 \
    libcairo2 \
    fonts-liberation \
    libx11-6 \
    libx11-xcb1 \
    libxcb1 \
    libxext6 \
    && rm -rf /var/lib/apt/lists/*

# install deps
#RUN go install github.com/cosmtrek/air@latest \
#RUN go install github.com/go-delve/delve/cmd/dlv@1.9.1
COPY go.mod /app/go.mod
COPY go.sum /app/go.sum
RUN mkdir -p /app/.dev
RUN mkdir -p /app/.test
RUN chown -R appuser:appuser /app


USER appuser
RUN go install github.com/cosmtrek/air@v1.40.4 && go install github.com/go-delve/delve/cmd/dlv@v1.26.3
RUN go mod download

CMD ["air", "-c", "/.dev-air/.air.docker.toml"]
