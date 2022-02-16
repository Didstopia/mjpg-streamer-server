#!/usr/bin/env bash

set -eo pipefail
set -x

if ! expr "$MJPG_STREAMER_INPUT" : ".*\.so.*" > /dev/null; then
  MJPG_STREAMER_INPUT="input_uvc.so $MJPG_STREAMER_INPUT"
fi

## TODO: Actually implement building and bundling the idleproxy binary
# exec /idleproxy

## TODO: Implement idleproxy starting and stopping mjpg-streamer on demand/automatically
exec mjpg_streamer \
  -i "/usr/local/lib/mjpg-streamer/$MJPG_STREAMER_INPUT -d $MJPG_STREAMER_CAMERA_DEVICE" \
  -o "/usr/local/lib/mjpg-streamer/output_http.so -w /usr/local/share/mjpg-streamer/www -p $MJPG_STREAMER_PORT"
