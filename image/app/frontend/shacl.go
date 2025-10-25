// Author: Eryk Kulikowski @ KU Leuven (2025). Apache 2.0 License

package frontend

import (
	_ "embed"
	"net/http"
	"os"

	"integration/app/logging"
)

//go:embed default_shacl_shapes.ttl
var defaultShaclShapes []byte

var shaclShapes = defaultShaclShapes

func init() {
	if override := os.Getenv("FRONTEND_SHACL_FILE"); override != "" {
		if b, err := os.ReadFile(override); err == nil {
			logging.Logger.Printf("using SHACL shapes from %v\n", override)
			shaclShapes = b
		} else {
			logging.Logger.Printf("failed to read SHACL shapes from %v: %v\n", override, err)
		}
	}
}

func GetShaclShapes(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/turtle; charset=utf-8")
	if _, err := w.Write(shaclShapes); err != nil {
		logging.Logger.Printf("failed to write SHACL shapes response: %v\n", err)
	}
}
