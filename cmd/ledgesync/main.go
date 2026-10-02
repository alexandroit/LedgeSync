// LedgeSync CLI is a secondary surface over the shared offline application service.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strings"

	"github.com/alexandroit/LedgeSync/internal/app"
	"github.com/alexandroit/LedgeSync/internal/config"
	"github.com/alexandroit/LedgeSync/internal/domain"
	"github.com/alexandroit/LedgeSync/internal/policy"
)

const version = "0.1.0-alpha.2"
const usage = `LedgeSync 0.1.0-alpha.2 — offline policy explorer

Usage:
  ledgesync browse --root DIRECTORY --json
  ledgesync browse --config project.json --json
  ledgesync config validate --config project.json
  ledgesync explain --config project.json --path relative/file --json
  ledgesync plan --config project.json [--output plan.json]
  ledgesync plan inspect --plan plan.json
  ledgesync capabilities
  ledgesync --version

All CLI previews use a fake empty destination. This headless CLI has no cloud
connection or credential import. Google Drive authorization is available only
in the desktop app's Connections screen with your own Desktop app OAuth client.
Cloud browsing, transfer, apply, deletion and scheduling are not implemented.
Git policy is patterns-only; no Git or rclone executable is needed at runtime.
`

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	os.Exit(run(ctx, os.Args[1:], os.Stdout, os.Stderr))
}
func output(w io.Writer, v any) error {
	e := json.NewEncoder(w)
	e.SetEscapeHTML(false)
	e.SetIndent("", "  ")
	return e.Encode(v)
}
func run(ctx context.Context, args []string, out, errOut io.Writer) int {
	if len(args) == 0 || args[0] == "--help" || args[0] == "-h" {
		fmt.Fprint(out, usage)
		return 0
	}
	if args[0] == "--version" || args[0] == "version" {
		fmt.Fprintln(out, "LedgeSync "+version)
		return 0
	}
	if args[0] == "capabilities" {
		if len(args) != 1 {
			return report(errOut, domain.Fail("CONFIG_INVALID", "unexpected arguments"))
		}
		return report(errOut, output(out, policy.Capabilities()))
	}
	command := args[0]
	args = args[1:]
	if command == "config" {
		if len(args) == 0 || args[0] != "validate" {
			return report(errOut, domain.Fail("CONFIG_INVALID", "expected config validate"))
		}
		command = "validate"
		args = args[1:]
	}
	if command == "plan" && len(args) > 0 && args[0] == "inspect" {
		command = "inspect"
		args = args[1:]
	}
	if !strings.Contains("|browse|validate|explain|plan|inspect|", "|"+command+"|") {
		return report(errOut, domain.Fail("CAPABILITY_UNSUPPORTED", "command %q is unavailable in the offline alpha", command))
	}
	f := flag.NewFlagSet("ledgesync "+command, flag.ContinueOnError)
	f.SetOutput(errOut)
	configPath := f.String("config", "", "project configuration")
	root := f.String("root", "", "local folder for default .gitignore preview")
	relative := f.String("path", "", "logical relative path")
	outPath := f.String("output", "", "new plan file outside source root")
	planPath := f.String("plan", "", "plan file to inspect")
	_ = f.Bool("json", false, "JSON output (default)")
	if err := f.Parse(args); err != nil {
		return 2
	}
	if f.NArg() != 0 {
		return report(errOut, domain.Fail("CONFIG_INVALID", "unexpected positional arguments"))
	}
	if command == "inspect" {
		if *planPath == "" {
			return report(errOut, domain.Fail("CONFIG_INVALID", "--plan is required"))
		}
		file, err := os.Open(*planPath)
		if err != nil {
			return report(errOut, domain.Fail("CONFIG_INVALID", "cannot read plan"))
		}
		defer file.Close()
		var p domain.Plan
		if err = config.DecodeJSON(file, &p, 32<<20); err != nil {
			return report(errOut, err)
		}
		if err = p.ValidateEnvelope(); err != nil {
			return report(errOut, err)
		}
		if err = p.ValidateDigest(); err != nil {
			return report(errOut, err)
		}
		return report(errOut, output(out, p))
	}
	if command == "validate" {
		if *configPath == "" {
			return report(errOut, domain.Fail("CONFIG_INVALID", "--config is required"))
		}
		_, err := config.Load(*configPath)
		if err != nil {
			return report(errOut, err)
		}
		return report(errOut, output(out, map[string]any{"valid": true, "schemaVersion": "1.1", "notice": "Shape and semantic validation only; source existence and adapter support are checked during preview."}))
	}
	if (*configPath == "") == (*root == "") {
		return report(errOut, domain.Fail("CONFIG_INVALID", "choose exactly one of --config or --root"))
	}
	service := app.NewService()
	var preview app.Preview
	var err error
	if *root != "" {
		preview, err = service.PreviewRoot(ctx, *root)
	} else {
		preview, err = service.Preview(ctx, *configPath)
	}
	if err != nil {
		return report(errOut, err)
	}
	if command == "explain" {
		if err := domain.ValidatePath(*relative); err != nil {
			return report(errOut, err)
		}
		for _, entry := range preview.Entries {
			if entry.Path == *relative {
				return report(errOut, output(out, entry.Explanation))
			}
		}
		return report(errOut, domain.Fail("PATH_UNSAFE", "path is not in the source inventory"))
	}
	if command == "browse" {
		return report(errOut, output(out, preview))
	}
	if *outPath != "" {
		if err := writePlan(*outPath, preview.SourceRoot, preview.Plan); err != nil {
			return report(errOut, err)
		}
	} else if err = output(out, preview.Plan); err != nil {
		return report(errOut, err)
	}
	for _, op := range preview.Plan.Operations {
		if op.Type == "conflict" {
			return 4
		}
	}
	return 0
}
func writePlan(filename, source string, p domain.Plan) error {
	absolute, err := filepath.Abs(filename)
	if err != nil {
		return domain.Fail("PATH_UNSAFE", "invalid output path")
	}
	parent, err := filepath.EvalSymlinks(filepath.Dir(absolute))
	if err != nil {
		return domain.Fail("PATH_UNSAFE", "output parent must already exist")
	}
	resolvedSource, err := filepath.EvalSymlinks(source)
	if err != nil {
		return domain.Fail("SOURCE_UNAVAILABLE", "source root disappeared")
	}
	sourceInfo, err := os.Stat(resolvedSource)
	if err != nil {
		return domain.Fail("SOURCE_UNAVAILABLE", "source identity unavailable")
	}
	// Compare real filesystem identities along the ancestry, including case aliases
	// and junction/symlink-resolved directories, instead of lexical prefix checks.
	var validatedParent os.FileInfo
	for ancestor := parent; ; ancestor = filepath.Dir(ancestor) {
		info, err := os.Stat(ancestor)
		if err != nil {
			return domain.Fail("PATH_UNSAFE", "output ancestry unavailable")
		}
		if os.SameFile(sourceInfo, info) {
			return domain.Fail("PATH_UNSAFE", "plan output must be outside the read-only source root")
		}
		if ancestor == parent {
			validatedParent = info
		}
		if filepath.Dir(ancestor) == ancestor {
			break
		}
	}
	outputRoot, err := os.OpenRoot(parent)
	if err != nil {
		return domain.Fail("PATH_UNSAFE", "cannot open output parent")
	}
	defer outputRoot.Close()
	opened, err := outputRoot.Stat(".")
	current, currentErr := os.Stat(parent)
	if err != nil || currentErr != nil || !os.SameFile(opened, current) || !os.SameFile(opened, validatedParent) {
		return domain.Fail("PATH_UNSAFE", "output parent changed")
	}
	f, err := outputRoot.OpenFile(filepath.Base(absolute), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return domain.Fail("PATH_UNSAFE", "output must be a new file in an accessible directory")
	}
	encodeErr := output(f, p)
	closeErr := f.Close()
	if encodeErr != nil {
		return encodeErr
	}
	return closeErr
}
func report(w io.Writer, err error) int {
	if err == nil {
		return 0
	}
	code := domain.ErrorCode(err)
	_ = output(w, map[string]string{"code": code, "message": err.Error()})
	switch code {
	case "CONFIG_INVALID":
		return 2
	case "CANCELLED":
		return 130
	case "CONFLICT", "PLAN_STALE":
		return 4
	default:
		return 6
	}
}
