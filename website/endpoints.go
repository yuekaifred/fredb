package website

import "strings"

type Endpoint struct {
	Method string
	Path   string
	Desc   string
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
	{Method: "GET", Path: "/key/KEY", Desc: "Fetch the value stored under a key."},
	{Method: "PUT", Path: "/key/KEY", Desc: "Add or update the value of a key."},
	{Method: "DELETE", Path: "/key/KEY", Desc: "Delete a key."},
	{Method: "GET", Path: "/range?start=START&end=END", Desc: "List every key/value pair between two keys, alphabetically."},
}
