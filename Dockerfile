FROM golang:1.22-bookworm
ENV CGO_ENABLED=1

RUN apt-get update \
    && DEBIAN_FRONTEND=noninteractive \
    apt-get install --no-install-recommends --assume-yes \
    build-essential \
    libsqlite3-dev

WORKDIR /build
COPY go.mod go.sum ./
RUN go mod download

COPY . .
ENV GOCACHE=/root/.cache/go-build
RUN --mount=type=cache,target="/root/.cache/go-build" \
    go build -buildvcs -o /usr/bin/wadoh-be .


# RUNNER
FROM debian:bookworm
RUN apt-get update \
    && DEBIAN_FRONTEND=noninteractive \
    apt-get install --no-install-recommends --assume-yes \
    libsqlite3-0

WORKDIR /etc/wadoh-be/
EXPOSE 50051

COPY --from=0 /usr/bin/wadoh-be /usr/bin/wadoh-be
ENTRYPOINT ["wadoh-be"]
