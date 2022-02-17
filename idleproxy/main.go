package main

import (
	"context"
	"didstopia/jpg-streamer-server/idleproxy/conwatch"
	"didstopia/jpg-streamer-server/idleproxy/daemon"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"
)

// TODO: Research the following:
// https://ieftimov.com/post/make-resilient-golang-net-http-servers-using-timeouts-deadlines-context-cancellation/
// https://stackoverflow.com/questions/51317122/how-to-get-number-of-idle-and-active-connections-in-go
// https://www.alexedwards.net/blog/an-introduction-to-handlers-and-servemuxes-in-go
// https://medium.com/honestbee-tw-engineer/gracefully-shutdown-in-go-http-server-5f5e6b83da5a

// const PORT = "80"
// const IDLE_TIMER = getEnv("IDLE_TIMER", "1")
// const DEBUG = true

// Options for the idle proxy
type Options struct {
	Port string `long:"port" description:"Port to listen on" default:"80"`
	// IdleTimer   time.Duration `long:"idle-timer" description:"Idle timer interval" default:"1s"`
	IdleTimeout time.Duration `long:"idle-timeout" description:"Idle connection timeout" default:"1m"`
	Debug       bool          `long:"debug" description:"Enable debug mode"`
}

var (
	options Options
	// ctx       context.Context
	// ctxCancel context.CancelFunc
	proxy             *http.Server
	router            *http.ServeMux
	connectionWatcher conwatch.ConnectionWatcher
	connectionCount   int
	// connectionState   http.ConnState
	// idleTimer *time.Timer
	process *daemon.Daemon
)

func loadOptions() {
	options = Options{}

	options.Port = getEnv("PORT", "80")

	// idleTimerDuration, err := time.ParseDuration(getEnv("IDLE_TIMER", "1s"))
	// if err != nil {
	// 	log.Fatalf("Invalid idle timer: %s", err)
	// }
	// options.IdleTimer = idleTimerDuration

	idleTimeoutDuration, err := time.ParseDuration(getEnv("IDLE_TIMEOUT", "1m"))
	if err != nil {
		log.Fatalf("Invalid idle timeout: %s", err)
	}
	options.IdleTimeout = idleTimeoutDuration
	// FIXME: Remove this after moving to env vars etc.
	options.IdleTimeout = time.Second * 15

	options.Debug = strings.ToLower(getEnv("DEBUG", "false")) == "true"
	// FIXME: Remove this after moving to env vars etc.
	options.Debug = true
	if options.Debug {
		log.Printf("Options: %+v", options)
	}
}

func main() {
	ctx, ctxCancel := context.WithCancel(context.Background())
	// defer ctxCancel()
	defer shutdown(ctx, ctxCancel)

	loadOptions()
	setupProxy(ctx)
	setupDaemon(ctx)

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
		Addr:      ":" + options.Port,
		ConnState: connectionWatcher.OnStateChange,
		Handler:   router,

		// FIXME: It looks like actively watching the stream is somehow considered idle?!

		// TODO: Allow timeouts to be configurable via env vars
		// ReadTimeout:       10 * time.Second,
		// WriteTimeout:      10 * time.Second,
		IdleTimeout: options.IdleTimeout,
		// IdleTimeout: 30 * time.Second,
		// ReadHeaderTimeout: 10 * time.Second,
	}
	// log.Println("Idle timeout:", proxy.IdleTimeout)

	go func() {
		if err := proxy.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("listen: %s\n", err)
		}
	}()

	log.Print("Proxy started on port ", options.Port)
}

func proxyHandler(res http.ResponseWriter, req *http.Request) {
	url := getProxyURL()
	if options.Debug {
		logRequest(url, req)
	}
	serveReverseProxy(url, res, req)
}

