# Reproducible test/build image. `docker build .` fails if vet or tests fail,
# so it doubles as CI. Day-to-day, prefer the Makefile (bind-mounts source,
# no rebuilds). Pin matches the latest stable Go at time of writing.
FROM golang:1.26.4

WORKDIR /app

# Module layer cached separately from source.
COPY go.mod ./
RUN go mod download

COPY . .
RUN go vet ./... && go test ./...
