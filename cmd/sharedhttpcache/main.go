package main

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/davecgh/go-spew/spew"
	"github.com/dylandreimerink/sharedhttpcache"

	"github.com/spf13/pflag"
	"github.com/spf13/viper"
)

//Config is the structure for the configuration file
type Config struct {
	//CacheConfig is the configurate that determins how the caching part of the caching server should behave
	CacheConfig *CacheConfig `mapstructure:"cache_config"`

	//ListenConfig is the configuration that determins how the http server part of the caching server should behave
	ListenConfig *ListenConfig `mapstructure:"listen_config"`

	//ForwardConfig is the configuration that determins how the http client part of the caching server should behave
	ForwardConfig *ForwardConfig `mapstructure:"forward_config"`
}

type ForwardConfig struct {
	//ForwardProxyMode if enabled the request will be forwared to the domain name / ip in the Host header
	ForwardProxyMode bool `mapstructure:"forward_proxy_mode"`

	//DefaultHost is the default hostname / ip the request will be forwared to if there is no host specific forward config
	DefaultForwardConfig *ForwardHostConfig `mapstructure:"default_forward_config"`

	//PerHostForwardConfig is used to match a requested hostname to the correct forward config
	PerHostForwardConfig map[string]*ForwardConfig
}

type ForwardHostConfig struct {
	//Host is the hostname of the origin server the request will be forwared to
	Host string `mapstructure:"host"`

	//EnableHTTP2 if true we will attempt to make a HTTP2 connection to the origin server
	EnableHTTP2 bool `mapstructure:"http2"`
}

type ListenConfig struct {
	//ListenAddress is the address on which the caching server will listen for http connections
	ListenAddress string `mapstructure:"address"`

	//EnableTLS if true the caching server will listen for tls/https connections
	EnableTLS bool `mapstructure:"tls"`

	//RedirectToTLS if true the http endpoint will always redirect to the https endpoint
	RedirectToTLS bool

	//ListenAddress is the address on which the caching server will listen for http connections
	TLSListenAddress string `mapstructure:"tls_address"`

	//EnableHTTP2 if true the caching server will accept HTTP2 connections
	EnableHTTP2 bool `mapstructure:"http2"`

	//AcceptAnyHost if true allows requests for any hostname
	//Usefull when using as forward proxy
	AcceptAnyHost bool `mapstructure:"accept_any_host"`

	//AcceptedHosts is a list of hostnames / ip addresses for which we accept requests
	//requests for hosts other than the once specified will retult in a 403 status code will be returned unless AcceptAnyHost is enabled
	AcceptedHosts []string
}

type CacheConfig struct {
	//CacheableMethods is a list of request methods for which responses may be cached.
	// It is not advisable to cache unsafe methods like POST. Tho it is possible to do so
	// Note that unsafe methods will not be cached even if they are in this list as per section 4 of RFC7234
	CacheableMethods []string `mapstructure:"cacheable_methods"`

	//SafeMethods is a list of "safe" request methods as defined in section 4.2.1 of RFC7231
	// Any method not in this list is considered unsafe and will not be cached
	// If a request is made with a unsafe method it may cause invalidation as per section 4.4 of RFC7234
	SafeMethods []string `mapstructure:"safe_methods"`

	//StatusCodeDefaultExpirationTimes is a map of times index by the http response code
	//
	// These times will be used as default expiration time unless the response contains a header which specifies a different
	//
	// Not all responses should be cached for the same duration.
	// A 307 Temporary Redirect for example should be cached for less time than a 301 Moved Permanently
	//
	// Codes not appearing in this list will be considered NOT understood and thus make a response uncacheable according to section 3 of RFC7234.
	StatusCodeDefaultExpirationTimes map[int]string `mapstructure:"default_expiration_per_status_code"`

	//CacheableFileExtensions is a list of cacheable file extensions
	// File extensions are used instead of MIME types because the same file extension can have separate MIME types
	// It is advised to only use static file types like stylesheets or images and not dynamic content like html
	CacheableFileExtensions []string `mapstructure:"cacheable_file_extensions"`

	//CacheIncompleteResponses enables or disables the optional feature mentioned in section 3.1 of RFC7234
	// Caching of incomplete requests will cache responses with status code 206 (Partial Content)
	//
	// Note that this carries a performance impact because ranges have to be accounted for when storing and querying the cached content
	CacheIncompleteResponses bool `mapstructure:"cache_incomplete_responses"`

	//CombinePartialResponses enables or disables the optional feature mentioned in section 3.3 of RFC7234
	// When this feature is enabled and incomplete responses are enabled
	// the caching server attempts to combine multiple incomplete responses into a complete response.
	//
	// Note that this carries a performance impact because at every time a new incomplete range is received reconstruction of the full resource will be attempted
	CombinePartialResponses bool `mapstructure:"combine_partial_responses"`

	//If ServeStaleOnError is true the cache will attempt to serve a stale response in case revalidation fails because the origin server returned a 5xx code or is unreachable
	//This setting respects the Cache-Control header of the client and server.
	ServeStaleOnError bool `mapstructure:"serve_stale_on_error"`

	//If HTTPWarnings is true warnings as described in section 5.5 of RFC7234 will be added to HTTP responses
	// This is a option because the feature will be removed from future HTTP specs https://github.com/httpwg/http-core/issues/139
	HTTPWarnings bool `mapstructure:"http_warnings"`
}

