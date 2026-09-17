// Package host provisions the freshly-launched nested-virt EC2 host over SSH:
// libvirt and its NAT network, sushy-tools BMCs, haproxy, and the cluster VM
// domains. The Client is the only code here that touches crypto/ssh.
package host

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"
)

// Client is the single abstraction every provisioning step goes through; it is
// the only code that knows crypto/ssh exists. One *ssh.Client is opened by
// Connect and reused across steps; each Run/RunCapture/Upload opens a fresh
// one-shot session.
type Client struct {
	addr   string // "<host-ip>:22"
	user   string // cloud user, e.g. "fedora"
	signer ssh.Signer
	out    io.Writer

	conn *ssh.Client
}

func NewClient(addr, user string, signer ssh.Signer, out io.Writer) *Client {
	return &Client{addr: addr, user: user, signer: signer, out: out}
}

func (c *Client) clientConfig() *ssh.ClientConfig {
	return &ssh.ClientConfig{
		User:            c.user,
		Auth:            []ssh.AuthMethod{ssh.PublicKeys(c.signer)},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(), // self-launched instance; hardening: pin via EC2 GetConsoleOutput
		Timeout:         15 * time.Second,
	}
}

// WaitReachable polls Dial until SSH answers or ctx/timeout expires — the host
// has just booted. It does not retain the connection.
func (c *Client) WaitReachable(ctx context.Context, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	cfg := c.clientConfig()
	for {
		conn, err := ssh.Dial("tcp", c.addr, cfg)
		if err == nil {
			c.closeLog(conn)
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("host %s not reachable over SSH within %s: %w", c.addr, timeout, err)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(5 * time.Second):
		}
	}
}

// Connect establishes the reused *ssh.Client.
func (c *Client) Connect(ctx context.Context) error {
	conn, err := ssh.Dial("tcp", c.addr, c.clientConfig())
	if err != nil {
		return fmt.Errorf("ssh dial %s: %w", c.addr, err)
	}
	c.conn = conn
	return nil
}

func (c *Client) Close() error {
	if c.conn == nil {
		return nil
	}
	return c.conn.Close()
}

// closeLog closes x for best-effort cleanup, noting any error on stderr. Used
// where the close error is not otherwise actionable (session/probe teardown).
func (c *Client) closeLog(x io.Closer) {
	if err := x.Close(); err != nil {
		fmt.Fprintf(os.Stderr, "warning: close: %v\n", err)
	}
}

// NodeKeyPath is where lifecycle places the ephemeral private key on the host so
// the host can ssh into cluster nodes and the ceph VM. Reaching those IPs uses
// nested ssh FROM the host (not a crypto/ssh direct-tcpip tunnel, which this
// environment's sshd refuses -- see rhwa-lab's odf.sh); the nodes/VM trust this
// key via install-config sshKey / cloud-init.
const NodeKeyPath = "/opt/ocpctl-agent/id_ed25519"

const nodeHeredoc = "OCPCTL_NODE_EOF"

func nodeSSHOpts() string {
	return "-i " + NodeKeyPath + " -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null -o LogLevel=ERROR -o ConnectTimeout=15"
}

// nestedSSH builds a host-side command that ssh'es into user@nodeIP and pipes
// script to `sudo bash -s` there via a quoted heredoc.
func nestedSSH(nodeIP, user, script string) string {
	return fmt.Sprintf("ssh %s %s@%s 'sudo bash -s' <<'%s'\n%s\n%s",
		nodeSSHOpts(), user, nodeIP, nodeHeredoc, script, nodeHeredoc)
}

// RunNode runs script as `sudo bash -s` on a node/VM (user@nodeIP), reached by
// nested ssh from the host. Output streams to out. The host runs the ssh as root
// (Run is sudo bash), so it can read the root-owned NodeKeyPath.
func (c *Client) RunNode(ctx context.Context, nodeIP, user, script string) error {
	return c.Run(ctx, nestedSSH(nodeIP, user, script))
}

