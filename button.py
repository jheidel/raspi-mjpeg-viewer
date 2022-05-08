#!/usr/bin/env python3

import logging
import os
import queue
import requests
import subprocess
import threading
import time
import RPi.GPIO as GPIO

# Connect button between this pin and ground
BUTTON_PIN = 4

# URL for gate actuation
URL="http://gatecontrol/trigger"

# Directory for media files
MEDIA_DIR="/home/pi/media/"

# Path for aplay executable
APLAY="/usr/bin/aplay"


sounds = queue.Queue()

def sound_worker():
    while True:
        s = sounds.get()
        p = os.path.join(MEDIA_DIR, s)
        logging.info("Playing sound %s" % p)
        cmd = [APLAY, p]
        p = subprocess.Popen(cmd)
        p.wait()
        logging.info("Aplay returned %d" % p.returncode)
        sounds.task_done()


def activate():
    sounds.put("press.wav")
    try:
        resp = requests.post(URL, timeout=2)
        logging.info("Response from %s: %s, %s" % (URL, resp, resp.content))
        resp.raise_for_status()
    except Exception as e:
        logging.error("%s returned %s" % (URL, e))
        sounds.put("error.wav")
    else:
        logging.info("Relay trigger successful")
        sounds.put("open.wav")


def button_press(e):
    logging.info("Button pressed, sending trigger")
    x = threading.Thread(target=activate)
    x.start()


def main():
    logging.basicConfig(
        format='[button] %(asctime)s %(levelname)-8s %(message)s',
        level=logging.INFO,
        datefmt='%Y-%m-%d %H:%M:%S')
    logging.info("Starting gpio listener")
    GPIO.setmode(GPIO.BCM)
    GPIO.setup(BUTTON_PIN, GPIO.IN, pull_up_down=GPIO.PUD_UP)
    GPIO.add_event_detect(BUTTON_PIN, edge=GPIO.FALLING, callback=button_press, bouncetime=2000)

    # Start sound background worker
    threading.Thread(target=sound_worker, daemon=True).start()

    while True:
        time.sleep(1)


if __name__ == '__main__':
    main()
