// Author: Eryk Kulikowski @ KU Leuven (2023). Apache 2.0 License

package dataverse

import (
	"github.com/libis/rdm-dataverse-go-api/api"
)

type Configuration struct {
	AdminApiKey string `json:"adminApiKey,omitempty"`
	UnblockKey  string `json:"unblockKey,omitempty"`
}

var Config = map[string]Configuration{}

func NewClient(pluginId, server, user, token string) *api.Client {
	res := api.NewClient(server)
	res.User = user
	res.Token = token
	c := Config[pluginId]
	res.AdminApiKey = c.AdminApiKey
	res.UnblockKey = c.UnblockKey
	return res
}
