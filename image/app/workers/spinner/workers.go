package spinner

import (
	"integration/app/logging"
	"integration/app/utils"
	"math/rand"
	"os"
	"os/signal"
	"syscall"
	"time"
)

func SpinWorkers(numberWorkers int) {
	// start workers in background
	for i := 0; i < numberWorkers; i++ {
		time.Sleep(time.Duration(rand.Intn(10000/numberWorkers)) * time.Millisecond)
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
	logging.Logger.Println("workers ready")

	utils.Wait.Wait()
	logging.Logger.Println("exit")
}
