package main

import (
	"encoding/json"
	"os"
	"text/template"
)

func check(err error) {
	if err != nil {
		panic(err)
	}
}

func main() {
	var project struct {
		Name string
		Info struct {
			CompanyName, ProductName, ProductVersion, Copyright string
			FileAssociations                                    []struct{ Ext, Name, Description, IconName, Role string }
			Protocols                                           []struct{ Scheme, Description, Role string }
		}
	}
	config, err := os.ReadFile(os.Args[2])
	check(err)
	check(json.Unmarshal(config, &project))
	source, err := os.ReadFile(os.Args[1])
	check(err)
	parsed, err := template.New("wails_tools").Option("missingkey=error").Parse(string(source))
	check(err)
	out, err := os.Create(os.Args[3])
	check(err)
	check(parsed.Execute(out, project))
	check(out.Close())
}
