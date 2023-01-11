# rdm-integration
With this application, files can be synchronized between different source repositories and a Dataverse repository. The application uses background processes for the synchronization of the files, as well as, for hashing of the local files when the source repository uses different hash type from Dataverse. The hashes are then used for the comparing of the files. The frontend application does not need to be running when the synchronization is running on the server, and multiple synchronizations for different users can run simultaneously, each on its own go-routine.

This is a work in progress and at this point only GitHub, GitLab and Irods plugins are implemented. Other plugins for other types of source repositories will be added as work progresses.

## Prerequisites
For building the frontend, you need to have [Angular CLI](https://github.com/angular/angular-cli) installed. If you want to run the application locally without the Docker containers, you will need to have the latest Go installed. If you wish to build the application's container, you will need to have the Docker installed. For running and building the applications using the make commands, you will need to have make installed.

## Running the application
In order to run the application locally, checkout in the same folder this repository (rdm-integration from https://github.com/libis/rdm-integration) and the frontend repository (rdm-integration-frontend from https://github.com/libis/rdm-integration-frontend). Then go to /rdm-integration directory and run "make run". Notice that if you do not run standard Libis RDM (Dataverse) locally, you will need to define environment variables as defined in /image/app/env.go, such that this application could connect to your Dataverse of choice.

You can also use make commands to build the docker image (make build) or push to the docker repository (make push).

In order to redeploy the integration application on Libis pilot/prod (after building with make build with env set to prod):
- ssh to the server
- make pull
- make stop-integration
- make up

## Writting a new plugin
In order to integrate a new repository type, you need to implement a new plugin for the backend. The plugins are implemented in the /image/app/plugin/impl folder (each having its own package). The new plugin implementation must be then registered in the /image/app/plugin/registry.go file. As can be seen in the same file, a plugin implements functions that are required by the Plugin type:
```
type Plugin struct {
	Query   func(req types.CompareRequest) (map[string]tree.Node, error)
	Options func(params types.OptionsRequest) ([]string, error)
	Streams func(ctx context.Context, in map[string]tree.Node, streamParams types.StreamParams) (map[string]types.Stream, error)
}
```

Note that the Plugin type is a struct and cannot hold any state, as it has no fields. Therefore, the plugin implementations are stateless and all state, caching, etc., are handled by the application, independently of the used plugin. This makes the plugins easier to implement. Each plugin implements these tree functions:
- Query: using the standard fields as provided in the "types.CompareRequest" (username, api token, URL, etc.) this function queries the repository for files. The result is a flat mapping of files found on the repository to their paths. A file is represented by a "tree.Node" type containing the file name, file path, hash type and hash value, etc. Notice that it does not contain the file itself.
- Options: this function lists branches (or folders in the case of IRODS, this is also the reason for choosing a more generic name "options" i.s.o. "branches") applicable for the current repository. It can be only called when the user has provided the credentials needed to call the repository (this is verified at frontend). These credentials are then provided in the "types.OptionsRequest" value.
- Streams: files are synchronized using streams from the source repository to the filesystem, where each file has its own stream. This function implements "types.Stream" objects for the provided files (the "in" parameter contains a filtered list of files that are going to be copied from the repository). Notably, a "types.Stream" object contains a function for opening a stream to the provided file and a function to close that stream.

After implementing the above-mentioned functions on the backend, you need to extend the frontend (https://github.com/libis/rdm-integration-frontend) by adding a frontend plugin in "plugin.service.ts". This is a straight forward implementation of the RepoPlugin type as defined in the "plugin.ts" model. It basically tells the frontend that there is a new repository type, which fields should be shown on the connect page and how these fields should be named, etc., for the given repository type.

## Sequence diagrams

### Get options
Listing branches, folders, etc. (depending on the repo plugin) that can be chosen in dropdown and on the connect page is a synchronous call. When retrieved, a branch or folder can be selected by the user as reference from where the files will be synchronized. The listing itself is implemented by a plugin and is described in the following sequence diagram:

```mermaid
sequenceDiagram
    Frontend->>+Backend: /api/plugin/options
    Backend->>Repo: Specific call, e.g., list branches
    Repo-->>Backend: List of branches
    Backend-->>-Frontend: List of options for the dropdown
```

### Get dataverse collections

```mermaid
sequenceDiagram
    Frontend->>+Backend: /api/common/collections
    loop Until all pages are retrieved
    	Backend->>Dataverse: /api/v1/mydata/retrieve
	Dataverse->>Backend: Dataverse collections
    end
    Backend-->>-Frontend: Dataverse collections
```

### Get datasets
Another dropdown on the connect page lets the user to specify to which dataset the files should be synchronized. This is also a synchronous call and the dropdown displays "Loading..." (the same is true for the get options call) until a response is recived from the backend. The backend uses the Dataverse /api/v1/mydata/retrieve api call and retrieves all pages in a loop (the call supports paging for the cases where the user has many datasets). This is depicted in the diagram below:

```mermaid
sequenceDiagram
    Frontend->>+Backend: /api/common/datasets
    loop Until all pages are retrieved
    	Backend->>Dataverse: /api/v1/mydata/retrieve
	Dataverse->>Backend: Datasets
    end
    Backend-->>-Frontend: Datasets
```

### Create new dataset
When a user wishes to synchronize the files to a new (not yet existing) dataset, the frontend provides an option of creating a new incomplete dataset. The newly created dataset has a minimal metadata that does is not valid. After the synchronization (in this case simply an upload from the source repository to the new dataset) is complete, the user is presented with a link to this dataset on the Dataverse installation where the datset was created. The user can then complete the metadata and make the new dataset valid and ready for review or publication.

The frontend needs to provide two parameters before the call to the backend can be made: a Dataverse application token proving the identity of the user (and proving that the user has permission to create new datasets), and optional Dataverse collection (depicted as "{{Datavere collection}}" path parameter in the sequence diagram below). When the collection is not provided (e.g., the collection selection is disabled in the frontend for the Libis RDM Dataverse installation), the default collection configured in the backend is used.

```mermaid
sequenceDiagram
    Frontend->>+Backend: /api/common/newdataset
    Backend->>Dataverse: POST /api/dataverses/{{Dataverse collection}}/datasets
    Dataverse-->>Backend: Response
    Backend-->>-Frontend: Persistent ID of the new datase
```

### Compare files


```mermaid
sequenceDiagram
    Frontend->>+Backend: /api/plugin/compare
    Backend->>+Goroutine: Compare using Key as ref.
    activate Goroutine
    Backend-->>Frontend: Key
    loop Until cached response ready
    	Frontend->>Backend: /api/common/cached
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
    	Frontend->>Backend: /api/common/compare
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

### Store changes
/api/common/store
