package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
	"github.com/patrickmn/go-cache"
	"github.com/thecsw/pid"
	"github.com/thecsw/rei"
)

var (
	// targetUrl is the URL shortener's target URL.
	targetUrl *string
	// monomi is the database connection.
	monomi *dangan

	// keyToUrlExpire is the time after which a key to url mapping expires.
	keyToUrlExpire = 24 * time.Hour
	// KeytoUrlCleanup is the time after which the key to url cache is cleaned up.
	KeytoUrlCleanup = 1 * time.Hour
	// keyToUrl is the key to url cache (faster than a redis network overhead).
	keyToUrl = cache.New(keyToUrlExpire, KeytoUrlCleanup)
)

func main() {
	// Only one monokuma instance can be running at a time
	defer pid.Start("monokuma").Stop()

	// Parse the flags.
	targetUrl = flag.String("url", "https://photos.sandyuraz.com/", "the url with short urls")
	port := flag.Int("port", 11037, "port at which to open the server")
	auth := flag.String("auth", "", "auth token (empty for no auth)")

	// Redis-basic related things.
	redisPort = flag.Int("redis-port", 6379, "redis port")
	redisHost = flag.String("redis-host", "localhost", "redis host")
	redisDB = flag.Int("redis-db", 0, "redis database")

	// Redis SSL specific.
	redisTLS = flag.Bool("redis-tls", false, "use TLS")
	redisClientCert = flag.String("redis-cert", "client.crt", "client certificate")
	redisClientKey = flag.String("redis-key", "client.key", "client key")
	redisCustomCA = flag.String("redis-ca", "ca.der", "CA certificate (in DER)")

	// Key generation tunings.
	keysize = flag.Int("key-size", 3, "size of the short url keys")
	alphabet = flag.String("alphabet", "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ", "alphabet used for key gen")
	maxNumGenTries = flag.Int("gen-tries", 100, "unique key gen number of tries")

	// Parse the flags.
	flag.Parse()

	// Set up the database connection.
	monomi = NewDangan()
	// Close the database connection when the server is shut down.
	defer monomi.Close()

	// Set up the router.
	r := chi.NewRouter()
	// Show the real IP.
	r.Use(middleware.RealIP)
	// Set up the middleware.
	r.Use(middleware.Logger)
	// Disable caching.
	r.Use(middleware.NoCache)
	// Remove trailing slashes.
	r.Use(middleware.RedirectSlashes)
	// Set up CORS.
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins:   []string{"*"},
		AllowedMethods:   []string{http.MethodGet, http.MethodPost},
		AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type"},
		ExposedHeaders:   []string{"Link"},
		AllowCredentials: true,
		MaxAge:           300,
	}))
	// Set up the routes.

	// Set up the API admin routes.
	r.Group(func(r chi.Router) {
		r.Use(rei.BearerMiddleware(*auth))
		r.Post("/create", createLink)
		r.Get("/export", exportLinks)
	})

	// Get the homepage.
	r.Get("/", hello)
	// Get a link.
	r.Get("/{key}", getLink)

	// Set up the server's timeouts.
	srv := &http.Server{
		Addr:              "0.0.0.0:" + strconv.Itoa(*port),
		Handler:           r,
		ReadTimeout:       5 * time.Second,
		ReadHeaderTimeout: 1 * time.Second,
		WriteTimeout:      10 * time.Second,
	}

	// Spin the local server up.
	go func() {
		log.Fatal(srv.ListenAndServe())
	}()
	fmt.Printf("server spun up on port %d for base host %s\n", *port, *targetUrl)

	// Wait for a SIGINT.
	sigint := make(chan os.Signal, 1)
	signal.Notify(sigint, os.Interrupt)
	<-sigint
	fmt.Println("farewell")
}

// hello is the homepage.
func hello(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("hello, this is sandy's url shortener, powered by https://github.com/thecsw/monokuma"))
}

// createLink creates a new link.
func createLink(w http.ResponseWriter, r *http.Request) {
	// Create the link.
	key, code, err := operationCreateLink(r.Body, r.URL.Query().Get("key"))

	// If there were no errors, return the key with the url.
	if err == nil && code == Success {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(strings.TrimRight(*targetUrl, "/") + "/" + key))
		return
	}

	// If there was an error, return the error.
	w.WriteHeader(monokumaHttpCode(code))
	w.Write([]byte(err.Error()))
}

// getLink gets a link.
func getLink(w http.ResponseWriter, r *http.Request) {
	// Get the key from the URL.
	key := chi.URLParam(r, "key")
	finalUrl, code, err := operationKeyToLink(key)

	// If there was an error, return an error.
	if err != nil {
		w.WriteHeader(monokumaHttpCode(code))
		w.Write([]byte(err.Error()))
		return
	}

	// If there was no error, redirect to the link.
	http.Redirect(w, r, finalUrl, http.StatusFound)
}

// exportLinks exports all the links.
func exportLinks(w http.ResponseWriter, r *http.Request) {
	links, code, err := operationExportLinks()

	// Return an error if found.
	if err != nil {
		w.WriteHeader(monokumaHttpCode(code))
		w.Write([]byte(err.Error()))
		return
	}

	// Give the links.
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(strings.Join(links, "\n")))
}

// monokumaHttpCode converts a MonokumaStatusCode to an HTTP status code.
func monokumaHttpCode(code MonokumaStatusCode) int {
	switch code {
	case LinkFound:
		return http.StatusFound
	case LinkNotFound:
		return http.StatusNotFound
	case BadKey, BadLink:
		return http.StatusBadRequest
	case Success:
		return http.StatusOK
	}
	return http.StatusInternalServerError
}
