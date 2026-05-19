package config

import (
	"encoding/json"
	"fmt"
	"os"
)

// Config holds the application configuration
type Config struct {
	Role       string `json:"role"`
	IP         string `json:"ip"`
	Port       string `json:"port"`
	MasterIP   string `json:"master_ip"`
	MasterPort string `json:"master_port"`
	DBUser     string `json:"db_user"`
	DBPass     string `json:"db_pass"`
	DBHost     string `json:"db_host"`
	DBPort     string `json:"db_port"`
	DBName     string `json:"db_name"`
}

var AppConfig *Config

// LoadConfig reads the JSON config file and unmarshals it
func LoadConfig(filename string) error {
	file, err := os.ReadFile(filename)
	if err != nil {
		return fmt.Errorf("could not read config file: %v", err)
	}

	AppConfig = &Config{}
	err = json.Unmarshal(file, AppConfig)
	if err != nil {
		return fmt.Errorf("could not unmarshal config: %v", err)
	}

	return nil
}
