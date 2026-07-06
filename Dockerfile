# Cross-compile the Windows nitpick binary in a container, with no local Go
# toolchain required. The final stage contains only the binary so it can be
# exported straight to the host with BuildKit's --output flag:
#
#   docker build --output .nitpick/bin .
#
# That writes the executable to .nitpick/bin/nitpick.exe. (Or use the Makefile
# target: `make docker-windows`.)

# --- build stage -------------------------------------------------------------
FROM golang:1.26 AS build
WORKDIR /src

# Module metadata first for layer caching. There is no go.sum because Nitpick
# has no external dependencies; go mod download is effectively a no-op.
COPY go.mod ./
RUN go mod download

COPY . .

ARG VERSION=dev
# Static, stripped, reproducible cross-build for 64-bit Windows.
ENV CGO_ENABLED=0 GOOS=windows GOARCH=amd64
RUN go build -trimpath -ldflags "-s -w" -o /out/nitpick.exe ./cmd/nitpick

# --- export stage ------------------------------------------------------------
# A scratch image holding only the binary, so `--output <dir>` writes just the
# executable to that host directory.
FROM scratch AS export
COPY --from=build /out/nitpick.exe /nitpick.exe
