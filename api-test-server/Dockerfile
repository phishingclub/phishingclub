FROM golang:1.25.13@sha256:cbff9d1a9041b316010f2da6b701b6c0d597718cb90928c85eb597334a0d23d4

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
