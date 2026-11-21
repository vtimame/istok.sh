# The sync hub image: `istok hub serve` as an unprivileged user, data in /data.
# The builder runs on the build machine and cross-compiles with cgo (SQLite),
# so a multi-platform build needs no emulation.
FROM --platform=$BUILDPLATFORM golang:1.26.5-bookworm@sha256:53eeac89074db483fdf0ab3be1df32bf6e47562263d2d0d6baa7f26acb4957dd AS build

ARG TARGETARCH
RUN set -eu; \
    packages="gcc libc6-dev"; \
    if [ "$TARGETARCH" = arm64 ] && [ "$(dpkg --print-architecture)" != arm64 ]; then packages="gcc-aarch64-linux-gnu libc6-dev-arm64-cross"; fi; \
    if [ "$TARGETARCH" = amd64 ] && [ "$(dpkg --print-architecture)" != amd64 ]; then packages="gcc-x86-64-linux-gnu libc6-dev-amd64-cross"; fi; \
    apt-get update; \
    apt-get install --yes --no-install-recommends $packages; \
    rm -rf /var/lib/apt/lists/*

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . ./

ARG VERSION=dev
ARG COMMIT=none
ARG BUILD_DATE=unknown
RUN set -eu; \
    case "$TARGETARCH:$(dpkg --print-architecture)" in \
        arm64:arm64|amd64:amd64) cc=gcc ;; \
        arm64:*) cc=aarch64-linux-gnu-gcc ;; \
        amd64:*) cc=x86_64-linux-gnu-gcc ;; \
        *) echo "unsupported architecture $TARGETARCH" >&2; exit 1 ;; \
    esac; \
    CGO_ENABLED=1 GOOS=linux GOARCH="$TARGETARCH" CC="$cc" go build -trimpath \
        -ldflags "-s -w \
            -X github.com/vtimame/istok.sh/internal/buildinfo.Version=$VERSION \
            -X github.com/vtimame/istok.sh/internal/buildinfo.Commit=$COMMIT \
            -X github.com/vtimame/istok.sh/internal/buildinfo.BuildDate=$BUILD_DATE" \
        -o /out/istok ./cmd/istok; \
    mkdir -p /out/data

FROM gcr.io/distroless/base-debian12:nonroot@sha256:7f0c72cd138b442ae0deeb69c08b1acf5525439ba251a49ad93c320a061567e5

COPY --from=build /out/istok /usr/local/bin/istok
COPY --from=build --chown=nonroot:nonroot /out/data /data

ENV ISTOK_HUB_DATA=/data \
    ISTOK_HUB_LISTEN=:7443

VOLUME /data
EXPOSE 7443

ENTRYPOINT ["/usr/local/bin/istok"]
CMD ["hub", "serve"]
