// Author: Eryk Kulikowski @ KU Leuven (2023). Apache 2.0 License

package dataverse

import (
	"fmt"
	"integration/app/dataverse/api"
)

type Configuration struct {
	AdminApiKey string
	UnblockKey  string
	RoleIds     []int
}

var Config = map[string]Configuration{}

func newClient(pluginId, server, user, token string) (*api.Client, bool) {
	res := api.NewClient(server)
	res.User = user
	res.Token = token
	c := Config[pluginId]
	res.AdminApiKey = c.AdminApiKey
	res.UnblockKey = c.UnblockKey
	urlSigning := user != "" && res.AdminApiKey != "" && res.UnblockKey != ""
	return res, urlSigning
}

func roleIds(pluginId string) string {
	roleIds := Config[pluginId].RoleIds
	if len(roleIds) == 0 {
		roleIds = []int{6, 7}
	}
	res := ""
	for _, v := range roleIds {
		res = fmt.Sprintf("%v%v%v", res, "&role_ids=", v)
	}
	return res
}
