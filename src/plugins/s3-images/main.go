// This file is an example of an S3-pulling plugin.  This is meant to be a more
// real-world plugin that could actually be used in a production environment
// (compared to the more general but dangerous "external-images" plugin).  This
// requires you to put your access keys in $HOME/.aws/credentials (or
// docker/s3credentials if you're using the docker-compose setup).  See
// docker/s3credentials.example for an example credentials file.
//
// When a resource is requested, if its IIIF id begins with "s3:", we treat the
// rest of the id as an s3 id to be pulled from the configured zone and bucket.
// As zone and bucket are configured on the server end, attack vectors seen in
// the external images plugin are effectively nullified.
//
// We assume the asset is already a format RAIS can serve (preferably JP2), and
// we cache it locally with the same extension it has in S3.  The IDToPath
// return is the cached path so that RAIS can use the cached file immediately
// after download.  The JP2 cache is configurable via `S3Cache` in the RAIS
// toml file or by setting `RAIS_S3CACHE` in the environment, and defaults to
// `/var/cache/rais-s3`.  Expiration of cached files must be managed externally
// (to avoid over-complicating this plugin).  A simple approach could be a cron
// job that wipes out all cached data if it hasn't been accessed in the past 24
// hours:
//
//     find /var/cache/rais-s3 -type f -atime +1 -exec rm {} \;
//
// Depending how fast the cache grows, this may or may not be sufficient.  A
// smarter plugin would auto-remove files as needed to keep the cache to a set
// limit, but we don't currently need such careful control.

package main

import (
	"errors"
	"path/filepath"
	"rais/src/iiif"
	"rais/src/plugins"
	"sync"
	"time"

	"github.com/spf13/viper"
	"github.com/uoregon-libraries/gopkg/fileutil"
	"github.com/uoregon-libraries/gopkg/logger"
)

var downloading = make(map[string]bool)
var m sync.RWMutex

var l *logger.Logger

var s3cache, s3zone, s3bucket string

// Disabled lets the plugin manager know not to add this plugin's functions to
// the global list unless sanity checks in Initialize() pass
var Disabled = true

// Initialize sets up package variables for the s3 pulls and verifies sanity of
// some of the configuration
func Initialize() {
	viper.SetDefault("S3Cache", "/var/local/rais-s3")
	s3cache = viper.GetString("S3Cache")
	s3zone = viper.GetString("S3Zone")
	s3bucket = viper.GetString("S3Bucket")

	if s3zone == "" {
		l.Infof("S3 plugin will not be enabled: S3Zone must be set in rais.toml or RAIS_S3ZONE must be set in the environment")
		return
	}

	if s3bucket == "" {
		l.Infof("S3 plugin will not be enabled: S3Bucket must be set in rais.toml or RAIS_S3BUCKET must be set in the environment")
		return
	}

	l.Debugf("Setting S3 cache location to %q", s3cache)
	l.Debugf("Setting S3 zone to %q", s3zone)
	l.Debugf("Setting S3 bucket to %q", s3bucket)
	Disabled = false

	if fileutil.IsDir(s3cache) {
		return
	}
	if !fileutil.MustNotExist(s3cache) {
		l.Fatalf("S3 plugin failure: %q must not exist or else must be a directory", s3cache)
	}
}

// SetLogger is called by the RAIS server's plugin manager to let plugins use
// the central logger
func SetLogger(raisLogger *logger.Logger) {
	l = raisLogger
}

// IDToPath implements the auto-download logic when a IIIF ID
// starts with "s3:"
func IDToPath(id iiif.ID) (path string, err error) {
	var ids = string(id)
	if ids[:3] != "s3:" {
		return "", plugins.ErrSkipped
	}

	// Check cache - don't re-download
	var s3ID = ids[3:]
	path = filepath.Join(s3cache, s3ID)

	// See if this file is currently being downloaded; if so we need to wait
	var timeout = time.Now().Add(time.Second * 10)
	for isDownloading(s3ID) {
		time.Sleep(time.Millisecond * 250)
		if time.Now().After(timeout) {
			return "", errors.New("timed out waiting for s3 download")
		}
	}

	l.Debugf("s3-images plugin: Checking for cached file at %q", path)
	if fileutil.MustNotExist(path) {
		err = pullImage(s3ID, path)
	} else {
		l.Debugf("s3-images plugin: cached file found")
	}

	return path, err
}

func pullImage(s3ID, path string) error {
	setIsDownloading(s3ID)
	defer clearIsDownloading(s3ID)
	return s3download(s3ID, path)
}

func isDownloading(s3ID string) bool {
	m.RLock()
	defer m.RUnlock()
	return downloading[s3ID]
}

func setIsDownloading(s3ID string) {
	m.Lock()
	defer m.Unlock()
	downloading[s3ID] = true
}

func clearIsDownloading(s3ID string) {
	m.Lock()
	defer m.Unlock()
	delete(downloading, s3ID)
}