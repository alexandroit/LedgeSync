// LedgeSync CLI is a secondary surface over shared application and authentication services.
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
	"syscall"

	"github.com/alexandroit/LedgeSync/internal/app"
	"github.com/alexandroit/LedgeSync/internal/config"
	"github.com/alexandroit/LedgeSync/internal/domain"
	"github.com/alexandroit/LedgeSync/internal/policy"
)

const version = "0.1.0-alpha.6"
const usage = `LedgeSync 0.1.0-alpha.6 — approved folder copies to Google Drive and local policy previews

Usage:
  ledgesync browse --root DIRECTORY --json
  ledgesync browse --config project.json --json
  ledgesync config validate --config project.json
  ledgesync explain --config project.json --path relative/file --json
  ledgesync plan --config project.json [--output plan.json]
  ledgesync plan inspect --plan plan.json
  ledgesync capabilities
  ledgesync auth status
  ledgesync auth connect [--no-browser]
  ledgesync auth disconnect
  ledgesync copy --root DIRECTORY --destination root|picker|FOLDER_ID
  ledgesync copy --config project.json --pick-destination [--no-browser]
  ledgesync copy --pair PAIR_ID
  ledgesync pairs list
  ledgesync pairs add --root DIRECTORY --destination root|FOLDER_ID [--name NAME]
  ledgesync pairs remove --pair PAIR_ID
  ledgesync automatic enable --pair PAIR_ID [--every MINUTES] [--watch]
  ledgesync automatic disable --pair PAIR_ID
  ledgesync automatic run [--pair PAIR_ID]
  ledgesync automatic watch
  ledgesync restore --pair PAIR_ID --to EMPTY_DIRECTORY
  ledgesync --version

Browse/explain/plan are offline previews against a fake destination.
Auth status reads local connection metadata, without checking the grant online.
Connect, disconnect, copy and "automatic enable" require an interactive terminal:
copy shows a fresh preview and requires its exact digest before uploading.
Saved pairs are shared with the desktop application. "automatic enable" authorizes
create-only copies bound to the reviewed preview; "automatic run" (for cron or a
systemd timer) and "automatic watch" execute only authorized pairs and pause them
when the account, destination, folder, configuration or ignore rules change.
"restore" downloads a pair's verified Drive copy into a new empty folder.
Nothing deletes or overwrites Drive files, and no service is installed or enabled.
For SSH servers, --no-browser displays loopback forwarding instructions for your browser.
Git policy is patterns-only; no Git or rclone executable is needed at runtime.
`

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
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
	if args[0] == "copy" && len(args) == 3 && args[1] == "--pair" {
		return runPairCopy(ctx, args[2], os.Stdin, out, errOut, interactiveTerminal(os.Stdin, out, errOut), newPairServices)
	}
	if args[0] == "pairs" || args[0] == "automatic" || args[0] == "restore" {
		return runPairs(ctx, args, os.Stdin, out, errOut, interactiveTerminal(os.Stdin, out, errOut), newPairServices)
	}
	if args[0] == "copy" || (args[0] == "auth" && len(args) > 1 && (args[1] == "connect" || args[1] == "disconnect")) {
		return runOnline(ctx, args, os.Stdin, out, errOut, interactiveTerminal(os.Stdin, out, errOut), newOnlineServices)
	}
	if args[0] == "auth" {
		return runAuthStatus(ctx, args[1:], out, errOut, newAuthStatusService)
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
		return report(errOut, domain.Fail("CAPABILITY_UNSUPPORTED", "command %q is unavailable in this CLI; use the desktop for approved uploads", command))
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
	case "PARTIAL":
		return 3
	default:
		return 6
	}
}
