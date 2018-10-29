package main

import (
	"fmt"
	"math"
	"net/http"
	"net/url"
	"os"
	"rais/src/iiif"
	"rais/src/magick"
	"rais/src/openjpeg"
	"rais/src/plugins"
	"rais/src/version"
	"strings"
	"sync"
	"time"

	"github.com/BurntSushi/toml"
	"github.com/hashicorp/golang-lru"
	"github.com/spf13/pflag"
	"github.com/spf13/viper"
	"github.com/uoregon-libraries/gopkg/interrupts"
	"github.com/uoregon-libraries/gopkg/logger"
)

var tilePath string
var infoCache *lru.Cache
var tileCache *lru.TwoQueueCache

// Logger is the server's central logger.Logger instance
var Logger *logger.Logger

const defaultAddress = ":12415"
const defaultInfoCacheLen = 10000

// cacheHits and cacheMisses allow some rudimentary tracking of cache value
var cacheHits, cacheMisses int64

var defaultLogLevel = logger.Debug.String()
var defaultPlugins = "s3-images.so,json-tracer.so"

func main() {
	// Defaults
	viper.SetDefault("Address", defaultAddress)
	viper.SetDefault("InfoCacheLen", defaultInfoCacheLen)
	viper.SetDefault("LogLevel", defaultLogLevel)
	viper.SetDefault("Plugins", defaultPlugins)

	// Allow all configuration to be in environment variables
	viper.SetEnvPrefix("RAIS")
	viper.AutomaticEnv()

	// Config file options
	viper.SetConfigName("rais")
	viper.AddConfigPath("/etc")
	viper.AddConfigPath(".")
	if err := viper.ReadInConfig(); err != nil {
		fmt.Printf("ERROR: Unable to read RAIS config: %s", err)
	}

	// CLI flags
	pflag.String("iiif-url", "", `Base URL for serving IIIF requests, e.g., "http://example.com/images/iiif"`)
	viper.BindPFlag("IIIFURL", pflag.CommandLine.Lookup("iiif-url"))
	pflag.String("address", defaultAddress, "http service address")
	viper.BindPFlag("Address", pflag.CommandLine.Lookup("address"))
	pflag.String("tile-path", "", "Base path for images")
	viper.BindPFlag("TilePath", pflag.CommandLine.Lookup("tile-path"))
	pflag.Int("iiif-info-cache-size", defaultInfoCacheLen, "Maximum cached image info entries (IIIF only)")
	viper.BindPFlag("InfoCacheLen", pflag.CommandLine.Lookup("iiif-info-cache-size"))
	pflag.String("capabilities-file", "", "TOML file describing capabilities, rather than everything RAIS supports")
	viper.BindPFlag("CapabilitiesFile", pflag.CommandLine.Lookup("capabilities-file"))
	pflag.String("log-level", defaultLogLevel, "Log level: the server will only log notifications at "+
		"this level and above (must be DEBUG, INFO, WARN, ERROR, or CRIT)")
	viper.BindPFlag("LogLevel", pflag.CommandLine.Lookup("log-level"))
	pflag.Int64("image-max-area", math.MaxInt64, "Maximum area (w x h) of images to be served")
	viper.BindPFlag("ImageMaxArea", pflag.CommandLine.Lookup("image-max-area"))
	pflag.Int("image-max-width", math.MaxInt32, "Maximum width of images to be served")
	viper.BindPFlag("ImageMaxWidth", pflag.CommandLine.Lookup("image-max-width"))
	pflag.Int("image-max-height", math.MaxInt32, "Maximum height of images to be served")
	viper.BindPFlag("ImageMaxHeight", pflag.CommandLine.Lookup("image-max-height"))
	pflag.String("plugins", defaultPlugins, "comma-separated plugin pattern list, e.g., "+
		`"s3-images.so,datadog.so,json-tracer.so,/opt/rais/plugins/*.so"`)
	viper.BindPFlag("Plugins", pflag.CommandLine.Lookup("plugins"))

	pflag.Parse()

	// Make sure required values exist
	if !viper.IsSet("TilePath") {
		fmt.Println("ERROR: --tile-path is required")
		pflag.Usage()
		os.Exit(1)
	}

	// Make sure we have a valid log level
	var level = logger.LogLevelFromString(viper.GetString("LogLevel"))
	if level == logger.Invalid {
		fmt.Println("ERROR: --log-level must be DEBUG, INFO, WARN, ERROR, or CRIT")
		pflag.Usage()
		os.Exit(1)
	}
	Logger = logger.New(level)
	openjpeg.Logger = Logger
	magick.Logger = Logger

	var plugPatterns = strings.Split(viper.GetString("Plugins"), ",")
	LoadPlugins(Logger, plugPatterns)

	// Pull all values we need for all cases
	tilePath = viper.GetString("TilePath")
	address := viper.GetString("Address")

	ih := NewImageHandler(tilePath)
	ih.Maximums.Area = viper.GetInt64("ImageMaxArea")
	ih.Maximums.Width = viper.GetInt("ImageMaxWidth")
	ih.Maximums.Height = viper.GetInt("ImageMaxHeight")

	// Handle IIIF data only if we have a IIIF URL
	iiifURL := viper.GetString("IIIFURL")
	if iiifURL == "" {
		fmt.Println("ERROR: --iiif-url must be set to the server's public URL")
		pflag.Usage()
		os.Exit(1)
	}

	Logger.Debugf("Attempting to start up IIIF at %s", viper.GetString("IIIFURL"))
	iiifBase, err := url.Parse(iiifURL)
	if err == nil && iiifBase.Scheme == "" {
		err = fmt.Errorf("empty scheme")
	}
	if err == nil && iiifBase.Host == "" {
		err = fmt.Errorf("empty host")
	}
	if err == nil && iiifBase.Path == "" {
		err = fmt.Errorf("empty path")
	}
	if err != nil {
		Logger.Fatalf("Invalid IIIF URL (%s) specified: %s", iiifURL, err)
	}

	icl := viper.GetInt("InfoCacheLen")
	if icl > 0 {
		infoCache, err = lru.New(icl)
		if err != nil {
			Logger.Fatalf("Unable to start info cache: %s", err)
		}
	}

	tcl := viper.GetInt("TileCacheLen")
	if tcl > 0 {
		Logger.Debugf("Creating a tile cache to hold up to %d tiles", tcl)
		tileCache, err = lru.New2Q(tcl)
		if err != nil {
			Logger.Fatalf("Unable to start info cache: %s", err)
		}
	}

	Logger.Infof("IIIF enabled at %s", iiifBase.String())
	ih.EnableIIIF(iiifBase)

	capfile := viper.GetString("CapabilitiesFile")
	if capfile != "" {
		ih.FeatureSet = &iiif.FeatureSet{}
		_, err := toml.DecodeFile(capfile, &ih.FeatureSet)
		if err != nil {
			Logger.Fatalf("Invalid file or formatting in capabilities file '%s'", capfile)
		}
		Logger.Debugf("Setting IIIF capabilities from file '%s'", capfile)
	}

	handle(ih.IIIFBase.Path+"/", http.HandlerFunc(ih.IIIFRoute))
	handle("/images/dzi/", http.HandlerFunc(ih.DZIRoute))
	handle("/version", http.HandlerFunc(VersionHandler))

	if tileCache != nil {
		go func() {
			for {
				time.Sleep(time.Minute * 10)
				Logger.Infof("Cache hits: %d; cache misses: %d", cacheHits, cacheMisses)
			}
		}()
	}

	Logger.Infof("RAIS v%s starting...", version.Version)
	var srv = &http.Server{
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 30 * time.Second,
		Addr:         address,
	}

	var wait sync.WaitGroup

	interrupts.TrapIntTerm(func() {
		wait.Add(1)
		Logger.Infof("Stopping RAIS...")
		srv.Shutdown(nil)

		if len(teardownPlugins) > 0 {
			Logger.Infof("Tearing down plugins")
			for _, plug := range teardownPlugins {
				plug()
			}
			Logger.Infof("Plugin teardown complete")
		}

		Logger.Infof("Stopped")
		wait.Done()
	})

	if err := srv.ListenAndServe(); err != nil {
		// Don't report a fatal error when we close the server
		if err != http.ErrServerClosed {
			Logger.Fatalf("Error starting listener: %s", err)
		}
	}
	wait.Wait()
}

// handle sends the pattern and raw handler to plugins, and sets up routing on
// whatever is returned (if anything).  All plugins which wrap handlers are
// allowed to run, but the behavior could definitely get weird depending on
// what a given plugin does.  Ye be warned.
func handle(pattern string, handler http.Handler) {
	for _, plug := range wrapHandlerPlugins {
		var h2, err = plug(pattern, handler)
		if err == nil {
			handler = h2
		} else if err != plugins.ErrSkipped {
			logger.Fatalf("Error trying to wrap handler %q: %s", pattern, err)
		}
	}
	http.Handle(pattern, handler)
}

// VersionHandler spits out the raw version string to the browser
func VersionHandler(w http.ResponseWriter, req *http.Request) {
	w.Write([]byte(version.Version))
}
