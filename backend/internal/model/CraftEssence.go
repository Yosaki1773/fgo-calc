package model

import "encoding/json"

type CraftEssence struct {
	Id      int      `json:"id"`
	Name    string   `json:"name"`
	Img     string   `json:"img"`
	Cost    int      `json:"cost"`
	Server  string   `json:"server"`
	Filters []Filter `json:"filters"`
}

type Filter struct {
	Traits []int
	Effect float64
}

func (f *Filter) UnmarshalJSON(data []byte) error {
	var raw []json.RawMessage
	json.Unmarshal(data, &raw)
	json.Unmarshal(raw[0], &f.Traits)
	json.Unmarshal(raw[1], &f.Effect)
	return nil
}