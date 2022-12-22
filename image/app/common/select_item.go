package common

type Token struct {
	Token string `json:"token"`
}

type SelectItem struct {
	Label string      `json:"label"`
	Value interface{} `json:"value"`
}
