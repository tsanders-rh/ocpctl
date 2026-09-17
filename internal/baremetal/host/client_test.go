package host

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
)

// testKeyPair returns a signer for the client and the matching public key to
// authorize on the server.
func testKeyPair(t *testing.T) (ssh.Signer, ssh.PublicKey) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := ssh.NewSignerFromSigner(priv)
	if err != nil {
		t.Fatal(err)
	}
	sshPub, err := ssh.NewPublicKey(pub)
	if err != nil {
		t.Fatal(err)
	}
	return signer, sshPub
}

// newTestSSHServer starts an in-process SSH server that authenticates
// `authorized` via publickey and executes each "exec" request through the local
// shell. A leading "sudo " is stripped so scripts written for the real host
// (which run as root via sudo) execute unprivileged in tests. Returns the
// listener address and a cleanup func.
func newTestSSHServer(t *testing.T, authorized ssh.PublicKey) (string, func()) {
	t.Helper()
	return startTestServer(t, authorized, "")
}

func newTestSSHServerWithPath(t *testing.T, authorized ssh.PublicKey, binDir string) (string, func()) {
	t.Helper()
	return startTestServer(t, authorized, binDir)
}

func startTestServer(t *testing.T, authorized ssh.PublicKey, binDir string) (string, func()) {
	t.Helper()
	_, hostPriv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	hostSigner, err := ssh.NewSignerFromSigner(hostPriv)
	if err != nil {
		t.Fatal(err)
	}
	authMarshaled := authorized.Marshal()
	cfg := &ssh.ServerConfig{
		PublicKeyCallback: func(_ ssh.ConnMetadata, key ssh.PublicKey) (*ssh.Permissions, error) {
			if bytes.Equal(key.Marshal(), authMarshaled) {
				return &ssh.Permissions{}, nil
			}
			return nil, errors.New("unauthorized")
		},
	}
	cfg.AddHostKey(hostSigner)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		for {
			nConn, err := ln.Accept()
			if err != nil {
				return
			}
			go serveTestConn(nConn, cfg, binDir)
		}
	}()
	return ln.Addr().String(), func() { ln.Close() }
}

func TestWaitReachableAndRun(t *testing.T) {
	signer, pub := testKeyPair(t)
	addr, cleanup := newTestSSHServer(t, pub)
	defer cleanup()

	var out bytes.Buffer
	c := NewClient(addr, "fedora", signer, &out)
	ctx := context.Background()

	if err := c.WaitReachable(ctx, 5*time.Second); err != nil {
		t.Fatalf("WaitReachable: %v", err)
	}
	if err := c.Connect(ctx); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	defer c.Close()

	// Run pipes the script to `sudo bash -s`; the test server strips sudo and
	// runs `bash -s`, so the script executes and its stdout streams to out.
	if err := c.Run(ctx, "echo hello-from-script"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !strings.Contains(out.String(), "hello-from-script") {
		t.Fatalf("Run output not streamed to out: %q", out.String())
	}
}

func TestRunNonZeroExitIsError(t *testing.T) {
	signer, pub := testKeyPair(t)
	addr, cleanup := newTestSSHServer(t, pub)
	defer cleanup()

	c := NewClient(addr, "fedora", signer, io.Discard)
	ctx := context.Background()
	if err := c.Connect(ctx); err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	if err := c.Run(ctx, "exit 7"); err == nil {
		t.Fatal("expected error for non-zero remote exit, got nil")
	}
}

func TestRunCaptureSplitsStdoutStderr(t *testing.T) {
	signer, pub := testKeyPair(t)
	addr, cleanup := newTestSSHServer(t, pub)
	defer cleanup()

	var out bytes.Buffer
	c := NewClient(addr, "fedora", signer, &out)
	ctx := context.Background()
	if err := c.Connect(ctx); err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	got, err := c.RunCapture(ctx, "printf out-value; printf err-value 1>&2")
	if err != nil {
		t.Fatalf("RunCapture: %v", err)
	}
	if got != "out-value" {
		t.Fatalf("RunCapture stdout = %q, want %q", got, "out-value")
	}
	if !strings.Contains(out.String(), "err-value") {
		t.Fatalf("stderr not streamed to out: %q", out.String())
	}
	if strings.Contains(out.String(), "out-value") {
		t.Fatalf("stdout leaked into out: %q", out.String())
	}
}

func TestUpload(t *testing.T) {
	signer, pub := testKeyPair(t)
	addr, cleanup := newTestSSHServer(t, pub)
	defer cleanup()

	c := NewClient(addr, "fedora", signer, io.Discard)
	ctx := context.Background()
	if err := c.Connect(ctx); err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	dst := filepath.Join(t.TempDir(), "uploaded.txt")
	if err := c.Upload(ctx, strings.NewReader("file-body"), dst, 0o644); err != nil {
		t.Fatalf("Upload: %v", err)
	}
	body, err := os.ReadFile(dst)
	if err != nil {
		t.Fatalf("read uploaded: %v", err)
	}
	if string(body) != "file-body" {
		t.Fatalf("uploaded content = %q, want %q", body, "file-body")
	}
}

func TestRunContextCancel(t *testing.T) {
	signer, pub := testKeyPair(t)
	addr, cleanup := newTestSSHServer(t, pub)
	defer cleanup()

	c := NewClient(addr, "fedora", signer, io.Discard)
	if err := c.Connect(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	ctx, cancel := context.WithCancel(context.Background())
	go func() { time.Sleep(200 * time.Millisecond); cancel() }()
	err := c.Run(ctx, "sleep 10")
	if err == nil {
		t.Fatal("expected error from cancelled context, got nil")
	}
}

func serveTestConn(nConn net.Conn, cfg *ssh.ServerConfig, binDir string) {
	sConn, chans, reqs, err := ssh.NewServerConn(nConn, cfg)
	if err != nil {
		nConn.Close()
		return
	}
	defer sConn.Close()
	go ssh.DiscardRequests(reqs)
	for newChan := range chans {
		if newChan.ChannelType() != "session" {
			newChan.Reject(ssh.UnknownChannelType, "only session")
			continue
		}
		ch, chReqs, err := newChan.Accept()
		if err != nil {
			continue
		}
		go handleTestSession(ch, chReqs, binDir)
	}
}

func handleTestSession(ch ssh.Channel, reqs <-chan *ssh.Request, binDir string) {
	for req := range reqs {
		if req.Type != "exec" {
			req.Reply(false, nil)
			continue
		}
		var payload struct{ Command string }
		if err := ssh.Unmarshal(req.Payload, &payload); err != nil {
			req.Reply(false, nil)
			continue
		}
		req.Reply(true, nil)
		// Strip ALL "sudo " occurrences so root-only scripts run unprivileged here.
		cmd := strings.ReplaceAll(payload.Command, "sudo ", "")
		sh := exec.Command("/bin/sh", "-c", cmd)
		sh.Stdin = ch
		sh.Stdout = ch
		sh.Stderr = ch.Stderr()
		if binDir != "" {
			sh.Env = append(os.Environ(), "PATH="+binDir+":"+os.Getenv("PATH"))
		}
		code := 0
		if err := sh.Run(); err != nil {
			var ee *exec.ExitError
			if errors.As(err, &ee) {
				code = ee.ExitCode()
			} else {
				code = 1
			}
		}
		status := struct{ Status uint32 }{uint32(code)}
		ch.SendRequest("exit-status", false, ssh.Marshal(&status))
		ch.Close()
		return
	}
}
