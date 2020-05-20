package main

import "log"
import "os"
import "os/signal"
import "net/http"
import "syscall"
import "strconv"
import "fmt"
import "time"
import "sync/atomic"
import "context"

import "switcheroo"

func main() {
	logger := log.New(os.Stdout, fmt.Sprintf("[server/pid=%d] ", os.Getpid()), log.Ltime)

	/* we'd like a http server on port 9999 ... */
	swo, err := switcheroo.NewSwitcherooWithSudoIptables("WhateverNamespace", 9999, logger)
	if err != nil {
		panic(err)
	}

	/* By default, switcheroo only routes network traffic, and
	 * loopback/localhost traffic is ignored; setting these flags enable
	 * localhost traffic too */
	swo.SetFlags(switcheroo.ENABLE_LOCALHOST | switcheroo.ENABLE_NETWORK)

	/* Begin(); returns a net.Listener to start your server on; and a
	 * finalizeFn() you must call after your server has started accepting
	 * traffic */
	listener, finalizeFn, err := swo.Begin()
	if err != nil {
		panic(err)
	}

	/* SIGTERM should be used for graceful shutdown; so install a signal
	 * handler */
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)

	/* simple request handler that sleeps for however many milliseconds
	 * requested (via query param `s`), and responds with "OK". The
	 * sleeping helps demonstrate that old requests aren't dropped. */
	var nRequests int32
	http.HandleFunc("/", func(w http.ResponseWriter, rq *http.Request) {
		atomic.AddInt32(&nRequests, 1)

		sstr := rq.URL.Query().Get("s")
		if len(sstr) > 0 {
			s, err := strconv.Atoi(sstr)
			if err != nil {
				panic(err)
			}
			time.Sleep(time.Duration(s) * time.Millisecond)
		}

		fmt.Fprintf(w, "OK")
	})

	/* start server in goroutine */
	httpserv := &http.Server{Addr: listener.Addr().String()}
	go func() {
		logger.Printf("now accepting HTTP requests on %s", listener.Addr().String())
		err := httpserv.Serve(listener)
		if err != nil && err != http.ErrServerClosed {
			log.Fatal(err)
		}
	}()

	/* ensure we're serving requests before finalizing */
	for i := 0; i < 50; i++ {
		if i > 0 {
			time.Sleep(5 * time.Millisecond)
		}
		u := "http://127.0.0.1" + listener.Addr().String()
		resp, err := http.Get(u)
		if err != nil {
			continue
		}
		defer resp.Body.Close()
		if resp.StatusCode != 200 {
			continue
		}
		break
	}

	/* Finalize the switcheroo; after successful return, traffic will start
	 * flowing to us, old processes get SIGTERM'd and stop receiving
	 * connections */
	err = finalizeFn()
	if err != nil {
		panic(err)
	}

	/* block until SIGTERM */
	<-stop

	logger.Printf("caught a kill; stopping http server gracefully...")
	t0 := time.Now()

	/* graceful shutdown */
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	err = httpserv.Shutdown(ctx)
	if err != nil {
		panic(err)
	}

	logger.Printf("server exits; %s after kill; served %d requests", time.Now().Sub(t0).String(), nRequests)
}
