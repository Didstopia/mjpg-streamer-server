package main

import (
	"context"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/didstopia/mjpg-streamer-server/idleproxy/conwatch"
	"github.com/didstopia/mjpg-streamer-server/idleproxy/daemon"
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
	Host     string `long:"host" description:"Hostname to listen on" default:"http://localhost"`
	Port     string `long:"port" description:"Port to listen on" default:"80"`
	ProxyURL string `long:"proxy-url" description:"URL of the proxy to use" default:"http://localhost:8080"`
	// IdleTimer   time.Duration `long:"idle-timer" description:"Idle timer interval" default:"1s"`
	IdleTimeout       time.Duration `long:"idle-timeout" description:"Idle connection timeout" default:"1m"`
	ProcessCWD        string        `long:"process-cwd" description:"Working directory for the spawned proxied process" default:"."`
	ProcessCMD        string        `long:"process-cmd" description:"Command to spawn the proxied process with" default:""`
	ProcessStartDelay time.Duration `long:"process-start-delay" description:"Delay before marking the proxied process as running" default:"0s"`
	Debug             bool          `long:"debug" description:"Enable debug mode"`
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

	// FIXME: Parse CLI args as options, as we aren't parsing those at all right now?!
	// TODO: Fallback to environment variable overrides, when CLI args are not set!

	options.Host = getEnv("HOST", "http://localhost")
	options.Port = getEnv("PORT", "80")
	options.ProxyURL = getEnv("PROXY_URL", "http://localhost:8080")

	// idleTimerDuration, err := time.ParseDuration(getEnv("IDLE_TIMER", "1s"))
	// if err != nil {
	// 	log.Fatalf("Invalid idle timer: %s", err)
	// }
	// options.IdleTimer = idleTimerDuration

	idleTimeoutDuration, err := time.ParseDuration(getEnv("IDLE_TIMEOUT", "1m"))
	if err != nil {
		log.Fatalf("Invalid idle timeout duration: %s", err)
	}
	options.IdleTimeout = idleTimeoutDuration

	options.ProcessCWD = getEnv("PROCESS_CWD", ".")
	options.ProcessCMD = getEnv("PROCESS_CMD", "")
	options.ProcessStartDelay, err = time.ParseDuration(getEnv("PROCESS_START_DELAY", "0s"))
	if err != nil {
		log.Fatalf("Invalid process start delay: %s", err)
	}

	// FIXME: Use better loggign that supports setting log levels etc.
	//        and ensure that toggling DEBUG only logs the appropriate messages!
	options.Debug = strings.ToLower(getEnv("DEBUG", "false")) == "true"
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
		ReadHeaderTimeout: 10 * time.Second,
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
	url := options.ProxyURL

	// TODO: I wonder if we should always try to start the process here,
	//       but also how would that work in tandem with the idle timer?
	// FIXME: The idle timer should be handling the process startup, right?

	// Handle the daemon process's health checking etc.
	if process != nil {
		// Sleep for a bit to give the process time to start up
		if process.Status != daemon.Running {
			// FIXME: This might cause issues with Octolapse, since it uses snapshots?
			//        Maybe if we just increase the idle timeout to several minutes?
			if options.Debug {
				log.Println("Daemon is not running, sleeping for a bit before checking again...")
			}
			time.Sleep(time.Millisecond * 250)
		}
	}

	// Query the URL to confirm it's up
	for {
		// Set default HTTP client timeout to 1 second
		http.DefaultClient.Timeout = time.Second * 1
		_, err := http.Head(url)
		if err != nil {
			// TODO: Filter our errors that contain "connection refused"
			if !strings.Contains(err.Error(), "connection refused") {
				log.Println("Error response from proxy url:", err)
			}

			// FIXME: This should fail eventually, but for now, just wait a bit and try again
			// Sleep for a bit and try again
			if options.Debug {
				log.Println("Daemon is not responding, sleeping for a bit before checking again...")
			}
			time.Sleep(time.Millisecond * 250)
		} else {
			// log.Println("Proxy url is up!")
			return url
		}
	}
}

func logRequest(proxyURL string, req *http.Request) {
	if options.Debug {
		log.Printf("[PROXY]: %s%s\n", proxyURL, req.URL.String())
	}
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
	if options.ProcessCMD == "" {
		log.Print("Skipping daemon setup, no process command specified...")
		return
	}

	log.Print("Setting up daemon...")

	process = &daemon.Daemon{
		Context:    ctx,
		Cwd:        options.ProcessCWD,
		Cmd:        options.ProcessCMD,
		StartDelay: options.ProcessStartDelay,
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

			// Handle the daemon process lifecycle events
			if process != nil {
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
			}

			// idleTimer.Reset(options.IdleTimer)
		}

		// Sleep for a bit to reduce CPU usage
		time.Sleep(100 * time.Millisecond)
	}
}

func getEnv(key, fallback string) string {
	if value, ok := os.LookupEnv("IDLEPROXY_" + key); ok && len(value) > 0 {
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

	// Shutdown the daemon process if it's running/enabled
	if process != nil {
		log.Println("Shutting down daemon...")
		if err := process.Stop(); err != nil {
			log.Println("Error stopping daemon:", err)
			exitCode = 1
		}
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