func getProxyURL() string {
	// FIXME: Use a more generic env var name, such as "server port", also "proxy port" instead of just "port"?
	url := "http://localhost:" + getEnv("MJPG_STREAMER_PORT", "8080")

	// Query the URL to confirm it's up
	for {
		// response, responseErr := http.Head(url)
		_, responseErr := http.Head(url)
		if responseErr != nil {
			log.Println("Error response from proxy url:", responseErr)
		} else {
			return url
			// _, responseBodyErr := ioutil.ReadAll(response.Body)
			// if responseBodyErr != nil {
			// 	log.Println("Error reading response body:", responseBodyErr)
			// } else {
			// 	return url
			// }
		}
	}
}

func logRequest(proxyURL string, req *http.Request) {
	log.Printf("%s%s\n", proxyURL, req.URL.String())
}

func serveReverseProxy(target string, res http.ResponseWriter, req *http.Request) {
	// FIXME: Always wait for the proxy target URL to be available,
	//        before responding to the incoming request, as this will
	//        give the daemon time to start up.

	url, _ := url.Parse(target)
	proxy := httputil.NewSingleHostReverseProxy(url)
	proxy.ServeHTTP(res, req)
	// TODO: Do we need to defer close the proxy?
	// TODO: How can we detect when there are no active connections?
}

func setupDaemon(ctx context.Context) {
	log.Print("Setting up daemon...")

	process = &daemon.Daemon{
		Context: ctx,
		Cwd:     "/mjpg/mjpg-streamer-master/mjpg-streamer-experimental",
		Cmd:     "/entry",
	}

	if err := process.Start(); err != nil {
		log.Fatalf("Error starting daemon: %s", err)
	}
}

func setupIdleTimer(ctx context.Context) {
	// idleTimer = time.NewTimer(options.IdleTimer)

	for {
		select {
		case <-ctx.Done():
			return
		// case <-idleTimer.C:
		default:
			newConnectionCount := connectionWatcher.Count()
			if newConnectionCount != connectionCount {
				if options.Debug {
					log.Printf("Connection count changed from %d to %d\n", connectionCount, newConnectionCount)
				}
				connectionCount = newConnectionCount
			}

			// Ensure the daemon is stopped if there are no active connections
			if newConnectionCount == 0 {
				if process.Status == daemon.Running {
					if options.Debug {
						log.Print("No active connections and daemon is running, stopping...")
					}
					if err := process.Stop(); err != nil {
						log.Fatalf("Error stopping daemon: %s", err)
					}
				}
			}

			// Ensure the daemon is running if there are active connections
			if newConnectionCount > 0 {
				if process.Status == daemon.Stopped {
					if options.Debug {
						log.Print("Active connections detected and daemon is not running, starting...")
					}
					if err := process.Start(); err != nil {
						log.Fatalf("Error starting daemon: %s", err)
					}
				}
			}

			// idleTimer.Reset(options.IdleTimer)
		}

		// Sleep for a bit to reduce CPU usage
		time.Sleep(100 * time.Millisecond)
	}
}

func getEnv(key, fallback string) string {
	if value, ok := os.LookupEnv(key); ok && len(value) > 0 {
		return value
	}
	return fallback
}

func shutdown(ctx context.Context, ctxCancel context.CancelFunc) {
	log.Println("Shutting down...")
	exitCode := 0

	// Cancel the context, forcing eg. the HTTP proxy to shutdown
	// all active connections, to avoid waiting indefinitely
	ctxCancel()

	// log.Println("Shutting down idle timer...")
	// idleTimer.Stop()

	log.Println("Shutting down daemon...")
	if err := process.Stop(); err != nil {
		log.Println("Error stopping daemon:", err)
		exitCode = 1
	}

	log.Println("Shutting down proxy...")
	if err := proxy.Shutdown(ctx); err != nil {
		// Ignore context cancellation errors
		if err.Error() != "context canceled" {
			log.Println("Error stopping proxy:", err)
			exitCode = 1
		}
	}

	log.Println("Terminating with exit code", exitCode)
	os.Exit(exitCode)
}
