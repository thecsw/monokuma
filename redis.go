package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/thecsw/rei"
)

const (
	// keyToLinkTable is the name of the table that maps keys to links.
	keyToLinkTable = "keytob64"

	// linkExistsTable is the name of the table that maps links's hashes to keys.
	linkExistsTable = "linkhashes"

	// monokumaUsernameEnv is the name of the environment variable that contains
	// the username for the redis server.
	monokumaUsernameEnv = "MONOKUMA_REDIS_USER"

	// monokumaPasswordEnv is the name of the environment variable that contains
	// the password for the redis server.
	monokumaPasswordEnv = "MONOKUMA_REDIS_PASS"

	// customKeyMaxLength is the max number of characters in a custom key. Arbitrarily chosen.
	customKeyMaxLength = 37

	connPusher = "pusher"
	connGetter = "getter"
)

var (
	// redisHost is the host of the redis server.
	redisHost *string
	// redisPort is the port of the redis server.
	redisPort *int
	// redisDB is the database of the redis server.
	redisDB *int
	// redisTLS is whether to use TLS for the redis connection.
	redisTLS *bool

	// redisClientCert is the name of the redis client certificate file.
	redisClientCert *string
	// redisClientKey is the name of the redis client key file.
	redisClientKey *string
	// redisCustomCA is the name of the custom CA file.
	redisCustomCA *string

	// redisUsername is the username for the redis server.
	redisUsername = "default" // not real credentials
	// redisPassword is the password for the redis server.
	redisPassword = "nopass" // not real credentials

	// maxNumGenTries is the maximum number of times to try to generate a unique key.
	maxNumGenTries *int
)

var (
	// errKeyExists is returned when a key already exists
	errKeyExists = errors.New("key already exists")
)

// dangan is a redis client, with two connections: one for pushing and one for
// getting. This is done because redis does not allow a single connection to
// both push and get.
type dangan struct {
	// rdb is the main redis client used for admin purposes (e.g. flushing the
	// database)
	rdb *redis.Client
	// pusher is the redis client used for pushing new links and keys.
	pusher *redis.Conn
	// getter is the redis client used for getting links from keys.
	getter *redis.Conn
}

// NewDangan creates a new dangan client.
func NewDangan() *dangan {
	// check if the redis username and password are set
	getEnvOrExit(monokumaUsernameEnv, &redisUsername)
	getEnvOrExit(monokumaPasswordEnv, &redisPassword)

	// create a new redis client
	rdb := redis.NewClient(&redis.Options{
		Addr:      *redisHost + ":" + rei.Itoa(*redisPort),
		Username:  redisUsername,
		Password:  redisPassword,
		DB:        *redisDB,
		TLSConfig: getRedisTLSConfig(),
	})

	// check if the redis server is reachable
	d := &dangan{
		rdb:    rdb,
		pusher: getConnection(rdb, connPusher),
		getter: getConnection(rdb, connGetter),
	}

	// start the keep alive loop
	go d.keepAlive()

	return d
}

const (
	// maxNumKeepAliveFailures is the maximum number of times to fail to ping
	// redis before exiting.
	maxNumKeepAliveFailures = 100
)

func (d *dangan) keepAlive() {
	numFailures := 0

	// ping the redis server with the given name.
	// if there's an error, log it and increment the number of failures.
	// if there's no error, reset the number of failures.
	pinger := func(name string, cmd redis.Cmdable) {
		_, err := cmd.Ping(context.Background()).Result()
		if err != nil {
			log.Printf("pinging redis on %s: %v\n", name, err)
			numFailures++
		}
		numFailures = 0
	}

	for {
		// if we've failed too many times, exit.
		if numFailures > maxNumKeepAliveFailures*3 {
			log.Fatalf("ping failed %d times", numFailures)
		}

		// ping the redis server
		pinger("client", d.rdb)
		pinger(connGetter, d.getter)
		pinger(connPusher, d.pusher)
		time.Sleep(10 * time.Second)
	}
}

// getRedisTLSConfig creates a new TLS config for the redis connection.
// If redisTLS is false, it will return nil.
func getRedisTLSConfig() *tls.Config {
	// if redisTLS is false, return nil.
	if !*redisTLS {
		return nil
	}

	// check if the redis client certificate and key exist.
	ensureFileExists(*redisClientCert, "redis client certificate")
	ensureFileExists(*redisClientKey, "redis client key")

	// load the client certificate and key
	tlsPair, err := tls.LoadX509KeyPair(*redisClientCert, *redisClientKey)
	// if there's an error, panic.
	if err != nil {
		log.Fatalf("loading redis client certificate and key: %v", err)
	}

	// create a new cert pool and add the custom CA to it.
	caPool := x509.NewCertPool()
	if rei.FileMustExist(*redisCustomCA) {
		// load the custom CA if exists.
		derCustomCA, err := os.ReadFile(*redisCustomCA)
		if err != nil {
			log.Fatalf("reading custom CA: %v", err)
		}
		caCert, err := x509.ParseCertificate(derCustomCA)
		if err != nil {
			log.Fatalf("parsing custom CA: %v", err)
		}
		caPool.AddCert(caCert)
	}

	// return the TLS config.
	return &tls.Config{
		MinVersion:   tls.VersionTLS12,
		RootCAs:      caPool,
		Certificates: []tls.Certificate{tlsPair},
	}
}

