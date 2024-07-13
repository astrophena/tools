#!/usr/bin/env bash

# Remove audio only from an audio file, preserving metadata.
# See https://superuser.com/questions/1642490/remove-audio-only-from-an-mp3-file.

for f in *; do ffmpeg -i "${f}" -c copy -aframes 1 "$(pwd)/stripped-${f}"; done
