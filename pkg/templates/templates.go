package templates

import (
	"bytes"
	"embed"
	"text/template"

	"github.com/chadsmith12/berth/pkg/plan"
)

//go:embed templates/*.tmpl
var FS embed.FS

var funcMap = template.FuncMap{
	"seq": func(n int) []int {
		s := make([]int, n)
		for i := range n {
			s[i] = i + 1
		}
		return s
	},
}

func render(name string, data any) (string, error) {
	t, err := template.New(name).Funcs(funcMap).ParseFS(FS, "templates/"+name)
	if err != nil {
		return "", err
	}
	var buf bytes.Buffer
	if err := t.Execute(&buf, data); err != nil {
		return "", err
	}
	return buf.String(), nil
}

func RenderDockerfile(p plan.Plan) (string, error) {
	return render("Dockerfile.tmpl", p)
}

func RenderCompose(p plan.Plan) (string, error) {
	return render("docker-compose.yml.tmpl", p)
}

func RenderEntrypoint(p plan.Plan) (string, error) {
	return render("entrypoint.sh.tmpl", p)
}

func RenderDockerignore(p plan.Plan) (string, error) {
	return render(".dockerignore.tmpl", p)
}
