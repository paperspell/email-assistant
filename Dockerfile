# syntax=docker/dockerfile:1

FROM golang:1.26-alpine AS build
ARG VERSION=dev
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
# CGO off: SQLite is pure Go (wasm). timetzdata: the digest needs zone data
# the runtime image does not carry.
RUN CGO_ENABLED=0 go build -trimpath -tags timetzdata \
      -ldflags="-s -w -X main.version=${VERSION}" \
      -o /out/email-agent ./cmd/email-agent \
 && mkdir -p /out/data

# distroless: no shell, no package manager, runs as an unprivileged user.
FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/email-agent /email-agent
# /data must be writable by the nonroot user before a volume is mounted over
# it, or a named volume inherits root ownership and the first write fails.
COPY --from=build --chown=nonroot:nonroot /out/data /data
# Every command defaults its database to $HOME/.email-agent; pointing HOME at
# the volume keeps `init`, `run` and `account add` on the same file, flag-free.
ENV HOME=/data
VOLUME /data
ENTRYPOINT ["/email-agent"]
CMD ["run"]
