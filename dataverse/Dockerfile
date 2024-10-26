# Author: Eryk Kulikowski @ KU Leuven (2024). Apache 2.0 License
ARG BASE_VERSION=unstable
FROM gdcc/base:${BASE_VERSION}

# The first section requires root privileges
USER root

# Install scripting tools
RUN apt-get -y update; apt-get -y install curl jq ed bc sed gawk

# Change application user and group id
ARG USER_ID=1000
ARG GROUP_ID=1000
RUN usermod -u ${USER_ID} payara \
 && groupmod -g ${GROUP_ID} payara
RUN chown -R -h payara:payara ${HOME_DIR} &>/dev/null || true

# The rest needs to run as application user
USER payara

# deploy WAR file
ARG DATAVERSE_WAR_URL=https://github.com/ErykKul/dataverse/releases/download/OIDC/dataverse-6.4.war
ADD --chown=payara:payara ${DATAVERSE_WAR_URL} ${DEPLOY_DIR}

VOLUME ${DOMAIN_DIR}/docroot
WORKDIR ${DOMAIN_DIR}/docroot