package main

import (
	"fmt"
	"io"
	"regexp"
	"strings"

	"github.com/patrickmn/go-cache"
	"github.com/thecsw/rei"
)

// MonokumaStatusCode is an enum for the status of a Monokuma request.
type MonokumaStatusCode uint8

const (
	// LinkFound indicates that the link was found.
	LinkFound MonokumaStatusCode = iota
	// LinkNotFound indicates that the link was not found.
	LinkNotFound
	// BadKey indicates that the key was bad.
	BadKey
	// BadLink indicates that the link was bad.
	BadLink
	// LinkRetrievalError indicates that the link retrieval failed.
	LinkRetrievalError
	// Uncategorized indicates that the error was uncategorized.
	Uncategorized
	// Success indicates that the operation was successful.
	Success
)

// keyRegexp is the regular expression for a Monokuma key.
// It must be between 3 and 10 characters long and only contain alphanumeric characters.
var keyRegexp = regexp.MustCompile(`^[-0-9a-zA-Z]{3,10}$`)

// operationCreateLink takes a link and returns a key.
func operationCreateLink(linkReader io.Reader, customKey string) (string, MonokumaStatusCode, error) {
	// Read the link.
	linkBytes, err := io.ReadAll(linkReader)
	if err != nil {
		return "", Uncategorized, fmt.Errorf("reading the link: %v", err)
	}

	// Trim the link.
	link := strings.TrimSpace(string(linkBytes))

	// If the link is empty, return an error.
	if len(link) < 1 {
		return "", BadLink, fmt.Errorf("link is empty")
	}

	// If the link is too long, return an error.
	if len(strings.Split(link, "\n")) > 1 {
		return "", BadLink, fmt.Errorf("link contains newlines")
	}

	// If the link does not match the regular expression, return an error.
	if !URLRegexp.MatchString(link) {
		return "", BadLink, fmt.Errorf("link is invalid")
	}

	// Check the key against the regular expression.
	if !keyRegexp.MatchString(customKey) {
		return "", BadKey, fmt.Errorf("key %s is invalid", customKey)
	}

	// Try to write the link.
	key, err := monomi.writeLink(rei.Btao([]byte(link)), customKey)
	if err != nil {
		return "", Uncategorized, fmt.Errorf("shortening the link: %v", err)
	}

	// Return the key.
	return key, Success, nil
}

// operationKeyToLink takes a key and returns the final link.
func operationKeyToLink(key string) (string, MonokumaStatusCode, error) {
	// Check the key against the regular expression.
	if !keyRegexp.MatchString(key) {
		return "", BadKey, fmt.Errorf("key %s is invalid", key)
	}

	// Check the cache for the key.
	if finalUrl, found := keyToUrl.Get(key); found {
		return finalUrl.(string), LinkFound, nil
	}

	// If the key is empty, return an error.
	linkb64, found, err := monomi.getLink(key)
	if err != nil {
		return "", LinkRetrievalError, fmt.Errorf("critical failure during retrieval: %v", err)
	}

	// If the key is not found, return an error.
	if !found {
		return "", LinkNotFound, fmt.Errorf("short url for %s not found", key)
	}

	// Decode the link.
	finalUrl := string(rei.AtobMust(linkb64))

	// Add the mapping to the cache.
	keyToUrl.Add(key, finalUrl, cache.DefaultExpiration)

	// Return the final link after it's been cached.
	return finalUrl, LinkFound, nil
}

// operationExportLinks exports all links.
func operationExportLinks() ([]string, MonokumaStatusCode, error) {
	// Get the links.
	links, err := monomi.exportLinks()

	// Return a generic error if possible.
	if err != nil {
		return nil, Uncategorized, fmt.Errorf("critical failure during export: %v", err)
	}

	// Got the links.
	return links, Success, nil
}
