package config

import (
	"github.com/spf13/viper"
)

// WebserverConfig configuration for the webserver
type WebserverConfig struct {
	Port    string `json:"port"`
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
	Timeout  string `json:"timeout"`
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
func ReadApplicationConfig(configFile string) (*HostConfig, error) {
	conf := viper.New()
	conf.SetConfigName("vendproxy")
	//conf.Set("Verbose", true)

	conf.AddConfigPath("/etc/vend/")
	conf.AddConfigPath("../configs/")
	conf.AddConfigPath("./")

	conf.AutomaticEnv()

	err := conf.ReadInConfig()

	if err != nil {
		return nil, err
	}

	hostConfiguration := &HostConfig{}

	if err != nil {
		return hostConfiguration, err
	}

	// errs := validate(conf)
	// if len(errs) > 0 {
	// 	return hostConfiguration, errs[0]
	// }
	err = conf.Unmarshal(hostConfiguration)

	// hardcode this for now
	// should load from a non-config file
	hostConfiguration.Oxipay.Version = "1.1"

	return hostConfiguration, err
}

// Validate ensure we have some basic validation of the configuration
// func validate(myconfig viper.Viper) []error {
// 	required := [3]string{"webserver", "database", "session"}
// 	var errs []error

// 	// We need to do more error checking here but let's at least make an
// 	// attempt
// 	for _, entry := range required {
// 		var tmpMap map[string]string
// 		configValue := myconfig.Get(entry).StringMap(tmpMap)
// 		if configValue == nil {
// 			newErr := fmt.Errorf("Config is missing a definition for %s", entry)
// 			errs = append(errs, newErr)
// 		}
// 	}

// 	// check the ensure the log level works
// 	return errs
// }
