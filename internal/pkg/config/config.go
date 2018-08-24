package config

import (
	"fmt"

	micro "github.com/micro/go-config"
	"github.com/micro/go-config/source/env"
	"github.com/micro/go-config/source/file"
)

// WebserverConfig configuration for the webserver
type WebserverConfig struct {
	Port    int    `json:"port"`
	Address string `json:"address"`
}

// SessionConfig configuration for the session
type SessionConfig struct {
	Domain   string `json:"domain"`
	Path     string `json:"path"`
	MaxAge   int    `json:"maxage"`
	HTTPOnly bool   `json:"httponly"`
	Secret   string `json:"secret"`
}

// DbConnection stores connection information for the database
type DbConnection struct {
	// @todo pull from config
	Username string `json:"username"`
	Password string `json:"password"`
	Host     string `json:"host"`
	Name     string `json:"name"`
	Timeout  int    `json:"timeout"`
}

// HostConfig data structure that represent a valid configuration file
type HostConfig struct {
	Webserver  WebserverConfig `json:"webserver"`
	Database   DbConnection    `json:"database"`
	Session    SessionConfig   `json:"session"`
	Oxipay     OxipayConfig    `json:"oxipay"`
	Background bool            `json:"background"`
	LogLevel   string          `json:"loglevel"`
}

// OxipayConfig data structure that represents a valid Oxipay configuration file entry
type OxipayConfig struct {
	GatewayURL string `json:"gatewayurl"`
	Version    string
}

// ReadApplicationConfig will load the application configuration from known places on the disk or environment
func ReadApplicationConfig(configFile string) (HostConfig, error) {

	conf := micro.NewConfig()
	// Load json file with encoder
	err := conf.Load(
		file.NewSource(file.WithPath(configFile)),
		// allow env overrides,
		// keys can't have _ as this is how it deals with nesting
		env.NewSource(),
	)
	var hostConfiguration HostConfig

	if err != nil {
		return hostConfiguration, err
	}

	errs := validate(conf)
	if len(errs) > 0 {
		return hostConfiguration, errs[0]
	}
	err = conf.Scan(&hostConfiguration)

	// hardcode this for now
	// should load from a non-config file
	hostConfiguration.Oxipay.Version = "1.1"

	// map[cache:map[address:10.0.0.2 port:6379] database:map[address:10.0.0.1 port:3306]]
	return hostConfiguration, err
}

// Validate ensure we have some basic validation of the configuration
func validate(myconfig micro.Config) []error {
	required := [3]string{"webserver", "database", "session"}
	var errs []error

	// We need to do more error checking here but let's at least make an
	// attempt
	for _, entry := range required {
		var tmpMap map[string]string
		configValue := myconfig.Get(entry).StringMap(tmpMap)
		if configValue == nil {
			newErr := fmt.Errorf("Config is missing a definition for %s", entry)
			errs = append(errs, newErr)
		}
	}

	// check the ensure the log level works
	return errs
}
