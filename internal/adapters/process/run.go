//go:build linux || darwin

package process

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"github.com/akkaraponph/evaris/internal/domain"
	"os"
	"os/exec"
	"sync"
	"syscall"
	"time"
)

type ProcessResult struct {
	Stdout, Stderr        []byte
	ExitCode              *int
	Executable            *domain.FileRef
	StartedAt, FinishedAt time.Time
	Reason                domain.Reason
}
type bounded struct {
	sync.Mutex
	b        []byte
	overflow bool
}

func (w *bounded) Write(p []byte) (int, error) {
	w.Lock()
	defer w.Unlock()
	n := len(p)
	left := (64 << 10) - len(w.b)
	if len(p) > left {
		w.overflow = true
		p = p[:left]
	}
	w.b = append(w.b, p...)
	return n, nil
}
func Run(ctx context.Context, cwd string, c domain.Command) (r ProcessResult) {
	r.StartedAt = time.Now().UTC()
	defer func() { r.FinishedAt = time.Now().UTC() }()
	duration := c.TimeoutSeconds
	if duration == 0 {
		duration = 30
	}
	ctx, cancel := context.WithTimeout(ctx, time.Duration(duration)*time.Second)
	defer cancel()
	b, e := os.ReadFile(c.Program)
	if e != nil {
		r.Reason = "TOOL_MISSING"
		return
	}
	r.Executable = &domain.FileRef{Path: c.Program, SHA256: domain.Digest(fmt.Sprintf("sha256:%x", sha256.Sum256(b))), Bytes: int64(len(b)), Executable: true}
	cmd := exec.CommandContext(ctx, c.Program, c.Args...)
	cmd.Dir = cwd
	cmd.Env = []string{}
	for _, k := range c.EnvAllowlist {
		if v, ok := os.LookupEnv(k); ok {
			cmd.Env = append(cmd.Env, k+"="+v)
		}
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return nil
		}
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
	cmd.WaitDelay = 250 * time.Millisecond
	out, errout := &bounded{}, &bounded{}
	cmd.Stdout = out
	cmd.Stderr = errout
	e = cmd.Run()
	stragglers := cmd.Process != nil && syscall.Kill(-cmd.Process.Pid, 0) == nil
	if cmd.Process != nil {
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
	r.Stdout = out.b
	r.Stderr = errout.b
	if cmd.ProcessState != nil {
		x := cmd.ProcessState.ExitCode()
		r.ExitCode = &x
	}
	switch {
	case ctx.Err() == context.DeadlineExceeded:
		r.Reason = "TIMEOUT"
	case ctx.Err() != nil:
		r.Reason = "CANCELED"
	case out.overflow || errout.overflow:
		r.Reason = "LIMIT_EXCEEDED"
	case stragglers || errors.Is(e, exec.ErrWaitDelay) || r.ExitCode != nil && *r.ExitCode < 0:
		r.Reason = "IO_ERROR"
	case e != nil && r.ExitCode == nil:
		r.Reason = "IO_ERROR"
	}
	return
}
