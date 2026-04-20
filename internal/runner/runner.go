package runner

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"

	"github.com/ProwlrBot/prowlrview/internal/adapter"
	"github.com/ProwlrBot/prowlrview/internal/graph"
	"gopkg.in/yaml.v3"
)

// Pipeline is the top-level YAML structure.
type Pipeline struct {
	Name   string  `yaml:"name"`
	Target string  `yaml:"target"`
	Stages []Stage `yaml:"stages"`
}

// Stage is one step in the pipeline.
type Stage struct {
	Name     string   `yaml:"name"`
	Run      string   `yaml:"run"`      // single command (templated)
	Parallel []string `yaml:"parallel"` // run these concurrently instead of Run
	Output   string   `yaml:"output"`   // optional: write stdout to this file
}

// Load reads and parses a pipeline YAML file.
func Load(path string) (*Pipeline, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var p Pipeline
	if err := yaml.Unmarshal(data, &p); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	return &p, nil
}

// Run executes the pipeline, feeding every stdout line into g.
// logFn receives status messages. Returns on completion or ctx cancel.
func Run(ctx context.Context, p *Pipeline, g *graph.Graph, logFn func(string)) error {
	target := p.Target
	for _, stage := range p.Stages {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		logFn(fmt.Sprintf("[run] stage: %s", stage.Name))
		if len(stage.Parallel) > 0 {
			if err := runParallel(ctx, stage.Parallel, target, g, logFn); err != nil {
				logFn(fmt.Sprintf("[run] stage %s error: %v", stage.Name, err))
			}
		} else {
			if err := runCmd(ctx, stage.Run, target, stage.Output, g, logFn); err != nil {
				logFn(fmt.Sprintf("[run] stage %s error: %v", stage.Name, err))
			}
		}
	}
	logFn("[run] pipeline complete")
	return nil
}

func runParallel(ctx context.Context, cmds []string, target string, g *graph.Graph, logFn func(string)) error {
	var wg sync.WaitGroup
	for _, c := range cmds {
		wg.Add(1)
		go func(cmd string) {
			defer wg.Done()
			if err := runCmd(ctx, cmd, target, "", g, logFn); err != nil {
				logFn(fmt.Sprintf("[run] parallel error (%s): %v", cmd, err))
			}
		}(c)
	}
	wg.Wait()
	return nil
}

func runCmd(ctx context.Context, cmdStr, target, outFile string, g *graph.Graph, logFn func(string)) error {
	// template substitution: {{target}} → actual target
	cmdStr = strings.ReplaceAll(cmdStr, "{{target}}", target)
	// split on whitespace (simple; doesn't handle quoted args)
	parts := strings.Fields(cmdStr)
	if len(parts) == 0 {
		return nil
	}
	cmd := exec.CommandContext(ctx, parts[0], parts[1:]...)
	cmd.Stderr = os.Stderr

	pr, pw := io.Pipe()
	cmd.Stdout = pw

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start %q: %w", parts[0], err)
	}

	// tee stdout → graph AND optional file
	var fw io.Writer
	if outFile != "" {
		outFile = strings.ReplaceAll(outFile, "{{target}}", target)
		f, err := os.Create(outFile)
		if err == nil {
			defer f.Close()
			fw = f
		}
	}

	done := make(chan error, 1)
	go func() {
		sc := bufio.NewScanner(pr)
		sc.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
		for sc.Scan() {
			line := sc.Bytes()
			adapter.ParseLine(line, g)
			if fw != nil {
				fw.Write(line)
				fw.Write([]byte{'\n'})
			}
		}
		done <- sc.Err()
	}()

	cmdErr := cmd.Wait()
	pw.Close()
	scanErr := <-done
	pr.Close()

	if cmdErr != nil && ctx.Err() == nil {
		return fmt.Errorf("cmd %q: %w", parts[0], cmdErr)
	}
	if scanErr != nil {
		logFn(fmt.Sprintf("[run] scan error: %v", scanErr))
	}
	return nil
}
