// Author: Eryk Kulikowski @ KU Leuven (2023). Apache 2.0 License

package main

import (
	"fmt"
	"integration/app/destination"
	"integration/app/logging"
	"integration/app/server"
	"integration/app/workers/spinner"
	"os"
	"os/exec"
	"strconv"
)

func main() {
	// spin workers if required (otherwise the workers are run independently, see also workers/main.go)
	numberWorkers := 0
	queue := "ALL"
	var err error
	oidcArgIdx := -1
	oidcArgs := []string{}
	for i := 0; i < len(os.Args); i++ {
		if os.Args[i] == "oidc" {
			oidcArgIdx = i
			if len(os.Args) > oidcArgIdx {
				oidcArgs = os.Args[oidcArgIdx+1:]
			}
			break
		}
	}
	if len(os.Args) > 1 && (oidcArgIdx < 0 || oidcArgIdx > 1) {
		numberWorkers, err = strconv.Atoi(os.Args[1])
		if err != nil {
			panic(fmt.Errorf("failed to parse number of workers from %v: %v", numberWorkers, err))
		}
	}
	if numberWorkers > 0 {
		destination.SetDataverseAsDestination()
		logging.Logger.Println("number workers:", numberWorkers)
		go server.Start()
		if oidcArgIdx > 0 {
			go spinner.SpinWorkers(numberWorkers, queue)
			err := exec.Command("/bin/oauth2-proxy", oidcArgs...).Run()
			fmt.Println(err)
		} else {
			spinner.SpinWorkers(numberWorkers, queue)
		}
	} else {
		logging.Logger.Println("http server only")
		if oidcArgIdx > 0 {
			go server.Start()
			err := exec.Command("/bin/oauth2-proxy", oidcArgs...).Run()
			fmt.Println(err)
		} else {
			server.Start()
		}
	}
}
