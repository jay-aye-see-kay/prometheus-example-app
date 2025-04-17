package main

import (
	"crypto/rand"
	"crypto/sha256"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"golang.org/x/net/http2"
	"golang.org/x/net/http2/h2c"
)

var (
	appVersion string
	version    = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "version",
		Help: "Version information about this binary",
		ConstLabels: map[string]string{
			"version": appVersion,
		},
	})

	httpRequestsTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "http_requests_total",
		Help: "Count of all HTTP requests",
	}, []string{"code", "method"})

	httpRequestDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name: "http_request_duration_seconds",
		Help: "Duration of all HTTP requests",
	}, []string{"code", "handler", "method"})
)

func main() {
	version.Set(1)
	bind := ""
	enableH2c := false
	flagset := flag.NewFlagSet(os.Args[0], flag.ExitOnError)
	flagset.StringVar(&bind, "bind", ":8080", "The socket to bind to.")
	flagset.BoolVar(&enableH2c, "h2c", false, "Enable h2c (http/2 over tcp) protocol.")
	flagset.Parse(os.Args[1:])

	r := prometheus.NewRegistry()
	r.MustRegister(httpRequestsTotal)
	r.MustRegister(httpRequestDuration)
	r.MustRegister(version)

	foundHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("Hello from example application."))
	})
	notfoundHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	})
	internalErrorHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})

	waitHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		waitSecStr := r.PathValue("waitSec")
		waitSec, _ := strconv.Atoi(waitSecStr)
		if waitSec < 1 {
			waitSec = 5 // errors, no value, and negative values all default to 5 seconds
		}
		time.Sleep(time.Duration(waitSec) * time.Second)
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("Waited for " + waitSecStr + " seconds."))
	})

	hashHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		iterationsStr := r.PathValue("iterations")
		iterations, _ := strconv.Atoi(iterationsStr)
		if iterations < 1 {
			iterations = 5 // errors, no value, and negative values all default to 5 iterations
		}
		mbStr := r.PathValue("mb")
		mb, _ := strconv.Atoi(mbStr)
		if mb < 1 {
			mb = 5 // errors, no value, and negative values all default to 5 mb
		}
		fmt.Printf("Hashing %d mb, %d times\n", mb, iterations)
		start := time.Now()
		for range iterations {
			hash := hashRandomData(mb * 1024 * 1024)
			fmt.Println("completed hash with result: " + hash)
		}
		elapsed := time.Since(start)
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(fmt.Sprintf("Hashing %d mb, %d times took %s", mb, iterations, elapsed)))
	})

	foundChain := promhttp.InstrumentHandlerDuration(
		httpRequestDuration.MustCurryWith(prometheus.Labels{"handler": "found"}),
		promhttp.InstrumentHandlerCounter(httpRequestsTotal, foundHandler),
	)

	mux := http.NewServeMux()
	mux.Handle("/", foundChain)
	mux.Handle("/err", promhttp.InstrumentHandlerCounter(httpRequestsTotal, notfoundHandler))
	mux.Handle("/internal-err", promhttp.InstrumentHandlerCounter(httpRequestsTotal, internalErrorHandler))
	mux.Handle("/wait/{waitSec}", promhttp.InstrumentHandlerCounter(httpRequestsTotal, waitHandler))
	mux.Handle("/wait/", promhttp.InstrumentHandlerCounter(httpRequestsTotal, waitHandler))
	mux.Handle("/hash/{mb}/{iterations}", promhttp.InstrumentHandlerCounter(httpRequestsTotal, hashHandler))
	mux.Handle("/hash/{mb}", promhttp.InstrumentHandlerCounter(httpRequestsTotal, hashHandler))
	mux.Handle("/hash/", promhttp.InstrumentHandlerCounter(httpRequestsTotal, hashHandler))
	mux.Handle("/metrics", promhttp.HandlerFor(r, promhttp.HandlerOpts{}))

	var srv *http.Server
	if enableH2c {
		srv = &http.Server{Addr: bind, Handler: h2c.NewHandler(mux, &http2.Server{})}
	} else {
		srv = &http.Server{Addr: bind, Handler: mux}
	}

	log.Fatal(srv.ListenAndServe())
}

func hashRandomData(bytesToProcess int) string {
	buffer := make([]byte, 1024) // 1KB buffer
	hasher := sha256.New()

	bytesProcessed := 0
	h := []byte{}
	for bytesProcessed < bytesToProcess {
		rand.Read(buffer) // Fill buffer with random data
		hasher.Write(buffer)
		h = hasher.Sum(nil)
		bytesProcessed += len(buffer)
	}

	return fmt.Sprintf("%x", h)
}
