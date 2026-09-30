package server

import (
	"context"
	"log/slog"
	"os"
	"sort"

	prettylog "github.com/rickliujh/loom/internal/log"
	"github.com/rickliujh/loom/pkg/bulk"
	"github.com/rickliujh/loom/pkg/config"
	"github.com/rickliujh/loom/pkg/engine"
	"github.com/rickliujh/loom/pkg/event"
	"github.com/rickliujh/loom/pkg/generate"
	"github.com/rickliujh/loom/pkg/module"
)

// execute runs j through the same entry points the CLI uses, with a logger
// that feeds both the job's event stream and its plain-text log.
func (m *jobManager) execute(j *job) error {
	m.mu.Lock()
	p := j.prepared
	ctx := j.ctx
	workspace := j.Workspace
	m.mu.Unlock()
	if p == nil {
		return context.Cause(ctx)
	}

	sink := j.events.sink()
	logger := slog.New(prettylog.NewMultiHandler(
		prettylog.NewEventHandler(sink, slog.LevelDebug),
		prettylog.NewPrettyHandler(j.text, &slog.HandlerOptions{Level: slog.LevelInfo}),
	))

	if workspace != "" {
		if err := os.MkdirAll(workspace, 0o700); err != nil {
			return err
		}
	}

	var err error
	switch p.req.Kind {
	case KindRun:
		err = m.executeRun(ctx, j, p, workspace, logger, sink)
	case KindDiff:
		err = m.executeDiff(ctx, j, p, workspace, logger, sink)
	case KindGenerate:
		err = m.executeGenerate(ctx, p, logger)
	case KindBulk:
		err = m.executeBulk(ctx, p, logger)
	}
	if err == nil && p.output != "" {
		// The module generate or bulk wrote is named only once it exists.
		if lf, lerr := config.Load(p.output); lerr == nil {
			m.update(j, func(j *job) { j.Module.Name = lf.Metadata.Name })
		}
	}

	// The CLI's closing status line, so log.txt reads like a terminal.
	if err != nil {
		prettylog.Failuref(j.text, "%v", err)
	} else {
		prettylog.Successf(j.text, "%s complete", p.req.Kind)
	}
	return err
}

// moduleDir resolves a job's module source: its directory, or a clone of a
// remote source logged through the job's logger.
func moduleDir(ctx context.Context, src *moduleSource, logger *slog.Logger) (string, func(), error) {
	if src.Local {
		return src.Dir, func() {}, nil
	}
	dir, cleanup, err := module.ResolveSourceContext(ctx, src.URL, "", logger)
	if err != nil {
		return "", nil, err
	}
	if cleanup == nil {
		cleanup = func() {}
	}
	return dir, cleanup, nil
}

func (m *jobManager) executeRun(ctx context.Context, j *job, p *preparedJob, workspace string, logger *slog.Logger, sink event.Sink) error {
	dir, cleanup, err := moduleDir(ctx, p.src, logger)
	if err != nil {
		return err
	}
	defer cleanup()

	targetPath := p.targetPath
	if targetPath == "" {
		targetPath = workspace
	} else if p.req.Mode == ModeLocal {
		// A local run keeps its results there, and the CLI expects
		// --target-path to exist; a form cannot create it, so the server
		// does. Other modes leave nothing behind, a directory included.
		if err := os.MkdirAll(targetPath, 0o755); err != nil {
			return err
		}
	}
	res, runErr := engine.Run(ctx, engine.RunRequest{
		ModuleDir:  dir,
		Params:     p.req.Params,
		DryRun:     p.req.Mode == ModeDryRun,
		LocalRun:   p.req.Mode == ModeLocal,
		TargetPath: targetPath,
		GitAuthor:  p.req.Author,
		GitEmail:   p.req.Email,
		Logger:     logger,
		Events:     sink,
	})
	m.update(j, func(j *job) {
		if res.ModuleName != "" {
			j.Module.Name = res.ModuleName
		}
		j.Result.PRs = prsOut(res.PRs)
		if p.req.Mode == ModeLocal {
			j.Result.Workspace = targetPath
		}
	})

	// A local run's result is its clones; read them back as a diff, as
	// full-mode diff does. Only the clones the run itself made are read:
	// reading stages every change, and a user's target path may hold a
	// checkout they are working in. A failed run still shows what it
	// changed.
	if p.req.Mode == ModeLocal && ctx.Err() == nil {
		d := &diffOut{Mode: engine.ModeFull, Incomplete: runErr != nil}
		dirs := make([]string, 0, len(res.DirLabels))
		for dir := range res.DirLabels {
			dirs = append(dirs, dir)
		}
		// The numbered clone dirs sort in the order the run made them.
		sort.Strings(dirs)
		targets, _ := engine.CollectDirDiffs(ctx, dirs, engine.CollectOptions{Labels: res.DirLabels, Files: true})
		d.Targets = nonNilTargets(targets)
		if len(dirs) == 0 {
			d.Note = "no target repository was cloned: a module without a target spec runs directly in the target path, which is not diffed"
		}
		emitDiffs(sink, d.Targets)
		m.setDiff(j, d)
	}
	return runErr
}

