## FIXME: Create GitHub Actions workflows for building multi-platform images for idleproxy!

# Compiler image
FROM didstopia/base:go-alpine-3.14 AS go-builder

# Copy the project 
COPY . /tmp/idleproxy/
WORKDIR /tmp/idleproxy/

# Install dependencies
RUN go mod download

# Build the binary
# RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -a -installsuffix cgo -ldflags="-w -s" -o /go/bin/idleproxy
RUN CGO_ENABLED=0 go build -a -installsuffix cgo -ldflags="-w -s" -o /go/bin/idleproxy



## FIXME: Fix scratch + ARM64 support (something about qemu + x86_64 + libmusl/musl)
## NOTE: It looks like we just can't use scratch with ARM64 yet, probably because it runs inside an x86 VM?
# Runtime image
# FROM scratch
FROM alpine:3.14

# Copy certificates
COPY --from=go-builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt

# Copy the idleproxy binary
COPY --from=go-builder /go/bin/idleproxy /go/bin/idleproxy

# Set default environment variables for idleproxy
ENV IDLEPROXY_HOST "http://localhost"
ENV IDLEPROXY_PORT "80"
ENV IDLEPROXY_PROXY_URL "http://localhost:8080"
ENV IDLEPROXY_IDLE_TIMEOUT "1m"
ENV IDLEPROXY_PROCESS_CWD "."
ENV IDLEPROXY_PROCESS_CMD ""
ENV IDLEPROXY_PROCESS_START_DELAY "0s"
ENV IDLEPROXY_DEBUG "false"

# Expose the default ports
EXPOSE 80
# EXPOSE 8080

# Run idleproxy as the main entrypoint
ENTRYPOINT ["/go/bin/idleproxy"]
