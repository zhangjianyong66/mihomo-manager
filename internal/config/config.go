package config

import (
	"os"
	"path/filepath"
	"strconv"
)

type Paths struct {
	MihomoBin       string
	ConfigDir       string
	ConfigFile      string
	BackupFile      string
	SubscriptionURL string
	LogFile         string
	NodeSpeedFile   string
	FastestNodeFile string
	APIAddr         string
}

func Load() Paths {
	home, _ := os.UserHomeDir()
	configDir := envOrDefault("CONFIG_DIR", filepath.Join(home, ".config", "mihomo"))
	apiPort := envOrDefault("MIHOMO_API_PORT", "9090")
	if _, err := strconv.Atoi(apiPort); err != nil {
		apiPort = "9090"
	}
	return Paths{
		MihomoBin:       envOrDefault("MIHOMO_BIN", filepath.Join(home, ".local", "bin", "mihomo")),
		ConfigDir:       configDir,
		ConfigFile:      filepath.Join(configDir, "config.yaml"),
		BackupFile:      filepath.Join(configDir, "config.yaml.bak"),
		SubscriptionURL: filepath.Join(configDir, "subscription.url"),
		LogFile:         filepath.Join(configDir, "mihomo.log"),
		NodeSpeedFile:   filepath.Join(configDir, "node_speed.txt"),
		FastestNodeFile: filepath.Join(configDir, "fastest_node.txt"),
		APIAddr:         "http://127.0.0.1:" + apiPort,
	}
}

func envOrDefault(k, d string) string {
	v := os.Getenv(k)
	if v == "" {
		return d
	}
	return v
}
