#!/usr/bin/env bash

# based on https://github.com/libis/rdm-build/blob/pre-6/images/dataverse/bin/bootstrap-job.sh
# and https://github.com/libis/rdm-build/blob/pre-6/images/dataverse/dvinstall/setup-once.sh

# wait for Dataverse to start up
until curl --output /dev/null --silent --head --fail http://localhost:8080/api/v1/info/version
do sleep 1; done

# Fail on any error
set -euo pipefail

. /scripts/setup-tools

ADMIN_PASSWORD=$(cat "/run/secrets/password")
API_KEY=$(cat "/run/secrets/api/key")

# admin user (and as superuser)
api PUT 'admin/settings/BuiltinUsers.KEY' -d "temporary-password"
datafile "builtin-users?password=${ADMIN_PASSWORD}&key=temporary-password" "$(data_file user-admin.json)"
adminKey="$(echo "${REPLY}" | jq .data.apiToken | tr -d \")"
echo $adminKey > /run/secrets/api/adminkey
api POST "admin/superuser/$(jq -r '.userName' $(data_file user-admin.json))"

# service account for guest Globus downloads (read-only access to public datasets)
datafile "builtin-users?password=${ADMIN_PASSWORD}&key=temporary-password" "$(data_file user-globus-download.json)"

api DELETE 'admin/settings/BuiltinUsers.KEY'
api PUT 'admin/settings/:BlockedApiKey' -d "${API_KEY}"
api PUT 'admin/settings/:BlockedApiPolicy' -d 'unblock-key'
api PUT 'admin/settings/:BlockedApiEndpoints' -d 'admin,builtin-users'

# authentication providers
superAdmin datafiles_loop 'admin/authenticationProviders/' authentication-providers

# metadata blocks
superAdmin api GET 'admin/datasetfield/loadNAControlledVocabularyValue'
superAdmin datafiles_loop 'admin/datasetfield/load' metadatablocks '*.tsv' 'text/tab-separated-values'

# builtin roles
superAdmin datafiles_loop 'admin/roles' roles

# Add the groups to the system.
superAdmin datafiles_loop 'admin/groups/ip' groups

# root Dataverse collection
file="$(data_file dv-root.json)"
superAdmin datafile "dataverses" "$file"
alias="$(jq -r '.alias' "$file")"
superAdmin api POST "dataverses/$alias/actions/:publish"

# metadata blocks for the root collection
superAdmin datafiles_loop 'dataverses/${/0}/metadatablocks' collections/metadatablocks

# default facets for the root collection
superAdmin datafiles_loop 'dataverses/${/0}/facets' collections/facets

# user groups for the root collection
superAdmin datafiles_loop 'dataverses/${/0}/groups' collections/groups

# group assignments for the root collection
superAdmin datafiles_loop 'dataverses/${/0}/groups/${/1}/roleAssignees' collections/group-assignments

# role assignments for the root collection
superAdmin datafiles_loop 'dataverses/${/0}/assignments' collections/role-assignments

# licenses
superAdmin datafiles_loop 'licenses' licenses

# default license
superAdmin api PUT "licenses/default/2"

# external tools
superAdmin datafiles_loop 'admin/externalTools' external-tools

# settings
# native/http upload method
superAdmin settings_loop 'admin/settings' settings

# solr
curl -f -s "http://localhost:8080/api/admin/index/solr/schema?unblock-key=${API_KEY}" | /scripts/update-fields.sh /solr/data/data/collection1/schema.xml
curl -f -sS "http://solr:8983/solr/admin/cores?action=RELOAD&core=collection1" >/dev/null

# reindex
superAdmin api DELETE "admin/index/timestamps"
superAdmin api GET "admin/index/continue"

# setup done
touch /dv/initialized