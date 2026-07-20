package config

import (
	"errors"
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
	WhitelistFile   string
	LogFile         string
	NodeSpeedFile   string
	FastestNodeFile string
	APIAddr         string
}

type ManagerEnvironment struct {
	HomeDir    string
	DataHome   string
	StateHome  string
	RuntimeDir string
	ConfigHome string
}

type ManagerPaths struct {
	DataDir        string
	Database       string
	GenerationsDir string
	StateDir       string
	CoreStateDir   string
	CoreLog        string
	RuntimeState   string
	RuntimeDir     string
	Socket         string
	Lock           string
	UserUnitDir    string
}

func LoadManagerPaths() (ManagerPaths, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return ManagerPaths{}, err
	}
	return ResolveManagerPaths(ManagerEnvironment{
		HomeDir:    home,
		DataHome:   os.Getenv("XDG_DATA_HOME"),
		StateHome:  os.Getenv("XDG_STATE_HOME"),
		RuntimeDir: os.Getenv("XDG_RUNTIME_DIR"),
		ConfigHome: os.Getenv("XDG_CONFIG_HOME"),
	})
}

func ResolveManagerPaths(env ManagerEnvironment) (ManagerPaths, error) {
	if env.HomeDir == "" || !filepath.IsAbs(env.HomeDir) {
		return ManagerPaths{}, errors.New("manager home directory must be absolute")
	}
	dataHome := absoluteOrDefault(env.DataHome, filepath.Join(env.HomeDir, ".local", "share"))
	stateHome := absoluteOrDefault(env.StateHome, filepath.Join(env.HomeDir, ".local", "state"))
	configHome := absoluteOrDefault(env.ConfigHome, filepath.Join(env.HomeDir, ".config"))
	if dataHome == "" || stateHome == "" || configHome == "" {
		return ManagerPaths{}, errors.New("manager XDG directories must be absolute")
	}

	dataDir := filepath.Join(dataHome, "mihomo-manager")
	stateDir := filepath.Join(stateHome, "mihomo-manager")
	runtimeDir := filepath.Join(stateDir, "run")
	if env.RuntimeDir != "" {
		if !filepath.IsAbs(env.RuntimeDir) {
			return ManagerPaths{}, errors.New("manager runtime directory must be absolute")
		}
		runtimeDir = filepath.Join(filepath.Clean(env.RuntimeDir), "mihomo-manager")
	}
	return ManagerPaths{
		DataDir:        dataDir,
		Database:       filepath.Join(dataDir, "state.db"),
		GenerationsDir: filepath.Join(dataDir, "generations"),
		StateDir:       stateDir,
		CoreStateDir:   filepath.Join(stateDir, "core"),
		CoreLog:        filepath.Join(stateDir, "core", "mihomo.log"),
		RuntimeState:   filepath.Join(stateDir, "core", "runtime.json"),
		RuntimeDir:     runtimeDir,
		Socket:         filepath.Join(runtimeDir, "mm.sock"),
		Lock:           filepath.Join(runtimeDir, "daemon.lock"),
		UserUnitDir:    filepath.Join(configHome, "systemd", "user"),
	}, nil
}

func absoluteOrDefault(value, fallback string) string {
	if value == "" {
		return filepath.Clean(fallback)
	}
	if !filepath.IsAbs(value) {
		return ""
	}
	return filepath.Clean(value)
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
		WhitelistFile:   filepath.Join(configDir, "whitelist.yaml"),
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
