package website

import "strings"

type ShapeRow struct {
	Label string
	Value string
}

type Endpoint struct {
	ID     string
	Method string
	Path   string
	Desc   string
	Rows   []ShapeRow
}

func (e Endpoint) MethodClass() string {
	return "method " + strings.ToLower(e.Method)
}

func (e Endpoint) PathHTML() string {
	s := e.Path
	for _, ph := range []string{"KEY", "START", "END"} {
		s = strings.ReplaceAll(s, ph, `<span class="path-param">`+ph+`</span>`)
	}
	return s
}

var endpoints = []Endpoint{
	{
		ID:     "get",
		Method: "GET",
		Path:   "/key/KEY",
		Desc:   "Fetch the value stored under a key.",
		Rows: []ShapeRow{
			{"header", "X-Api-Key: your key."},
			{"path param", "key"},
			{"body", "—"},
			{"response", "200, raw value as text"},
		},
	},
	{
		ID:     "put",
		Method: "PUT",
		Path:   "/key/KEY",
		Desc:   "Add or update the value of a key.",
		Rows: []ShapeRow{
			{"header", "X-Api-Key: your key"},
			{"path param", "key"},
			{"body", "value"},
			{"response", "204, empty"},
		},
	},
	{
		ID:     "delete",
		Method: "DELETE",
		Path:   "/key/KEY",
		Desc:   "Delete a key.",
		Rows: []ShapeRow{
			{"header", "X-Api-Key: your key"},
			{"path param", "key"},
			{"body", "—"},
			{"response", "204, empty"},
		},
	},
	{
		ID:     "range",
		Method: "GET",
		Path:   "/range?start=START&end=END",
		Desc:   "List every key/value pair between two keys, determined alphabetically.",
		Rows: []ShapeRow{
			{"header", "X-Api-Key: your key"},
			{"query params", "start, end: key range bounds"},
			{"body", "—"},
			{"response", "200, JSON array of key/value pairs"},
		},
	},
}
