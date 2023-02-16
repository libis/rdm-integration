// Author: Eryk Kulikowski @ KU Leuven (2023). Apache 2.0 License

package spinner

import (
	"integration/app/core"
	"integration/app/logging"
	"math/rand"
	"os"
	"os/signal"
	"syscall"
	"time"
)

func SpinWorkers(numberWorkers int) {
	// start workers in background
	for i := 0; i < numberWorkers; i++ {
		if numberWorkers > 1 {
			time.Sleep(time.Duration(rand.Intn(10000/numberWorkers)) * time.Millisecond)
		}
		core.Wait.Add(1)
		go core.ProcessJobs()
	}

	// wait for termination
	signalChannel := make(chan os.Signal, 2)
	signal.Notify(signalChannel, os.Interrupt, syscall.SIGTERM)
	go func() {
		sig := <-signalChannel
		switch sig {
		case os.Interrupt, syscall.SIGTERM:
			logging.Logger.Println("quiting...")
			close(core.Stop)
		}
	}()
	logging.Logger.Println("workers ready")

	core.Wait.Wait()
	logging.Logger.Println("exit")
}
