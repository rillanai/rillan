package main

import (
	"fmt"
	"strings"
	"time"

	"github.com/rillanai/rillan/internal/agent"
	"github.com/spf13/cobra"
)

func newSkillCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "skill",
		Short: "Manage installed markdown skills",
	}

	cmd.AddCommand(newSkillInstallCommand())
	cmd.AddCommand(newSkillRemoveCommand())
	cmd.AddCommand(newSkillListCommand())
	cmd.AddCommand(newSkillShowCommand())

	return cmd
}

func newSkillInstallCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "install <path>",
		Short: "Install a markdown skill into managed Rillan storage",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			skill, err := agent.InstallSkill(args[0], time.Now())
			if err != nil {
				return err
			}
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "installed skill %s at %s\n", skill.ID, skill.ManagedPath)
			return nil
		},
	}
}

func newSkillRemoveCommand() *cobra.Command {
	var force bool
	cmd := &cobra.Command{
		Use:   "remove <id>",
		Short: "Remove an installed markdown skill",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			removed, err := agent.RemoveSkill(args[0], force)
			if err != nil {
				return err
			}
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "removed skill %s\n", removed.ID)
			return nil
		},
	}
	cmd.Flags().BoolVar(&force, "force", false, "Remove the skill even if the current project still enables it")
	return cmd
}

func newSkillListCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List installed markdown skills",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			skills, err := agent.ListInstalledSkills()
			if err != nil {
				return err
			}
			for _, skill := range skills {
				_, _ = fmt.Fprintf(cmd.OutOrStdout(), "- id: %s\n  display_name: %s\n  installed_at: %s\n  checksum: %s\n", skill.ID, skill.DisplayName, skill.InstalledAt, skill.Checksum)
			}
			return nil
		},
	}
}

func newSkillShowCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "show <id>",
		Short: "Show metadata for an installed markdown skill",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			skill, err := agent.GetInstalledSkill(args[0])
			if err != nil {
				return err
			}
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "id: %s\ndisplay_name: %s\nsource_path: %s\nmanaged_path: %s\nchecksum: %s\nparser_version: %s\ncapability_summary: %s\n", skill.ID, skill.DisplayName, skill.SourcePath, skill.ManagedPath, skill.Checksum, skill.ParserVersion, strings.TrimSpace(skill.CapabilitySummary))
			return nil
		},
	}
}
