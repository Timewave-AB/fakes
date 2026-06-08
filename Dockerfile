# Reproducible test/build image. `docker build .` fails if vet or tests fail,
# so it doubles as CI. Day-to-day, prefer `docker compose run` (bind-mounts
# source, no rebuilds). GO_VERSION defaults to the latest stable Go; override it
# to test the lowest supported version: docker build --build-arg GO_VERSION=1.22 .
ARG GO_VERSION=1.26.4
FROM golang:${GO_VERSION}

WORKDIR /app

# Module layer cached separately from source.
COPY go.mod ./
RUN go mod download

COPY . .
RUN go vet ./... && go test ./...