func (conf *CacheConfig) toRealCacheConfig() (*sharedhttpcache.CacheConfig, error) {
	for index, method := range conf.CacheableMethods {
		conf.CacheableMethods[index] = strings.ToUpper(method)
	}

	for index, method := range conf.SafeMethods {
		conf.SafeMethods[index] = strings.ToUpper(method)
	}

	statusCodeDefaultExpirationTimes := map[int]time.Duration{}
	for statusCode, durationString := range conf.StatusCodeDefaultExpirationTimes {
		duration, err := time.ParseDuration(durationString)
		if err != nil {
			return nil, fmt.Errorf("Unable to parse duration in 'default_expiration_per_status_code'[%d]: %w", statusCode, err)
		}

		statusCodeDefaultExpirationTimes[statusCode] = duration
	}

	cacheConfig := &sharedhttpcache.CacheConfig{
		CacheableMethods:                 conf.CacheableMethods,
		SafeMethods:                      conf.SafeMethods,
		CacheIncompleteResponses:         conf.CacheIncompleteResponses,
		CombinePartialResponses:          conf.CombinePartialResponses,
		ServeStaleOnError:                conf.ServeStaleOnError,
		HTTPWarnings:                     conf.HTTPWarnings,
		StatusCodeDefaultExpirationTimes: statusCodeDefaultExpirationTimes,
		CacheableFileExtensions:          conf.CacheableFileExtensions,
	}

	return cacheConfig, nil
}

func init() {
	viper.SetDefault("cache_config.cacheable_methods", []string{http.MethodGet})
	viper.SetDefault("cache_config.safe_methods", []string{http.MethodGet, http.MethodHead, http.MethodOptions, http.MethodTrace})
	viper.SetDefault("cache_config.cache_incomplete_responses", true)
	viper.SetDefault("cache_config.combine_partial_responses", true)
	viper.SetDefault("cache_config.serve_stale_on_error", true)
	viper.SetDefault("cache_config.http_warnings", true)
	viper.SetDefault("cache_config.cacheable_file_extensions", []string{
		"bmp", "ejs", "jpeg", "pdf", "ps", "ttf",
		"class", "eot", "jpg", "pict", "svg", "webp",
		"css", "eps", "js", "pls", "svgz", "woff",
		"csv", "gif", "mid", "png", "swf", "woff2",
		"doc", "ico", "midi", "ppt", "tif", "xls",
		"docx", "jar", "otf", "pptx", "tiff", "xlsx",
	})
	viper.SetDefault("cache_config.default_expiration_per_status_code", map[int]string{
		200: "2h",
		206: "2h",
		301: "2h",
		302: "20m",
		303: "20m",
		403: "1m",
		404: "3m",
		410: "3m",
	})
}

var config Config

func main() {

	err := initConfig()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error while reading config: %s\n", err.Error())
		os.Exit(1)
	}

	err = viper.Unmarshal(&config)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error while unmarshalling config: %s\n", err.Error())
		os.Exit(1)
	}

	spew.Dump(config)
}

func initConfig() error {
	flagSet := pflag.NewFlagSet("test", pflag.ContinueOnError)

	flagSet.String("config", "config.yaml", "The path to the sharedhttpcache config file")

	//Make it so that when the -help, --help or -h flag is given the usage is printed and the program exits
	flagSet.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage of %s:\n", os.Args[0])
		flagSet.PrintDefaults()
		os.Exit(0)
	}

	err := flagSet.Parse(os.Args)
	if err != nil {
		return err
	}

	configPath, err := flagSet.GetString("config")
	if err != nil {
		return err
	}

	viper.SetConfigType("yaml")

	configBytes, err := ioutil.ReadFile(configPath)
	if err != nil {
		return err
	}

	err = viper.ReadConfig(bytes.NewReader(configBytes))
	if err != nil {
		return err
	}

	return nil
}
