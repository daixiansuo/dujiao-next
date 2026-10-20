// Package pgdump exports PostgreSQL custom archives with the official pg_dump.
package pgdump

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/dujiao-next/internal/modules/backup/contract"
	"github.com/dujiao-next/internal/modules/backup/domain"
)

const diagnosticLimit = 8 * 1024

var versionPattern = regexp.MustCompile(`\b([0-9]+)(?:\.([0-9]+))?(?:\.[0-9]+)?\b`)

// Dumper uses the official client tools for both version checks and exports.
type Dumper struct {
	dsn   string
	path  string
	probe func(context.Context, string) (string, int, error)
}

var _ contract.Dumper = (*Dumper)(nil)

func New(dsn, path string) *Dumper {
	if path == "" {
		path = "pg_dump"
	}
	d := &Dumper{dsn: dsn, path: path}
	d.probe = d.serverVersion
	return d
}

func (d *Dumper) Check(ctx context.Context) (domain.ToolInfo, error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	info := domain.ToolInfo{}
	connection, err := prepareDSN(d.dsn)
	if err != nil {
		return info, err
	}
	stdout, stderr := &boundedOutput{}, &boundedOutput{}
	cmd := exec.CommandContext(ctx, d.path, "--version")
	cmd.Stdout, cmd.Stderr = stdout, stderr
	cmd.WaitDelay = time.Second
	if err := cmd.Run(); err != nil {
		return info, commandError(ctx, "pg_dump version check", err, stderr, connection.secrets)
	}
	version := versionPattern.FindString(stdout.String())
	client, err := versionNumber(version)
	if err != nil {
		return info, errors.New("unrecognized pg_dump version")
	}
	info.PgDumpVersion = version
	server, serverNum, err := d.probe(ctx, d.dsn)
	if err != nil {
		// A failed probe must never expose the original DSN.
		if ctx.Err() != nil {
			return info, fmt.Errorf("PostgreSQL version check: %w", ctx.Err())
		}
		return info, errors.New("cannot determine PostgreSQL server version; check connection settings and access")
	}
	info.ServerVersion = server
	if majorVersion(client) < majorVersion(serverNum) {
		return info, fmt.Errorf("pg_dump %s is older than PostgreSQL server %s", version, server)
	}
	return info, nil
}

func (d *Dumper) Dump(ctx context.Context, path string) (domain.ToolInfo, error) {
	info, err := d.Check(ctx)
	if err != nil {
		return info, err
	}
	connection, err := prepareDSN(d.dsn)
	if err != nil {
		return info, err
	}
	stderr := &boundedOutput{}
	cmd := exec.CommandContext(ctx, d.path, "--format=custom", "--no-password", "--file", path, "--dbname", connection.dsn)
	cmd.Env = childEnvironment(connection.env)
	cmd.Stderr = stderr
	cmd.WaitDelay = time.Second
	if err := cmd.Run(); err != nil {
		return info, commandError(ctx, "pg_dump export", err, stderr, connection.secrets)
	}
	return info, nil
}

func (d *Dumper) serverVersion(ctx context.Context, dsn string) (string, int, error) {
	connection, err := prepareDSN(dsn)
	if err != nil {
		return "", 0, err
	}
	dumpPath, err := exec.LookPath(d.path)
	if err != nil {
		return "", 0, err
	}
	// Use libpq from the same client installation. pgx treats some valid libpq
	// SSL settings (e.g. sslcrl, ssl_min_protocol_version) as server parameters;
	// probing through psql preserves those settings without weakening TLS.
	psqlPath := filepath.Join(filepath.Dir(dumpPath), "psql")
	stdout, stderr := &boundedOutput{}, &boundedOutput{}
	cmd := exec.CommandContext(ctx, psqlPath, "--no-psqlrc", "--no-password", "--tuples-only", "--no-align",
		"--set", "ON_ERROR_STOP=1", "--command",
		"SELECT current_setting('server_version'), current_setting('server_version_num')",
		"--dbname", connection.dsn)
	cmd.Env = childEnvironment(connection.env)
	cmd.Stdout, cmd.Stderr = stdout, stderr
	cmd.WaitDelay = time.Second
	if err := cmd.Run(); err != nil {
		return "", 0, commandError(ctx, "PostgreSQL version check", err, stderr, connection.secrets)
	}
	version, rawNumber, ok := strings.Cut(strings.TrimSpace(stdout.String()), "|")
	number, err := strconv.Atoi(rawNumber)
	if !ok || err != nil || number <= 0 || stdout.truncated || !versionPattern.MatchString(version) {
		return "", 0, errors.New("unrecognized PostgreSQL server version")
	}
	return version, number, nil
}

func versionNumber(version string) (int, error) {
	m := versionPattern.FindStringSubmatch(version)
	if m == nil {
		return 0, errors.New("invalid PostgreSQL version")
	}
	major, _ := strconv.Atoi(m[1])
	minor, _ := strconv.Atoi(m[2])
	if major < 10 {
		return major*10000 + minor*100, nil
	}
	return major * 10000, nil
}

func majorVersion(number int) int {
	if number < 100000 {
		return number / 100
	}
	return number / 10000 * 100
}

func childEnvironment(overrides map[string]string) []string {
	var env []string
	for _, value := range os.Environ() {
		key, _, _ := strings.Cut(value, "=")
		if _, overridden := overrides[key]; !overridden {
			env = append(env, value)
		}
	}
	for key, value := range overrides {
		env = append(env, key+"="+value)
	}
	return env
}

type boundedOutput struct {
	data      []byte
	truncated bool
}

func (b *boundedOutput) Write(p []byte) (int, error) {
	n := len(p)
	remaining := diagnosticLimit - len(b.data)
	if len(p) > remaining {
		b.truncated = true
		p = p[:remaining]
	}
	b.data = append(b.data, p...)
	return n, nil
}

func (b *boundedOutput) String() string { return string(b.data) }

func commandError(ctx context.Context, operation string, err error, output *boundedOutput, secrets []string) error {
	if ctx.Err() != nil {
		return fmt.Errorf("%s: %w", operation, ctx.Err())
	}
	text := output.String()
	for _, secret := range append(secrets, os.Getenv("PGPASSWORD"), os.Getenv("PGSSLPASSWORD")) {
		if secret == "" {
			continue
		}
		for _, form := range []string{secret, url.QueryEscape(secret), url.PathEscape(secret), strings.NewReplacer("\\", "\\\\", "'", "\\'").Replace(secret)} {
			text = strings.ReplaceAll(text, form, "[REDACTED]")
			if output.truncated {
				// A secret can straddle the retained stderr boundary.
				for n := len(form) - 1; n > 0; n-- {
					if strings.HasSuffix(text, form[:n]) {
						text = strings.TrimSuffix(text, form[:n]) + "[REDACTED]"
						break
					}
				}
			}
		}
	}
	if output.truncated {
		text += " [truncated]"
	}
	if text = strings.TrimSpace(text); text != "" {
		return fmt.Errorf("%s: %w: %s", operation, err, text)
	}
	return fmt.Errorf("%s: %w", operation, err)
}
