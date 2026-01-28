// Author: Eryk Kulikowski @ KU Leuven (2023). Apache 2.0 License

package types

type SelectItem struct {
	Label    string      `json:"label"`
	Value    interface{} `json:"value"`
	Selected bool        `json:"selected,omitempty"`
}