func (m *jobManager) executeDiff(ctx context.Context, j *job, p *preparedJob, workspace string, logger *slog.Logger, sink event.Sink) error {
	dir, cleanup, err := moduleDir(ctx, p.src, logger)
	if err != nil {
		return err
	}
	defer cleanup()

	res, diffErr := engine.Diff(ctx, engine.DiffRequest{
		ModuleDir: dir,
		Params:    p.req.Params,
		Quick:     p.req.Quick,
		Workspace: workspace,
		GitAuthor: p.req.Author,
		GitEmail:  p.req.Email,
		// The UI always shows what a failed run changed, flagged
		// incomplete: `loom diff --partial`.
		CollectOnFailure: true,
		// The API serves parsed files; git's own text is the CLI's.
		Files:  true,
		Logger: logger,
		Events: sink,
	})
	m.update(j, func(j *job) {
		if res.ModuleName != "" {
			j.Module.Name = res.ModuleName
		}
	})
	incomplete := res.Incomplete || (diffErr != nil && len(res.Targets) > 0)
	m.setDiff(j, &diffOut{Mode: res.Mode, Incomplete: incomplete, Targets: nonNilTargets(res.Targets)})
	return diffErr
}

func (m *jobManager) executeGenerate(ctx context.Context, p *preparedJob, logger *slog.Logger) error {
	// The job may have waited in the queue since the output was checked;
	// something outside the server may have written there meanwhile.
	if !p.req.Overwrite {
		if err := checkOutputEmpty(p.output, p.req.Output); err != nil {
			return err
		}
	}
	return generate.Run(ctx, generate.Options{
		Ref:        p.req.Refs[0],
		Params:     p.req.Values,
		OutputDir:  p.output,
		ModuleName: p.req.Name,
		TokenEnv:   p.req.TokenEnv,
		Provider:   m.s.generateProvider,
	}, logger)
}

func (m *jobManager) executeBulk(ctx context.Context, p *preparedJob, logger *slog.Logger) error {
	ref := p.src.URL
	if p.src.Local {
		ref = p.src.Dir
	}
	var items []map[string]any
	if p.req.Items != nil {
		items = make([]map[string]any, len(p.req.Items))
		for i, it := range p.req.Items {
			if it == nil {
				it = Params{}
			}
			items[i] = it
		}
	}
	return bulk.RunContext(ctx, bulk.Options{
		ModuleRef: ref,
		OutputDir: p.output,
		Name:      p.req.Name,
		Items:     items,
		NameParam: p.req.NameParam,
	}, logger)
}

// setDiff stores a job's diff and its summary, and persists it.
func (m *jobManager) setDiff(j *job, d *diffOut) {
	m.mu.Lock()
	j.diff = d
	j.Result.Diff = d.summary()
	m.mu.Unlock()
	if m.history != nil {
		if err := m.history.saveDiff(j.ID, d); err != nil {
			m.s.logger.Warn("saving job diff", "job", j.ID, "error", err)
		}
	}
}

func nonNilTargets(t []engine.TargetDiff) []engine.TargetDiff {
	if t == nil {
		return []engine.TargetDiff{}
	}
	for i := range t {
		if t[i].Path == nil {
			t[i].Path = []string{}
		}
		if t[i].Files == nil {
			t[i].Files = []engine.FileDiff{}
		}
	}
	return t
}

// emitDiffs reports a local run's changed files as diff.file events, as a
// diff job does.
func emitDiffs(sink event.Sink, targets []engine.TargetDiff) {
	for _, t := range targets {
		for i := range t.Files {
			f := t.Files[i]
			sink.Emit(event.Event{Type: event.DiffFile, Path: t.Path, Target: t.Label, Diff: &f})
		}
	}
}
