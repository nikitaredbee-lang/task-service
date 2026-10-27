// Package api хранит OpenAPI спецификацию. Она встраивается в бинарник,
// и сервис сам отдает ее по /swagger/openapi.yaml.
package api

import _ "embed"

//go:embed openapi.yaml
var OpenAPI []byte
