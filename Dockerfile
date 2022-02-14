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
    make

# Download and install mjpg-streamer
RUN curl -fsSLO --compressed --retry 3 --retry-delay 10 \
    https://github.com/jacksonliam/mjpg-streamer/archive/master.tar.gz \
    && mkdir /mjpg \
    && tar xzf master.tar.gz -C /mjpg
WORKDIR /mjpg/mjpg-streamer-master/mjpg-streamer-experimental
RUN make
RUN make install

# Set default environment variables
ENV MJPG_STREAMER_INPUT "input_uvc.so"
ENV MJPG_STREAMER_PORT "8080"
ENV MJPG_STREAMER_CAMERA_DEVICE "/dev/video0"

# Setup the main entrypoint script
COPY entry.sh /entry
RUN chmod +x /entry
ENTRYPOINT [ "/entry" ]
