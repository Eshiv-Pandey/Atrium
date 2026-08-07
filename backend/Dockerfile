# Multi-stage build: a toolchain-sized builder, and a final image containing
# nothing but the binaries.
#
# The result is ~15 MB instead of ~800 MB, and — more to the point — it has no
# shell, no package manager, and no libc. A vulnerability in any of those cannot
# be exploited in an image that does not contain them.

FROM golang:1.23-alpine AS builder

WORKDIR /src

# Dependencies are copied and downloaded before the source, so editing a .go
# file does not invalidate the module cache layer. This is the difference
# between a 3-second rebuild and a 60-second one.
COPY go.mod go.sum ./
RUN go mod download

COPY . .

# CGO_ENABLED=0 produces a static binary, which is what allows the distroless
# static base below — with cgo on, the binary would need a libc the final image
# does not have.
#
# -trimpath strips local filesystem paths from the binary, and the ldflags strip
# the symbol table and DWARF data. Smaller, and it stops the image from
# advertising the build machine's directory layout.
ENV CGO_ENABLED=0 GOOS=linux
RUN go build -trimpath -ldflags="-s -w" -o /out/server ./cmd/server && \
    go build -trimpath -ldflags="-s -w" -o /out/seed   ./cmd/seed

# gcr.io/distroless/static-debian12 ships CA certificates, /etc/passwd, and
# timezone data — the three things a static Go binary actually needs from a
# filesystem — and nothing else.
FROM gcr.io/distroless/static-debian12:nonroot

# The API is stateless and writes nothing to disk, so it has no reason to run as
# root. distroless' nonroot user is uid 65532.
USER nonroot:nonroot

COPY --from=builder /out/server /server
COPY --from=builder /out/seed   /seed

EXPOSE 8080

ENTRYPOINT ["/server"]
