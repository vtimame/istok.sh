FROM golang:1.26.5-bookworm@sha256:53eeac89074db483fdf0ab3be1df32bf6e47562263d2d0d6baa7f26acb4957dd

RUN apt-get update \
    && apt-get install --yes --no-install-recommends \
        ca-certificates \
        gcc \
        libc6-dev \
        git \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /workspace

COPY go.mod go.sum ./
RUN go mod download

COPY . ./
RUN CGO_ENABLED=1 go build -trimpath -o /usr/local/bin/istok ./cmd/istok

COPY docker/hardening/run.sh /usr/local/bin/run-index-hardening
RUN chmod 0755 /usr/local/bin/run-index-hardening

ENV ISTOK_HARDENING_BINARY=/usr/local/bin/istok \
    GOCACHE=/tmp/go-build \
    GOTMPDIR=/tmp/go-tmp \
    GOFLAGS=-mod=readonly

ENTRYPOINT ["/usr/local/bin/run-index-hardening"]
