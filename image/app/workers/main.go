// Author: Eryk Kulikowski @ KU Leuven (2023). Apache 2.0 License

package main

import (
	"crypto/tls"
	"integration/app/destination"
	"integration/app/logging"
	"integration/app/workers/spinner"
	"net/http"
	"os"
	"strconv"
)

func main() {
	destination.SetDataverseAsDestination()
	http.DefaultTransport.(*http.Transport).TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
	numberWorkers := -1
	queue := "ALL"
	var err error
	if len(os.Args) > 1 {
		numberWorkers, err = strconv.Atoi(os.Args[1])
		if err != nil {
			logging.Logger.Println("failed to parse number of workers from", numberWorkers)
		}
	}
	if len(os.Args) > 2 {
		queue = os.Args[2]
	}
	if numberWorkers < 0 {
		numberWorkers = 200
	}
	logging.Logger.Println("nuber workers:", numberWorkers)
	spinner.SpinWorkers(numberWorkers, queue)
}
