package main

import (
    "math/rand"
	"crypto/tls"
	"integration/app/logging"
	"integration/app/utils"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"
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
		numberWorkers = 1
	}
	logging.Logger.Println("nuber workers:", numberWorkers)

	// start workers in background
	for i := 0; i < numberWorkers; i++ {
		time.Sleep(time.Duration(rand.Intn(10)) * time.Second)
		go utils.ProcessJobs()
	}

	// wait for termination
	signalChannel := make(chan os.Signal, 2)
    signal.Notify(signalChannel, os.Interrupt, syscall.SIGTERM)
    go func() {
        sig := <-signalChannel
        switch sig {
        case os.Interrupt, syscall.SIGTERM:
			logging.Logger.Println("quiting...")
			close(utils.Stop)
        }
    }()

	utils.Wait.Wait()
}
