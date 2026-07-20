package mihomo

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/zhangjianyong66/mihomo-manager/internal/core"
	"github.com/zhangjianyong66/mihomo-manager/internal/domain"
)

type AdapterOptions struct {
	Binary            string
	ValidationTimeout time.Duration
	StopTimeout       time.Duration
	HTTPClient        *http.Client
}

type Adapter struct {
	options AdapterOptions
}

func NewAdapter(options AdapterOptions) *Adapter {
	if options.ValidationTimeout <= 0 {
		options.ValidationTimeout = 10 * time.Second
	}
	if options.StopTimeout <= 0 {
		options.StopTimeout = 5 * time.Second
	}
	if options.HTTPClient == nil {
		options.HTTPClient = &http.Client{Timeout: 2 * time.Second}
	}
	return &Adapter{options: options}
}

func (a *Adapter) Type() domain.CoreType { return domain.CoreTypeMihomo }

func (a *Adapter) Render(ctx context.Context, snapshot core.ProfileSnapshot) (core.RenderedConfig, error) {
	select {
	case <-ctx.Done():
		return core.RenderedConfig{}, ctx.Err()
	default:
	}
	if snapshot.Mode != domain.ProfileModeManaged {
		return core.RenderedConfig{}, core.ErrUnsupportedProfile
	}
	content, endpoint, err := renderManaged(snapshot)
	if err != nil {
		return core.RenderedConfig{}, err
	}
	return core.NewRenderedConfig(content, endpoint), nil
}

func (a *Adapter) Validate(ctx context.Context, spec core.RuntimeSpec) error {
	content, err := readConfig(spec.ConfigPath)
	if err != nil {
		return fmt.Errorf("read mihomo config: %w", err)
	}
	configEndpoint, err := inspectStatic(content)
	if err != nil {
		return err
	}
	expectedEndpoint, _, err := parseControllerEndpoint(spec.ControllerEndpoint)
	if err != nil || configEndpoint != expectedEndpoint {
		return fmt.Errorf("runtime endpoint does not match mihomo config: %w", core.ErrInvalidConfig)
	}
	if strings.TrimSpace(a.options.Binary) == "" {
		return errors.New("mihomo binary path is empty")
	}
	return validateNative(ctx, a.options.Binary, spec, a.options.ValidationTimeout)
}

func (a *Adapter) Start(ctx context.Context, spec core.RuntimeSpec) (core.Process, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}
	if strings.TrimSpace(a.options.Binary) == "" {
		return nil, errors.New("mihomo binary path is empty")
	}
	return startProcess(a.options.Binary, spec, a.options.StopTimeout)
}

func (a *Adapter) Runtime(endpoint string) (core.RuntimeClient, error) {
	return newRuntimeClient(endpoint, a.options.HTTPClient)
}
