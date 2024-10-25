#!/usr/bin/env bash

if [ ! -f /dv/initialized ]; then
    bash /opt/payara/scripts/setup.sh &
fi
