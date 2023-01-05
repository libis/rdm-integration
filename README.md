# rdm-integration
This is a work in progress and at this point only for GitHub, GitLab and Irods integration into the Dataverse (other systems will be added as work progresses).

In order to add a new repository type, you need to implement on the backend:
- specific compare method, similar to, e.g., /image/app/ir/irods.go and register it in main.go
- implement listing branches (or folders, etc.), if applicable, in /image/app/utils/streams.go
- implement specific streams in /image/app/utils/streams.go that can download files from the new repository type

After implementing the above-mentioned methods on the backend, you need to extend the frontend (https://github.com/libis/rdm-integration-frontend):
- add new repository type in the dropdown on the connect page
- adjust the required fields, error messages, credentials, etc. in the connect page component
- implement branches/folders dropdown items lookup (if applicable for the given repository type)
- add the specific compare method from the backend in the data.service.ts
- in submit.service.ts add the specific stream parameters as required by the newly implemented function in /image/app/utils/streams.go

In order to run the application locally, checkout in the same folder this repository (rdm-integration from https://github.com/libis/rdm-integration) and the frontend (rdm-integration-frontend from https://github.com/libis/rdm-integration-frontend). Then go to /rdm-integration and run "make run". Notice that if you do not run standard Libis rdm (Dataverse) locally, you will need to define environment variables as defined in /image/app/env.go

You can also use make commands to build the docker image (make build) or push to the docker repository (make push).

In order to redeploy the integration application on pilot/prod (after building with make build with env set to prod):
- ssh to the server
- make pull
- make stop-integration
- make up