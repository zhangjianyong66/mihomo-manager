package cli

import (
	"encoding/json"
	"fmt"
	"io"

	"github.com/spf13/cobra"
	"github.com/zhangjianyong66/mihomo-manager/internal/app"
)

const APIVersion = "mm/v1"

type OutputFormat string

const (
	OutputTable OutputFormat = "table"
	OutputJSON  OutputFormat = "json"
)

func (f OutputFormat) String() string { return string(f) }

func (f *OutputFormat) Set(value string) error {
	parsed, err := ParseOutputFormat(value)
	if err != nil {
		return err
	}
	*f = parsed
	return nil
}

func (f OutputFormat) Type() string { return "format" }

func ParseOutputFormat(value string) (OutputFormat, error) {
	switch OutputFormat(value) {
	case OutputTable:
		return OutputTable, nil
	case OutputJSON:
		return OutputJSON, nil
	default:
		return "", &app.Error{
			Code:    app.ErrorCodeInvalidArgument,
			Message: fmt.Sprintf("不支持的输出格式 %q，可选值为 table、json", value),
			Details: map[string]any{
				"value":   value,
				"allowed": []string{OutputTable.String(), OutputJSON.String()},
			},
		}
	}
}

type OutputOptions struct {
	Format      OutputFormat
	ShowSecrets bool
}

func BindOutputOptions(cmd *cobra.Command, options *OutputOptions) {
	if options.Format == "" {
		options.Format = OutputTable
	}
	cmd.Flags().Var(&options.Format, "output", "输出格式：table 或 json")
	cmd.Flags().BoolVar(&options.ShowSecrets, "show-secrets", false, "显示完整敏感信息")
	cmd.SetFlagErrorFunc(invalidFlagError)
}

type Result struct {
	Kind     string
	Data     func(showSecrets bool) any
	Table    func(io.Writer, bool) error
	Warnings []string
}

type Presenter struct {
	stdout  io.Writer
	stderr  io.Writer
	options OutputOptions
}

func NewPresenter(stdout, stderr io.Writer, options OutputOptions) Presenter {
	if stdout == nil {
		stdout = io.Discard
	}
	if stderr == nil {
		stderr = io.Discard
	}
	if options.Format == "" {
		options.Format = OutputTable
	}
	return Presenter{stdout: stdout, stderr: stderr, options: options}
}

func (p Presenter) WriteResult(result Result) error {
	if result.Kind == "" {
		return &app.Error{Code: app.ErrorCodeInternal, Message: "输出类型不能为空"}
	}
	switch p.options.Format {
	case OutputTable:
		if result.Table == nil {
			return &app.Error{Code: app.ErrorCodeInternal, Message: "缺少表格输出实现"}
		}
		if err := result.Table(p.stdout, p.options.ShowSecrets); err != nil {
			return err
		}
		for _, warning := range result.Warnings {
			if _, err := fmt.Fprintf(p.stderr, "警告: %s\n", warning); err != nil {
				return err
			}
		}
		return nil
	case OutputJSON:
		var data any
		if result.Data != nil {
			data = result.Data(p.options.ShowSecrets)
		}
		warnings := result.Warnings
		if warnings == nil {
			warnings = []string{}
		}
		return writeJSON(p.stdout, successEnvelope{
			APIVersion: APIVersion,
			Kind:       result.Kind,
			Data:       data,
			Warnings:   warnings,
		})
	default:
		_, err := ParseOutputFormat(p.options.Format.String())
		return err
	}
}

type successEnvelope struct {
	APIVersion string   `json:"apiVersion"`
	Kind       string   `json:"kind"`
	Data       any      `json:"data"`
	Warnings   []string `json:"warnings"`
}

func writeJSON(w io.Writer, value any) error {
	encoder := json.NewEncoder(w)
	encoder.SetEscapeHTML(false)
	return encoder.Encode(value)
}
