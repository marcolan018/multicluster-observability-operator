// Copyright (c) 2020 Red Hat, Inc.

package config

import (
	"errors"

	"gopkg.in/yaml.v2"
)

// Config is for s3 compatiable configuration
type Config struct {
	Bucket    string `yaml:"bucket"`
	Endpoint  string `yaml:"endpoint"`
	Insecure  bool   `yaml:"insecure"`
	AccessKey string `yaml:"access_key"`
	SecretKey string `yaml:"secret_key"`
}

func validate(conf Config) error {

	if conf.Bucket == "" {
		return errors.New("no s3 bucket in config file")
	}

	if conf.Endpoint == "" {
		return errors.New("no s3 endpoint in config file")
	}

	if conf.AccessKey == "" {
		return errors.New("no s3 access_key in config file")
	}

	if conf.SecretKey == "" {
		return errors.New("no s3 secret_key in config file")
	}

	return nil
}

// IsValidS3Conf is used to validate s3 configuration
func IsValidS3Conf(data []byte) (bool, error) {
	var objectConfg ObjectStorgeConf
	err := yaml.Unmarshal(data, &objectConfg)
	if err != nil {
		return false, err
	}

	if objectConfg.Type != "s3" {
		return false, errors.New("invalid type config, only s3 type is supported")
	}

	err = validate(objectConfg.Config)
	if err != nil {
		return false, err
	}

	return true, nil
}
