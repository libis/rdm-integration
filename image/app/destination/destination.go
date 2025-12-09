// Author: Eryk Kulikowski @ KU Leuven (2023). Apache 2.0 License

package destination

import (
	"integration/app/core"
	"integration/app/dataverse"
)

func SetDataverseAsDestination() {
	core.Destination = core.DestinationPlugin{
		IsDirectUpload:        dataverse.IsDirectUpload,
		CheckPermission:       dataverse.CheckPermission,
		CreateNewRepo:         dataverse.CreateNewDataset,
		GetDatasetVersion:     dataverse.GetDatasetVersion,
		GetRepoUrl:            dataverse.GetDatasetUrl,
		WriteOverWire:         dataverse.ApiAddReplaceFile,
		SaveAfterDirectUpload: dataverse.SaveAfterDirectUpload,
		CleanupLeftOverFiles:  dataverse.CleanupLeftOverFiles,
		DeleteFile:            dataverse.DeleteFile,
		Options:               dataverse.DvObjects,
		DownloadableOptions:   dataverse.DownloadableDvObjects,
		GetStream:             dataverse.DownloadFile,
		Query:                 dataverse.GetNodeMap,
		GetUserEmail:          dataverse.GetUserEmail,
		GetDatasetMetadata:    dataverse.GetDatasetMetadata,
		GetDataFileDDI:        dataverse.GetDataFileDDI,
	}
}
