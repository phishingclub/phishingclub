# development docker file
FROM golang:1.25.1

EXPOSE 8000 8001

WORKDIR /app

# Add user
# Add group with ID 1000 and user with ID 1000
RUN groupadd -g 1000 appuser && \
    useradd -r -u 1000 -g appuser appuser -d /home/appuser -m

# install deps
#RUN go install github.com/cosmtrek/air@latest \
#RUN go install github.com/go-delve/delve/cmd/dlv@1.9.1
COPY go.mod /app/go.mod
COPY go.sum /app/go.sum
RUN mkdir -p /app/.dev
RUN mkdir -p /app/.test
RUN chown -R appuser:appuser /app


USER appuser
RUN go install github.com/cosmtrek/air@v1.40.4 && go install github.com/go-delve/delve/cmd/dlv@latest
RUN go mod tidy

CMD ["air", "-c", "/.dev-air/.air.docker.toml"]
