# Compiler image
FROM golang:1.17-bullseye AS go-builder

# Copy the project 
COPY idleproxy/ /tmp/idleproxy/
WORKDIR /tmp/idleproxy/

# Install dependencies
RUN go mod download

# Build the binary
# RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -a -installsuffix cgo -ldflags="-w -s" -o /go/bin/idleproxy
RUN CGO_ENABLED=0 go build -a -installsuffix cgo -ldflags="-w -s" -o /go/bin/idleproxy



# Final image
FROM ubuntu:20.04

# Install dependencies
RUN apt-get update && apt-get install -y --no-install-recommends \
    apt-transport-https \
    ca-certificates \
    curl \
    gnupg-agent \
    software-properties-common \
    cmake \
    libjpeg8-dev \
    gcc \
    g++ \
    make \
    v4l-utils

# Download and install mjpg-streamer
RUN curl -fsSLO --compressed --retry 3 --retry-delay 10 \
    https://github.com/jacksonliam/mjpg-streamer/archive/master.tar.gz \
    && mkdir /mjpg \
    && tar xzf master.tar.gz -C /mjpg
WORKDIR /mjpg/mjpg-streamer-master/mjpg-streamer-experimental
RUN make
RUN make install

# Copy the idleproxy binary
COPY --from=go-builder /go/bin/idleproxy /go/bin/idleproxy

# Set default environment variables for mjpg-streamer
ENV MJPG_STREAMER_INPUT "input_uvc.so"
ENV MJPG_STREAMER_PORT "8080"
ENV MJPG_STREAMER_CAMERA_DEVICE "/dev/video0"

# Set default environment variables for idleproxy
ENV IDLEPROXY_HOST "http://localhost"
ENV IDLEPROXY_PORT "80"
ENV IDLEPROXY_PROXY_URL "http://localhost:8080"
ENV IDLEPROXY_IDLE_TIMEOUT "1m"
ENV IDLEPROXY_PROCESS_CWD "/mjpg/mjpg-streamer-master/mjpg-streamer-experimental"
ENV IDLEPROXY_PROCESS_CMD "/entry"
ENV IDLEPROXY_DEBUG "false"

# Expose the default ports
EXPOSE 80
EXPOSE 8080

# Setup the main entrypoint script
COPY entry.sh /entry
RUN chmod +x /entry
# ENTRYPOINT [ "/entry" ]

# Run idleproxy as the main entrypoint
ENTRYPOINT ["/go/bin/idleproxy"]
