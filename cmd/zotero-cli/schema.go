package main

import (
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// CommandSchema describes a CLI command for machine consumption.
type CommandSchema struct {
	Name        string       `json:"name"`
	Description string       `json:"description"`
	Usage       string       `json:"usage"`
	Args        string       `json:"args,omitempty"`
	Flags       []FlagSchema `json:"flags,omitempty"`
}

// FlagSchema describes a single flag for a command.
type FlagSchema struct {
	Name         string `json:"name"`
	Shorthand    string `json:"shorthand,omitempty"`
	Type         string `json:"type"`
	Default      string `json:"default,omitempty"`
	Description  string `json:"description"`
	Required     bool   `json:"required,omitempty"`
}

func newSchemaCmd(root *cobra.Command) *cobra.Command {
	return &cobra.Command{
		Use:   "schema",
		Short: "Show CLI command schema (for AI agents)",
		Long:  "Output a JSON description of all commands and flags. Useful for AI agents to discover available functionality.",
		RunE: func(cmd *cobra.Command, args []string) error {
			schemas := buildSchemas(root)
			data, err := json.MarshalIndent(schemas, "", "  ")
			if err != nil {
				return err
			}
			fmt.Println(string(data))
			return nil
		},
	}
}

func buildSchemas(root *cobra.Command) []CommandSchema {
	var schemas []CommandSchema
	for _, cmd := range root.Commands() {
		if cmd.Hidden || cmd.Name() == "help" || cmd.Name() == "completion" || cmd.Name() == "schema" {
			continue
		}
		s := CommandSchema{
			Name:        cmd.Name(),
			Description: cmd.Short,
			Usage:       cmd.UseLine(),
		}
		if ann, ok := cmd.Annotations["args"]; ok {
			s.Args = ann
		}
		cmd.Flags().VisitAll(func(f *pflag.Flag) {
			if f.Hidden {
				return
			}
			fs := FlagSchema{
				Name:        f.Name,
				Shorthand:   f.Shorthand,
				Type:        f.Value.Type(),
				Default:     f.DefValue,
				Description: f.Usage,
			}
			if ann, ok := cmd.Annotations["required_"+f.Name]; ok && ann == "true" {
				fs.Required = true
			}
			s.Flags = append(s.Flags, fs)
		})
		schemas = append(schemas, s)
	}
	return schemas
}
