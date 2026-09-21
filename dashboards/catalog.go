// Package dashboards exposes the same reviewed definitions provisioned to Grafana.
package dashboards

import (
	"embed"
	"encoding/json"
	"fmt"
	"sort"
)

//go:embed managed/*.json
var files embed.FS

type Source struct {
	Type string `json:"type"`
	UID  string `json:"uid"`
}
type Target struct {
	LegendFormat string `json:"legendFormat,omitempty"`
	RefID        string `json:"refId"`
	Expr         string `json:"expr,omitempty"`
	Query        string `json:"query,omitempty"`
	Instant      bool   `json:"instant,omitempty"`
	Source       Source `json:"datasource,omitempty"`
}
type Variable struct {
	Name       string          `json:"name"`
	Label      string          `json:"label"`
	Type       string          `json:"type"`
	Query      json.RawMessage `json:"query"`
	Source     Source          `json:"datasource"`
	IncludeAll bool            `json:"includeAll"`
	AllValue   string          `json:"allValue"`
	Regex      string          `json:"regex"`
	Current    struct {
		Value json.RawMessage `json:"value"`
	} `json:"current"`
}

func (v Variable) Default() string {
	var value string
	if json.Unmarshal(v.Current.Value, &value) != nil {
		return "__all__"
	}
	if value == "$__all" {
		return "__all__"
	}
	if v.Type == "textbox" && value == ".*" {
		return ""
	}
	return value
}

func (v Variable) Expression() string {
	var s string
	if json.Unmarshal(v.Query, &s) == nil {
		return s
	}
	var o struct {
		Query string `json:"query"`
	}
	_ = json.Unmarshal(v.Query, &o)
	return o.Query
}

type Panel struct {
	ID          int      `json:"id"`
	Title       string   `json:"title"`
	Type        string   `json:"type"`
	Description string   `json:"description,omitempty"`
	Source      Source   `json:"datasource"`
	Targets     []Target `json:"targets"`
	FieldConfig struct {
		Defaults struct {
			Unit string `json:"unit"`
		} `json:"defaults"`
	} `json:"fieldConfig"`
	Options struct {
		ReduceOptions json.RawMessage `json:"reduceOptions,omitempty"`
		Content       string          `json:"content,omitempty"`
		Mode          string          `json:"mode,omitempty"`
	} `json:"options"`
	Panels          []Panel           `json:"panels,omitempty"`
	Transformations []json.RawMessage `json:"transformations,omitempty"`
}
type Dashboard struct {
	UID        string  `json:"uid"`
	Title      string  `json:"title"`
	Panels     []Panel `json:"panels"`
	Templating struct {
		List []Variable `json:"list"`
	} `json:"templating"`
}

func Load() ([]Dashboard, error) {
	entries, err := files.ReadDir("managed")
	if err != nil {
		return nil, err
	}
	result := []Dashboard{}
	for _, e := range entries {
		raw, err := files.ReadFile("managed/" + e.Name())
		if err != nil {
			return nil, err
		}
		var d Dashboard
		if err = json.Unmarshal(raw, &d); err != nil {
			return nil, fmt.Errorf("catalog %s: %w", e.Name(), err)
		}
		var panels []Panel
		var flatten func([]Panel)
		flatten = func(ps []Panel) {
			for _, p := range ps {
				if p.Type != "row" {
					panels = append(panels, p)
				}
				flatten(p.Panels)
			}
		}
		flatten(d.Panels)
		d.Panels = panels
		result = append(result, d)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Title < result[j].Title })
	return result, nil
}
