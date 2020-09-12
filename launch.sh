#!/bin/bash

# Add me to the end of ~/.profile

DIR=/home/pi/go/src/github.com/jheidel/raspi-mjpeg-viewer

[[ -z $DISPLAY && $XDG_VTNR -eq 1 ]] && /usr/bin/startx ${DIR}/raspi-mjpeg-viewer --config=${DIR}/config.json 1> >(logger -s -t mjpeg-viewer) 2>&1
