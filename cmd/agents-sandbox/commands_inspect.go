package main

import (
	"github.com/spf13/cobra"

	"github.com/inoio/agents-sandbox/internal/sandbox/image"
	sandbox "github.com/inoio/agents-sandbox/internal/sandbox/vm"
	"github.com/inoio/agents-sandbox/internal/sandbox/volume"
	"github.com/inoio/agents-sandbox/internal/termio"
)

// buildVolumeInspectCmd builds `volume inspect <name>`, which prints the full
// detail of a home volume.
func buildVolumeInspectCmd(ui termio.UI) *cobra.Command {
	var format string
	cmd := &cobra.Command{
		Use:   cmdInspect + " <name>",
		Args:  cobra.ExactArgs(1),
		Short: "Show detailed information about a home volume",
		RunE: func(cmd *cobra.Command, args []string) error {
			mode, err := resolveListOutput(format, ui.Quiet())
			if err != nil {
				return err
			}
			detail, err := volume.InspectVolume(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			return printInspect(ui, mode, detail.Name,
				func() { printVolumeInspect(ui, detail) },
				newVolumeInspectJSON(detail))
		},
	}
	cmd.Flags().StringVar(&format, flagFormat, "", "Output format (json)")
	return cmd
}

// buildImageInspectCmd builds `image inspect <ref>`, which prints a cached
// runner image's manifest, layers, and OCI config.
func buildImageInspectCmd(ui termio.UI) *cobra.Command {
	var format string
	cmd := &cobra.Command{
		Use:   cmdInspect + " <ref>",
		Args:  cobra.ExactArgs(1),
		Short: "Show detailed information about a cached runner image",
		RunE: func(cmd *cobra.Command, args []string) error {
			mode, err := resolveListOutput(format, ui.Quiet())
			if err != nil {
				return err
			}
			detail, err := image.InspectImage(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			return printInspect(ui, mode, detail.Reference,
				func() { printImageInspect(ui, detail) },
				newImageInspectJSON(detail))
		},
	}
	cmd.Flags().StringVar(&format, flagFormat, "", "Output format (json)")
	return cmd
}

// buildSandboxInspectCmd builds `sandbox inspect <name>`, which prints a
// sandbox VM's status, image, labels, mounts, resources, ID, and backend.
func buildSandboxInspectCmd(ui termio.UI) *cobra.Command {
	var format string
	cmd := &cobra.Command{
		Use:   cmdInspect + " <name>",
		Args:  cobra.ExactArgs(1),
		Short: "Show detailed information about a sandbox VM",
		RunE: func(cmd *cobra.Command, args []string) error {
			mode, err := resolveListOutput(format, ui.Quiet())
			if err != nil {
				return err
			}
			detail, err := sandbox.InspectSandbox(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			return printInspect(ui, mode, detail.Name,
				func() { printSandboxInspect(ui, detail) },
				newSandboxInspectJSON(detail))
		},
	}
	cmd.Flags().StringVar(&format, flagFormat, "", "Output format (json)")
	return cmd
}
