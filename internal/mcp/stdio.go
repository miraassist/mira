package mcp

import (
	"bufio"
	"bytes"
	"io"
	"log/slog"
	"os/exec"
	"sync"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// stdioTransport runs a server as a subprocess, like the SDK's
// CommandTransport, but tolerates servers that log to stdout. Several
// popular servers print banners and debug lines there; the SDK treats the
// first such line as a broken stream and drops the connection. Here every
// line that is not a JSON object is logged and skipped.
func stdioTransport(cmd *exec.Cmd, log *slog.Logger) (sdk.Transport, error) {
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	pr, pw := io.Pipe()
	go filterJSONLines(stdout, pw, log)
	return &sdk.IOTransport{Reader: pr, Writer: &processStdin{WriteCloser: stdin, cmd: cmd, reader: pr}}, nil
}

// filterJSONLines copies JSON-RPC frames (one JSON object per line) from the
// process to the transport and logs everything else.
func filterJSONLines(src io.Reader, dst *io.PipeWriter, log *slog.Logger) {
	sc := bufio.NewScanner(src)
	sc.Buffer(make([]byte, 64*1024), 32<<20)
	for sc.Scan() {
		line := bytes.TrimSpace(sc.Bytes())
		if len(line) == 0 {
			continue
		}
		if line[0] != '{' {
			log.Debug("mcp server wrote a non-protocol line to stdout", "line", string(line))
			continue
		}
		if _, err := dst.Write(append(line, '\n')); err != nil {
			return
		}
	}
	dst.CloseWithError(sc.Err())
}

// processStdin closes the server the way the MCP spec asks: close stdin,
// give the process time to exit, then kill it.
type processStdin struct {
	io.WriteCloser
	cmd    *exec.Cmd
	reader *io.PipeReader
	once   sync.Once
}

func (p *processStdin) Close() error {
	var err error
	p.once.Do(func() {
		err = p.WriteCloser.Close()
		done := make(chan struct{})
		go func() { _ = p.cmd.Wait(); close(done) }()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			_ = p.cmd.Process.Kill()
			<-done
		}
		_ = p.reader.Close()
	})
	return err
}
