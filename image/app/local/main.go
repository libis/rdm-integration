package main

import (
	"fmt"
	"integration/app/logging"
	"integration/app/server"
	"integration/app/workers/spinner"
	"os/exec"
	"runtime"
)

func main() {
	go server.Start()
	openbrowser("http://localhost:7788/")
	spinner.SpinWorkers(1)
}

func openbrowser(url string) {
	var err error

	switch runtime.GOOS {
	case "linux":
		err = exec.Command("xdg-open", url).Start()
	case "windows":
		err = exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Start()
	case "darwin":
		err = exec.Command("open", url).Start()
	default:
		err = fmt.Errorf("unsupported platform")
	}
	if err != nil {
		logging.Logger.Fatal(err)
	}
}
