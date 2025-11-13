# Author: Eryk Kulikowski @ KU Leuven (2024). Apache 2.0 License
ARG BASE_VERSION=6.6-noble
FROM gdcc/base:${BASE_VERSION}

# The first section requires root privileges
USER root

# Install scripting tools
RUN apt-get -y update; apt-get -y install curl jq ed bc sed gawk

# Change application user and group id
ARG USER_ID=1000
ARG GROUP_ID=1000
# Handle macOS case where GID 20 is the staff group
RUN if getent group ${GROUP_ID} >/dev/null && [ "$(getent group ${GROUP_ID} | cut -d: -f1)" != "payara" ]; then \
        echo "Group ID ${GROUP_ID} already exists as $(getent group ${GROUP_ID} | cut -d: -f1), keeping payara group as is"; \
    else \
        groupmod -g ${GROUP_ID} payara; \
    fi \
 && usermod -u ${USER_ID} payara
RUN chown -R -h payara:payara ${HOME_DIR} &>/dev/null || true

# The rest needs to run as application user
USER payara

# deploy WAR file
ARG DATAVERSE_WAR_URL=https://github.com/IQSS/dataverse/releases/download/v6.6/dataverse-6.6.war
ADD --chown=payara:payara ${DATAVERSE_WAR_URL} ${DEPLOY_DIR}

VOLUME ${DOMAIN_DIR}/docroot
WORKDIR ${DOMAIN_DIR}/docroot