// RunNodeCapture runs script as `sudo bash -s` on a node/VM and returns trimmed
// stdout; stderr streams to out. The outer sudo lets the host-side ssh (run as the
// login user by RunCapture) read the root-owned NodeKeyPath.
func (c *Client) RunNodeCapture(ctx context.Context, nodeIP, user, script string) (string, error) {
	return c.RunCapture(ctx, "sudo "+nestedSSH(nodeIP, user, script))
}

// Run pipes script to `sudo bash -s`, streaming combined stdout/stderr to out.
func (c *Client) Run(ctx context.Context, script string) error {
	sess, err := c.conn.NewSession()
	if err != nil {
		return fmt.Errorf("new session: %w", err)
	}
	defer c.closeLog(sess)
	// crypto/ssh streams stdout and stderr in separate goroutines; when both
	// target the same writer they must go through one lock or their writes race.
	w := &syncWriter{w: c.out}
	sess.Stdout = w
	sess.Stderr = w
	stdin, err := sess.StdinPipe()
	if err != nil {
		return fmt.Errorf("stdin pipe: %w", err)
	}
	if err := sess.Start("sudo bash -s"); err != nil {
		return fmt.Errorf("start remote shell: %w", err)
	}
	go func() {
		if _, werr := io.WriteString(stdin, script); werr != nil {
			fmt.Fprintf(os.Stderr, "warning: write remote stdin: %v\n", werr)
		}
		c.closeLog(stdin)
	}()
	return c.wait(ctx, sess)
}

// RunCapture runs cmd verbatim and returns trimmed stdout; stderr streams to
// out. Used for small values (domuuid, health checks, podman logs).
func (c *Client) RunCapture(ctx context.Context, cmd string) (string, error) {
	sess, err := c.conn.NewSession()
	if err != nil {
		return "", fmt.Errorf("new session: %w", err)
	}
	defer c.closeLog(sess)
	var stdout strings.Builder
	sess.Stdout = &stdout
	sess.Stderr = c.out
	if err := sess.Start(cmd); err != nil {
		return "", fmt.Errorf("start %q: %w", cmd, err)
	}
	if err := c.wait(ctx, sess); err != nil {
		return "", err
	}
	return strings.TrimSpace(stdout.String()), nil
}

// Upload writes content to remotePath via `sudo tee`, then chmods to mode.
func (c *Client) Upload(ctx context.Context, content io.Reader, remotePath string, mode os.FileMode) error {
	sess, err := c.conn.NewSession()
	if err != nil {
		return fmt.Errorf("new session: %w", err)
	}
	defer c.closeLog(sess)
	sess.Stdin = content
	sess.Stdout = io.Discard
	sess.Stderr = c.out
	cmd := fmt.Sprintf("sudo tee %s >/dev/null && sudo chmod %o %s", remotePath, mode.Perm(), remotePath)
	if err := sess.Start(cmd); err != nil {
		return fmt.Errorf("start upload %s: %w", remotePath, err)
	}
	if err := c.wait(ctx, sess); err != nil {
		return fmt.Errorf("upload %s: %w", remotePath, err)
	}
	return nil
}

// syncWriter serializes concurrent writes to w, so the stdout and stderr
// copier goroutines crypto/ssh starts can share one destination safely.
type syncWriter struct {
	mu sync.Mutex
	w  io.Writer
}

func (s *syncWriter) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.w.Write(p)
}

// wait blocks on the session, closing the host connection if ctx is cancelled
// (crypto/ssh sessions are not context-aware).
func (c *Client) wait(ctx context.Context, sess *ssh.Session) error {
	done := make(chan error, 1)
	go func() { done <- sess.Wait() }()
	select {
	case <-ctx.Done():
		c.closeLog(c.conn)
		return ctx.Err()
	case err := <-done:
		return err
	}
}
