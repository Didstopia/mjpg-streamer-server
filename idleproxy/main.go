package main

import (
	"context"
	"didstopia/jpg-streamer-server/idleproxy/conwatch"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"os/signal"
	"syscall"
	"time"
)

// TODO: Research the following:
// https://ieftimov.com/post/make-resilient-golang-net-http-servers-using-timeouts-deadlines-context-cancellation/
// https://stackoverflow.com/questions/51317122/how-to-get-number-of-idle-and-active-connections-in-go
// https://www.alexedwards.net/blog/an-introduction-to-handlers-and-servemuxes-in-go
// https://medium.com/honestbee-tw-engineer/gracefully-shutdown-in-go-http-server-5f5e6b83da5a

const PORT = "80"
const IDLE_TIMER = 1
const DEBUG = true

var (
	// ctx       context.Context
	// ctxCancel context.CancelFunc
	proxy             *http.Server
	router            *http.ServeMux
	connectionWatcher conwatch.ConnectionWatcher
	connectionCount   int
	connectionState   http.ConnState
	idleTimer         = time.NewTimer(time.Second * IDLE_TIMER)
)

func main() {
	ctx, ctxCancel := context.WithCancel(context.Background())
	defer ctxCancel()
	defer shutdown(ctx)

	go setupProxy(ctx)
	go setupIdleTimer(ctx)

	exit := make(chan os.Signal, 1)
	signal.Notify(exit, syscall.SIGINT, syscall.SIGTERM)
	<-exit
}

func setupProxy(ctx context.Context) {
	log.Print("Setting up proxy...")

	router = http.NewServeMux()
	router.HandleFunc("/", proxyHandler)

	proxy = &http.Server{
		Addr:      ":" + PORT,
		ConnState: connectionWatcher.OnStateChange,
		Handler:   router,

		// FIXME: It looks like actively watching the stream is somehow considered idle?!

		// TODO: Allow timeouts to be configurable via env vars
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       60 * time.Second, // TODO: Increase this to 15, 30 or 60 minutes, also test that Octolapse etc. does NOT go idle!
		ReadHeaderTimeout: 10 * time.Second,
	}

	go func() {
		if err := proxy.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("listen: %s\n", err)
		}
	}()

	log.Print("Proxy started on port ", PORT)
}

func proxyHandler(res http.ResponseWriter, req *http.Request) {
	url := getProxyURL()
	if DEBUG {
		logRequest(url, req)
	}
	serveReverseProxy(url, res, req)
}

func getProxyURL() string {
	if DEBUG {
		return "http://192.168.0.4:38080"
	}
	return "http://localhost:" + os.Getenv("MJPG_STREAMER_PORT")
}

func logRequest(proxyURL string, req *http.Request) {
	log.Printf("%s%s\n", proxyURL, req.URL.String())
}

func serveReverseProxy(target string, res http.ResponseWriter, req *http.Request) {
	url, _ := url.Parse(target)
	proxy := httputil.NewSingleHostReverseProxy(url)
	proxy.ServeHTTP(res, req)
	// TODO: Do we need to defer close the proxy?
	// TODO: How can we detect when there are no active connections?
}

func setupIdleTimer(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-idleTimer.C:
			newConnectionCount := connectionWatcher.Count()
			if newConnectionCount != connectionCount {
				if DEBUG {
					log.Printf("Connection count changed from %d to %d\n", connectionCount, newConnectionCount)
				}
				connectionCount = newConnectionCount
			}

			// FIXME: This is dumb, we should just use a DEBUG env var and log to stdout in conwatch directly!
			newConnectionState := connectionWatcher.State()
			if newConnectionState != connectionState {
				if DEBUG {
					log.Printf("Connection state changed from %s to %s\n", connectionState, newConnectionState)
				}
				connectionState = newConnectionState
			}

			// TODO: If connection count > 0, make sure the mjpg-streamer server is running
			// TODO: If connection count == 0, stop the mjpg-streamer server
			// TODO: When starting or stopping the mjpg-streamer server, be sure
			//       to keep in mind that the server may take a while to spin up/down!

			idleTimer.Reset(time.Second * IDLE_TIMER)
		}
	}
}

func shutdown(ctx context.Context) {
	// TODO: Stop the timer
	// TODO: Gracefully shutdown the http server and active connections
	log.Println("Shutting down...")
	idleTimer.Stop()
	if err := proxy.Shutdown(ctx); err != nil {
		log.Fatalf("Proxy Shutdown Failed:%+v", err)
	}
	log.Println("Shutdown complete")
	os.Exit(0)
}
