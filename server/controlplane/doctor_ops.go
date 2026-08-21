package controlplane

import (
	"context"
	"os"
	"runtime"
	"strings"
	"time"

	"github.com/ailiheizi/restoreweave/client/command"
	"github.com/ailiheizi/restoreweave/server/internal/identify"
	"github.com/ailiheizi/restoreweave/server/internal/repository"
)

type doctorInput struct {
	Source     string `json:"source"`
	Repository string `json:"repository"`
}

func (d *Dispatcher) handleDoctorCheck(ctx context.Context, env command.Envelope, started time.Time) command.Result {
	var input doctorInput
	if len(env.Input) > 0 {
		if err := decodeInput(env.Input, &input); err != nil {
			return invalidInputResult(env, started, err)
		}
	}
	checks := make([]command.DoctorCheck, 0, 8)
	add := func(id, scope string, ok bool, message string) {
		checks = append(checks, command.DoctorCheck{ID: id, Scope: scope, OK: ok, Message: message})
	}

	add("controller", "core", true, "restoreweaved is serving doctor.check")

	if err := d.store.Ping(ctx); err != nil {
		add("catalog", "core", false, err.Error())
	} else {
		add("catalog", "core", true, "catalog reachable at "+d.catalogPath)
	}

	add("identify", "core", true, "host detector "+IdentifyBuiltinID+" digest "+identify.RulesDigest())
	add("processors", "core", true, "default ingest stays in-process; processor failure must not block exact ingest, verify, or restore")

	if runtime.GOOS != "linux" {
		add("sandbox", "optional", true, runtime.GOOS+" has no Linux bubblewrap gate; optional isolated parsers stay out of the default pack")
	} else {
		add("sandbox", "optional", true, "Linux bubblewrap is only required if isolated heavy parsers join the default pack")
	}

	if d.exact != nil && d.exact.Repo != nil {
		root := d.exact.Repo.Root()
		profile := repository.DescribeProfile(d.exact.Repo)
		if want := strings.TrimSpace(input.Repository); want != "" && want != root {
			add("repository", "core", false, "doctor repository path does not match the running exact lane")
		} else if manifests, err := d.exact.ListSnapshots(ctx); err != nil {
			add("repository", "core", false, err.Error())
		} else {
			add("repository", "core", true, "repository profile "+profile.Repository+" + "+profile.Compression+" at "+root)
			ok := true
			message := "no published snapshot yet"
			if len(manifests) > 0 {
				latest := manifests[len(manifests)-1]
				if _, err := d.exact.Verify(ctx, latest.SnapshotRef); err != nil {
					ok = false
					message = err.Error()
				} else {
					message = "latest snapshot " + latest.SnapshotRef + " verified at full-bytes"
				}
			}
			add("recovery", "core", ok, message)
		}
	} else {
		add("repository", "core", false, "exact lane is not attached")
		add("recovery", "core", false, "cannot verify snapshots without the exact lane")
	}

	engineMessage := "configured repository is not a selected release engine"
	if d.exact != nil && d.exact.Repo != nil {
		profile := repository.DescribeProfile(d.exact.Repo)
		engineMessage = "repository profile " + profile.Repository + " is not a selected release engine"
	}
	add("engine", "core", true, engineMessage)

	if src := strings.TrimSpace(input.Source); src != "" {
		info, err := os.Stat(src)
		switch {
		case err != nil:
			add("source", "core", false, err.Error())
		case !info.IsDir():
			add("source", "core", false, "source is not a directory")
		default:
			add("source", "core", true, "source directory is readable")
		}
	}

	ok := true
	for _, check := range checks {
		if check.Scope == "core" && !check.OK {
			ok = false
		}
	}
	data := command.DoctorData{OK: ok, Checks: checks}
	if !ok {
		return degradedResult(env, started, data, "one or more core doctor checks failed")
	}
	return succeeded(env, started, data)
}
