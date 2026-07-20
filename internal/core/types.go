package core

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/zhangjianyong66/mihomo-manager/internal/domain"
)

type Proxy struct {
	ID       domain.NodeID
	Name     string
	Protocol string
	Spec     json.RawMessage
}

type ProfileSnapshot struct {
	ProfileID          domain.ProfileID
	Revision           int64
	Mode               domain.ProfileMode
	ExternalConfigPath string
	ControllerEndpoint string
	MixedPort          int
	SocksPort          int
	Proxies            []Proxy
}

func (s ProfileSnapshot) Validate() error {
	if err := s.ProfileID.Validate(); err != nil {
		return err
	}
	if s.Revision < 1 {
		return errors.New("profile revision must be at least 1")
	}
	if err := s.Mode.Validate(); err != nil {
		return err
	}
	if s.Mode == domain.ProfileModeManaged {
		if strings.TrimSpace(s.ExternalConfigPath) != "" {
			return errors.New("managed profile must not reference an external config")
		}
		if len(s.Proxies) == 0 {
			return errors.New("managed profile must contain at least one proxy")
		}
	} else if strings.TrimSpace(s.ExternalConfigPath) == "" {
		return errors.New("external profile config path must not be empty")
	}
	for _, proxy := range s.Proxies {
		if err := proxy.ID.Validate(); err != nil {
			return err
		}
		if strings.TrimSpace(proxy.Name) == "" || strings.TrimSpace(proxy.Protocol) == "" {
			return errors.New("proxy name and protocol must not be empty")
		}
		if !json.Valid(proxy.Spec) {
			return fmt.Errorf("proxy %s spec must be valid JSON", proxy.ID)
		}
	}
	return nil
}

type RenderedConfig struct {
	Content            []byte
	SHA256             string
	ControllerEndpoint string
}

func NewRenderedConfig(content []byte, endpoint string) RenderedConfig {
	sum := sha256.Sum256(content)
	return RenderedConfig{
		Content:            append([]byte(nil), content...),
		SHA256:             hex.EncodeToString(sum[:]),
		ControllerEndpoint: endpoint,
	}
}

type RuntimeSpec struct {
	ProfileID          domain.ProfileID   `json:"profileId"`
	Revision           int64              `json:"revision"`
	Mode               domain.ProfileMode `json:"mode"`
	GenerationID       string             `json:"generationId"`
	ConfigDir          string             `json:"configDir"`
	ConfigPath         string             `json:"configPath"`
	LogPath            string             `json:"logPath"`
	ControllerEndpoint string             `json:"controllerEndpoint"`
	SourceSHA256       string             `json:"sourceSha256"`
}

type Process interface {
	PID() int
	Done() <-chan error
	Stop(context.Context) error
}

type RuntimeClient interface {
	Ready(context.Context) error
}

type Adapter interface {
	Type() domain.CoreType
	Render(context.Context, ProfileSnapshot) (RenderedConfig, error)
	Validate(context.Context, RuntimeSpec) error
	Start(context.Context, RuntimeSpec) (Process, error)
	Runtime(string) (RuntimeClient, error)
}
