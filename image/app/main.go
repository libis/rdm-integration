package main

import (
	"fmt"
	"integration/app/logging"
	"integration/app/server"
	"integration/app/workers/spinner"
	"os"
	"strconv"
)

func main() {
	// spin workers if required (otherwise the workers are run independetly, see also workers/main.go)
	numberWorkers := 0
	var err error
	if len(os.Args) > 1 {
		numberWorkers, err = strconv.Atoi(os.Args[1])
		if err != nil {
			panic(fmt.Errorf("failed to parse number of workers from %v: %v", numberWorkers, err))
		}
	}
	if numberWorkers > 0 {
		logging.Logger.Println("nuber workers:", numberWorkers)
		go server.Start()
		spinner.SpinWorkers(numberWorkers)
	} else {
		logging.Logger.Println("http server only")
		server.Start()
	}
}
