package main

import (
	"flag"
	"fmt"
	"io"
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
	"github.com/thecsw/pid"
	"github.com/thecsw/rei"
)

var (
	// targetUrl is the URL shortener's target URL.
	targetUrl *string
	// monomi is the database connection.
	monomi *dangan
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
	// Set up the middleware.
	r.Use(middleware.Logger)
	// Disable caching.
	r.Use(middleware.NoCache)
	// Recover from panics.
	r.Use(middleware.Recoverer)
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
	// Get the real IP.
	r.Use(middleware.RealIP)

	// Set up the bearer middleware.
	r.Use(rei.BearerMiddleware(*auth))

	// Set up the profiler.
	r.Mount("/debug", middleware.Profiler())

	// Set up the routes.
	// -----------------
	// Create a new link.
	r.Post("/create", createLink)
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
	w.Write([]byte("hello, this is sandy's url shortener"))
}

// createLink creates a new link.
func createLink(w http.ResponseWriter, r *http.Request) {
	// Read the body.
	body, err := io.ReadAll(r.Body)

	// If there was an error reading the body, return an error.
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprintf(w, "couldn't read POST body: %s", err.Error())
		return
	}

	// If the body is empty, return an error.
	if len(body) < 1 {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte("empty body"))
		return
	}

	// Trim the body.
	link := strings.TrimSpace(string(body))

	// If the body is too long, return an error.
	if len(strings.Split(link, "\n")) > 1 {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte("too many lines, need only 1"))
		return
	}

	// If the body is not a link, return an error.
	if !URLRegexp.MatchString(link) {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte("not a link"))
		return
	}

	// If the body is a link, create a new link.
	key, err := monomi.writeLink(rei.Btao([]byte(link)), r.URL.Query().Get("key"))
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprintf(w, "couldn't create the link: %s", err.Error())
		return
	}

	// Return the new link.
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(strings.TrimRight(*targetUrl, "/") + "/" + key))
}

// getLink gets a link.
func getLink(w http.ResponseWriter, r *http.Request) {
	// Get the key from the URL.
	key := chi.URLParam(r, "key")

	// If the key is empty, return an error.
	linkb64, found, err := monomi.getLink(key)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("critical failure during retrieval: " + err.Error()))
		return
	}

	// If the key is not found, return an error.
	if !found {
		w.WriteHeader(http.StatusNotFound)
		fmt.Fprintf(w, "short url for %s not found", key)
		return
	}

	// Redirect to the link.
	http.Redirect(w, r, string(rei.Atob(linkb64)), http.StatusMovedPermanently)
}
