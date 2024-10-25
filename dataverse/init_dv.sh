#!/usr/bin/env bash

# Check if already initialized
[[ -f "/dv/init/initialized" ]] && exit 0

bash /opt/payara/scripts/setup.sh &