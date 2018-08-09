package config

import (
	"testing"
)

func TestLoadConfig(t *testing.T) {
	myconfig, err := ReadApplicationConfig("../../../configs/vendproxy.json")

	if err != nil {
		t.Error(err)
	}
	_ = myconfig
}
