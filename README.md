# monokuma

This is Sandy's custom URL shortener that works through Redis.

## Motivation

I wanted to learn how to use custom TLS Redis and I wanted to make a URL shortener.
So I did both.

Done. hah.

Well, on my website, I also have many gallery pages, which are powered by OneDrive that
have really long URLs. I wanted to shorten them so that I can share them more easily.

## Setting up Redis

First, you need to have Redis installed and running through one of the modified redis
config files in the `redis` folder. There are two config files, one for a secure TLS
connection and one for a non-secure connection. You can use either one, but I recommend
using the secure one.

Please update the username and password in the config file to your own username and
password. You can generate a password by running `openssl rand -base64 32` in your
terminal.

When it comes to SSL certificates, you can either use a self-signed certificate or
you can use a certificate from a certificate authority. You can use my commands
for generating yourself a custom CA, server certificate, and client certificate
for two-way authentication. See [thecsw/certificates](https://github.com/thecsw/certificates).

## Setting up the server

First, build the golang binary by running `go build` in the root directory of this
repository. Then, you can run the binary by running `./monokuma` in the root directory
of this repository.

Here is the list of command-line flags that you can use:
```
Usage of ./monokuma:
  -alphabet string
    	alphabet used for key gen (default "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ")
  -auth string
    	auth token (empty for no auth)
  -gen-tries int
    	unique key gen number of tries (default 100)
  -key-size int
    	size of the short url keys (default 3)
  -port int
    	port at which to open the server (default 11037)
  -redis-ca string
    	CA certificate (in DER) (default "ca.der")
  -redis-cert string
    	client certificate (default "client.crt")
  -redis-db int
    	redis database
  -redis-host string
    	redis host (default "localhost")
  -redis-key string
    	client key (default "client.key")
  -redis-port int
    	redis port (default 6379)
  -redis-tls
    	use TLS
  -url string
    	the url with short urls (default "https://photos.sandyuraz.com/")
```

### Redis credentials

You will have to set up environment variables for the Redis credentials:

- `MONOKUMA_REDIS_USER` - the username for Redis (default: `user`)
- `MONOKUMA_REDIS_PASS` - the password for Redis (default: `pass`)

An example is included in the script `monokuma.example.sh`. You can copy this script
and modify it to your needs.

### Secure Redis

These are all "sensible" defaults, so you don't need to change them unless you want to. 
If you use TLS, you will need to provide the CA certificate, client certificate, and
client key. If you don't use TLS, you don't need to provide any of those.

Don't forget to enable secure monokuma through `-redi-tls` flag.

See other redis flags to change the host, port, and database.

### Server flags

You can change the port that the server runs on through the `-port` flag. You can also
change the URL that the short URLs redirect to through the `-url` flag.

You can also change the alphabet used for generating the short URLs through the
`-alphabet` flag. The default is `abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ`.

You can also change the size of the short URLs through the `-key-size` flag. The default
is 3. This means that the short URLs will be 3 characters long. If you want to change
the number of tries for generating a unique key, you can use the `-gen-tries` flag.
The default is 100.

You can also set an auth token through the `-auth` flag. This will require you to
provide the auth token in the URL `Authorization` header (`Bearer AUTH_VALUE`). 
The default is no auth.

## Using the server

You can use the server by sending a `POST` request to the `/create` endpoint with
the url to shorten in the body of the request. The server will return a raw string
of the shortened URL.

## Custom short URLs

You can also create custom short URLs by sending a `POST` request to the `/create`
endpoint with the url to shorten in the body of the request and the custom short
name with a query parameter `key`. The server will return a raw string of the
shortened URL.

So, like this: `/create?key=custom_short_name` with the url to shorten in the body.

## Caveats

Each unique URL will have a unique key. This means that if you shorten the same URL
twice, you will get the same shortened URL. This is because the server will check
if the URL already exists in the database and if it does, it will return the same
shortened URL.

## LICENSE

[Apache License](LICENSE). Go wild.
