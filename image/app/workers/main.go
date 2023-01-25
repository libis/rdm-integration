// Author: Eryk Kulikowski @ KU Leuven (2023). Apache 2.0 License

package main

import (
	"crypto/tls"
	"integration/app/logging"
	"integration/app/workers/spinner"
	"net/http"
	"os"
	"strconv"
)

func main() {
	http.DefaultTransport.(*http.Transport).TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
	numberWorkers := 0
	var err error
	if len(os.Args) > 1 {
		numberWorkers, err = strconv.Atoi(os.Args[1])
		if err != nil {
			logging.Logger.Println("failed to parse number of workers from", numberWorkers)
		}
	}
	if numberWorkers <= 0 {
		numberWorkers = 200
	}
	logging.Logger.Println("nuber workers:", numberWorkers)
	spinner.SpinWorkers(numberWorkers)
}
