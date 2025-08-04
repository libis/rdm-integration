// Author: Eryk Kulikowski @ KU Leuven (2023). Apache 2.0 License

package spinner

import (
	"integration/app/config"
	"integration/app/core"
	"integration/app/logging"
	"math/rand"
	"os"
	"os/signal"
	"syscall"
	"time"
)

func SpinWorkers(numberWorkers int, queue string) {
	// start workers in background
	for i := 0; i < numberWorkers; i++ {
		// Reduce random sleep time and only apply for multiple workers to avoid thundering herd
		if numberWorkers > 1 {
			// Stagger worker startup with smaller delays
			maxDelay := 1000 // 1 second max
			if numberWorkers < 100 {
				maxDelay = 10000 / numberWorkers // Scale delay based on number of workers
			}
			time.Sleep(time.Duration(rand.Intn(maxDelay)) * time.Millisecond)
		}
		if queue == "ALL" {
			core.Wait.Add(1)
			go core.ProcessJobs("") // sync/hashing queue
			for _, q := range config.GetConfig().Options.ComputationQueues {
				core.Wait.Add(1)
				go core.ProcessJobs(q.Value)
			}
		} else {
			core.Wait.Add(1)
			go core.ProcessJobs(queue)
		}
	}

	// wait for termination
	signalChannel := make(chan os.Signal, 2)
	signal.Notify(signalChannel, os.Interrupt, syscall.SIGTERM)
	go func() {
		sig := <-signalChannel
		switch sig {
		case os.Interrupt, syscall.SIGTERM:
			logging.Logger.Println("quitting...")
			close(core.Stop)
		}
	}()
	logging.Logger.Println("workers ready")

	core.Wait.Wait()
	logging.Logger.Println("exit")
}
