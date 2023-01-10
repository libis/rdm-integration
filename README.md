# rdm-integration
This is a work in progress and at this point only for GitHub, GitLab and Irods integration into the Dataverse (other systems will be added as work progresses).

With this application, files can be synchronized between different source repositories and a Dataverse repository. The application uses background processes for the synchronization of the files, as well as, for hashing of the local files when the source repository uses different hash type from Dataverse. The hashes are then used for the comparing of the files. The frontend application does not need to be running when the synchronization is running on the server, and multiple synchronizations for different users can run simultaneously, each on its own go-routine.

In order to integrate a new repository type, you need to implement a new plugin for the backend. The plugins are implemented in the /image/app/plugin/impl folder (each having its own package). The new plugin implementation must be then registered in the /image/app/plugin/registry.go file. As can be seen in the same file, a plugin implements functions that are required by the Plugin type:
```
type Plugin struct {
	Query   func(req types.CompareRequest) (map[string]tree.Node, error)
	Options func(params types.OptionsRequest) ([]string, error)
	Streams func(ctx context.Context, in map[string]tree.Node, streamParams types.StreamParams) (map[string]types.Stream, error)
}
```

Note that the Plugin type is a struct and cannot hold any state, as it has no fields. Therefore, the plugin implementations are stateless and all state, caching, etc., are handled by the application, independently of the used plugin. This makes the plugins easier to implement. Each plugin implements these tree functions:
- Query: using the standard fields as provided in the "types.CompareRequest" (username, api token, URL, etc.) this function queries the repository for files. The result is a flat mapping of files found on the repository to their paths. A file is represented by a "tree.Node" type containing the file name, file path, hash type and hash value, etc.
- Options: this function lists branches (or folders in the case of IRODS, this is also the reason for choosing a more generic name "options" i.s.o. "branches") applicable for the current repository. It can be only called when the user has provided the credentials needed to call the repository. These credentials are then provided in the "types.OptionsRequest" value.
- Streams: files are synchronized using streams from the source repository to the filesystem, where each file has its own stream. This function implements "types.Stream" objects for the provided files (the "in" parameter contains a filtered list of files that are going to be copied from the repository). Notably, a "types.Stream" object contains a function for opening a stream to the provided file and a function to close that stream.

After implementing the above-mentioned functions on the backend, you need to extend the frontend (https://github.com/libis/rdm-integration-frontend) by adding a frontend plugin in "plugin.service.ts". This is a straight forward implementation of the RepoPlugin type as defined in the "plugin.ts" model. It basically tells the frontend that there is a new repository type, which field should be shown on the connect page and how these fields should be named, etc., for the given repository type.

In order to run the application locally, checkout in the same folder this repository (rdm-integration from https://github.com/libis/rdm-integration) and the frontend (rdm-integration-frontend from https://github.com/libis/rdm-integration-frontend). Then go to /rdm-integration and run "make run". Notice that if you do not run standard Libis rdm (Dataverse) locally, you will need to define environment variables as defined in /image/app/env.go

You can also use make commands to build the docker image (make build) or push to the docker repository (make push).

In order to redeploy the integration application on pilot/prod (after building with make build with env set to prod):
- ssh to the server
- make pull
- make stop-integration
- make up

## Sequence diagrams

### GET options
Listing branches, folders, etc., that can be chosen in dropdown and on the connect page is a synchronous call. When retrieved, a branch or folder can be selected by the user as reference from where the files will be synchronized. The listing itself is implemented by a plugin and is described in the following sequence diagram:

```mermaid
sequenceDiagram
    Frontend->>+Backend: GET options
    Backend->>Repo: Specific call, e.g., list branches
    Repo-->>Backend: List of branches
    Backend-->>-Frontend: List of options for the dropdown
```

### Compare files

```mermaid
sequenceDiagram
    Frontend->>+Backend: /api/plugin/compare
    Backend->>+Goroutine: Compare using Key as ref.
    activate Goroutine
    Backend-->>Frontend: Key
    loop Until cached response ready
    	Frontend->>Backend: api/common/cached
	Backend->>Redis: Get(key)
	Redis-->>Backend: Cached response if ready
        Backend-->>Frontend: Cached response if ready
    end
    Goroutine->>Dataverse: List files
    Dataverse-->>Goroutine: List of files
    Goroutine->>Repo: List files
    Repo-->>Goroutine: List of files
    Goroutine->>Redis: Get known hashes
    Redis-->>Goroutine: Known hashes
    Goroutine->>Redis: Hashing job for unknown hashes
    Goroutine->>Redis: Cached response is ready
    deactivate Goroutine
    loop Until all hashes known
    	Frontend->>Backend: api/common/compare
	Backend->>Redis: Get(key)
	Redis-->>Backend: Response
        Backend-->>Frontend: Not all hashes known
    end
    Worker->>Redis: Get new job
    Redis-->>Worker: Hashing job
    activate Worker
    loop Until all hashes known
    	Worker-->>Worker: Calculate N hashes
	Worker->>Redis: Store calculated hashes
    end
    Worker->>Redis: All hashes known
    deactivate Worker
```
