package website

import "strings"

func loadExample(id, ext string) string {
	b, err := staticFS.ReadFile("static/_examples/" + id + "/example." + ext)
	if err != nil {
		panic(err)
	}
	return strings.TrimRight(string(b), "\n")
}

func init() {
	for i := range endpoints {
		e := &endpoints[i]
		e.Curl = loadExample(e.ID, "sh")
		e.JS = loadExample(e.ID, "js")
		e.Go = loadExample(e.ID, "go")
		e.Python = loadExample(e.ID, "py")
	}
}

type ShapeRow struct {
	Kind  string
	Value string
}

type Endpoint struct {
	ID     string
	Method string
	Path   string
	Desc   string
	Rows   []ShapeRow
	Curl   string
	JS     string
	Go     string
	Python string
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

const apiHost = "db.fredyang.com"

var endpoints = []Endpoint{
	{
		ID:     "get",
		Method: "GET",
		Path:   "/key/KEY",
		Desc:   "Get a value from a key.",
		Rows: []ShapeRow{
			{"header", "X-Api-Key: your key"},
			{"path param", "key"},
			{"body", "—"},
			{"response", "200, raw value as text"},
		},
	},
	{
		ID:     "put",
		Method: "PUT",
		Path:   "/key/KEY",
		Desc:   "Add or update a value from a key.",
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
		Desc:   "Delete a key/value pair.",
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
		Desc:   "Get a list of key/value pairs between two keys, determined alphabetically.",
		Rows: []ShapeRow{
			{"header", "X-Api-Key: your key"},
			{"query params", "start, end: key range bounds"},
			{"body", "—"},
			{"response", "200, JSON array of key/value pairs"},
		},
	},
}
