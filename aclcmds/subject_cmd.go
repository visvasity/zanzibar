// Copyright (c) 2026 Visvasity LLC

package aclcmds

import (
	"context"
	"flag"

	"github.com/visvasity/cli"
)

// SubjectCommand is a subject-taking aclcmds command (Grant, Revoke, Check,
// ListTuples, Expand, ListObjects, ListUsers): it plugs into the cli framework,
// accepts subject-mapping options, and carries a one-line purpose. DeleteObject
// and the config commands are not subject commands and are used directly.
type SubjectCommand interface {
	cli.Command

	// SetSubjectOptions configures how the command maps user subjects between
	// human emails (as entered and displayed) and stored identities (see
	// SubjectOptions). Every subject-taking command implements it.
	SetSubjectOptions(SubjectOptions)

	// Purpose is the command's one-line summary, forwarded by the wrapper.
	Purpose() string
}

// SubjectCmdWrapper wraps a SubjectCommand with a -synthetic flag and a set of
// [SubjectOptions]. Without -synthetic the options are applied before the command
// runs, so it maps human emails to and from synthetic identities; with -synthetic
// the options are skipped and subjects are used verbatim (as already-synthetic
// ids). It forwards Purpose to the wrapped command.
//
// An application that maps subjects (for example a gateway whose stored subjects
// are synthetic account identities) builds one [SubjectOptions] and wraps each
// subject command with it:
//
//	opts := aclcmds.SubjectOptions{IsSynthetic: app.IsSynthetic, Resolve: app.Resolve, Format: app.Format}
//	cli.NewGroup("acl", "Access control",
//	    aclcmds.NewSubjectCmd(new(aclcmds.Grant), opts),
//	    aclcmds.NewSubjectCmd(new(aclcmds.Check), opts),
//	    new(aclcmds.DeleteObject),
//	    // ...
//	)
type SubjectCmdWrapper[T SubjectCommand] struct {
	cmd       T
	opts      SubjectOptions
	synthetic bool
}

// NewSubjectCmd wraps a fresh subject-command instance with the given
// subject-mapping options, e.g. NewSubjectCmd(new(aclcmds.Grant), opts).
func NewSubjectCmd[T SubjectCommand](cmd T, opts SubjectOptions) *SubjectCmdWrapper[T] {
	return &SubjectCmdWrapper[T]{cmd: cmd, opts: opts}
}

// Purpose forwards the wrapped command's purpose.
func (w *SubjectCmdWrapper[T]) Purpose() string { return w.cmd.Purpose() }

// Command implements cli.Command. It adds the -synthetic flag to the wrapped
// command's flag set and applies the subject options unless -synthetic is set.
func (w *SubjectCmdWrapper[T]) Command() (string, *flag.FlagSet, cli.CmdFunc) {
	name, fset, run := w.cmd.Command()
	fset.BoolVar(&w.synthetic, "synthetic", false, "When true, use/print raw synthetic subject ids instead of resolving human emails")
	return name, fset, func(ctx context.Context, args []string) error {
		if !w.synthetic {
			w.cmd.SetSubjectOptions(w.opts)
		}
		return run(ctx, args)
	}
}
