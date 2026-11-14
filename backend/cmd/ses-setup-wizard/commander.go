package main

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// command is one program the wizard runs: the AWS CLI, Terraform or the
// send check. Env holds only what the wizard adds to its own environment.
type command struct {
	Dir  string
	Env  []string
	Name string
	Args []string
}

func (c command) String() string {
	return strings.TrimSpace(c.Name + " " + strings.Join(c.Args, " "))
}

// commander runs commands. Output captures what the command prints, for
// the wizard to read; Run hands it the terminal, for the operator to watch
// (and, for Terraform, to answer). Both return an error when the command
// cannot start or exits non-zero.
type commander interface {
	Output(ctx context.Context, c command) ([]byte, error)
	Run(ctx context.Context, c command) error
}

// execCommander runs commands for real.
type execCommander struct{}

func (execCommander) Output(ctx context.Context, c command) ([]byte, error) {
	cmd := c.cmd(ctx)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		if msg := strings.TrimSpace(stderr.String()); msg != "" {
			return out, fmt.Errorf("%s: %w: %s", c.Name, err, msg)
		}
		return out, fmt.Errorf("%s: %w", c.Name, err)
	}
	return out, nil
}

func (execCommander) Run(ctx context.Context, c command) error {
	cmd := c.cmd(ctx)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s: %w", c.Name, err)
	}
	return nil
}

func (c command) cmd(ctx context.Context) *exec.Cmd {
	cmd := exec.CommandContext(ctx, c.Name, c.Args...)
	cmd.Dir = c.Dir
	cmd.Env = append(os.Environ(), c.Env...)
	return cmd
}
