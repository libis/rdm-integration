package frontend

import (
	_ "embed"
	"integration/app/logging"
	"net/http"
	"os"
)

//go:embed default_frontend_config.json
var config []byte

func init() {
	// read configuration
	configFile := os.Getenv("FRONTEND_CONFIG_FILE")
	b, err := os.ReadFile(configFile)
	if err != nil {
		logging.Logger.Printf("config file %v not found: using default frontend config file\n", configFile)
	} else {
		config = b
	}
}

func Config(w http.ResponseWriter, r *http.Request) {
	w.Write(config)
}
