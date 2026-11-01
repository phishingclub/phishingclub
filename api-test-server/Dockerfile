FROM golang:1.27.3@sha256:2920bb124f1a2e12a1d39f0cab756ae0068441ee1da7655dec7f5e374944095b

WORKDIR /app

# Add user
# Add group with ID 1000 and user with ID 1000
RUN groupadd -g 1000 appuser && \
    useradd -r -u 1000 -g appuser appuser -d /home/appuser -m

COPY go.mod /app/go.mod
COPY main.go /app/main.go
RUN chown -R appuser:appuser /app

USER appuser
RUN go mod tidy

CMD ["go", "run", "main.go"]
