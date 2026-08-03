package main

import (
	"fmt"
	"os"

	"github.com/BurntSushi/toml"
)

type Config struct {
	BPQ    BPQConfig    `toml:"bpq"`
	Server ServerConfig `toml:"server"`
	Layout LayoutConfig `toml:"layout"`
	Ports  []PortConfig `toml:"port"`
}

type BPQConfig struct {
	WebURL   string `toml:"web_url"`
	FBBPort  int    `toml:"fbb_port"`
	Username string `toml:"username"`
	Password string `toml:"password"`
}

type ServerConfig struct {
	Listen         string `toml:"listen"`
	ActionPassword string `toml:"action_password"`
}

type LayoutConfig struct {
	Columns int `toml:"columns"`
}

type PortConfig struct {
	Num       int    `toml:"num"`
	Label     string `toml:"label"`
	Service   string `toml:"service"`
	Row       int    `toml:"row"`
	Col       int    `toml:"col"`
	DeafCheck bool   `toml:"deaf_check"` // restart if L2 frames heard stagnant for 1h
}

func LoadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}
	var cfg Config
	if _, err := toml.Decode(string(data), &cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return &cfg, nil
}

func (c *Config) Validate() error {
	if c.Server.ActionPassword == "" {
		return fmt.Errorf("server.action_password must not be empty")
	}
	if len(c.Ports) == 0 {
		return fmt.Errorf("at least one [[port]] must be configured")
	}
	if c.Layout.Columns == 0 {
		c.Layout.Columns = 3
	}
	if c.BPQ.FBBPort == 0 {
		c.BPQ.FBBPort = 8011
	}
	if c.Server.Listen == "" {
		c.Server.Listen = "127.0.0.1:9090"
	}
	return nil
}
