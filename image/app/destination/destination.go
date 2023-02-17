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
		GetRepoUrl:            dataverse.GetDatasetUrl,
		WriteOverWire:         dataverse.ApiAddReplaceFile,
		SaveAfterDirectUpload: dataverse.SaveAfterDirectUpload,
		CleanupLeftOverFiles:  dataverse.CleanupLeftOverFiles,
		DeleteFile:            dataverse.DeleteFile,
		Options:               dataverse.DvObjects,
		GetStream:             dataverse.DownloadFile,
		Query:                 dataverse.GetNodeMap,
		GetUserEmail:          dataverse.GetUserEmail,
	}
}
