#!/usr/bin/env bash

if [ ! -f /dv/initialized ]; then
    bash /scripts/setup.sh &
fi