// getConnection creates a new connection to the redis server with the given name.
func getConnection(rdb *redis.Client, name string) *redis.Conn {
	conn := rdb.Conn()
	if err := conn.ClientSetName(context.Background(), name).Err(); err != nil {
		log.Fatalf("setting client name to %s: %v", name, err)
	}
	// check if the connection is working
	_, err := conn.Ping(context.Background()).Result()
	if err != nil {
		log.Fatalf("pinging redis on %s: %v", name, err)
	}
	return conn
}

// writeLink writes a new link to the database. If customKey is provided, it
// will be used as the key. Otherwise, a new key will be generated.
func (d *dangan) writeLink(linkb64, customKey string) (key string, err error) {
	hash, key, exists, err := d.isLinkAlreadyShortened(linkb64)
	if err != nil {
		return "", fmt.Errorf("link creation ('%s') hash check: %w", linkb64, err)
	}
	if exists {
		return
	}
	// get a unique key for the link (if customKey is provided, it will be used)
	key, err = d.getUniqueKey(customKey)
	if err != nil {
		if errors.Is(err, errKeyExists) {
			return
		}
		err = fmt.Errorf("getting unique key for link ('%s'): %w", linkb64, err)
		return
	}
	// save the link and key
	err = d.pusher.HSet(context.TODO(), keyToLinkTable, key, linkb64).Err()
	if err != nil {
		err = fmt.Errorf("saving key and link (key='%s', link='%s'): %w", key, linkb64, err)
		return
	}
	// save the hash of the link to check if it's already shortened later on (see isLinkAlreadyShortened)
	err = d.pusher.HSet(context.TODO(), linkExistsTable, hash, key).Err()
	if err != nil {
		err = fmt.Errorf("saving hash of link (link='%s', hash='%s'): %w", linkb64, hash, err)
	}
	return
}

// getUniqueKey returns a unique key. If customKey is provided, it will be used
// as the key. Otherwise, a new key will be generated. If the key already
// exists, an error is returned.
func (d *dangan) getUniqueKey(customKey string) (string, error) {
	// First, let's check if the custom key is provided and it's new
	if len(customKey) > 0 {
		// see if it's too long
		if len(customKey) > customKeyMaxLength {
			return "", fmt.Errorf("custom key is too long, max size is %d", customKeyMaxLength)
		}
		// move on
		exists, err := d.keyExists(keyToLinkTable, customKey)
		// some generic error
		if err != nil {
			return "", fmt.Errorf("existence of custom key ('%s'): %w", customKey, err)
		}
		// if it exists, send an error
		if exists {
			return "", fmt.Errorf("custom key already exists", errKeyExists)
		}
		return customKey, nil
	}
	// Now, let's try generate the key until we find a unique one or we reach the
	// maximum number of tries (maxNumGenTries).
	for i := 0; i < *maxNumGenTries; i++ {
		key := gen()
		exists, err := d.keyExists(keyToLinkTable, key)
		if err != nil {
			return "",
				fmt.Errorf("existence of generated key #%d ('%s'): %w", i+1, key, err)
		}
		// try again
		if exists {
			continue
		}
		return key, nil
	}
	// We failed to generate a unique key after maxNumGenTries--sad
	return "", fmt.Errorf("couldn't generate a unique key after %d tries", maxNumGenTries)
}

// exportLinks returns all the links in the database in the format:
// key,link
func (d *dangan) exportLinks() ([]string, error) {
	links, err := d.getter.HGetAll(context.Background(), keyToLinkTable).Result()
	if err != nil {
		return nil, fmt.Errorf("getting all links: %w", err)
	}
	out := make([]string, 0, len(links))
	for key, link := range links {
		out = append(out, fmt.Sprintf("%s,%s", key, link))
	}
	return out, nil
}

// keyExists returns true if the given key exists in the given hash table. It
// returns false if the key does not exist. If there is an error, it returns
// false and the error.
func (d *dangan) keyExists(table, key string) (bool, error) {
	_, err := d.getter.HGet(context.TODO(), table, key).Result()
	// exists
	if err == nil {
		return true, nil
	}
	// does not exist
	if err == redis.Nil {
		return false, nil
	}
	// error
	return false, fmt.Errorf("key existence check (table='%s', key='%s'): %w", table, key, err)
}

// getLink returns the link for the given key. If the key does not exist, it
// returns an empty string, false, and nil error.
func (d *dangan) getLink(key string) (link string, found bool, err error) {
	link, err = d.getter.HGet(context.TODO(), keyToLinkTable, key).Result()
	// key does not exist
	if err == redis.Nil {
		err = nil
		return
	} else if err != nil {
		// some other error
		err = fmt.Errorf("retrieving link for key ('%s'): %w", key, err)
		return
	}
	// key exists
	found = true
	return
}

// isLinkAlreadyShortened checks if the link is already shortened. If it is,
// it returns the hash, key, exists, and nil error. If it isn't, it returns
// empty hash and key, false exists, and nil error.
func (d *dangan) isLinkAlreadyShortened(linkb64 string) (
	hash string, key string, exists bool, err error,
) {
	// Check if the link's hash is already stored
	hash = rei.Sha256([]byte(linkb64))
	key, err = d.getter.HGet(context.TODO(), linkExistsTable, hash).Result()
	if err != nil {
		if err == redis.Nil {
			// does not exist
			err = nil
		} else {
			err = fmt.Errorf("hash lookup ('%s'): %w", hash, err)
		}
		return
	}
	exists = true
	return
}

// Close closes the dangan client.
func (d *dangan) Close() {
	// close the redis connections
	d.pusher.Close()
	d.getter.Close()
	// close the redis client
	d.rdb.Close()
}
