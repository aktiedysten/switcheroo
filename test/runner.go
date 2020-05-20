package main

import "os/exec"
import "time"
import "os"
import "net/http"
import "fmt"
import "io/ioutil"
import "os/signal"
import "syscall"

func main() {
	serverPath, err := exec.LookPath("./server")
	if err != nil {
		panic(err)
	}

	cleanupPath, err := exec.LookPath("./cleanup")
	if err != nil {
		panic(err)
	}

	/* run HTTP server and restart every 2 seconds; pass stdout/err thru
	 * as-is */
	go func() {
		for {
			cmd := exec.Command(serverPath)
			cmd.Stdout = os.Stdout
			cmd.Stderr = os.Stderr
			err := cmd.Start()
			if err != nil {
				panic(err)
			}
			time.Sleep(2 * time.Second)
		}
	}()

	/* wait a bit and then start spamming the "incoming port" with HTTP
	 * requests using a whole bunch of goroutines; we panic if there's ANY
	 * problem (wrong status/response, I/O failure,...), so test success
	 * depends on not dropping a single connection */
	time.Sleep(500 * time.Millisecond)
	for i := 0; i < 500; i++ {
		go func(i int) {
			for {
				resp, err := http.Get(fmt.Sprintf("http://127.0.0.1:9999?s=%d", i+10))
				if err != nil {
					panic(err)
				}
				if resp.StatusCode != 200 {
					panic(fmt.Sprintf("expected 200, got %d", resp.StatusCode))
				}
				all, err := ioutil.ReadAll(resp.Body)
				if err != nil {
					panic(err)
				}
				if string(all) != "OK" {
					panic("expected OK, got " + string(all))
				}
				resp.Body.Close()

			}
		}(i)
	}

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)

	<-stop

	err = exec.Command(cleanupPath).Run()
	if err != nil {
		panic(err)
	}
}
